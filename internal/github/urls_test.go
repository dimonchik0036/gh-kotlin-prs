package github

import "testing"

func TestURLs(t *testing.T) {
	if got := ProfileURL("alice_user"); got != "https://github.com/alice_user" {
		t.Errorf("ProfileURL = %q", got)
	}
	if got := TeamURL("ExampleOrg", "core-team"); got != "https://github.com/orgs/ExampleOrg/teams/core-team" {
		t.Errorf("TeamURL = %q", got)
	}
	if got := PRURL("ExampleOrg/project", 7); got != "https://github.com/ExampleOrg/project/pull/7" {
		t.Errorf("PRURL = %q", got)
	}
}

func TestParseTeamURL(t *testing.T) {
	for url, want := range map[string][2]string{
		"https://github.com/orgs/JetBrains/teams/kotlin-analysis-api": {"JetBrains", "kotlin-analysis-api"},
		"https://github.com/orgs/ExampleOrg/teams/core-team/":         {"ExampleOrg", "core-team"},
	} {
		org, slug, ok := ParseTeamURL(url)
		if !ok || org != want[0] || slug != want[1] {
			t.Errorf("ParseTeamURL(%q) = %q, %q, %v", url, org, slug, ok)
		}
	}
	for _, url := range []string{"https://github.com/alice_user", "https://github.com/orgs/ExampleOrg", "https://example.org/orgs/a/teams/b", "https://github.com/orgs/a/teams/b/members"} {
		if _, _, ok := ParseTeamURL(url); ok {
			t.Errorf("ParseTeamURL(%q) recognized a team", url)
		}
	}
}

func TestParseProfileURL(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/judy_user":         "judy_user",
		"https://github.com/judy-doe/":         "judy-doe",
		"https://github.com/judy?tab=repos":    "judy",
		"https://github.com/judy#top":          "judy",
		"https://github.com/orgs/JetBrains":    "",
		"https://github.com/JetBrains/kotlin":  "",
		"https://example.org/judy":             "",
		"http://github.com/judy":               "",
		"https://github.com/":                  "",
		"https://github.com/orgs/x/teams/core": "",
	} {
		if got, ok := ParseProfileURL(url); got != want || ok != (want != "") {
			t.Errorf("%s: %q, %v", url, got, ok)
		}
	}
}
