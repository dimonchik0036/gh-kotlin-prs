package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/swiftbar"
)

const pluginPath = "/Users/alice_user/Plugins/kotlin-prs.2m.sh"

func swiftbarEnv(t *testing.T, client github.Client, configYAML string) (env, *strings.Builder, *strings.Builder) {
	t.Helper()
	e, out, errOut := testEnv(t, client, configYAML)
	e.getenv = func(k string) string {
		return map[string]string{"SWIFTBAR_PLUGIN_PATH": pluginPath, "GH_KOTLIN_PRS_GH": "/opt/homebrew/bin/gh", "GH_KOTLIN_PRS_SCRIPT": "2"}[k]
	}
	return e, out, errOut
}

func TestSwiftbarMenu(t *testing.T) {
	e, out, errOut := swiftbarEnv(t, &fakeClient{t: t}, "")
	if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	for _, want := range []string{
		"\n---\nYour move: ", "updated just now\n", "\nMine (3) | font=Menlo-Bold size=12\n",
		"--Details in the interactive view | bash=exec param1=/opt/homebrew/bin/gh param2=kotlin-prs param3=--pr param4=90006 terminal=true\n",
		"--/dry-run⋯ | bash=exec param1=/opt/homebrew/bin/gh param2=kotlin-prs param3=--pr param4=90006 param5=--post param6=dry-run terminal=true ",
		"\nOpen the interactive view | bash=exec param1=/opt/homebrew/bin/gh param2=kotlin-prs terminal=true\n",
		"\nRefresh now | bash=" + pluginPath + " param1=refresh terminal=false refresh=true\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the menu lacks %q:\n%s", want, out.String())
		}
	}
}

// What went wrong shows in the menu, with the last cached data; the plugin exits 0.
func TestSwiftbarErrors(t *testing.T) {
	e, out, _ := swiftbarEnv(t, nil, "")
	e.newClient = func() (github.Client, string, error) {
		return nil, "", errors.New("authentication token not found for host github.com")
	}
	if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK ||
		!strings.HasPrefix(out.String(), "! | sfimage=") || !strings.Contains(out.String(), "Couldn't fetch your PRs: authentication token not found") {
		t.Errorf("exit %d:\n%s", got, out.String())
	}

	client := &fakeClient{t: t}
	e, out, _ = swiftbarEnv(t, client, "")
	clock := now
	e.now = func() time.Time { return clock }
	run(context.Background(), []string{"list", "--format", "swiftbar"}, e)
	clock = now.Add(14 * time.Minute)
	out.Reset()
	e.newClient = func() (github.Client, string, error) { return failing{}, "test-account", nil }
	if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK ||
		!strings.Contains(out.String(), "Refresh failed: search: HTTP 502") || !strings.Contains(out.String(), "data from 14m ago") || !strings.Contains(out.String(), "Mine (3)") {
		t.Errorf("exit %d:\n%s", got, out.String())
	}
	// The title marks the data once it has missed a refresh: older than twice --max-age.
	for _, tt := range []struct {
		maxAge string
		stale  bool
	}{{"0s", true}, {"5m", true}, {"10m", false}} {
		out.Reset()
		run(context.Background(), []string{"list", "--format", "swiftbar", "--max-age", tt.maxAge}, e)
		title, _, _ := strings.Cut(out.String(), " | ")
		if strings.HasSuffix(title, "!") != tt.stale || !strings.Contains(out.String(), "data from 14m ago") {
			t.Errorf("--max-age %s: title %q", tt.maxAge, title)
		}
	}
}

type failing struct{}

func (failing) DoWithContext(context.Context, string, map[string]any, any) error {
	return errors.New("HTTP 502")
}

