package update

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
)

func TestNewer(t *testing.T) {
	for _, tt := range []struct {
		current, latest string
		want            bool
	}{
		{"v0.10.0", "v0.11.0", true},
		{"v0.10.0", "v0.10.1", true},
		{"v0.9.0", "v0.10.0", true},
		{"v0.11.0-rc.1", "v0.11.0", true},
		{"v0.10.0", "v0.10.0", false},
		{"v0.11.0", "v0.10.0", false},
		{"v0.11.0", "v0.11.0-rc.1", false},
		{"dev (5efe46a)", "v0.11.0", false},
		{"dev", "v0.11.0", false},
		{"v0.10.0", "", false},
		{"v0.10.0", "nightly", false},
	} {
		if got := Newer(tt.current, tt.latest); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v", tt.current, tt.latest, got)
		}
	}
}

// release answers the latest-release query with tag ("" for a repository without one),
// or fails with err.
type release struct {
	tag     string
	err     error
	queries int
}

func (r *release) DoWithContext(_ context.Context, query string, vars map[string]any, resp any) error {
	r.queries++
	if !strings.Contains(query, "latestRelease") || vars["owner"] != "dimonchik0036" || vars["name"] != "gh-kotlin-prs" {
		return errors.New("unexpected query")
	}
	if r.err != nil {
		return r.err
	}
	var latest any
	if r.tag != "" {
		latest = map[string]any{"tagName": r.tag, "url": "https://github.com/" + Repo + "/releases/tag/" + r.tag}
	}
	data, _ := json.Marshal(map[string]any{"repository": map[string]any{"latestRelease": latest}})
	return json.Unmarshal(data, resp)
}

func TestCheck(t *testing.T) {
	ctx := context.Background()
	u, ok := Check(ctx, &release{tag: "v0.11.0"}, "v0.10.0", InstallGH)
	if want := (Info{Current: "v0.10.0", Latest: "v0.11.0", URL: "https://github.com/dimonchik0036/gh-kotlin-prs/releases/tag/v0.11.0", Install: InstallGH}); !ok || u != want {
		t.Errorf("a newer release: %+v, %v", u, ok)
	}
	for _, tt := range []struct {
		name    string
		client  *release
		current string
		asks    bool
	}{
		{"the latest", &release{tag: "v0.10.0"}, "v0.10.0", true},
		{"no release", &release{}, "v0.10.0", true},
		{"a failed query", &release{err: errors.New("HTTP 502")}, "v0.10.0", true},
		{"a local build", &release{tag: "v0.11.0"}, "dev (5efe46a)", false},
	} {
		if _, ok := Check(ctx, tt.client, tt.current, InstallGH); ok || (tt.client.queries > 0) != tt.asks {
			t.Errorf("%s: %v after %d queries", tt.name, ok, tt.client.queries)
		}
	}
}

func TestReadInstall(t *testing.T) {
	// gh's layout: ~/.local/share/gh/extensions/gh-kotlin-prs/{gh-kotlin-prs,manifest.yml}.
	install := func(manifest string) string {
		dir := filepath.Join(t.TempDir(), "gh-kotlin-prs")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		exe := filepath.Join(dir, "gh-kotlin-prs")
		if err := os.WriteFile(exe, nil, 0o755); err != nil {
			t.Fatal(err)
		}
		if manifest != "" {
			if err := os.WriteFile(filepath.Join(dir, "manifest.yml"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return exe
	}
	const gh = "owner: dimonchik0036\nname: gh-kotlin-prs\nhost: github.com\ntag: v0.10.0\nispinned: false\npath: /x/gh-kotlin-prs\n"
	pinned := strings.Replace(gh, "ispinned: false", "ispinned: true", 1)
	link := filepath.Join(t.TempDir(), "gh-kotlin-prs")
	if err := os.Symlink(install(pinned), link); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, exe string
		want      Install
	}{
		{"gh extension install", install(gh), InstallGH},
		{"--pin", install(pinned), InstallPinned},
		{"--pin, through a symlink", link, InstallPinned},
		{"another extension's manifest", install(strings.Replace(gh, "name: gh-kotlin-prs", "name: gh-other", 1)), InstallOther},
		{"no manifest", install(""), InstallOther},
		{"a broken manifest", install("owner: [\n"), InstallOther},
		{"no executable", "", InstallOther},
	} {
		if got := ReadInstall(tt.exe); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestEvent(t *testing.T) {
	u := Info{Current: "v0.10.0", Latest: "v0.11.0", URL: "https://example.org/releases/tag/v0.11.0", Install: InstallGH}
	ev, ok := u.Event()
	want := notify.Event{Kind: notify.Update, Title: "gh kotlin-prs v0.11.0 is out", Body: "You have v0.10.0: gh extension upgrade kotlin-prs", URL: u.URL}
	if !ok || ev != want {
		t.Errorf("gh install: %+v", ev)
	}
	u.Install = InstallOther
	if ev, ok := u.Event(); !ok || ev.Body != "You have v0.10.0" {
		t.Errorf("other install: %+v", ev)
	}
	u.Install = InstallPinned
	if _, ok := u.Event(); ok {
		t.Error("a pinned install is notified")
	}
}

func TestAnnounce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	for i, tt := range []struct {
		latest string
		want   bool
	}{
		{"v0.11.0", true},
		{"v0.11.0", false},
		{"v0.12.0", true},
		{"v0.12.0", false},
	} {
		if got := Announce(dir, tt.latest); got != tt.want {
			t.Errorf("%d: Announce(%s) = %v", i, tt.latest, got)
		}
	}
	if markers, _ := filepath.Glob(filepath.Join(dir, "update-announced-*")); len(markers) != 1 || filepath.Base(markers[0]) != "update-announced-v0.12.0" {
		t.Errorf("markers %q, want v0.12.0's only", markers)
	}
	if Announce("", "v0.13.0") || Announce(dir, "../v0.13.0") || Announce(dir, "") {
		t.Error("announced without a cache dir or a version")
	}
}

// Of the plugin and the TUI asking at once, one announces.
func TestAnnounceOnce(t *testing.T) {
	dir := t.TempDir()
	results := make(chan bool)
	for range 20 {
		go func() { results <- Announce(dir, "v0.11.0") }()
	}
	announced := 0
	for range 20 {
		if <-results {
			announced++
		}
	}
	if announced != 1 {
		t.Errorf("announced %d times", announced)
	}
}
