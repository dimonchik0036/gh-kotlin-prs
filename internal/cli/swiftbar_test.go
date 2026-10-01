package cli

import (
	"context"
	"errors"
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
		return map[string]string{"SWIFTBAR_PLUGIN_PATH": pluginPath, "GH_KOTLIN_PRS_GH": "/opt/homebrew/bin/gh"}[k]
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
	path := filepath.Join(dir, "kotlin-prs.3m.sh")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 || out.String() != "wrote "+path+"\n" {
		t.Fatalf("%v, %v, %q", info, err, out.String())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "gh=/opt/homebrew/bin/gh\n") || !strings.Contains(string(data), "--max-age 1m30s\n") {
		t.Errorf("script:\n%s", data)
	}
	errOut.Reset()
	if got := run(context.Background(), []string{"swiftbar", "install", "--dir", dir, "--interval", "5m"}, e); got != exitError ||
		!strings.Contains(errOut.String(), "already has the plugin (kotlin-prs.3m.sh); --force replaces it") {
		t.Errorf("no --force: exit %d, stderr %q", got, errOut.String())
	}
	if got := run(context.Background(), []string{"swiftbar", "install", "--dir", dir, "--interval", "5m", "--force"}, e); got != exitOK {
		t.Errorf("--force: exit %d", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "kotlin-prs.5m.sh" {
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
	if _, err := os.Stat(filepath.Join(other, "kotlin-prs.3m.sh")); err != nil {
		t.Error(err)
	}
}

func TestSwiftbarScript(t *testing.T) {
	e, out, _ := testEnv(t, nil, "")
	if got := run(context.Background(), []string{"swiftbar", "script", "--interval", "30s"}, e); got != exitOK ||
		!strings.HasPrefix(out.String(), "#!/bin/bash\n") || !strings.Contains(out.String(), "--max-age 15s\n") ||
		!strings.Contains(out.String(), "# <xbar.version>v0.0.0-test</xbar.version>\n") {
		t.Errorf("exit %d:\n%s", got, out.String())
	}
}

// A live run notifies of what changed since the last live run; a cached one doesn't.
func TestSwiftbarNotifications(t *testing.T) {
	e, _, errOut := swiftbarEnv(t, &fakeClient{t: t}, "")
	var started [][]string
	e.start = func(argv []string) error { started = append(started, argv); return nil }
	menu := func(args ...string) {
		t.Helper()
		if got := run(context.Background(), append([]string{"list", "--format", "swiftbar"}, args...), e); got != exitOK {
			t.Fatalf("exit %d, stderr %q", got, errOut.String())
		}
	}
	menu()
	prs, ok := swiftbar.LoadBaseline(e.cacheDir)
	if !ok || len(started) != 0 {
		t.Fatalf("the first run: baseline %v, notified %q", ok, started)
	}
	// Before, #90006's dry-run was still running.
	for i := range prs {
		if prs[i].Number == 90006 {
			prs[i].DryRun.State, prs[i].Next = model.RunRunning, model.NextCI
		}
	}
	if err := swiftbar.SaveBaseline(e.cacheDir, prs); err != nil {
		t.Fatal(err)
	}
	menu("--max-age", "1h")
	if len(started) != 0 {
		t.Errorf("a cached run notified: %q", started)
	}
	menu()
	if len(started) != 1 || started[0][0] != "open" || started[0][1] != "-g" ||
		!strings.HasPrefix(started[0][2], "swiftbar://notify?") || strings.Contains(started[0][2], "href=") ||
		!strings.Contains(started[0][2], "plugin=kotlin-prs") || !strings.Contains(started[0][2], "title=%2390006+dry-run+failed") ||
		!strings.Contains(started[0][2], "&bash=exec&param1="+url.QueryEscape("/opt/homebrew/bin/gh")+"&param2=kotlin-prs&param3=--pr&param4=90006&terminal=true") {
		t.Errorf("notified %q", started)
	}
	menu()
	if len(started) != 1 {
		t.Errorf("notified again: %q", started)
	}
}

// The plugin runs the notify.command hook from the config, with the event on stdin.
func TestSwiftbarNotifyCommand(t *testing.T) {
	events := filepath.Join(t.TempDir(), "events.jsonl")
	e, _, errOut := swiftbarEnv(t, &fakeClient{t: t}, "notify: {command: [sh, -c, 'cat >> \""+events+"\"']}\n")
	e.start = func([]string) error { return nil }
	menu := func() {
		t.Helper()
		if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK {
			t.Fatalf("exit %d, stderr %q", got, errOut.String())
		}
	}
	menu()
	prs, _ := swiftbar.LoadBaseline(e.cacheDir)
	for i := range prs {
		if prs[i].Number == 90006 {
			prs[i].DryRun.State, prs[i].Next = model.RunRunning, model.NextCI
		}
	}
	if err := swiftbar.SaveBaseline(e.cacheDir, prs); err != nil {
		t.Fatal(err)
	}
	menu()
	data, err := os.ReadFile(events)
	if err != nil || !strings.Contains(string(data), `"kind":"runFailed"`) || !strings.Contains(string(data), `"number":90006`) {
		t.Errorf("the hook wrote %q, %v; stderr %q", data, err, errOut.String())
	}
}
