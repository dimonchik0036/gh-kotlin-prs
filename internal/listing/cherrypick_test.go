package listing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/demo"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

type recorder struct {
	github.Client
	queries []string
}

func (r *recorder) DoWithContext(ctx context.Context, query string, vars map[string]any, resp any) error {
	r.queries = append(r.queries, query)
	return r.Client.DoWithContext(ctx, query, vars, resp)
}

// fixture writes a synthetic PR response, the viewer dimonchik0036's, into dir.
func fixture(t *testing.T, dir string, number int, author, state, body, extra string) {
	t.Helper()
	typename := "User"
	if author == "agent" {
		typename = "Bot"
	}
	pr := fmt.Sprintf(`{"number": %d, "title": "[2.5.0] Example change", "state": %q, "author": {"__typename": %q, "login": %q},
	  "body": %q, "headRefName": "rrr/2.5.0/topic/example-%d", "baseRefName": "2.5.0",
	  "createdAt": "2026-10-01T10:00:00Z", "updatedAt": "2026-10-01T11:00:00Z"%s}`, number, state, typename, author, body, number, extra)
	data := `{"data": {"viewer": {"login": "dimonchik0036"}, "repository": {"pullRequest": ` + pr + `}}}`
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("pr-%d.json", number)), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The bot's cherry-picks that name me as the original author are mine, open or merged,
// unless someone else is assigned; those that only mention me aren't. A bot's PR assigned
// to me is mine, a person's isn't. Only the others' that are mine are fetched with the body.
func TestCherryPicks(t *testing.T) {
	dir := t.TempDir()
	original := "Original pull request: https://github.com/JetBrains/kotlin/pull/5 by @"
	fixture(t, dir, 901, "KotlinBuild", "OPEN", original+"dimonchik0036\n", "")
	fixture(t, dir, 902, "KotlinBuild", "OPEN", original+"alice_user\n\ncc @dimonchik0036", "")
	fixture(t, dir, 903, "KotlinBuild", "MERGED", original+"dimonchik0036\n", `, "mergedAt": "2026-10-01T11:00:00Z"`)
	fixture(t, dir, 904, "dimonchik0036", "OPEN", "", "")
	assigned := func(logins ...string) string {
		nodes := make([]string, len(logins))
		for i, l := range logins {
			nodes[i] = `{"login": "` + l + `"}`
		}
		return `, "assignees": {"nodes": [` + strings.Join(nodes, ", ") + `]}`
	}
	fixture(t, dir, 905, "KotlinBuild", "OPEN", original+"dimonchik0036\n", assigned("bob_user"))
	fixture(t, dir, 906, "agent", "OPEN", "Bump the versions.", assigned("dimonchik0036"))
	fixture(t, dir, 907, "carol_user", "OPEN", "", assigned("dimonchik0036"))
	fixtures, err := demo.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	client := &recorder{Client: fixtures}
	cfg := config.Default()
	d, err := Fetch(context.Background(), client, cfg, fixtures.Now(), allSections)
	if err != nil {
		t.Fatal(err)
	}
	prs := d.Classify(cfg, fixtures.Now())
	sections := bySection(prs)
	if mine := sections[model.SectionMine]; !slices.Equal(mine, []int{904, 901, 906}) {
		t.Errorf("Mine = %v, want [904 901 906]", mine)
	}
	if merged := sections[model.SectionMerged]; !slices.Equal(merged, []int{903}) {
		t.Errorf("Recently merged = %v, want [903]", merged)
	}
	for _, pr := range prs {
		if (pr.Number == 901 || pr.Number == 903) && (pr.Author != "dimonchik0036" || pr.CherryPickOf == nil || pr.CherryPickOf.Number != 5) {
			t.Errorf("#%d: author %q, cherry-pick of %+v", pr.Number, pr.Author, pr.CherryPickOf)
		}
	}
	details := client.queries[len(client.queries)-1]
	if !strings.Contains(details, "pr901: pullRequest(number: 901) { ...PR body }") || !strings.Contains(details, "pr904: pullRequest(number: 904) { ...PR }") ||
		!strings.Contains(details, "pr906: pullRequest(number: 906) { ...PR body }") ||
		strings.Contains(details, "pr902") || strings.Contains(details, "pr905") || strings.Contains(details, "pr907") {
		t.Errorf("details query:\n%s", details)
	}
}
