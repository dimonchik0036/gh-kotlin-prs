package cli

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/tui"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/update"
)

// releaseEnv is the plugin of a v0.10.0 release build that gh installed (pinned or not),
// with v0.11.0 out. started collects the notifications.
func releaseEnv(t *testing.T, configYAML string, pinned bool) (e env, client *fakeClient, started *[][]string, out *strings.Builder) {
	t.Helper()
	client = &fakeClient{t: t, latest: "v0.11.0"}
	e, out, _ = swiftbarEnv(t, client, configYAML)
	e.version = "v0.10.0"
	dir := t.TempDir()
	manifest := "owner: dimonchik0036\nname: gh-kotlin-prs\nhost: github.com\ntag: v0.10.0\nispinned: false\n"
	if pinned {
		manifest = strings.Replace(manifest, "ispinned: false", "ispinned: true", 1)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	e.executable = func() (string, error) { return filepath.Join(dir, "gh-kotlin-prs"), nil }
	started = &[][]string{}
	e.start = func(argv []string) error { *started = append(*started, argv); return nil }
	return e, client, started, out
}

func releaseQueries(c *fakeClient) int {
	n := 0
	for _, q := range c.queries {
		if strings.Contains(q, "latestRelease") {
			n++
		}
	}
	return n
}

// The plugin shows a newer release and announces it once; the answer is cached for 12h.
func TestSwiftbarRelease(t *testing.T) {
	e, client, started, out := releaseEnv(t, "", false)
	// Within the day the fake search expects.
	clock := now.Truncate(24 * time.Hour)
	e.now = func() time.Time { return clock }
	menu := func() string {
		t.Helper()
		out.Reset()
		if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK {
			t.Fatalf("exit %d", got)
		}
		return out.String()
	}
	notes := "https://github.com/dimonchik0036/gh-kotlin-prs/releases/tag/v0.11.0"
	if got := menu(); !strings.Contains(got, "\nUpgrade to v0.11.0 | bash=/opt/homebrew/bin/gh param1=extension param2=upgrade param3=kotlin-prs ") ||
		!strings.HasSuffix(got, "\nWhat's new in v0.11.0 | href="+notes+"\n") {
		t.Errorf("the menu:\n%s", got)
	}
	if len(*started) != 1 || !strings.Contains((*started)[0][2], "title=gh+kotlin-prs+v0.11.0+is+out") ||
		!strings.Contains((*started)[0][2], "href="+url.QueryEscape(notes)) || strings.Contains((*started)[0][2], "bash=") {
		t.Errorf("notified %q", *started)
	}
	clock = clock.Add(time.Minute)
	if got := menu(); !strings.Contains(got, "Upgrade to v0.11.0") || len(*started) != 1 || releaseQueries(client) != 1 {
		t.Errorf("a minute later: notified %q after %d queries", *started, releaseQueries(client))
	}
	clock = clock.Add(13 * time.Hour)
	menu()
	if len(*started) != 1 || releaseQueries(client) != 2 {
		t.Errorf("13h later: notified %q after %d queries", *started, releaseQueries(client))
	}
	// The cache drops what it hasn't written for 3 days, on its next write: not the marker.
	old := clock.Add(-4 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(e.cacheDir, "update-announced-v0.11.0"), old, old); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if got := run(context.Background(), []string{"list", "--format", "swiftbar", "--max-age", "0s"}, e); got != exitOK || len(*started) != 1 {
		t.Errorf("after the cache's pruning: exit %d, notified %q", got, *started)
	}
	// Another release, once the cached answer is gone.
	client.latest = "v0.12.0"
	entries, _ := filepath.Glob(filepath.Join(e.cacheDir, "LatestRelease-*.json"))
	for _, path := range entries {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if len(entries) != 1 {
		t.Fatalf("cache entries of the release: %q", entries)
	}
	if got := menu(); !strings.Contains(got, "Upgrade to v0.12.0") || len(*started) != 2 || !strings.Contains((*started)[1][2], "v0.12.0") {
		t.Errorf("another release: notified %q", *started)
	}
}

func TestSwiftbarReleaseQuiet(t *testing.T) {
	menu := func(e env, out *strings.Builder) string {
		t.Helper()
		if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK {
			t.Fatalf("exit %d", got)
		}
		return out.String()
	}
	// Pinned: the release in the menu, no upgrade item, no notification.
	e, _, started, out := releaseEnv(t, "", true)
	if got := menu(e, out); !strings.Contains(got, "\nv0.11.0 is out (pinned to v0.10.0) | href=") || strings.Contains(got, "Upgrade") || len(*started) != 0 {
		t.Errorf("pinned: notified %q:\n%s", *started, got)
	}
	// The update event off: the menu still shows the release.
	e, _, started, out = releaseEnv(t, "notify: {events: [merged]}\n", false)
	if got := menu(e, out); !strings.Contains(got, "Upgrade to v0.11.0") || len(*started) != 0 {
		t.Errorf("events without update: notified %q:\n%s", *started, got)
	}
	// updateCheck off, or a local build: no query at all.
	e, client, started, out := releaseEnv(t, "updateCheck: false\n", false)
	if got := menu(e, out); strings.Contains(got, "v0.11.0") || releaseQueries(client) != 0 || len(*started) != 0 {
		t.Errorf("updateCheck false: %d queries:\n%s", releaseQueries(client), got)
	}
	e, client, _, out = releaseEnv(t, "", false)
	e.version = "dev (5efe46a)"
	if got := menu(e, out); strings.Contains(got, "v0.11.0") || releaseQueries(client) != 0 {
		t.Errorf("a local build: %d queries:\n%s", releaseQueries(client), got)
	}
}

// The TUI gets the same check, and the announcement is shared with the plugin: once.
func TestTUIRelease(t *testing.T) {
	e, _, _, _ := releaseEnv(t, "", false)
	e.interactive = func() bool { return true }
	type result struct {
		info         update.Info
		ok, announce bool
	}
	var got []result
	e.runTUI = func(ctx context.Context, opts tui.Options) error {
		for range 2 {
			info, ok, announce := opts.CheckUpdate(ctx)
			got = append(got, result{info, ok, announce})
		}
		return nil
	}
	if code := run(context.Background(), nil, e); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if len(got) != 2 || !got[0].ok || got[0].info.Latest != "v0.11.0" || got[0].info.Install != update.InstallGH ||
		!got[0].announce || !got[1].ok || got[1].announce {
		t.Errorf("checks %+v", got)
	}
}