func TestSwiftbarInstall(t *testing.T) {
	dir := t.TempDir()
	e, out, errOut := testEnv(t, nil, "")
	if got := run(context.Background(), []string{"swiftbar", "install", "--dir", dir}, e); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	path := filepath.Join(dir, "kotlin-prs.30s.sh")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 || out.String() != "wrote "+path+"\n" {
		t.Fatalf("%v, %v, %q", info, err, out.String())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "gh=/opt/homebrew/bin/gh\n") || !strings.Contains(string(data), "--max-age 3m0s\n") {
		t.Errorf("script:\n%s", data)
	}
	errOut.Reset()
	if got := run(context.Background(), []string{"swiftbar", "install", "--dir", dir, "--interval", "2m"}, e); got != exitError ||
		!strings.Contains(errOut.String(), "already has the plugin (kotlin-prs.30s.sh); --force replaces it") {
		t.Errorf("no --force: exit %d, stderr %q", got, errOut.String())
	}
	if got := run(context.Background(), []string{"swiftbar", "install", "--dir", dir, "--interval", "2m", "--force"}, e); got != exitOK {
		t.Errorf("--force: exit %d", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "kotlin-prs.2m.sh" {
		t.Errorf("the folder holds %v", entries)
	}
	for _, tt := range []struct {
		args   []string
		exit   int
		stderr string
	}{
		{[]string{"swiftbar", "install"}, exitError, "SwiftBar has no plugin folder yet: open SwiftBar and choose one, or pass --dir"},
		{[]string{"swiftbar", "install", "--dir", filepath.Join(dir, "missing")}, exitError, "doesn't exist"},
		{[]string{"swiftbar", "install", "--dir", dir, "--interval", "1500ms"}, exitUsage, "isn't a whole number of seconds"},
		{[]string{"swiftbar", "install", "--dir", dir, "--interval", "5m", "--max-age", "1m"}, exitUsage,
			"--max-age 1m0s is shorter than --interval 5m0s: every run would fetch"},
		{[]string{"swiftbar", "install", "--dir", dir, "--max-age", "90500ms"}, exitUsage, "--max-age 1m30.5s isn't a positive whole number of seconds"},
	} {
		errOut.Reset()
		if got := run(context.Background(), tt.args, e); got != tt.exit || !strings.Contains(errOut.String(), tt.stderr) {
			t.Errorf("%q: exit %d, stderr %q", tt.args, got, errOut.String())
		}
	}
	// SwiftBar's own folder is the default.
	other := t.TempDir()
	e.swiftbarDir = func() string { return other }
	if got := run(context.Background(), []string{"swiftbar", "install"}, e); got != exitOK {
		t.Errorf("SwiftBar's folder: exit %d", got)
	}
	if _, err := os.Stat(filepath.Join(other, "kotlin-prs.30s.sh")); err != nil {
		t.Error(err)
	}
}

func TestSwiftbarScript(t *testing.T) {
	e, out, _ := testEnv(t, nil, "")
	if got := run(context.Background(), []string{"swiftbar", "script", "--interval", "1m", "--max-age", "5m"}, e); got != exitOK ||
		!strings.HasPrefix(out.String(), "#!/bin/bash\n") || !strings.Contains(out.String(), "--max-age 5m0s\n") ||
		!strings.Contains(out.String(), "\nexport GH_KOTLIN_PRS_SCRIPT=2\n") {
		t.Errorf("exit %d:\n%s", got, out.String())
	}
}

// A live run notifies of what changed since the last live run; a cached one doesn't.
func TestSwiftbarNotifications(t *testing.T) {
	e, _, errOut := swiftbarEnv(t, &fakeClient{t: t}, "")
	clock := now
	e.now = func() time.Time { return clock }
	var started [][]string
	e.start = func(argv []string) error { started = append(started, argv); return nil }
	runs := func(args ...string) {
		t.Helper()
		if got := run(context.Background(), args, e); got != exitOK {
			t.Fatalf("%q: exit %d, stderr %q", args, got, errOut.String())
		}
	}
	menu := func(args ...string) { t.Helper(); runs(append([]string{"list", "--format", "swiftbar"}, args...)...) }
	menu()
	b, ok := swiftbar.LoadBaseline(e.cacheDir)
	if !ok || len(started) != 0 || !b.FetchedAt.Equal(now) {
		t.Fatalf("the first run: baseline %v at %v, notified %q", ok, b.FetchedAt, started)
	}
	// Before, #90006's dry-run was still running.
	for i := range b.PRs {
		if b.PRs[i].Number == 90006 {
			b.PRs[i].DryRun.State, b.PRs[i].Next = model.RunRunning, model.NextCI
		}
	}
	if err := swiftbar.SaveBaseline(e.cacheDir, b); err != nil {
		t.Fatal(err)
	}
	// The cache answers with the baseline's own data: nothing newer.
	clock = clock.Add(10 * time.Second)
	menu("--max-age", "1h")
	if len(started) != 0 {
		t.Errorf("data no newer than the baseline notified: %q", started)
	}
	// Someone else (the TUI, `list`) fetches; the plugin's next run answers from the cache
	// with that newer data and notifies, once.
	clock = clock.Add(time.Minute)
	runs("list", "--format", "json")
	clock = clock.Add(10 * time.Second)
	menu("--max-age", "1h")
	if len(started) != 1 || started[0][0] != "open" || started[0][1] != "-g" ||
		!strings.HasPrefix(started[0][2], "swiftbar://notify?") || strings.Contains(started[0][2], "href=") ||
		!strings.Contains(started[0][2], "plugin=kotlin-prs") || !strings.Contains(started[0][2], "title=%2390006+dry-run+failed") ||
		!strings.Contains(started[0][2], "&bash=exec&param1="+url.QueryEscape("/opt/homebrew/bin/gh")+"&param2=kotlin-prs&param3=--pr&param4=90006&terminal=true") {
		t.Errorf("notified %q", started)
	}
	menu("--max-age", "1h")
	clock = clock.Add(time.Minute)
	menu()
	if len(started) != 1 {
		t.Errorf("notified again: %q", started)
	}
	if b, _ := swiftbar.LoadBaseline(e.cacheDir); !b.FetchedAt.Equal(clock) {
		t.Errorf("the baseline is from %v, want the live run's %v", b.FetchedAt, clock)
	}
	// A baseline newer than the data (the cache can't go back, but a clock can) stays.
	future := clock.Add(time.Hour)
	b, _ = swiftbar.LoadBaseline(e.cacheDir)
	b.FetchedAt = future
	if err := swiftbar.SaveBaseline(e.cacheDir, b); err != nil {
		t.Fatal(err)
	}
	menu("--max-age", "1h")
	if b, _ := swiftbar.LoadBaseline(e.cacheDir); !b.FetchedAt.Equal(future) || len(started) != 1 {
		t.Errorf("older data replaced the baseline (%v) or notified %q", b.FetchedAt, started)
	}
}

// The plugin runs the notify.command hook from the config, with the event on stdin.
func TestSwiftbarNotifyCommand(t *testing.T) {
	for _, channel := range []bool{true, false} {
		t.Run(fmt.Sprintf("swiftbar=%t", channel), func(t *testing.T) {
			events := filepath.Join(t.TempDir(), "events.jsonl")
			e, _, errOut := swiftbarEnv(t, &fakeClient{t: t}, fmt.Sprintf("notify: {swiftbar: %t, command: [sh, -c, 'cat >> \"%s\"']}\n", channel, events))
			var started [][]string
			e.start = func(argv []string) error { started = append(started, argv); return nil }
			menu := func() {
				t.Helper()
				if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK {
					t.Fatalf("exit %d, stderr %q", got, errOut.String())
				}
			}
			menu()
			b, _ := swiftbar.LoadBaseline(e.cacheDir)
			for i := range b.PRs {
				if b.PRs[i].Number == 90006 {
					b.PRs[i].DryRun.State, b.PRs[i].Next = model.RunRunning, model.NextCI
				}
			}
			b.FetchedAt = b.FetchedAt.Add(-time.Minute) // the data the next run fetches is newer
			if err := swiftbar.SaveBaseline(e.cacheDir, b); err != nil {
				t.Fatal(err)
			}
			menu()
			// The hook runs whatever the channel; SwiftBar's notification only with it.
			data, err := os.ReadFile(events)
			if err != nil || !strings.Contains(string(data), `"kind":"runFailed"`) || !strings.Contains(string(data), `"number":90006`) {
				t.Errorf("the hook wrote %q, %v; stderr %q", data, err, errOut.String())
			}
			if len(started) != map[bool]int{true: 1, false: 0}[channel] {
				t.Errorf("notified through SwiftBar %q", started)
			}
			// The baseline moved on either way: turning the channel back on replays nothing.
			e.configPath = filepath.Join(t.TempDir(), "config.yml")
			started = nil
			menu()
			if len(started) != 0 {
				t.Errorf("replayed %q", started)
			}
		})
	}
}

// update rewrites the installed plugin with the current script and its own settings.
func TestSwiftbarUpdate(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "my config.yml")
	if err := os.WriteFile(cfg, []byte("icons: ascii\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, out, errOut := testEnv(t, nil, "")
	if got := run(context.Background(), []string{"swiftbar", "install", "--dir", dir, "--interval", "1m", "--max-age", "5m", "--config", cfg}, e); got != exitOK {
		t.Fatalf("install: exit %d, stderr %q", got, errOut.String())
	}
	path := filepath.Join(dir, "kotlin-prs.1m.sh")
	installed, _ := os.ReadFile(path)
	// gh moved on the PATH since: the plugin keeps the gh it was written with.
	e.lookGH = func() (string, error) { return "/usr/local/bin/gh", nil }
	out.Reset()
	if got := run(context.Background(), []string{"swiftbar", "update", "--dir", dir}, e); got != exitOK {
		t.Fatalf("update: exit %d, stderr %q", got, errOut.String())
	}
	if updated, _ := os.ReadFile(path); string(updated) != string(installed) || out.String() != "updated "+path+" (a run every 1m0s, a fetch every 5m0s)\n" {
		t.Errorf("the round trip changed the plugin (%q):\n%s", out.String(), updated)
	}
	// A new --interval renames it; --max-age replaces its own.
	if got := run(context.Background(), []string{"swiftbar", "update", "--dir", dir, "--interval", "45s", "--max-age", "2m"}, e); got != exitOK {
		t.Fatalf("update --interval: exit %d, stderr %q", got, errOut.String())
	}
	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, "kotlin-prs.45s.sh"))
	if len(entries) != 1 || entries[0].Name() != "kotlin-prs.45s.sh" || !strings.Contains(string(data), "--max-age 2m0s\n") ||
		!strings.Contains(string(data), "export GH_KOTLIN_PRS_CONFIG="+swiftbar.Quote(cfg)+"\n") || !strings.Contains(string(data), "gh=/opt/homebrew/bin/gh\n") {
		t.Errorf("the folder holds %v:\n%s", entries, data)
	}
}

// A v0.5.2 plugin (format 1) every 3m, --max-age 1m30s: its interval meant freshness,
// so it becomes a fetch every 3m and a run every 30s, under the new file name.
func TestSwiftbarUpdateFormat1(t *testing.T) {
	dir := t.TempDir()
	old, err := os.ReadFile("../../testdata/swiftbar-script-format1.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kotlin-prs.3m.sh"), old, 0o755); err != nil {
		t.Fatal(err)
	}
	e, out, errOut := testEnv(t, nil, "")
	e.swiftbarDir = func() string { return dir }
	if got := run(context.Background(), []string{"swiftbar", "update"}, e); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, "kotlin-prs.30s.sh"))
	want := swiftbar.Script(swiftbar.ScriptOptions{GH: "/opt/homebrew/bin/gh", Config: "/Users/alice_user/my config.yml", MaxAge: 3 * time.Minute})
	if len(entries) != 1 || string(data) != want || !strings.Contains(out.String(), "kotlin-prs.30s.sh (a run every 30s, a fetch every 3m0s)") {
		t.Errorf("the folder holds %v (%q):\n%s", entries, out.String(), data)
	}
	if info, _ := os.Stat(filepath.Join(dir, "kotlin-prs.30s.sh")); info.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", info.Mode())
	}
}

