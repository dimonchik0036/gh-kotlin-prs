package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repo != "JetBrains/kotlin" || cfg.Owner() != "JetBrains" || cfg.Name() != "kotlin" {
		t.Errorf("repo = %q", cfg.Repo)
	}
	if cfg.GateBot != "KotlinBuild" || cfg.OwnersBot != "kotlin-safemerge" {
		t.Errorf("bots = %q, %q", cfg.GateBot, cfg.OwnersBot)
	}
	if time.Duration(cfg.Refresh) != 3*time.Minute || time.Duration(cfg.RequestedTimeout) != 10*time.Minute || cfg.Icons != "unicode" {
		t.Errorf("durations = %v, %v", cfg.Refresh, cfg.RequestedTimeout)
	}
}

func TestParsePartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("teams: [kotlin-analysis-api]\nrequestedTimeout: 15m\nicons: ascii\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Teams, []string{"kotlin-analysis-api"}) || time.Duration(cfg.RequestedTimeout) != 15*time.Minute || cfg.Icons != "ascii" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.Repo != "JetBrains/kotlin" || time.Duration(cfg.Refresh) != 3*time.Minute {
		t.Errorf("unset keys lost their defaults: %+v", cfg)
	}
}

func TestParseErrors(t *testing.T) {
	for _, data := range []string{"refresh: soon\n", "refresh: 0s\n", "startupMaxAge: -1m\n", "repo: kotlin\n", "bots: {\n", "icons: emoji\n", "issueProjects: [KT-1]\n", "issueURL: https://example.org/\n", "hyperlinks: sometimes\n"} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("Parse(%q) succeeded", data)
		}
	}
}

func TestIsBot(t *testing.T) {
	cfg := Default()
	for login, want := range map[string]bool{
		"KotlinBuild":           true,
		"kotlin-safemerge":      true,
		"kotlin-safemerge[bot]": true,
		"kodee-bot":             true,
		"dependabot[bot]":       true,
		"dimonchik0036":         false,
	} {
		if got := cfg.IsBot(login); got != want {
			t.Errorf("IsBot(%q) = %v", login, got)
		}
	}
	if !SameLogin("kotlin-safemerge[bot]", "Kotlin-SafeMerge") {
		t.Error("SameLogin ignores case and the [bot] suffix")
	}
}

func TestIssues(t *testing.T) {
	cfg := Default()
	re := cfg.IssuePattern()
	for text, want := range map[string]string{
		"KT-123: Fix":         "KT-123",
		"rr/x/kt-73995-value": "kt-73995",
		"KTIJ-35000 in IDE":   "KTIJ-35000",
		"KTI-2 infra":         "KTI-2",
		"MKT-1 isn't one":     "",
		"KT-12x isn't either": "",
	} {
		if got := re.FindString(text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
	if got := cfg.IssueLink("KT-123"); got != "https://youtrack.jetbrains.com/issue/KT-123" {
		t.Errorf("IssueLink = %q", got)
	}
	custom, err := Parse([]byte("issueProjects: [abc]\nissueURL: https://example.org/browse/{id}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(custom.IssueProjects, []string{"ABC"}) || custom.IssuePattern().FindString("abc-7 kt-1") != "abc-7" {
		t.Errorf("custom projects = %q", custom.IssueProjects)
	}
	none, err := Parse([]byte("issueProjects: []\n"))
	if err != nil || none.IssuePattern().MatchString("KT-1 x") || none.TrailerPattern().MatchString("^KT-1 Fixed\n") || none.IssuePattern().MatchString("") {
		t.Errorf("empty issueProjects: %v, matches", err)
	}
}

func TestKeys(t *testing.T) {
	cfg, err := Parse([]byte("keys: {copy: c, refresh: [r, F5], build: B}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for action, want := range map[string][]string{"copy": {"c"}, "refresh": {"r", "F5"}, "build": {"B"}, "quit": {"q"}, "help": {"?"}} {
		if got := cfg.Keys[action]; !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", action, got, want)
		}
	}
	defaults := Default()
	if err := defaults.Keys.validate(); err != nil {
		t.Errorf("the defaults: %v", err)
	}
	for data, want := range map[string]string{
		"keys: {paste: p}\n":         `unknown action "paste"`,
		"keys: {copy: b}\n":          `"b" is bound to both build and copy`,
		"keys: {refresh: [r, r]}\n":  `refresh lists "r" twice`,
		"keys: {refresh: []}\n":      "refresh has no key",
		"keys: {quit: ctrl+c}\n":     "ctrl+c always quits",
		"keys: {copy: {a: b}}\n":     "copy must be a key or a list of keys",
		"keys: [copy]\n":             "keys must map actions to keys",
		"keys: {copy: \"\"}\n":       `"" is not a key name`,
		"keys: {copy: c, open: c}\n": `"c" is bound to both open and copy`,
	} {
		if _, err := Parse([]byte(data)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", data, err, want)
		}
	}
}

func TestNotify(t *testing.T) {
	cfg, err := Parse([]byte("notify: {bell: true, events: [runFailed, merged], command: [notify-send, '{title}', '{body}']}\n"))
	if err != nil {
		t.Fatal(err)
	}
	n := cfg.Notify
	if !n.Bell || !slices.Equal(n.Events, []string{"runFailed", "merged"}) || n.Terminal != "auto" || n.Timeout != Duration(10*time.Second) ||
		!slices.Equal(n.Command, []string{"notify-send", "{title}", "{body}"}) {
		t.Errorf("notify = %+v", n)
	}
	defaults := Default()
	if len(defaults.Notify.Events) != len(NotifyEvents) || defaults.Notify.validate() != nil {
		t.Errorf("defaults %+v", defaults.Notify)
	}
	for data, want := range map[string]string{
		"notify: {events: [ping]}\n":           `unknown event "ping"`,
		"notify: {events: [merged, merged]}\n": `"merged" listed twice`,
		"notify: {terminal: osc8}\n":           "terminal must be one of auto, osc9, osc777, osc99, none",
		"notify: {timeout: 0s}\n":              "timeout must be positive",
		"notify: {command: ['', x]}\n":         "command must start with a program",
	} {
		if _, err := Parse([]byte(data)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", data, err, want)
		}
	}
}

// With only HOME set, as under SwiftBar, the config is ~/.config/gh-kotlin-prs/config.yml.
func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/Users/alice_user")
	if got := DefaultPath(); got != "/Users/alice_user/.config/gh-kotlin-prs/config.yml" {
		t.Errorf("DefaultPath() = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := DefaultPath(); got != "/tmp/xdg/gh-kotlin-prs/config.yml" {
		t.Errorf("DefaultPath() = %q", got)
	}
}
