// Package demo serves the fixtures of a directory as if they came from GitHub, for
// screenshots and trying the tool without a login: GH_KOTLIN_PRS_DEMO=<dir>. It plugs
// into the CLI's client and clock seams; classification and rendering are the real ones.
package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
)

// Client answers the CLI's queries from fixture files (testdata/raw/pr-*.json).
type Client struct {
	viewer string
	prs    map[int]json.RawMessage
	raw    map[int]*github.PullRequest
	// now is the fixtures' clock: the next full hour after their newest timestamp.
	now time.Time
}

// Load reads every pr-*.json in dirs, a list of directories like $PATH
// ("testdata/raw:testdata/demo"). A PR number in two of them is an error.
func Load(dirs string) (*Client, error) {
	var files []string
	for _, dir := range filepath.SplitList(dirs) {
		found, err := filepath.Glob(filepath.Join(dir, "pr-*.json"))
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("demo: no pr-*.json fixtures in %s", dir)
		}
		files = append(files, found...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("demo: no fixtures in %q", dirs)
	}
	c := &Client{prs: map[int]json.RawMessage{}, raw: map[int]*github.PullRequest{}}
	var newest time.Time
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Data struct {
				Viewer     struct{ Login string } `json:"viewer"`
				Repository struct {
					PullRequest json.RawMessage `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, fmt.Errorf("demo: %s: %w", f, err)
		}
		var pr github.PullRequest
		if err := json.Unmarshal(envelope.Data.Repository.PullRequest, &pr); err != nil {
			return nil, fmt.Errorf("demo: %s: %w", f, err)
		}
		if _, dup := c.prs[pr.Number]; dup {
			return nil, fmt.Errorf("demo: %s: #%d is in another fixture too", f, pr.Number)
		}
		c.viewer = envelope.Data.Viewer.Login
		c.prs[pr.Number] = envelope.Data.Repository.PullRequest
		c.raw[pr.Number] = &pr
		newest = latest(newest, data)
	}
	c.now = newest.Truncate(time.Hour).Add(time.Hour)
	return c, nil
}

var timestamp = regexp.MustCompile(`"(?:createdAt|updatedAt|submittedAt|committedDate|mergedAt)": "([^"]+)"`)

func latest(t time.Time, data []byte) time.Time {
	for _, m := range timestamp.FindAllSubmatch(data, -1) {
		if u, err := time.Parse(time.RFC3339, string(m[1])); err == nil && u.After(t) {
			t = u
		}
	}
	return t
}

// Now is the fixtures' clock.
func (c *Client) Now() time.Time { return c.now }

var alias = regexp.MustCompile(`pr(\d+): pullRequest\(number: \d+\)`)

// DoWithContext answers the section search, the batched details query and the single-PR
// query of internal/github.
func (c *Client) DoWithContext(_ context.Context, query string, vars map[string]any, resp any) error {
	var out any
	switch {
	case strings.Contains(query, "mine: search"):
		out = c.search(vars)
	case alias.MatchString(query):
		repo := map[string]json.RawMessage{}
		for _, m := range alias.FindAllStringSubmatch(query, -1) {
			n, _ := strconv.Atoi(m[1])
			if pr, ok := c.prs[n]; ok {
				repo["pr"+m[1]] = pr
			}
		}
		out = map[string]any{"rateLimit": map[string]any{}, "repository": repo}
	default:
		n, _ := vars["number"].(int)
		pr, ok := c.prs[n]
		if !ok {
			return fmt.Errorf("demo: no fixture for #%d", n)
		}
		out = map[string]any{"viewer": map[string]any{"login": c.viewer}, "rateLimit": map[string]any{},
			"repository": map[string]any{"pullRequest": pr}}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, resp)
}

// search sorts the fixtures into the sections the way GitHub's search would.
func (c *Client) search(vars map[string]any) map[string]any {
	since := time.Time{}
	if q, _ := vars["merged"].(string); q != "" {
		if _, after, ok := strings.Cut(q, "merged:>="); ok {
			since, _ = time.Parse(time.RFC3339, strings.Fields(after)[0])
		}
	}
	sections := map[string][]any{"mine": {}, "personal": {}, "reviewed": {}, "requested": {}, "merged": {}}
	add := func(section string, n int) {
		sections[section] = append(sections[section], map[string]any{"number": n})
	}
	for _, n := range slices.Sorted(maps.Keys(c.raw)) {
		pr := c.raw[n]
		mine := strings.EqualFold(pr.Author.LoginOrEmpty(), c.viewer)
		switch {
		case pr.State == "MERGED":
			if mine && pr.MergedAt != nil && !pr.MergedAt.Before(since) {
				sections["merged"] = append(sections["merged"], json.RawMessage(c.prs[n]))
			}
			continue
		case pr.State != "OPEN":
			continue
		case mine:
			add("mine", n)
			continue
		}
		personal, team := false, false
		for _, rr := range pr.ReviewRequests.Nodes {
			switch r := rr.RequestedReviewer; {
			case r == nil:
			case r.Slug != "":
				team = true
			case strings.EqualFold(r.Login, c.viewer):
				personal = true
			}
		}
		if personal {
			add("personal", n)
		}
		if personal || team {
			add("requested", n)
		}
		if slices.ContainsFunc(pr.Reviews.Nodes, func(r github.Review) bool { return strings.EqualFold(r.Author.LoginOrEmpty(), c.viewer) }) {
			add("reviewed", n)
		}
	}
	out := map[string]any{"viewer": map[string]any{"login": c.viewer}, "rateLimit": map[string]any{}}
	for k, v := range sections {
		out[k] = map[string]any{"nodes": v}
	}
	return out
}
