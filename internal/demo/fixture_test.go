package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

var update = flag.Bool("update", false, "rewrite the derived demo fixture")

// demoFixture is the one derived fixture: what the request-review recording needs and no
// real PR has, a PR of mine whose code-owner rows nobody was asked for. It's #90006 with
// another code-owners table, kept out of testdata/raw so that no golden sees it.
const demoFixture = "../../testdata/demo/pr-90010.json"

// TestDemoFixture checks that the derived fixture is what the derivation makes of #90006;
// `go test ./internal/demo -run TestDemoFixture -update` regenerates it.
func TestDemoFixture(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/raw/pr-90006.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := deriveAssignment(raw)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(demoFixture, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(demoFixture)
	if err != nil {
		t.Fatalf("%v (go test ./internal/demo -run TestDemoFixture -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s isn't what the derivation makes of #90006: go test ./internal/demo -run TestDemoFixture -update", demoFixture)
	}
}

// codeOwnersRow is a row of the bot's table: its paths, its team (members) or individual
// owners, its mark and the people of its Approval column (none for UNASSIGNED).
type codeOwnersRow struct {
	paths     []string
	team      string
	owners    []string // "login", "login ⏳", "login (QA)"
	mark      string
	approvals []string // "login", "login 🔒"
}

// assignmentTable is shaped like a PR touching five subsystems: one owner commented and
// waits for a re-request (🔄), one row is approved (✅ 🔒), three were never assigned:
// judy_user owns two of those, the third shares nobody.
var assignmentTable = []codeOwnersRow{
	{paths: []string{"/analysis/"}, team: "kotlin-analysis-api", mark: "🔄", approvals: []string{"dave_user"},
		owners: []string{"bob_user", "carol_user", "dave_user", "dimonchik0036", "erin_user"}},
	{paths: []string{"/compiler/fir/", "/compiler/frontend.common.jvm/", "/core/descriptors.jvm/", "/core/deserialization.common.jvm/"},
		team: "kotlin-frontend", mark: "✅", approvals: []string{"trent_user 🔒"},
		owners: []string{"quinn_user", "rupert_user", "sybil_user", "trent_user", "ursula_user (QA)", "victor_user ⏳"}},
	{paths: []string{"/compiler/testData/codegen/asmLike/"}, team: "kotlin-jvm", mark: "❌",
		owners: []string{"judy_user", "kevin_user ⏳", "laura_user", "mallory_user (QA)", "dimonchik0036"}},
	{paths: []string{"/core/descriptors.runtime/"}, mark: "❌", owners: []string{"niaj_user", "judy_user", "olivia_user ⏳"}},
	{paths: []string{"/plugins/parcelize/"}, team: "kotlin-parcelize", mark: "❌",
		owners: []string{"peggy_user", "heidi_user (QA)", "ivan_user ⏳"}},
}

// codeOwnersHTML is the table in the bot's HTML: zero-width spaces after "/", ".", "_"
// and "-" in paths, user links in <code>, a team as <details>, marks after the links.
func codeOwnersHTML(rows []codeOwnersRow) string {
	user := func(entry string) string {
		login, suffix, _ := strings.Cut(entry, " ")
		if suffix != "" {
			suffix = " " + suffix
		}
		return fmt.Sprintf(`<a href="https://github.com/%s"><b><code>%s</code></b></a>%s`, login, login, suffix)
	}
	zwsp := strings.NewReplacer("/", "/\u200b", ".", ".\u200b", "_", "_\u200b", "-", "-\u200b")
	var b strings.Builder
	b.WriteString("### Code Owners\n\n<table><tr><th>Rule</th><th>Owners</th><th>Approval</th></tr>")
	for _, r := range rows {
		paths := make([]string, len(r.paths))
		for i, p := range r.paths {
			paths[i] = "<code>" + zwsp.Replace(p) + "</code>"
		}
		b.WriteString("<tr><td>" + strings.Join(paths, ", ") + "</td><td>")
		if r.team != "" {
			_, _ = fmt.Fprintf(&b, `<details><summary><a href="https://github.com/orgs/JetBrains/teams/%s">%s</a></summary><ul>`, r.team, r.team)
			for _, o := range r.owners {
				b.WriteString("<li>" + user(o) + "</li>")
			}
			b.WriteString("</ul></details>")
		} else {
			owners := make([]string, len(r.owners))
			for i, o := range r.owners {
				owners[i] = user(o)
			}
			b.WriteString(strings.Join(owners, ", "))
		}
		b.WriteString(`</td><td align="center">` + r.mark + "<br>")
		if len(r.approvals) == 0 {
			b.WriteString("<b><code>UNASSIGNED</code></b>")
		}
		approvals := make([]string, len(r.approvals))
		for i, a := range r.approvals {
			approvals[i] = user(a)
		}
		b.WriteString(strings.Join(approvals, ", ") + "</td></tr>")
	}
	b.WriteString("</table>\n\n<!-- CODE_OWNERS_REVIEW_COMMENT -->")
	return b.String()
}

// deriveAssignment makes #90010 of #90006's response: another number, title and branch,
// assignmentTable as the bot's comment and the check's summary, no runs (no commands, no
// gate comments: the reviews lead the row), no pending review request, and dave_user's
// commenting review behind the 🔄.
func deriveAssignment(raw []byte) ([]byte, error) {
	text := strings.NewReplacer("/pull/90006", "/pull/90010", "GITHUB-90006", "GITHUB-90010").Replace(string(raw))
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		return nil, err
	}
	pr := doc["data"].(map[string]any)["repository"].(map[string]any)["pullRequest"].(map[string]any)
	pr["number"] = 90010
	pr["title"] = "KT-990011: Example change"
	pr["headRefName"] = "topic/KT-990011-example"
	table := codeOwnersHTML(assignmentTable)
	replaced := 0
	comments := pr["comments"].(map[string]any)
	var kept []any
	for _, c := range comments["nodes"].([]any) {
		c := c.(map[string]any)
		body := c["body"].(string)
		author, _ := c["author"].(map[string]any)["login"].(string)
		switch {
		case strings.Contains(body, "<!-- CODE_OWNERS_REVIEW_COMMENT -->"):
			c["body"] = table
			replaced++
		case strings.HasPrefix(body, "/") || author == "KotlinBuild":
			continue
		}
		kept = append(kept, c)
	}
	comments["nodes"] = kept
	for _, commit := range pr["commits"].(map[string]any)["nodes"].([]any) {
		rollup, _ := commit.(map[string]any)["commit"].(map[string]any)["statusCheckRollup"].(map[string]any)
		if rollup == nil {
			continue
		}
		for _, ctx := range rollup["contexts"].(map[string]any)["nodes"].([]any) {
			if ctx := ctx.(map[string]any); ctx["name"] == "Code Owners Approval" {
				ctx["summary"] = table
				replaced++
			}
		}
	}
	if replaced != 2 {
		return nil, fmt.Errorf("replaced %d code-owners tables, want the comment's and the check's", replaced)
	}
	pr["reviewRequests"] = map[string]any{"nodes": []any{}}
	reviews := pr["reviews"].(map[string]any)
	reviews["nodes"] = append(reviews["nodes"].([]any), map[string]any{
		"author": map[string]any{"__typename": "User", "login": "dave_user"}, "comments": map[string]any{"totalCount": 0},
		"state": "COMMENTED", "submittedAt": "2026-10-01T07:12:05Z", "url": "https://github.com/JetBrains/kotlin/pull/90010#pullrequestreview-1125000001",
	})
	// As the fixtures are: indented, the bot's HTML unescaped.
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// With testdata/demo next to testdata/raw, #90010 is a PR of mine to assign: three rows
// nobody was asked for, two sharing judy_user, and dave_user to re-request.
func TestDemoAssignment(t *testing.T) {
	c, err := Load("../../testdata/raw" + string(filepath.ListSeparator) + "../../testdata/demo")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	d, err := listing.Fetch(context.Background(), c, cfg, c.Now(), []model.Section{model.SectionMine})
	if err != nil {
		t.Fatal(err)
	}
	pr, ok := d.Show(cfg, c.Now(), 90010)
	if !ok || pr.Section != model.SectionMine || pr.Next != model.NextMe || pr.Primary() != "re-request review from dave_user" || len(pr.Runs) != 0 {
		t.Fatalf("#90010: %v, %q, %d runs", ok, pr.Texts(), len(pr.Runs))
	}
	texts := strings.Join(pr.Texts(), "; ")
	for _, want := range []string{"re-request review from dave_user", "assign reviewers for /compiler/testData/codegen/asmLike/, /core/descriptors.runtime/, /plugins/parcelize/"} {
		if !strings.Contains(texts, want) {
			t.Errorf("reasons %q lack %q", texts, want)
		}
	}
	var rows []string
	for _, s := range actions.Subsystems(pr) {
		var logins []string
		for _, c := range s.Candidates {
			logins = append(logins, c.Login)
		}
		rows = append(rows, fmt.Sprintf("%s +%d %s: %s", s.Path, s.More, s.Status, strings.Join(logins, " ")))
	}
	want := []string{
		"/analysis/ +0 re-request: dave_user: bob_user carol_user dave_user erin_user",
		"/compiler/fir/ +3 ✓ trent_user: quinn_user rupert_user sybil_user trent_user ursula_user victor_user",
		"/compiler/testData/codegen/asmLike/ +0 unassigned: judy_user laura_user mallory_user kevin_user",
		"/core/descriptors.runtime/ +0 unassigned: niaj_user judy_user olivia_user",
		"/plugins/parcelize/ +0 unassigned: peggy_user heidi_user ivan_user",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
	if got := actions.PreSelected(pr); len(got) != 1 || got[0] != "dave_user" {
		t.Errorf("pre-selected %q", got)
	}
}
