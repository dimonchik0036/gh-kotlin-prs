package swiftbar

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/demo"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/render"
)

// Synthetic PRs of the release workflow and the bots' PRs, through the whole pipeline: the
// demo client's searches, the classification and the menu. alice_user looks: the author of
// cherry-picks, and a release engineer (kotlin-release) of others'.

const (
	viewer    = "alice_user"
	aggregate = "Aggregate (2.5.0) (2.5.0)"
	external  = "K2 All Projects (External) (User Projects)"
)

func at(h, m int) string { return fmt.Sprintf("2026-10-01T%02d:%02d:00Z", h, m) }

// owners is a code-owners table row: a team's path, its members, the mark and who it's
// assigned to (none: UNASSIGNED).
type owners struct {
	path, team, mark  string
	members, assigned []string
}

func ownersTable(rows ...owners) string {
	link := func(login string) string {
		return fmt.Sprintf(`<a href="https://github.com/%s"><b><code>%s</code></b></a>`, login, login)
	}
	var b strings.Builder
	b.WriteString("### Code Owners\n\n<table><tr><th>Rule</th><th>Owners</th><th>Approval</th></tr>")
	for _, r := range rows {
		fmt.Fprintf(&b, `<tr><td><code>%s</code></td><td><details><summary><a href="https://github.com/orgs/JetBrains/teams/%s">%s</a></summary><ul>`, r.path, r.team, r.team)
		for _, m := range r.members {
			b.WriteString("<li>" + link(m) + "</li>")
		}
		fmt.Fprintf(&b, `</ul></details></td><td align="center">%s<br>`, r.mark)
		if len(r.assigned) == 0 {
			b.WriteString("<b><code>UNASSIGNED</code></b>")
		}
		for i, a := range r.assigned {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(link(a))
		}
		b.WriteString("</td></tr>")
	}
	b.WriteString("</table>\n\n<!-- CODE_OWNERS_REVIEW_COMMENT -->")
	return b.String()
}

// pr is a synthetic PR response.
type pr struct {
	number             int
	author, authorType string
	body, base, branch string
	state, mergedAt    string
	assignees          []string
	requested          []string
	approvedBy         []string
	owners             []owners
	ownersOK           bool
	gates              map[string]string // status context → state
	comments           []map[string]any
}

