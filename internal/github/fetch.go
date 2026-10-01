package github

import (
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
)

//go:embed pr.graphql
var prFragment string

// Client is the part of go-gh's GraphQL client we use; tests substitute a fake.
type Client interface {
	DoWithContext(ctx context.Context, query string, variables map[string]any, response any) error
}

// DefaultClient is gh's GraphQL client for its default host. account identifies the
// credentials (the host and the token) for the keys of the cache; never store it as is.
func DefaultClient() (client Client, account string, err error) {
	host, _ := auth.DefaultHost()
	token, _ := auth.TokenForHost(host)
	// With the host and token given, go-gh doesn't resolve them (and run `gh auth token`) again.
	gql, err := api.NewGraphQLClient(api.ClientOptions{Host: host, AuthToken: token})
	if err != nil {
		return nil, "", err
	}
	return gql, host + "\x00" + token, nil
}

// Search holds the PR numbers of every section, as returned by GitHub search.
type Search struct {
	Viewer string
	// Mine: open PRs authored by the viewer.
	Mine []int
	// Personal: review requested from the viewer personally.
	Personal []int
	// Reviewed: reviewed by the viewer, authored by someone else.
	Reviewed []int
	// Requested: review requested from the viewer or one of their teams.
	Requested []int
	// Merged: the viewer's PRs merged since the cut-off, with a few fields only.
	Merged    []PullRequest
	RateLimit RateLimit
}

const searchQuery = `query Sections($mine: String!, $personal: String!, $reviewed: String!, $requested: String!, $merged: String!) {
  viewer { login }
  rateLimit { limit cost remaining resetAt }
  mine: search(type: ISSUE, query: $mine, first: 50) { nodes { ... on PullRequest { number } } }
  personal: search(type: ISSUE, query: $personal, first: 50) { nodes { ... on PullRequest { number } } }
  reviewed: search(type: ISSUE, query: $reviewed, first: 50) { nodes { ... on PullRequest { number } } }
  requested: search(type: ISSUE, query: $requested, first: 50) { nodes { ... on PullRequest { number } } }
  merged: search(type: ISSUE, query: $merged, first: 50) {
    nodes { ... on PullRequest { number title url isDraft state createdAt updatedAt mergedAt headRefName author { __typename login } } }
  }
}`

type numberNodes struct {
	Nodes []struct {
		Number int `json:"number"`
	} `json:"nodes"`
}

func (n numberNodes) numbers() []int {
	var out []int
	for _, node := range n.Nodes {
		if node.Number != 0 {
			out = append(out, node.Number)
		}
	}
	return out
}

// SearchSections runs every section search in one request. Searches return only numbers:
// GitHub charges search connections by their page size, so fetching details for
// the unique PRs afterwards (FetchPRs) is several times cheaper than inlining them.
// Merged PRs are searched from the start of mergedSince's UTC day, so that the query
// stays the same (and cacheable) all day, then filtered by mergedSince.
func SearchSections(ctx context.Context, client Client, repo string, mergedSince time.Time) (*Search, error) {
	scope := "repo:" + repo + " is:pr "
	day := mergedSince.UTC().Truncate(24 * time.Hour)
	vars := map[string]any{
		"mine":      scope + "is:open author:@me",
		"personal":  scope + "is:open user-review-requested:@me",
		"reviewed":  scope + "is:open reviewed-by:@me -author:@me",
		"requested": scope + "is:open review-requested:@me",
		"merged":    scope + "is:merged author:@me merged:>=" + day.Format(time.RFC3339),
	}
	var resp struct {
		Viewer    struct{ Login string } `json:"viewer"`
		RateLimit RateLimit              `json:"rateLimit"`
		Mine      numberNodes            `json:"mine"`
		Personal  numberNodes            `json:"personal"`
		Reviewed  numberNodes            `json:"reviewed"`
		Requested numberNodes            `json:"requested"`
		Merged    Nodes[PullRequest]     `json:"merged"`
	}
	if err := client.DoWithContext(ctx, searchQuery, vars, &resp); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return &Search{
		Viewer:    resp.Viewer.Login,
		Mine:      resp.Mine.numbers(),
		Personal:  resp.Personal.numbers(),
		Reviewed:  resp.Reviewed.numbers(),
		Requested: resp.Requested.numbers(),
		Merged:    slices.DeleteFunc(resp.Merged.Nodes, func(pr PullRequest) bool { return pr.MergedAt == nil || pr.MergedAt.Before(mergedSince) }),
		RateLimit: resp.RateLimit,
	}, nil
}

