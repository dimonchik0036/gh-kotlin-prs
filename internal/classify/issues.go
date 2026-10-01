package classify

import (
	"slices"
	"strings"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// issues collects the issues a PR references, primary first: trailers in its commit
// messages (`^KT-123 Fixed`, then `^KT-123 Obsolete`, then bare `^KT-123`), then IDs in
// the branch name, then in the title. Each ID appears once; a trailer's resolution wins.
func (c *Classifier) issues(raw *github.PullRequest) []model.Issue {
	issues := make([]model.Issue, 0) // non-nil: JSON "issues" is [] when empty
	index := map[string]int{}
	add := func(id string, source model.IssueSource, resolution model.IssueResolution) {
		id = strings.ToUpper(id)
		if i, ok := index[id]; ok {
			if source == model.IssueTrailer && resolutionRank(resolution) < resolutionRank(issues[i].Resolution) {
				issues[i].Resolution = resolution
			}
			return
		}
		index[id] = len(issues)
		issues = append(issues, model.Issue{ID: id, URL: c.Config.IssueLink(id), Source: source, Resolution: resolution})
	}

	trailers := c.Config.TrailerPattern()
	for _, n := range raw.History.Nodes {
		for _, m := range trailers.FindAllStringSubmatch(n.Commit.Message, -1) {
			add(m[1], model.IssueTrailer, trailerResolution(m[2]))
		}
	}
	// Trailers by resolution; the sort is stable, so first appearance breaks ties.
	slices.SortStableFunc(issues, func(a, b model.Issue) int {
		return resolutionRank(a.Resolution) - resolutionRank(b.Resolution)
	})
	for i, issue := range issues {
		index[issue.ID] = i
	}

	ids := c.Config.IssuePattern()
	for _, id := range ids.FindAllString(raw.HeadRefName, -1) {
		add(id, model.IssueBranch, model.IssueRelated)
	}
	for _, id := range ids.FindAllString(raw.Title, -1) {
		add(id, model.IssueTitle, model.IssueRelated)
	}
	return issues
}

func trailerResolution(word string) model.IssueResolution {
	switch strings.ToLower(word) {
	case "fixed":
		return model.IssueFixed
	case "obsolete":
		return model.IssueObsolete
	}
	return model.IssueRelated
}

func resolutionRank(r model.IssueResolution) int {
	switch r {
	case model.IssueFixed:
		return 0
	case model.IssueObsolete:
		return 1
	}
	return 2
}
