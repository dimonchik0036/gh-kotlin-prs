package cache

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var start = time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)

const query = "query Sections($q: String!) { viewer { login } }"

// fakeGitHub answers every query with the next response, counting the calls.
type fakeGitHub struct {
	calls int
	login string
	err   error
}

func (f *fakeGitHub) DoWithContext(_ context.Context, _ string, _ map[string]any, resp any) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return json.Unmarshal([]byte(`{"viewer": {"login": "`+f.login+`"}, "rateLimit": {"cost": 1, "remaining": 4999}}`), resp)
}

type response struct {
	Viewer struct{ Login string } `json:"viewer"`
}

type fixture struct {
	t      *testing.T
	github *fakeGitHub
	now    time.Time
	debug  strings.Builder
	client *Client
}

func newFixture(t *testing.T, maxAge time.Duration) *fixture {
	f := &fixture{t: t, github: &fakeGitHub{login: "alice_user"}, now: start}
	f.client = &Client{Next: f.github, Dir: filepath.Join(t.TempDir(), "gh-kotlin-prs"), Account: "github.com\x00token-a",
		MaxAge: maxAge, Now: func() time.Time { return f.now }, Debug: &f.debug}
	return f
}

// query runs the query and returns the login it got.
func (f *fixture) query(vars map[string]any) string {
	f.t.Helper()
	var resp response
	if err := f.client.DoWithContext(context.Background(), query, vars, &resp); err != nil {
		f.t.Fatal(err)
	}
	return resp.Viewer.Login
}

func (f *fixture) entries() []string {
	f.t.Helper()
	files, err := os.ReadDir(f.client.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		f.t.Fatal(err)
	}
	var names []string
	for _, file := range files {
		names = append(names, file.Name())
	}
	return names
}

func (f *fixture) only() string {
	f.t.Helper()
	names := f.entries()
	if len(names) != 1 {
		f.t.Fatalf("entries %q, want one", names)
	}
	return filepath.Join(f.client.Dir, names[0])
}

var vars = map[string]any{"q": "repo:JetBrains/kotlin is:pr author:@me"}

func TestHitMissExpiry(t *testing.T) {
	f := newFixture(t, 5*time.Minute)
	if got := f.query(vars); got != "alice_user" || f.github.calls != 1 {
		t.Fatalf("miss: %q after %d calls", got, f.github.calls)
	}
	f.github.login = "bob_user" // what GitHub would answer now
	f.now = start.Add(4 * time.Minute)
	if got := f.query(vars); got != "alice_user" || f.github.calls != 1 {
		t.Errorf("hit: %q after %d calls, want the cached alice_user", got, f.github.calls)
	}
	f.now = start.Add(5 * time.Minute)
	if got := f.query(vars); got != "bob_user" || f.github.calls != 2 {
		t.Errorf("expired: %q after %d calls, want a fetch", got, f.github.calls)
	}
	f.now = start.Add(6 * time.Minute)
	if got := f.query(vars); got != "bob_user" || f.github.calls != 2 {
		t.Errorf("the refetch was cached anew: %q after %d calls", got, f.github.calls)
	}
	want := []string{
		"Sections: cost 1, remaining 4999 (not cached)",
		"Sections: from the cache, fetched 4m0s ago",
		"Sections: cost 1, remaining 4999 (cached 5m0s ago, --max-age 5m0s)",
		"Sections: from the cache, fetched 1m0s ago",
	}
	if got := strings.Split(strings.TrimSpace(f.debug.String()), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("debug:\n%s\nwant:\n%s", f.debug.String(), strings.Join(want, "\n"))
	}
}

