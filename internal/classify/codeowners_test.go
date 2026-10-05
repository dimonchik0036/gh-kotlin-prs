package classify

import (
	"slices"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

func ownersTable(t *testing.T, pr string) []model.CodeOwnerRule {
	t.Helper()
	rules, ok := ParseCodeOwners(commentBody(t, pr, "kotlin-safemerge", codeOwnersMarker))
	if !ok {
		t.Fatalf("pr-%s: not a code-owners comment", pr)
	}
	return rules
}

func owner(t *testing.T, rule model.CodeOwnerRule, login string) model.Owner {
	t.Helper()
	i := slices.IndexFunc(rule.Owners, func(o model.Owner) bool { return o.Login == login })
	if i < 0 {
		t.Fatalf("%v: no owner %s", rule.Paths, login)
	}
	return rule.Owners[i]
}

func TestParseCodeOwnersMarks(t *testing.T) {
	tests := []struct {
		name, pr  string
		rule      int
		paths     []string
		teams     []string
		mark      model.ApprovalMark
		assignees []model.Assignee
	}{
		{
			name: "❌ with an assignee", pr: "90005", rule: 0,
			paths: []string{"/analysis/"}, teams: []string{"kotlin-analysis-api"},
			mark: model.MarkNoReview, assignees: []model.Assignee{{Login: "bob_user"}},
		},
		{
			name: "❌ UNASSIGNED", pr: "90001", rule: 4,
			paths: []string{"/plugins/parcelize/"},
			mark:  model.MarkNoReview,
		},
		{
			name: "🔴 changes requested", pr: "90001", rule: 1,
			paths: []string{"/compiler/ir/backend.jvm/", "/compiler/testData/checkLocalVariablesTable/", "/compiler/testData/codegen/asmLike/", "/plugins/kapt/"},
			teams: []string{"kotlin-jvm"}, mark: model.MarkChangesRequested, assignees: []model.Assignee{{Login: "grace_user"}},
		},
		{
			name: "🔄 needs a re-request", pr: "90003", rule: 1,
			paths: []string{"/compiler/fir/", "/compiler/testData/diagnostics/"}, teams: []string{"kotlin-frontend"},
			mark: model.MarkReRequest, assignees: []model.Assignee{{Login: "sybil_user"}},
		},
		{
			name: "✅ 🔒 final approval", pr: "90004", rule: 0,
			paths: []string{"/analysis/", "/compiler/psi/"}, teams: []string{"kotlin-analysis-api"},
			mark: model.MarkApproved, assignees: []model.Assignee{{Login: "dave_user", Final: true}},
		},
		{
			name: "two top-level teams", pr: "90001", rule: 3,
			paths: []string{"/core/compiler.common.jvm/"}, teams: []string{"kotlin-frontend", "kotlin-jvm"},
			mark: model.MarkChangesRequested, assignees: []model.Assignee{{Login: "grace_user"}},
		},
		{
			name: "no owners column", pr: "90005", rule: 1,
			paths: []string{"*Generated.java"},
			mark:  model.MarkApproved, assignees: []model.Assignee{{Login: "trent_user", Final: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := ownersTable(t, tt.pr)
			if tt.rule >= len(rules) {
				t.Fatalf("only %d rules", len(rules))
			}
			r := rules[tt.rule]
			if !slices.Equal(r.Paths, tt.paths) {
				t.Errorf("paths = %q, want %q", r.Paths, tt.paths)
			}
			if !slices.Equal(r.Teams, tt.teams) {
				t.Errorf("teams = %q, want %q", r.Teams, tt.teams)
			}
			if r.Mark != tt.mark {
				t.Errorf("mark = %s, want %s", r.Mark, tt.mark)
			}
			if !slices.Equal(r.Assignees, tt.assignees) {
				t.Errorf("assignees = %+v, want %+v", r.Assignees, tt.assignees)
			}
		})
	}
}

func TestParseCodeOwnersOwners(t *testing.T) {
	t.Run("⏳ and QA members", func(t *testing.T) {
		rule := ownersTable(t, "90005")[2]
		if len(rule.Owners) != 12 {
			t.Errorf("%d owners, want 12", len(rule.Owners))
		}
		if o := owner(t, rule, "victor_user"); !o.Unavailable || o.Role != "" || o.Team != "kotlin-frontend" {
			t.Errorf("victor_user = %+v, want unavailable kotlin-frontend member", o)
		}
		if o := owner(t, rule, "alice2_user"); !o.Unavailable || o.Role != "QA" {
			t.Errorf("alice2_user = %+v, want unavailable QA", o)
		}
		if o := owner(t, rule, "heidi3_user"); o.Unavailable || o.Role != "QA" {
			t.Errorf("heidi3_user = %+v, want available QA", o)
		}
		if o := owner(t, rule, "quinn_user"); o.Unavailable || o.Role != "" {
			t.Errorf("quinn_user = %+v, want a plain member", o)
		}
	})
	t.Run("nested teams", func(t *testing.T) {
		rule := ownersTable(t, "90001")[2]
		if !slices.Equal(rule.Teams, []string{"kotlin-compiler"}) {
			t.Errorf("teams = %q, want only the top-level team", rule.Teams)
		}
		if o := owner(t, rule, "mallory_user"); o.Team != "kotlin-compiler" {
			t.Errorf("mallory_user in %q, want kotlin-compiler", o.Team)
		}
		if o := owner(t, rule, "quinn_user"); o.Team != "kotlin-frontend" {
			t.Errorf("quinn_user in %q, want the nested kotlin-frontend", o.Team)
		}
		if o := owner(t, rule, "alice3_user"); o.Team != "kotlin-native" {
			t.Errorf("alice3_user in %q, want the nested kotlin-native", o.Team)
		}
	})
	t.Run("individual owners", func(t *testing.T) {
		rule := ownersTable(t, "90001")[4]
		if len(rule.Teams) != 0 || len(rule.Owners) != 6 {
			t.Fatalf("teams %q, %d owners; want none and 6", rule.Teams, len(rule.Owners))
		}
		if o := owner(t, rule, "carol3_user"); o.Team != "" {
			t.Errorf("carol3_user = %+v, want an individual owner", o)
		}
	})
	t.Run("individual owners next to a team", func(t *testing.T) {
		rule := ownersTable(t, "90008")[5]
		if rule.Paths[0] != "/plugins/atomicfu/" {
			t.Fatalf("rule 5 is %q", rule.Paths)
		}
		if o := owner(t, rule, "trent4_user"); o.Team != "" {
			t.Errorf("trent4_user = %+v, want an individual owner", o)
		}
		if o := owner(t, rule, "quinn_user"); o.Team != "kotlin-frontend" {
			t.Errorf("quinn_user = %+v, want a kotlin-frontend member", o)
		}
	})
}

func TestParseCodeOwnersRejectsOtherComments(t *testing.T) {
	if _, ok := ParseCodeOwners(commentBody(t, "90004", "kotlin-safemerge", "Command rejected")); ok {
		t.Error("a rejection is not a code-owners comment")
	}
}

func TestCodeOwnersSource(t *testing.T) {
	analysis := ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{"alice_user", "bob_user"}, mark: "❌", assignees: []string{"alice_user"}}
	approved := ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{"alice_user", "bob_user"}, mark: "✅", assignees: []string{"alice_user 🔒"}}
	t.Run("the check summary when the comment is out of reach", func(t *testing.T) {
		pr := newPR(me).ownersCheck("FAILURE", analysis).mine()
		if pr.CodeOwners.State != model.CodeOwnersMissing || len(pr.CodeOwners.Rules) != 1 || pr.CodeOwners.Rules[0].Mark != model.MarkNoReview {
			t.Errorf("code owners = %+v", pr.CodeOwners)
		}
	})
	t.Run("the check summary wins over the comment", func(t *testing.T) {
		pr := newPR(me).owners(analysis).ownersCheck("SUCCESS", approved).mine()
		if pr.CodeOwners.State != model.CodeOwnersOK || pr.CodeOwners.Rules[0].Mark != model.MarkApproved {
			t.Errorf("code owners = %+v", pr.CodeOwners)
		}
	})
	t.Run("the comment without a check", func(t *testing.T) {
		pr := newPR(me).owners(analysis).mine()
		if pr.CodeOwners.State != model.CodeOwnersMissing || len(pr.CodeOwners.Rules) != 1 {
			t.Errorf("code owners = %+v", pr.CodeOwners)
		}
	})
}

