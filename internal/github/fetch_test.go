package github

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

type recordingClient struct {
	queries []string
}

var alias = regexp.MustCompile(`pr(\d+): pullRequest`)

func (c *recordingClient) DoWithContext(_ context.Context, query string, _ map[string]any, resp any) error {
	c.queries = append(c.queries, query)
	repo := map[string]any{}
	for _, m := range alias.FindAllStringSubmatch(query, -1) {
		var n int
		_ = json.Unmarshal([]byte(m[1]), &n)
		repo["pr"+m[1]] = map[string]any{"number": n}
	}
	body, _ := json.Marshal(map[string]any{"rateLimit": map[string]any{"cost": 1, "remaining": 100}, "repository": repo})
	return json.Unmarshal(body, resp)
}

func TestFetchPRsBatches(t *testing.T) {
	var numbers []int
	for n := 1; n <= 45; n++ {
		numbers = append(numbers, n)
	}
	client := &recordingClient{}
	prs, limit, err := FetchPRs(context.Background(), client, "JetBrains", "kotlin", numbers)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.queries) != 2 || len(prs) != 45 || limit.Cost != 2 {
		t.Errorf("%d requests, %d PRs, cost %d; want 2, 45, 2", len(client.queries), len(prs), limit.Cost)
	}
	if !strings.Contains(client.queries[0], "fragment PR on PullRequest") {
		t.Error("the fragment is missing from the query")
	}
}

type searchClient struct{ vars map[string]any }

func (c *searchClient) DoWithContext(_ context.Context, _ string, vars map[string]any, resp any) error {
	c.vars = vars
	return json.Unmarshal([]byte(`{"merged": {"nodes": [
	  {"number": 1, "mergedAt": "2026-09-30T08:00:00Z"},
	  {"number": 2, "mergedAt": "2026-09-30T14:00:00Z"},
	  {"number": 3, "mergedAt": "2026-10-01T09:00:00Z"}]}}`), resp)
}

// The merged search covers the whole UTC day, so it's the same query all day; the
// result is cut to the window.
func TestSearchMergedWindow(t *testing.T) {
	client := &searchClient{}
	since := time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC)
	search, err := SearchSections(context.Background(), client, "JetBrains/kotlin", since)
	if err != nil {
		t.Fatal(err)
	}
	if q := client.vars["merged"].(string); !strings.HasSuffix(q, " merged:>=2026-09-30T00:00:00Z") {
		t.Errorf("merged query %q", q)
	}
	var got []int
	for _, pr := range search.Merged {
		got = append(got, pr.Number)
	}
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("merged %v, want [2 3]", got)
	}
}

// The same PRs in any order make the same queries.
func TestFetchPRsOrder(t *testing.T) {
	a, b := &recordingClient{}, &recordingClient{}
	if _, _, err := FetchPRs(context.Background(), a, "JetBrains", "kotlin", []int{3, 1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := FetchPRs(context.Background(), b, "JetBrains", "kotlin", []int{2, 3, 1, 3}); err != nil {
		t.Fatal(err)
	}
	if a.queries[0] != b.queries[0] || strings.Count(a.queries[0], ": pullRequest(") != 3 {
		t.Errorf("queries differ:\n%s\n%s", a.queries[0], b.queries[0])
	}
}

func TestFetchPRsNothing(t *testing.T) {
	client := &recordingClient{}
	prs, _, err := FetchPRs(context.Background(), client, "JetBrains", "kotlin", nil)
	if err != nil || len(prs) != 0 || len(client.queries) != 0 {
		t.Errorf("no numbers should mean no request: %v, %d, %d", err, len(prs), len(client.queries))
	}
}

// The fixture script must build the same query as PRQuery, or fixtures drift from production.
func TestFetchFixturesScriptMatchesPRQuery(t *testing.T) {
	script, err := os.ReadFile("../../scripts/fetch-fixtures.sh")
	if err != nil {
		t.Fatal(err)
	}
	wrapper, _, _ := strings.Cut(PRQuery(), "fragment PR")
	escaped := strings.ReplaceAll(strings.TrimSpace(wrapper), "$", `\$`)
	if !strings.Contains(string(script), escaped) {
		t.Errorf("scripts/fetch-fixtures.sh doesn't contain the PRQuery wrapper:\n%s", escaped)
	}
}

func TestFixturesDecode(t *testing.T) {
	data, err := os.ReadFile("../../testdata/raw/pr-90005.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data PRResponse `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	pr := envelope.Data.Repository.PullRequest
	if pr == nil || pr.Number != 90005 || pr.Author.Login != "dimonchik0036" || len(pr.Comments.Nodes) == 0 || len(pr.Commits.Nodes) != 1 {
		t.Fatalf("decoded %+v", pr)
	}
	if pr.Commits.Nodes[0].Commit.StatusCheckRollup == nil {
		t.Error("no status check rollup")
	}
}