func (p pr) json() map[string]any {
	url := fmt.Sprintf("https://github.com/JetBrains/kotlin/pull/%d", p.number)
	authorType := p.authorType
	if authorType == "" {
		authorType = "User"
	}
	node := func(m map[string]any) []map[string]any { return []map[string]any{m} }
	var assignees, requests, reviews []map[string]any
	for _, a := range p.assignees {
		assignees = append(assignees, map[string]any{"login": a})
	}
	for _, r := range p.requested {
		requests = append(requests, map[string]any{"requestedReviewer": map[string]any{"__typename": "User", "login": r}})
	}
	for i, r := range p.approvedBy {
		reviews = append(reviews, map[string]any{"author": map[string]any{"__typename": "User", "login": r}, "state": "APPROVED",
			"submittedAt": at(11, i), "url": fmt.Sprintf("%s#pullrequestreview-%d", url, i), "comments": map[string]any{"totalCount": 0}})
	}
	conclusion, title := "FAILURE", "Waiting for code owner approvals"
	if p.ownersOK {
		conclusion, title = "SUCCESS", "All code owners have approved"
	}
	contexts := node(map[string]any{"__typename": "CheckRun", "name": "Code Owners Approval", "conclusion": conclusion,
		"status": "COMPLETED", "title": title, "summary": ownersTable(p.owners...)})
	build := 700 + 10*(p.number%100)
	for _, ctx := range []string{aggregate, external} {
		if state, ok := p.gates[ctx]; ok {
			build++
			contexts = append(contexts, map[string]any{"__typename": "StatusContext", "context": ctx, "state": state,
				"createdAt": at(12, 0), "targetUrl": fmt.Sprintf("https://buildserver.labs.intellij.net/build/%d", build)})
		}
	}
	for i := range p.comments {
		p.comments[i]["url"] = fmt.Sprintf("%s#issuecomment-%d", url, i)
	}
	state := p.state
	if state == "" {
		state = "OPEN"
	}
	var mergedAt any
	if p.mergedAt != "" {
		mergedAt = p.mergedAt
	}
	prTitle := "Example change"
	if p.base != "master" {
		prTitle = "[" + p.base + "] " + prTitle
	}
	return map[string]any{
		"number": p.number, "title": prTitle, "url": url, "state": state, "isDraft": false,
		"createdAt": at(9, 0), "updatedAt": at(13, 0), "mergedAt": mergedAt,
		"author": map[string]any{"__typename": authorType, "login": p.author}, "body": p.body,
		"assignees": map[string]any{"nodes": assignees}, "headRefName": p.branch, "headRefOid": strings.Repeat("0", 40),
		"baseRefName": p.base, "mergeable": "MERGEABLE",
		"reviewRequests":           map[string]any{"nodes": requests},
		"latestOpinionatedReviews": map[string]any{"nodes": reviews},
		"reviews":                  map[string]any{"nodes": reviews},
		"comments":                 map[string]any{"nodes": p.comments},
		"timelineItems":            map[string]any{"nodes": node(map[string]any{"__typename": "PullRequestCommit", "commit": map[string]any{"committedDate": at(10, 0)}})},
		"commits": map[string]any{"nodes": node(map[string]any{"commit": map[string]any{"committedDate": at(10, 0),
			"statusCheckRollup": map[string]any{"contexts": map[string]any{"nodes": contexts}}}})},
	}
}

func cherryPickOf(original int, author string) string {
	return fmt.Sprintf("Original pull request: https://github.com/JetBrains/kotlin/pull/%d by @%s\n", original, author)
}

