package github

import (
	"context"
	"encoding/json"
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
