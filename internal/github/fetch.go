package github

import (
	"bytes"
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
	Merged []PullRequest
	// MaybeMine: open PRs of others that may be the viewer's: the cherry-pick bot's that
	// mention the viewer, and those assigned to the viewer. Each comes with its author, body
	// and assignees, which tell whose it is (a mention may be just that, an assignment a
	// person's PR). Each PR once.
	MaybeMine []PullRequest
	// MaybeMineMerged: the same, merged since the cut-off, with Merged's fields too.
	MaybeMineMerged []PullRequest
	RateLimit       RateLimit
}

const searchQuery = `query Sections($mine: String!, $personal: String!, $reviewed: String!, $requested: String!, $merged: String!, $cherryPicks: String!, $cherryPicksMerged: String!, $assigned: String!, $assignedMerged: String!) {
  viewer { login }
  rateLimit { limit cost remaining resetAt }
  mine: search(type: ISSUE, query: $mine, first: 50) { nodes { ... on PullRequest { number } } }
  personal: search(type: ISSUE, query: $personal, first: 50) { nodes { ... on PullRequest { number } } }
  reviewed: search(type: ISSUE, query: $reviewed, first: 50) { nodes { ... on PullRequest { number } } }
  requested: search(type: ISSUE, query: $requested, first: 50) { nodes { ... on PullRequest { number } } }
  merged: search(type: ISSUE, query: $merged, first: 50) {
    nodes { ... on PullRequest { number title url isDraft state createdAt updatedAt mergedAt headRefName author { __typename login } } }
  }
  cherryPicks: search(type: ISSUE, query: $cherryPicks, first: 20) { nodes { ...Whose } }
  cherryPicksMerged: search(type: ISSUE, query: $cherryPicksMerged, first: 20) { nodes { ...Whose ...Merged } }
  assigned: search(type: ISSUE, query: $assigned, first: 20) { nodes { ...Whose } }
  assignedMerged: search(type: ISSUE, query: $assignedMerged, first: 20) { nodes { ...Whose ...Merged } }
}

fragment Whose on PullRequest { number author { __typename login } body assignees(first: 5) { nodes { login } } }

fragment Merged on PullRequest { title url isDraft state createdAt updatedAt mergedAt headRefName }`

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
//
// A bot's PRs aren't the viewer's on GitHub, but they may be: a cherry-pick's body names
// the original PR's author, and a bot's PR assigned to the viewer is theirs to handle.
// Those that mention the viewer or are assigned to them come with what tells whose they are,
// 20 per search: each one's assignees count as a connection of their own, and with 50 the
// request would cost 2 points instead of 1.
func SearchSections(ctx context.Context, client Client, repo, cherryPickBot string, mergedSince time.Time) (*Search, error) {
	scope := "repo:" + repo + " is:pr "
	day := mergedSince.UTC().Truncate(24 * time.Hour)
	merged := " merged:>=" + day.Format(time.RFC3339)
	cherryPicks := "author:" + cherryPickBot + " mentions:@me"
	if cherryPickBot == "" {
		cherryPicks = "author:@me -author:@me" // finds nothing
	}
	vars := map[string]any{
		"mine":              scope + "is:open author:@me",
		"personal":          scope + "is:open user-review-requested:@me",
		"reviewed":          scope + "is:open reviewed-by:@me -author:@me",
		"requested":         scope + "is:open review-requested:@me",
		"merged":            scope + "is:merged author:@me" + merged,
		"cherryPicks":       scope + "is:open " + cherryPicks,
		"cherryPicksMerged": scope + "is:merged " + cherryPicks + merged,
		"assigned":          scope + "is:open assignee:@me -author:@me",
		"assignedMerged":    scope + "is:merged assignee:@me -author:@me" + merged,
	}
	var resp struct {
		Viewer            struct{ Login string } `json:"viewer"`
		RateLimit         RateLimit              `json:"rateLimit"`
		Mine              numberNodes            `json:"mine"`
		Personal          numberNodes            `json:"personal"`
		Reviewed          numberNodes            `json:"reviewed"`
		Requested         numberNodes            `json:"requested"`
		Merged            Nodes[PullRequest]     `json:"merged"`
		CherryPicks       Nodes[PullRequest]     `json:"cherryPicks"`
		CherryPicksMerged Nodes[PullRequest]     `json:"cherryPicksMerged"`
		Assigned          Nodes[PullRequest]     `json:"assigned"`
		AssignedMerged    Nodes[PullRequest]     `json:"assignedMerged"`
	}
	if err := client.DoWithContext(ctx, searchQuery, vars, &resp); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	before := func(pr PullRequest) bool { return pr.MergedAt == nil || pr.MergedAt.Before(mergedSince) }
	return &Search{
		Viewer:          resp.Viewer.Login,
		Mine:            resp.Mine.numbers(),
		Personal:        resp.Personal.numbers(),
		Reviewed:        resp.Reviewed.numbers(),
		Requested:       resp.Requested.numbers(),
		Merged:          slices.DeleteFunc(resp.Merged.Nodes, before),
		MaybeMine:       unique(slices.Concat(resp.CherryPicks.Nodes, resp.Assigned.Nodes), func(PullRequest) bool { return false }),
		MaybeMineMerged: unique(slices.Concat(resp.CherryPicksMerged.Nodes, resp.AssignedMerged.Nodes), before),
		RateLimit:       resp.RateLimit,
	}, nil
}

