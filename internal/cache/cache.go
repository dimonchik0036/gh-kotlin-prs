// Package cache keeps the raw GraphQL responses with their fetch time in
// $XDG_CACHE_HOME/gh-kotlin-prs/, so that `--max-age` can skip a fetch. It sits between
// the commands and GitHub as a github.Client: a response is cached as GitHub sent it,
// and the commands classify it anew, with the current clock, on every run.
package cache

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
)

// Format is the version of the entry files. Entries of another format are ignored and
// overwritten by the next fetch: bump it whenever an entry would read differently.
const Format = 1

// retention is how long an entry is kept after its last write. Keys change with the
// PRs fetched and the day of the merged search, so entries nobody asks for again would
// pile up otherwise (the details of ~40 PRs are about 1 MB).
const retention = 3 * 24 * time.Hour

// DefaultDir is $XDG_CACHE_HOME/gh-kotlin-prs, falling back to ~/.cache. It's "" when
// there's no home directory, which turns the cache off.
func DefaultDir() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "gh-kotlin-prs")
}

// Client answers a query from its cache entry when that is younger than MaxAge, and
// otherwise asks Next and writes the response to the entry. Problems with the cache
// never fail a query: an unreadable entry is a miss, a failed write only a debug note.
type Client struct {
	Next github.Client
	// Dir holds the entries; "" turns the cache off.
	Dir string
	// Account identifies the credentials Next uses (github.DefaultClient). It's part of
	// the key, so another account never sees these responses; it's never stored.
	Account string
	// MaxAge: an entry younger than this answers the query. 0 always fetches.
	MaxAge time.Duration
	Now    func() time.Time
	// Debug, when set, gets a line per query: its cost, or the cache entry that answered it,
	// and any entry the cache had to ignore or couldn't write.
	Debug io.Writer
}

// entry is the file of one query: the response's data as GitHub sent it.
type entry struct {
	Format    int             `json:"format"`
	FetchedAt time.Time       `json:"fetchedAt"`
	Data      json.RawMessage `json:"data"`
}

// DoWithContext answers the query from the cache or from Next (see Client).
func (c *Client) DoWithContext(ctx context.Context, query string, variables map[string]any, response any) error {
	op := operation(query)
	path := c.path(op, query, variables)
	why := ""
	if c.MaxAge > 0 && path != "" {
		fetchedAt, err := c.load(path, response, c.MaxAge)
		var stale staleError
		switch {
		case err == nil:
			c.debugf("%s: from the cache, fetched %s ago\n", op, c.Now().Sub(fetchedAt).Round(time.Second))
			return nil
		case errors.Is(err, os.ErrNotExist):
			why = " (not cached)"
		case errors.As(err, &stale):
			why = fmt.Sprintf(" (cached %s ago, --max-age %s)", stale.age.Round(time.Second), c.MaxAge)
		default:
			c.debugf("cache: ignoring %s: %v\n", path, err)
			why = " (cache entry ignored)"
		}
	}
	var data json.RawMessage
	if err := c.Next.DoWithContext(ctx, query, variables, &data); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil // no content: nothing to decode or keep
	}
	if err := json.Unmarshal(data, response); err != nil {
		return err
	}
	if c.Debug != nil {
		var limit struct {
			RateLimit github.RateLimit `json:"rateLimit"`
		}
		_ = json.Unmarshal(data, &limit)
		c.debugf("%s: cost %d, remaining %d%s\n", op, limit.RateLimit.Cost, limit.RateLimit.Remaining, why)
	}
	if path != "" {
		if err := c.write(path, entry{Format: Format, FetchedAt: c.Now().UTC(), Data: data}); err != nil {
			c.debugf("cache: not saved: %v\n", err)
		}
	}
	return nil
}

// ErrMiss is the error of Offline when the cache has no usable entry for a query.
var ErrMiss = errors.New("not in the cache")

// Offline is a github.Client that answers from the cache only: entries at most MaxAge
// old, any age when MaxAge is 0. Anything else is ErrMiss, and nothing is fetched.
// Oldest is when the oldest entry it answered with was fetched.
type Offline struct {
	Cache  *Client
	MaxAge time.Duration
	// Fallback lists operations whose miss is answered by their newest entry with the
	// same variables but another query, if that's young enough: the details of another
	// set of PRs.
	Fallback []string
	Oldest   time.Time
}

// DoWithContext answers the query from its cache entry (see Offline).
func (o *Offline) DoWithContext(_ context.Context, query string, variables map[string]any, response any) error {
	op := operation(query)
	path := o.Cache.path(op, query, variables)
	if path == "" {
		return ErrMiss
	}
	fetchedAt, err := o.Cache.load(path, response, o.MaxAge)
	var stale staleError
	switch {
	case err == nil:
	case slices.Contains(o.Fallback, op) && (errors.Is(err, os.ErrNotExist) || errors.As(err, &stale)):
		if fetchedAt, err = o.newest(op, variables, response); err != nil {
			return err
		}
		o.Cache.debugf("%s: from the cache for another query, fetched %s ago\n", op, o.Cache.Now().Sub(fetchedAt).Round(time.Second))
		o.Oldest = oldest(o.Oldest, fetchedAt)
		return nil
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s: %w", op, ErrMiss)
	case errors.As(err, &stale):
		return fmt.Errorf("%s: %w: cached %s ago", op, ErrMiss, stale.age.Round(time.Second))
	default:
		o.Cache.debugf("cache: ignoring %s: %v\n", path, err)
		return fmt.Errorf("%s: %w", op, ErrMiss)
	}
	o.Cache.debugf("%s: from the cache, fetched %s ago\n", op, o.Cache.Now().Sub(fetchedAt).Round(time.Second))
	o.Oldest = oldest(o.Oldest, fetchedAt)
	return nil
}