// The bot lists individual owners first, then teams; a team without members is a bare
// link, and members can carry a role.
func TestParseCodeOwnersOwnersCell(t *testing.T) {
	body := "### Code Owners\n\n<table><tr><th>Rule</th><th>Owners</th><th>Approval</th></tr>" +
		"<tr><td><code>/\u200bplugins/\u200batomicfu/\u200b</code></td><td>" +
		`<a href="https://github.com/alice_user"><b><code>alice_user</code></b></a> (PM) ⏳, <a href="https://github.com/bob_user"><b><code>bob_user</code></b></a><br>` +
		`<a href="https://github.com/orgs/JetBrains/teams/kotlin-empty">kotlin-empty</a>` +
		`<details><summary><a href="https://github.com/orgs/JetBrains/teams/kotlin-libraries">kotlin-libraries</a></summary><ul>` +
		`<li><a href="https://github.com/carol_user"><b><code>carol_user</code></b></a> (QA)</li>` +
		`<li><a href="https://github.com/orgs/JetBrains/teams/kotlin-nested">kotlin-nested</a></li></ul></details>` +
		`</td><td align="center">❌<br><b><code>UNASSIGNED</code></b></td></tr></table>` + "\n\n<!-- CODE_OWNERS_REVIEW_COMMENT -->"
	rules, ok := ParseCodeOwners(body)
	if !ok || len(rules) != 1 {
		t.Fatalf("rules = %+v, %v", rules, ok)
	}
	r := rules[0]
	if !slices.Equal(r.Paths, []string{"/plugins/atomicfu/"}) {
		t.Errorf("paths = %q", r.Paths)
	}
	if !slices.Equal(r.Teams, []string{"kotlin-empty", "kotlin-libraries"}) {
		t.Errorf("teams = %q, want the bare link and the details team, not the nested one", r.Teams)
	}
	want := []model.Owner{
		{Login: "alice_user", Role: "PM", Unavailable: true},
		{Login: "bob_user"},
		{Login: "carol_user", Team: "kotlin-libraries", Role: "QA"},
	}
	if !slices.Equal(r.Owners, want) {
		t.Errorf("owners = %+v, want %+v", r.Owners, want)
	}
	if r.Mark != model.MarkNoReview || len(r.Assignees) != 0 {
		t.Errorf("approval = %s %+v", r.Mark, r.Assignees)
	}
}