func TestSwiftbarUpdateRefuses(t *testing.T) {
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, "kotlin-prs.5m.sh"), []byte("#!/bin/bash\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	several := t.TempDir()
	for _, name := range []string{"kotlin-prs.30s.sh", "kotlin-prs.3m.sh"} {
		if err := os.WriteFile(filepath.Join(several, name), []byte(swiftbar.Script(swiftbar.ScriptOptions{GH: "/opt/homebrew/bin/gh", MaxAge: time.Minute})), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	empty := t.TempDir()
	for _, tt := range []struct {
		args   []string
		exit   int
		stderr string
	}{
		{[]string{"swiftbar", "update", "--dir", foreign}, exitError,
			`kotlin-prs.5m.sh: not written by "gh kotlin-prs swiftbar install"; ` + "`gh kotlin-prs swiftbar install --force` replaces it"},
		{[]string{"swiftbar", "update", "--dir", empty}, exitError, "has no plugin to update; install it with `gh kotlin-prs swiftbar install`"},
		{[]string{"swiftbar", "update", "--dir", several}, exitError, "has several plugins (kotlin-prs.30s.sh, kotlin-prs.3m.sh)"},
		{[]string{"swiftbar", "update"}, exitError, "SwiftBar has no plugin folder yet"},
		{[]string{"swiftbar", "update", "--dir", several + "/missing"}, exitError, "doesn't exist"},
		{[]string{"swiftbar", "update", "--dir", empty, "--max-age", "10s"}, exitError, "has no plugin to update"},
	} {
		e, _, errOut := testEnv(t, nil, "")
		if got := run(context.Background(), tt.args, e); got != tt.exit || !strings.Contains(errOut.String(), tt.stderr) {
			t.Errorf("%q: exit %d, stderr %q", tt.args, got, errOut.String())
		}
	}
	// A max-age below the interval is refused, and nothing is written.
	dir := t.TempDir()
	e, _, errOut := testEnv(t, nil, "")
	run(context.Background(), []string{"swiftbar", "install", "--dir", dir}, e)
	if got := run(context.Background(), []string{"swiftbar", "update", "--dir", dir, "--max-age", "10s"}, e); got != exitUsage ||
		!strings.Contains(errOut.String(), "--max-age 10s is shorter than --interval 30s") {
		t.Errorf("exit %d, stderr %q", got, errOut.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 || entries[0].Name() != "kotlin-prs.30s.sh" {
		t.Errorf("the folder holds %v", entries)
	}
}

// An older plugin script than this binary writes gets an item to update it, run in the
// background on its folder; a current or newer one (a downgraded binary) doesn't.
func TestSwiftbarUpdateItem(t *testing.T) {
	const item = "\nUpdate the plugin script | bash=/opt/homebrew/bin/gh param1=kotlin-prs param2=swiftbar param3=update " +
		"param4=--dir param5=/Users/alice_user/Plugins terminal=false refresh=true\n"
	for format, shown := range map[string]bool{"": true, "1": true, "2": false, "3": false} {
		e, out, errOut := swiftbarEnv(t, &fakeClient{t: t}, "")
		env := e.getenv
		e.getenv = func(k string) string {
			if k == "GH_KOTLIN_PRS_SCRIPT" {
				return format
			}
			return env(k)
		}
		if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK {
			t.Fatalf("exit %d, stderr %q", got, errOut.String())
		}
		if strings.Contains(out.String(), item) != shown || strings.Count(out.String(), "Update the plugin script") > 1 {
			t.Errorf("format %q: shown %v, want %v:\n%s", format, !shown, shown, out.String())
		}
	}
}
