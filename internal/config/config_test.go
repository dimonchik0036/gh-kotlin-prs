package config

import (
	"os"
	"path/filepath"
	"slices"
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