// Team links are recognized by their shape, whatever the organization.
func TestParseCodeOwnersOtherOrg(t *testing.T) {
	body := "<table><tr><th>Rule</th><th>Owners</th><th>Approval</th></tr>" +
		"<tr><td><code>/src/</code></td><td>" +
		`<a href="https://github.com/orgs/ExampleOrg/teams/core">core</a>` +
		`<details><summary><a href="https://github.com/orgs/ExampleOrg/teams/docs">docs</a></summary><ul>` +
		`<li><a href="https://github.com/alice_user"><b><code>alice_user</code></b></a></li></ul></details>` +
		`</td><td align="center">❌<br><b><code>UNASSIGNED</code></b></td></tr></table>` + "\n<!-- CODE_OWNERS_REVIEW_COMMENT -->"
	rules, ok := ParseCodeOwners(body)
	if !ok || len(rules) != 1 {
		t.Fatalf("rules = %+v, %v", rules, ok)
	}
	if !slices.Equal(rules[0].Teams, []string{"core", "docs"}) || len(rules[0].Owners) != 1 || rules[0].Owners[0].Team != "docs" {
		t.Errorf("rule = %+v", rules[0])
	}
}

func TestPRURLFallback(t *testing.T) {
	c := testClassifier()
	c.Config.Repo = "ExampleOrg/project"
	b := newPR(me)
	b.pr.URL, b.pr.Number = "", 7
	if got := c.PR(b.build(), model.SectionMine).URL; got != "https://github.com/ExampleOrg/project/pull/7" {
		t.Errorf("URL = %q", got)
	}
}

