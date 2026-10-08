package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
)

// prsGitHub answers the details queries with bare PRs, counting the fetches per number.
type prsGitHub struct{ fetched map[int]int }

var alias = regexp.MustCompile(`pr(\d+): pullRequest`)

func (g *prsGitHub) DoWithContext(_ context.Context, query string, vars map[string]any, resp any) error {
	if n, ok := vars["number"].(int); ok {
		g.fetched[n]++
		return json.Unmarshal([]byte(fmt.Sprintf(`{"repository": {"pullRequest": {"number": %d}}}`, n)), resp)
	}
	var prs []string
	for _, m := range alias.FindAllStringSubmatch(query, -1) {
		var n int
		_, _ = fmt.Sscan(m[1], &n)
		g.fetched[n]++
		prs = append(prs, fmt.Sprintf(`"pr%d": {"number": %d}`, n, n))
	}
	return json.Unmarshal([]byte(`{"repository": {`+strings.Join(prs, ", ")+`}}`), resp)
}

// ForgetPR drops the cached details of one PR: its own query and the batches with it.
func TestForgetPR(t *testing.T) {
	g := &prsGitHub{fetched: map[int]int{}}
	c := &cache.Client{Next: g, Dir: t.TempDir(), Account: "a", MaxAge: time.Hour, Now: time.Now}
	ctx := context.Background()
	fetch := func() {
		t.Helper()
		for _, batch := range [][]int{{7, 8}, {9}} {
			if _, _, err := github.FetchPRs(ctx, c, "JetBrains", "kotlin", batch, nil); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := github.FetchPR(ctx, c, "JetBrains", "kotlin", 7); err != nil {
			t.Fatal(err)
		}
	}
	fetch()
	github.ForgetPR(c, "JetBrains", "kotlin", 7)
	fetch()
	// #7 twice in a batch and twice alone, #8 refetched with it, #9's batch from the cache.
	if want := map[int]int{7: 4, 8: 2, 9: 1}; fmt.Sprint(g.fetched) != fmt.Sprint(want) {
		t.Errorf("fetched %v, want %v", g.fetched, want)
	}
}