func releaseMenu(t *testing.T) Menu {
	t.Helper()
	fir := func(mark string, assigned ...string) owners {
		return owners{path: "/compiler/fir/", team: "kotlin-frontend", mark: mark, members: []string{"bob_user"}, assigned: assigned}
	}
	release := func(mark string, assigned ...string) owners {
		return owners{path: "*", team: "kotlin-release", mark: mark, members: []string{"olivia_user", viewer}, assigned: assigned}
	}
	cherry := func(number int, original string) pr {
		return pr{number: number, author: "KotlinBuild", body: cherryPickOf(number-100, original), base: "2.5.0",
			branch: fmt.Sprintf("rrr/2.5.0/topic/example-%d", number)}
	}
	prs := []pr{}
	// Mine: a failed Aggregate.
	p := cherry(901, viewer)
	p.owners, p.requested, p.approvedBy = []owners{fir("✅", "bob_user"), release("❌", "olivia_user")}, []string{"olivia_user"}, []string{"bob_user"}
	p.gates = map[string]string{aggregate: "FAILURE", external: "FAILURE"}
	prs = append(prs, p)
	// Mine: the release engineer's to merge, the User Projects red.
	p = cherry(902, viewer)
	p.owners, p.requested, p.approvedBy = []owners{fir("✅", "bob_user"), release("❌", "olivia_user")}, []string{"olivia_user"}, []string{"bob_user"}
	p.gates = map[string]string{aggregate: "SUCCESS", external: "FAILURE"}
	prs = append(prs, p)
	// Mine, written by me on a release-run branch: the Aggregate running.
	p = pr{number: 903, author: viewer, base: "2.5.0", branch: "rrrn/2.5.0/topic/example-903",
		owners: []owners{fir("❌", "bob_user"), release("❌")}, requested: []string{"bob_user"},
		gates: map[string]string{aggregate: "PENDING"}}
	prs = append(prs, p)
	// Mine: an agent's PR on master, assigned to me.
	p = pr{number: 904, author: "agent", authorType: "Bot", body: "Bump the versions.", base: "master", branch: "topic/example-904",
		assignees: []string{viewer}, owners: []owners{fir("✅", "bob_user")}, ownersOK: true, approvedBy: []string{"bob_user"}}
	prs = append(prs, p)
	// Review: I'm the release engineer, carol_user's cherry-pick is ready.
	p = cherry(905, "carol_user")
	p.owners, p.requested, p.approvedBy = []owners{fir("✅", "bob_user"), release("❌", viewer)}, []string{viewer}, []string{"bob_user"}
	p.gates = map[string]string{aggregate: "SUCCESS", external: "FAILURE"}
	prs = append(prs, p)
	// Review: dave_user's isn't, its Aggregate running: hidden, whatever my request says.
	p = cherry(906, "dave_user")
	p.owners, p.requested, p.approvedBy = []owners{fir("✅", "bob_user"), release("❌", viewer)}, []string{viewer}, []string{"bob_user"}
	p.gates = map[string]string{aggregate: "PENDING"}
	prs = append(prs, p)
	// Recently merged: a cherry-pick of mine.
	p = cherry(907, viewer)
	p.state, p.mergedAt, p.ownersOK = "MERGED", at(14, 30), true
	prs = append(prs, p)
	// Mine: erin_user's, assigned to me, who asked for it.
	p = cherry(909, "erin_user")
	p.assignees, p.owners, p.requested, p.approvedBy = []string{viewer}, []owners{fir("✅", "bob_user"), release("❌", "olivia_user")}, []string{"olivia_user"}, []string{"bob_user"}
	p.gates = map[string]string{aggregate: "SUCCESS"}
	prs = append(prs, p)
	// Not mine: handed to erin_user, who asked for it.
	p = cherry(908, viewer)
	p.assignees, p.gates = []string{"erin_user"}, map[string]string{aggregate: "SUCCESS"}
	prs = append(prs, p)

	dir := t.TempDir()
	for _, p := range prs {
		// Indented as the fixtures are: the demo clock reads their `"createdAt": "…"`.
		data, err := json.MarshalIndent(map[string]any{"data": map[string]any{"viewer": map[string]any{"login": viewer},
			"repository": map[string]any{"pullRequest": p.json()}}}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("pr-%d.json", p.number)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fixtures, err := demo.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	d, err := listing.Fetch(context.Background(), fixtures, cfg, fixtures.Now(), allSections)
	if err != nil {
		t.Fatal(err)
	}
	shown, hidden := listing.Filter(d.Classify(cfg, fixtures.Now()), false, false)
	return Menu{PRs: shown, Hidden: hidden, Sections: allSections, Viewer: d.Viewer, Icons: render.Unicode,
		FetchedAt: fixtures.Now().Add(-time.Minute), Now: fixtures.Now(), Plugin: plugin}
}

// The release workflow and the bots' PRs in the menu: QG in place of DR and SM, whose move
// it is, the original PR, /test-public among the commands.
func TestGoldenReleaseMenu(t *testing.T) {
	out := Render(releaseMenu(t))
	golden(t, "swiftbar-release.txt", out)
	for _, want := range []string{
		"#901", "QG ✗", "quality gate failed: " + aggregate,
		"#902", "QG ✓", "Waiting on the release engineer", "waiting for the release engineer: olivia_user", "(not blocking)",
		"Cherry-pick of #802 | href=https://github.com/JetBrains/kotlin/pull/802",
		"/test-public⋯",
		"#903", "QG ⟳", "quality gate running: " + aggregate,
		"#904", "ready to /safe-merge",
		"#905", "ready to approve and merge", "Cherry-pick of #805 |", "by carol_user",
		"#909", "Cherry-pick of #809 by erin_user",
		"#907",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the menu lacks %q", want)
		}
	}
	for _, absent := range []string{"#906", "#908", "nina"} {
		if strings.Contains(out, absent) {
			t.Errorf("the menu has %q", absent)
		}
	}
}