// A person is whoever a profile link names, whatever its text says: the bot may show full
// names next to logins. The marks come from all text up to the next link, <br> or <li>;
// a display name is kept only when it's clearly one.
func TestParseCodeOwnersPeople(t *testing.T) {
	link := func(href, inner string) string { return `<a href="` + href + `">` + inner + `</a>` }
	profile := "https://github.com/judy"
	for _, tt := range []struct {
		name  string
		cell  string
		owner model.Owner
		none  bool
	}{
		{"today's format", link(profile, "<b><code>judy</code></b>") + " (QA) ⏳", model.Owner{Login: "judy", Role: "QA", Unavailable: true}, false},
		{"a name after the link", link(profile, "<b><code>judy</code></b>") + " Judy Doe ⏳ (QA)",
			model.Owner{Login: "judy", Name: "Judy Doe", Role: "QA", Unavailable: true}, false},
		{"a name in the link", link(profile, "<b><code>judy</code></b> Judy Doe") + " ⏳",
			model.Owner{Login: "judy", Name: "Judy Doe", Unavailable: true}, false},
		{"only a name: the login from the href", link(profile, "<b><code>Judy Doe</code></b>"),
			model.Owner{Login: "judy", Name: "Judy Doe"}, false},
		{"the name, then the login", link(profile, "<b><code>Judy Doe (judy)</code></b>") + " (QA) ⏳",
			model.Owner{Login: "judy", Name: "Judy Doe", Role: "QA", Unavailable: true}, false},
		{"the name, then the login: any profile name", link(profile, "<b><code>J. Doe 2nd (Judy) &amp; co 👩\u200d💻 (judy)</code></b>"),
			model.Owner{Login: "judy", Name: "J. Doe 2nd (Judy) & co 👩\u200d💻"}, false},
		{"the name, then the login: marks in the name aren't marks", link(profile, "<b><code>Judy ⏳🔒 (QA) (judy)</code></b>") + " (PM)",
			model.Owner{Login: "judy", Name: "Judy ⏳🔒 (QA)", Role: "PM"}, false},
		{"the name, then the login: no control characters", link(profile, "<b><code>Judy\x1b[31m\tDoe\u202e (judy)</code></b>"),
			model.Owner{Login: "judy", Name: "Judy [31m Doe"}, false},
		{"the name, then the login in another case", link(profile, "<b><code>Judy Doe (Judy)</code></b>"),
			model.Owner{Login: "judy", Name: "Judy Doe"}, false},
		{"the name, then another login: unsure", link(profile, "<b><code>Judy Doe (kevin)</code></b>"),
			model.Owner{Login: "judy"}, false},
		{"another host, the name, then the login", link("https://example.org/people/42", "<code>Judy Doe (judy-doe)</code>"),
			model.Owner{Login: "judy-doe", Name: "Judy Doe"}, false},
		{"another host, a name and a role", link("https://example.org/people/42", "<code>Judy Doe (QA)</code>"), model.Owner{}, true},
		{"a name in an inline element", link(profile, "<b><code>judy</code></b>") + " <sub>Judy Doe</sub> ⏳",
			model.Owner{Login: "judy", Name: "Judy Doe", Unavailable: true}, false},
		{"a trailing slash and a fragment", link("https://github.com/judy/#profile", "<code>judy</code>"), model.Owner{Login: "judy"}, false},
		{"another host, a login text", link("https://example.org/people/42", "<code>judy-doe</code>") + " (PM)",
			model.Owner{Login: "judy-doe", Role: "PM"}, false},
		{"another host, a name text", link("https://example.org/people/42", "<code>Judy Doe</code>"), model.Owner{}, true},
		{"two names: unsure", link(profile, "<code>judy</code> Judy Doe") + " <sub>J. Doe</sub>", model.Owner{Login: "judy"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := "<table><tr><th>Rule</th><th>Owners</th><th>Approval</th></tr><tr><td><code>/src/</code></td><td>" + tt.cell +
				`</td><td align="center">❌<br><b><code>UNASSIGNED</code></b></td></tr></table>` + "\n<!-- CODE_OWNERS_REVIEW_COMMENT -->"
			rules, ok := ParseCodeOwners(body)
			if !ok || len(rules) != 1 || len(rules[0].Assignees) != 0 {
				t.Fatalf("rules = %+v, %v", rules, ok)
			}
			switch {
			case tt.none && len(rules[0].Owners) != 0:
				t.Errorf("owners = %+v, want none", rules[0].Owners)
			case !tt.none && (len(rules[0].Owners) != 1 || rules[0].Owners[0] != tt.owner):
				t.Errorf("owners = %+v, want %+v", rules[0].Owners, tt.owner)
			}
		})
	}
}

// The approval cell reads people the same way: 🔒 and ⏳ from any text after the link.
func TestParseCodeOwnersAssigneesWithNames(t *testing.T) {
	body := "<table><tr><th>Rule</th><th>Owners</th><th>Approval</th></tr><tr><td><code>/src/</code></td><td></td>" +
		`<td align="center">✅<br><a href="https://github.com/judy"><b><code>judy</code></b></a> <sub>Judy Doe</sub> 🔒, ` +
		`<a href="https://github.com/kevin/"><b><code>Kevin Roe</code></b></a> ⏳, ` +
		`<a href="https://github.com/laura"><b><code>laura</code></b></a>, ` +
		`<a href="https://github.com/mike"><b><code>Mike Poe (mike)</code></b></a> 🔒</td></tr></table>` + "\n<!-- CODE_OWNERS_REVIEW_COMMENT -->"
	rules, ok := ParseCodeOwners(body)
	if !ok || len(rules) != 1 {
		t.Fatalf("rules = %+v, %v", rules, ok)
	}
	want := []model.Assignee{{Login: "judy", Name: "Judy Doe", Final: true}, {Login: "kevin", Name: "Kevin Roe", Unavailable: true},
		{Login: "laura"}, {Login: "mike", Name: "Mike Poe", Final: true}}
	if r := rules[0]; r.Mark != model.MarkApproved || !slices.Equal(r.Assignees, want) {
		t.Errorf("approval = %s %+v, want %+v", r.Mark, r.Assignees, want)
	}
}
