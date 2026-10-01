package cli

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/classify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

const mergedWindow = 24 * time.Hour

type listOptions struct {
	mine, review bool
	waitingOnMe  bool
	all          bool
	noTeams      bool
	noMerged     bool
	format       string
	maxAge       time.Duration
}

// sections returns the sections to show, in display order.
func (o listOptions) sections() []model.Section {
	both := o.mine == o.review
	var out []model.Section
	if both || o.mine {
		out = append(out, model.SectionMine)
	}
	if both || o.review {
		out = append(out, model.SectionReview)
		if !o.noTeams {
			out = append(out, model.SectionTeams)
		}
	}
	if (both || o.mine) && !o.noMerged {
		out = append(out, model.SectionMerged)
	}
	return out
}

// listing is what `list` shows.
type listing struct {
	viewer string
	prs    []model.PR
	// hidden counts the rows of each section that only --all shows.
	hidden map[model.Section]model.Hidden
}

// add applies --all and --waiting-on-me.
func (l *listing) add(pr model.PR, o listOptions) {
	switch {
	case o.waitingOnMe && pr.Next != model.NextMe:
	case pr.Hidden && !o.all:
		h := l.hidden[pr.Section]
		if pr.Draft {
			h.Drafts++
		} else {
			h.NotWaiting++
		}
		l.hidden[pr.Section] = h
	default:
		l.prs = append(l.prs, pr)
	}
}

// collect runs the searches, fetches the PRs of the requested sections and classifies them.
func collect(ctx context.Context, client github.Client, cfg config.Config, now time.Time, opts listOptions) (*listing, error) {
	search, err := github.SearchSections(ctx, client, cfg.Repo, now.Add(-mergedWindow))
	if err != nil {
		return nil, err
	}
	shown := opts.sections()

	// A PR goes to the first section it qualifies for: Mine, Review (personal request or
	// reviewed by me), then Team requests (requested from a team only).
	section := map[int]model.Section{}
	var order []int
	assign := func(s model.Section, numbers ...[]int) {
		for _, n := range slices.Concat(numbers...) {
			if _, ok := section[n]; !ok {
				section[n] = s
				order = append(order, n)
			}
		}
	}
	assign(model.SectionMine, search.Mine)
	assign(model.SectionReview, search.Personal, search.Reviewed)
	assign(model.SectionTeams, search.Requested)

	var numbers []int
	for _, n := range order {
		if slices.Contains(shown, section[n]) {
			numbers = append(numbers, n)
		}
	}
	raw, _, err := github.FetchPRs(ctx, client, cfg.Owner(), cfg.Name(), numbers)
	if err != nil {
		return nil, err
	}

	c := &classify.Classifier{Config: cfg, Viewer: search.Viewer, Now: now}
	l := &listing{viewer: search.Viewer, hidden: map[model.Section]model.Hidden{}}
	for _, n := range numbers {
		pr, ok := raw[n]
		if !ok {
			continue
		}
		if section[n] == model.SectionTeams && !requestsConfiguredTeam(pr, cfg.Teams) {
			continue
		}
		l.add(c.PR(pr, section[n]), opts)
	}
	if slices.Contains(shown, model.SectionMerged) {
		for i := range search.Merged {
			l.add(c.Merged(&search.Merged[i]), opts)
		}
	}
	return l, nil
}

// requestsConfiguredTeam: with `teams` configured, only requests to those teams count.
func requestsConfiguredTeam(pr *github.PullRequest, teams []string) bool {
	if len(teams) == 0 {
		return true
	}
	for _, rr := range pr.ReviewRequests.Nodes {
		if r := rr.RequestedReviewer; r != nil && r.Slug != "" && slices.ContainsFunc(teams, func(t string) bool { return strings.EqualFold(t, r.Slug) }) {
			return true
		}
	}
	return false
}
