// Package cache keeps the raw GraphQL responses with their fetch time in
// $XDG_CACHE_HOME/gh-kotlin-prs/, so that `--max-age` can skip a fetch. It sits between
// the commands and GitHub as a github.Client: a response is cached as GitHub sent it,
// and the commands classify it anew, with the current clock, on every run.
package cache

import (
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
		switch e, err := c.read(path); {
		case errors.Is(err, os.ErrNotExist):
			why = " (not cached)"
		case err != nil:
			c.debugf("cache: ignoring %s: %v\n", path, err)
			why = " (cache entry ignored)"
		default:
			age := c.Now().Sub(e.FetchedAt)
			if age < 0 || age >= c.MaxAge {
				why = fmt.Sprintf(" (cached %s ago, --max-age %s)", age.Round(time.Second), c.MaxAge)
				break
			}
			// Decode into a fresh value first: a failed decode would leave a mix of
			// cached and fetched fields in response.
			if err := json.Unmarshal(e.Data, reflect.New(reflect.TypeOf(response).Elem()).Interface()); err != nil {
				c.debugf("cache: ignoring %s: %v\n", path, err)
				why = " (cache entry ignored)"
				break
			}
			c.debugf("%s: from the cache, fetched %s ago\n", op, age.Round(time.Second))
			return json.Unmarshal(e.Data, response)
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

var operationName = regexp.MustCompile(`^\s*query\s+(\w+)`)

// operation is the query's operation name, for file names and debug lines.
func operation(query string) string {
	if m := operationName.FindStringSubmatch(query); m != nil {
		return m[1]
	}
	return "query"
}

// path is the entry file of a query: its operation name and a hash of the account, the
// query and its variables. "" when the cache is off.
func (c *Client) path(op, query string, variables map[string]any) string {
	if c.Dir == "" {
		return ""
	}
	vars, err := json.Marshal(variables) // map keys come out sorted
	if err != nil {
		return ""
	}
	h := sha256.New()
	for _, part := range [][]byte{[]byte(c.Account), []byte(query), vars} {
		h.Write(part)
		h.Write([]byte{0})
	}
	return filepath.Join(c.Dir, op+"-"+hex.EncodeToString(h.Sum(nil))[:32]+".json")
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
