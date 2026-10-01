package github

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
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