// prBatch caps the PRs per request, keeping each well under GitHub's node limit.
const prBatch = 40

// FetchPRs fetches full details for the given PRs, aliased into as few requests as possible.
// It returns the summed query cost. The PRs are queried in order of their numbers, so the
// same PRs make the same queries (the cache keys) whatever order they come in.
func FetchPRs(ctx context.Context, client Client, owner, name string, numbers []int) (map[int]*PullRequest, RateLimit, error) {
	prs := make(map[int]*PullRequest, len(numbers))
	var limit RateLimit
	for batch := range slices.Chunk(slices.Compact(slices.Sorted(slices.Values(numbers))), prBatch) {
		var q strings.Builder
		q.WriteString("query PullRequests($owner: String!, $name: String!) {\n  rateLimit { limit cost remaining resetAt }\n  repository(owner: $owner, name: $name) {\n")
		for _, n := range batch {
			writef(&q, "    pr%d: pullRequest(number: %d) { ...PR }\n", n, n)
		}
		q.WriteString("  }\n}\n")
		q.WriteString(prFragment)

		var resp struct {
			RateLimit  RateLimit               `json:"rateLimit"`
			Repository map[string]*PullRequest `json:"repository"`
		}
		vars := map[string]any{"owner": owner, "name": name}
		if err := client.DoWithContext(ctx, q.String(), vars, &resp); err != nil {
			return nil, limit, fmt.Errorf("fetch PRs: %w", err)
		}
		for _, pr := range resp.Repository {
			if pr != nil {
				prs[pr.Number] = pr
			}
		}
		limit.Cost += resp.RateLimit.Cost
		limit.Limit = resp.RateLimit.Limit
		limit.Remaining = resp.RateLimit.Remaining
		limit.ResetAt = resp.RateLimit.ResetAt
	}
	return prs, limit, nil
}

// PRQuery is the single-PR query, also used by scripts/fetch-fixtures.sh.
func PRQuery() string {
	return `query PullRequest($owner: String!, $name: String!, $number: Int!) {
  viewer { login }
  rateLimit { limit cost remaining resetAt }
  repository(owner: $owner, name: $name) { pullRequest(number: $number) { ...PR } }
}
` + prFragment
}

// PRResponse is the decoded data of PRQuery.
type PRResponse struct {
	Viewer     struct{ Login string } `json:"viewer"`
	RateLimit  RateLimit              `json:"rateLimit"`
	Repository struct {
		PullRequest *PullRequest `json:"pullRequest"`
	} `json:"repository"`
}

func FetchPR(ctx context.Context, client Client, owner, name string, number int) (*PRResponse, error) {
	var resp PRResponse
	vars := map[string]any{"owner": owner, "name": name, "number": number}
	if err := client.DoWithContext(ctx, PRQuery(), vars, &resp); err != nil {
		return nil, fmt.Errorf("fetch #%d: %w", number, err)
	}
	if resp.Repository.PullRequest == nil {
		return nil, fmt.Errorf("fetch #%d: not found", number)
	}
	return &resp, nil
}

// writef is fmt.Fprintf into a strings.Builder, whose writes never fail.
func writef(b *strings.Builder, format string, args ...any) {
	_, _ = fmt.Fprintf(b, format, args...)
}