// The default (--max-age 0) always fetches, and still keeps every response.
func TestMaxAgeZeroFetchesAndWrites(t *testing.T) {
	f := newFixture(t, 0)
	f.query(vars)
	f.query(vars)
	if f.github.calls != 2 {
		t.Errorf("%d calls, want 2", f.github.calls)
	}
	data, err := os.ReadFile(f.only())
	if err != nil {
		t.Fatal(err)
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	if e.Format != Format || !e.FetchedAt.Equal(start) || !strings.Contains(string(e.Data), `"alice_user"`) {
		t.Errorf("entry %s", data)
	}
	if strings.Contains(f.debug.String(), "cache") {
		t.Errorf("--max-age 0 mentions the cache: %q", f.debug.String())
	}
}

// Another account, other variables or another query never share an entry.
func TestKeys(t *testing.T) {
	f := newFixture(t, time.Hour)
	f.query(vars)
	f.client.Account = "github.com\x00token-b"
	f.query(vars)
	f.query(map[string]any{"q": "repo:JetBrains/kotlin is:pr review-requested:@me"})
	if f.github.calls != 3 || len(f.entries()) != 3 {
		t.Errorf("%d calls, entries %q; want 3 and 3", f.github.calls, f.entries())
	}
	for _, name := range f.entries() {
		if !strings.HasPrefix(name, "Sections-") || strings.Contains(name, "token") {
			t.Errorf("entry name %q", name)
		}
		data, _ := os.ReadFile(filepath.Join(f.client.Dir, name))
		if strings.Contains(string(data), "token-") {
			t.Errorf("%s stores the account: %s", name, data)
		}
	}
}

// An unreadable entry is a miss with a debug note, and the fetch replaces it.
func TestCorruptEntries(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":       "{\"format\": 1, \"fetchedAt\": \"2026-10-01T13:00:00Z\", \"da",
		"another format": `{"format": 999, "fetchedAt": "2026-10-01T13:00:00Z", "data": {"viewer": {"login": "mallory_user"}}}`,
		"wrong shape":    `{"format": 1, "fetchedAt": "2026-10-01T13:00:00Z", "data": {"viewer": [1, 2]}}`,
		"empty":          "",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, time.Hour)
			f.query(vars)
			path := f.only()
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := f.query(vars); got != "alice_user" || f.github.calls != 2 {
				t.Errorf("%q after %d calls, want a fetch", got, f.github.calls)
			}
			if !strings.Contains(f.debug.String(), "cache: ignoring "+path) || !strings.Contains(f.debug.String(), "(cache entry ignored)") {
				t.Errorf("no debug note:\n%s", f.debug.String())
			}
			f.query(vars)
			if f.github.calls != 2 {
				t.Error("the fetch didn't replace the entry")
			}
		})
	}
}

// A failed fetch keeps nothing; a cache that can't be written fails nothing, and the
// entry it had stays whole.
func TestWriteFailures(t *testing.T) {
	f := newFixture(t, 0)
	f.github.err = errors.New("HTTP 502")
	var resp response
	if err := f.client.DoWithContext(context.Background(), query, vars, &resp); err == nil || len(f.entries()) != 0 {
		t.Errorf("a failed fetch: %v, entries %q", err, f.entries())
	}
	f.github.err = nil
	f.query(vars)
	path := f.only()
	before, _ := os.ReadFile(path)
	dir := f.client.Dir
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	f.github.login = "bob_user"
	if got := f.query(vars); got != "bob_user" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(f.debug.String(), "cache: not saved: ") {
		t.Errorf("no debug note:\n%s", f.debug.String())
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) || len(f.entries()) != 1 {
		t.Errorf("the old entry changed: %s, entries %q", after, f.entries())
	}

	f.client.Dir = filepath.Join(f.only(), "sub") // a file where the directory should be
	if got := f.query(vars); got != "bob_user" {
		t.Errorf("got %q", got)
	}
}

// Writes go through a temp file renamed over the entry: none is left behind.
func TestAtomicWrite(t *testing.T) {
	f := newFixture(t, 0)
	for range 3 {
		f.query(vars)
	}
	if names := f.entries(); len(names) != 1 || strings.HasPrefix(names[0], ".tmp-") {
		t.Errorf("entries %q", names)
	}
	info, err := os.Stat(f.only())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("entry mode %v, want 0600", perm)
	}
}