func oldest(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

// newest decodes the newest usable entry of the operation and variables, by the fetch
// time it stores, into response.
func (o *Offline) newest(op string, variables map[string]any, response any) (time.Time, error) {
	paths, _ := filepath.Glob(filepath.Join(o.Cache.Dir, op+"-"+o.Cache.scope(op, variables)+"-*.json"))
	type candidate struct {
		path      string
		fetchedAt time.Time
	}
	var candidates []candidate
	for _, path := range paths {
		if at, err := fetchedAt(path); err == nil {
			candidates = append(candidates, candidate{path, at})
		}
	}
	slices.SortFunc(candidates, func(a, b candidate) int { return b.fetchedAt.Compare(a.fetchedAt) })
	for _, c := range candidates {
		if at, err := o.Cache.load(c.path, response, o.MaxAge); err == nil {
			return at, nil
		}
	}
	return time.Time{}, fmt.Errorf("%s: %w", op, ErrMiss)
}

// fetchedAt reads an entry's fetch time without its data: the fields come first.
func fetchedAt(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(bufio.NewReader(f))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return time.Time{}, fmt.Errorf("%s: not an entry", path)
	}
	var e entry
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return time.Time{}, err
		}
		switch key {
		case "format":
			err = dec.Decode(&e.Format)
		case "fetchedAt":
			err = dec.Decode(&e.FetchedAt)
		default: // the data
			if e.Format != Format || e.FetchedAt.IsZero() {
				return time.Time{}, fmt.Errorf("%s: format %d", path, e.Format)
			}
			return e.FetchedAt, nil
		}
		if err != nil {
			return time.Time{}, err
		}
	}
	return time.Time{}, fmt.Errorf("%s: no data", path)
}

// staleError is an entry older than the age asked for.
type staleError struct{ age time.Duration }

func (e staleError) Error() string { return fmt.Sprintf("cached %s ago", e.age) }

// load decodes the entry at path into response and returns when it was fetched. The
// entry must be younger than maxAge, unless that's 0. An error wrapping os.ErrNotExist
// means there's none, a staleError that it's too old; any other, that it's unusable.
func (c *Client) load(path string, response any, maxAge time.Duration) (time.Time, error) {
	e, err := c.read(path)
	if err != nil {
		return time.Time{}, err
	}
	if age := c.Now().Sub(e.FetchedAt); maxAge > 0 && (age < 0 || age >= maxAge) {
		return time.Time{}, staleError{age}
	}
	// Decode into a fresh value first: a failed decode would leave a mix of cached and
	// fetched fields in response.
	if err := json.Unmarshal(e.Data, reflect.New(reflect.TypeOf(response).Elem()).Interface()); err != nil {
		return time.Time{}, err
	}
	return e.FetchedAt, json.Unmarshal(e.Data, response)
}

var operationName = regexp.MustCompile(`^\s*query\s+(\w+)`)

// operation is the query's operation name, for file names and debug lines.
func operation(query string) string {
	if m := operationName.FindStringSubmatch(query); m != nil {
		return m[1]
	}
	return "query"
}

// path is the entry file of a query: its operation, its scope and a hash of the
// account, the query and the scope (so the variables). "" when the cache is off.
func (c *Client) path(op, query string, variables map[string]any) string {
	scope := c.scope(op, variables)
	if scope == "" {
		return ""
	}
	return filepath.Join(c.Dir, op+"-"+scope+"-"+hash(c.Account, query, scope)[:32]+".json")
}

// scope is the part of an entry's name shared by every query of one operation with the
// same variables for the account, whatever the query text: the details queries of
// different sets of PRs, for one. "" when the cache is off.
func (c *Client) scope(op string, variables map[string]any) string {
	if c.Dir == "" {
		return ""
	}
	vars, err := json.Marshal(variables) // map keys come out sorted
	if err != nil {
		return ""
	}
	return hash(c.Account, op, string(vars))[:16]
}

func hash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// read returns the entry at path: an error wrapping os.ErrNotExist when there's none,
// another one when it's unusable.
func (c *Client) read(path string) (entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return entry{}, err
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return entry{}, err
	}
	if e.Format != Format {
		return entry{}, fmt.Errorf("format %d, want %d", e.Format, Format)
	}
	return e, nil
}

// write replaces the entry at path atomically (a temp file renamed over it), so a reader
// sees the old entry or the new one, never part of one. It then drops expired entries.
func (c *Client) write(path string, e entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	c.prune()
	return nil
}

// prune removes the entries and leftover temp files not written for retention.
func (c *Client) prune() {
	files, err := os.ReadDir(c.Dir)
	if err != nil {
		return
	}
	cutoff := c.Now().Add(-retention)
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".tmp-") {
			continue
		}
		if info, err := f.Info(); err == nil && info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(c.Dir, name))
		}
	}
}

func (c *Client) debugf(format string, args ...any) {
	if c.Debug != nil {
		_, _ = fmt.Fprintf(c.Debug, format, args...)
	}
}