// unique drops the PRs without a number (not a PR), dropped ones, and repeats.
func unique(prs []PullRequest, drop func(PullRequest) bool) []PullRequest {
	var out []PullRequest
	for _, pr := range prs {
		if pr.Number != 0 && !drop(pr) && !slices.ContainsFunc(out, func(o PullRequest) bool { return o.Number == pr.Number }) {
			out = append(out, pr)
		}
	}
	return out
}

// prBatch caps the PRs per request, keeping each well under GitHub's node limit.
const prBatch = 40

// FetchPRs fetches full details for the given PRs, aliased into as few requests as possible,
// with the body of those in withBody (the cherry-picks: other bodies are human text, which
// the tool doesn't read). It returns the summed query cost. The PRs are queried in order
// of their numbers, so the same PRs make the same queries (the cache keys) whatever order
// they come in.
func FetchPRs(ctx context.Context, client Client, owner, name string, numbers, withBody []int) (map[int]*PullRequest, RateLimit, error) {
	prs := make(map[int]*PullRequest, len(numbers))
	var limit RateLimit
	for batch := range slices.Chunk(slices.Compact(slices.Sorted(slices.Values(numbers))), prBatch) {
		var q strings.Builder
		q.WriteString("query PullRequests($owner: String!, $name: String!) {\n  rateLimit { limit cost remaining resetAt }\n  repository(owner: $owner, name: $name) {\n")
		for _, n := range batch {
			body := ""
			if slices.Contains(withBody, n) {
				body = " body"
			}
			writef(&q, "    pr%d: pullRequest(number: %d) { ...PR%s }\n", n, n, body)
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

// PRQuery is the single-PR query, also used by scripts/fetch-fixtures.sh. It has the body,
// which a cherry-pick needs: one PR's body costs nothing worth saving.
func PRQuery() string {
	return `query PullRequest($owner: String!, $name: String!, $number: Int!) {
  viewer { login }
  rateLimit { limit cost remaining resetAt }
  repository(owner: $owner, name: $name) { pullRequest(number: $number) { ...PR body } }
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

// Forgetter drops cached responses of an operation (cache.Client.Forget).
type Forgetter interface {
	Forget(op string, variables map[string]any, match func(data []byte) bool)
}

// ForgetPR drops what f holds of the PR: its own query (FetchPR) and every details query
// that fetched it (FetchPRs), so the next ones fetch it anew.
func ForgetPR(f Forgetter, owner, name string, number int) {
	alias := []byte(fmt.Sprintf(`"pr%d"`, number))
	f.Forget("PullRequests", map[string]any{"owner": owner, "name": name}, func(data []byte) bool { return bytes.Contains(data, alias) })
	f.Forget("PullRequest", map[string]any{"owner": owner, "name": name, "number": number}, nil)
}

// writef is fmt.Fprintf into a strings.Builder, whose writes never fail.
func writef(b *strings.Builder, format string, args ...any) {
	_, _ = fmt.Fprintf(b, format, args...)
}