// Writing drops entries and temp files older than the retention, nothing else.
func TestPrune(t *testing.T) {
	f := newFixture(t, 0)
	f.query(vars)
	old := start.Add(-retention - time.Hour)
	for _, name := range []string{"PullRequests-0123.json", ".tmp-123", "notes.txt"} {
		path := filepath.Join(f.client.Dir, name)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	recent := filepath.Join(f.client.Dir, "PullRequest-4567.json")
	if err := os.WriteFile(recent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(recent, start, start); err != nil {
		t.Fatal(err)
	}
	f.query(map[string]any{"q": "other"})
	names := strings.Join(f.entries(), " ")
	if strings.Contains(names, "PullRequests-0123") || strings.Contains(names, ".tmp-123") ||
		!strings.Contains(names, "notes.txt") || !strings.Contains(names, "PullRequest-4567") {
		t.Errorf("after pruning: %s", names)
	}
}

// Without a directory the cache is off: every query is fetched, nothing is written.
func TestNoDir(t *testing.T) {
	f := newFixture(t, time.Hour)
	dir := f.client.Dir
	f.client.Dir = ""
	f.query(vars)
	f.query(vars)
	if _, err := os.Stat(dir); f.github.calls != 2 || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%d calls, %s: %v", f.github.calls, dir, err)
	}
}

func TestDefaultDir(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	if got := DefaultDir(); got != "/tmp/xdg-cache/gh-kotlin-prs" {
		t.Errorf("DefaultDir() = %q", got)
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/alice_user")
	if got := DefaultDir(); got != "/home/alice_user/.cache/gh-kotlin-prs" {
		t.Errorf("DefaultDir() = %q", got)
	}
}

// Offline answers only from the cache, at any age or up to its MaxAge, and remembers
// the oldest entry it used.
func TestOffline(t *testing.T) {
	f := newFixture(t, 0)
	f.query(vars)
	f.now = start.Add(10 * time.Minute)
	other := map[string]any{"q": "other"}
	f.query(other)
	f.now = start.Add(time.Hour)

	offline := &Offline{Cache: f.client}
	var resp response
	for _, v := range []map[string]any{other, vars} {
		if err := offline.DoWithContext(context.Background(), query, v, &resp); err != nil || resp.Viewer.Login != "alice_user" {
			t.Fatalf("any age: %v, %q", err, resp.Viewer.Login)
		}
	}
	if !offline.Oldest.Equal(start) {
		t.Errorf("oldest %v, want %v", offline.Oldest, start)
	}

	bounded := &Offline{Cache: f.client, MaxAge: 55 * time.Minute}
	if err := bounded.DoWithContext(context.Background(), query, other, &resp); err != nil {
		t.Errorf("50m old, MaxAge 55m: %v", err)
	}
	if err := bounded.DoWithContext(context.Background(), query, vars, &resp); !errors.Is(err, ErrMiss) || !strings.Contains(err.Error(), "cached 1h0m0s ago") {
		t.Errorf("1h old, MaxAge 55m: %v", err)
	}
	if err := offline.DoWithContext(context.Background(), query, map[string]any{"q": "never"}, &resp); !errors.Is(err, ErrMiss) {
		t.Errorf("no entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.client.Dir, filepath.Base(f.client.path("Sections", query, vars))), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := offline.DoWithContext(context.Background(), query, vars, &resp); !errors.Is(err, ErrMiss) || !strings.Contains(f.debug.String(), "cache: ignoring ") {
		t.Errorf("a corrupt entry: %v, debug %q", err, f.debug.String())
	}
	if f.github.calls != 2 {
		t.Errorf("Offline fetched: %d calls", f.github.calls)
	}
	f.client.Dir = ""
	if err := offline.DoWithContext(context.Background(), query, vars, &resp); !errors.Is(err, ErrMiss) {
		t.Errorf("without a cache: %v", err)
	}
}

// With Fallback, a miss is answered by the newest entry of the same operation and
// variables, by its fetch time, within MaxAge: the details of another set of PRs.
func TestOfflineFallback(t *testing.T) {
	f := newFixture(t, 0)
	ask := func(q string) {
		var resp response
		if err := f.client.DoWithContext(context.Background(), q, vars, &resp); err != nil {
			t.Fatal(err)
		}
	}
	const (
		older = "query Sections { older }"
		newer = "query Sections { newer }"
		asked = "query Sections { asked }"
	)
	f.github.login = "bob_user"
	f.now = start.Add(10 * time.Minute)
	ask(newer)
	// Written last, fetched first: the stored fetch time decides, not the file's.
	f.now = start.Add(5 * time.Minute)
	f.github.login = "carol_user"
	ask(older)
	f.now = start.Add(20 * time.Minute)

	offline := &Offline{Cache: f.client, MaxAge: 30 * time.Minute, Fallback: []string{"Sections"}}
	var resp response
	if err := offline.DoWithContext(context.Background(), asked, vars, &resp); err != nil || resp.Viewer.Login != "bob_user" || !offline.Oldest.Equal(start.Add(10*time.Minute)) {
		t.Errorf("fallback: %v, %q, oldest %v", err, resp.Viewer.Login, offline.Oldest)
	}
	for name, o := range map[string]*Offline{
		"too old":         {Cache: f.client, MaxAge: 5 * time.Minute, Fallback: []string{"Sections"}},
		"other operation": {Cache: f.client, MaxAge: 30 * time.Minute, Fallback: []string{"PullRequests"}},
	} {
		if err := o.DoWithContext(context.Background(), asked, vars, &resp); !errors.Is(err, ErrMiss) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := offline.DoWithContext(context.Background(), asked, map[string]any{"q": "other"}, &resp); !errors.Is(err, ErrMiss) {
		t.Errorf("other variables: %v", err)
	}
	f.client.Account = "github.com\x00token-b"
	if err := offline.DoWithContext(context.Background(), asked, vars, &resp); !errors.Is(err, ErrMiss) {
		t.Errorf("another account: %v", err)
	}
}
