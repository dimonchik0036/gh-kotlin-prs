// Package listing gets what `list` and the TUI show: Fetch asks GitHub (through any
// github.Client, such as the cache), Classify turns the raw data into rows with a given
// clock, as often as the clock moves, and Filter applies --all and --waiting-on-me.
package listing

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/classify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// MergedWindow is how far back Recently merged goes.
const MergedWindow = 24 * time.Hour

// Data is one refresh, raw: the searches and the details of the PRs in the sections asked for.
type Data struct {
	Viewer string
	// RateLimit is the budget after the last request.
	RateLimit github.RateLimit
	// FetchedAt is when the data was fetched, set by the caller (the oldest cache entry
	// for cached data).
	FetchedAt time.Time

	// section maps each fetched PR to the first section it qualifies for, in search order.
	section map[int]model.Section
	order   []int
	prs     map[int]*github.PullRequest
	merged  []github.PullRequest
}

// Fetch runs the searches and fetches the PRs of the given sections: two requests, or
// more for over 40 PRs.
func Fetch(ctx context.Context, client github.Client, cfg config.Config, now time.Time, sections []model.Section) (*Data, error) {
	search, err := github.SearchSections(ctx, client, cfg.Repo, cfg.CherryPickBot, now.Add(-MergedWindow))
	if err != nil {
		return nil, err
	}
	d := &Data{Viewer: search.Viewer, RateLimit: search.RateLimit, section: map[int]model.Section{}}
	// Of the others' PRs that may be mine, those I answer for are: a bot's assigned to me, a
	// cherry-pick naming me with nobody assigned. Not a person's PR assigned to me, nor a
	// mention alone.
	mine := func(pr github.PullRequest) bool {
		owners, _ := classify.Owners(cfg, &pr)
		return slices.ContainsFunc(owners, func(o string) bool { return config.SameLogin(o, search.Viewer) })
	}
	var others []int
	for _, pr := range search.MaybeMine {
		if mine(pr) {
			others = append(others, pr.Number)
		}
	}

	// A PR goes to the first section it qualifies for: Mine, Review (personal request or
	// reviewed by me), then Team requests (requested from a team only).
	assign := func(s model.Section, numbers ...[]int) {
		for _, n := range slices.Concat(numbers...) {
			if _, ok := d.section[n]; !ok {
				d.section[n] = s
				d.order = append(d.order, n)
			}
		}
	}
	assign(model.SectionMine, search.Mine, others)
	assign(model.SectionReview, search.Personal, search.Reviewed)
	assign(model.SectionTeams, search.Requested)
	d.order = slices.DeleteFunc(d.order, func(n int) bool { return !slices.Contains(sections, d.section[n]) })

	prs, limit, err := github.FetchPRs(ctx, client, cfg.Owner(), cfg.Name(), d.order, others)
	if err != nil {
		return nil, err
	}
	d.prs = prs
	if len(d.order) > 0 {
		d.RateLimit = limit
	}
	if slices.Contains(sections, model.SectionMerged) {
		d.merged = slices.Concat(search.Merged, slices.DeleteFunc(search.MaybeMineMerged, func(pr github.PullRequest) bool { return !mine(pr) }))
	}
	return d, nil
}

// Cached is the newest snapshot in the cache with every part at most maxAge old, its
// FetchedAt the oldest part's; false when there's none. After midnight UTC the merged
// search has a new key, so it falls back to the previous day's. When the PRs of the
// search changed since their details were fetched, it takes the newest details there
// are: rows of PRs missing from them are left out until the refresh.
func Cached(ctx context.Context, c *cache.Client, cfg config.Config, now time.Time, sections []model.Section, maxAge time.Duration) (*Data, bool) {
	for _, at := range []time.Time{now, now.Add(-24 * time.Hour)} {
		offline := &cache.Offline{Cache: c, MaxAge: maxAge, Fallback: []string{"PullRequests"}}
		if d, err := Fetch(ctx, offline, cfg, at, sections); err == nil {
			d.FetchedAt = offline.Oldest
			return d, true
		}
	}
	return nil, false
}

// Classify builds the rows with the clock at now, the hidden ones included (PR.Hidden).
func (d *Data) Classify(cfg config.Config, now time.Time) []model.PR {
	c := &classify.Classifier{Config: cfg, Viewer: d.Viewer, Now: now}
	var out []model.PR
	for _, n := range d.order {
		pr, ok := d.prs[n]
		if !ok {
			continue
		}
		if d.section[n] == model.SectionTeams && !requestsConfiguredTeam(pr, cfg.Teams) {
			continue
		}
		out = append(out, c.PR(pr, d.section[n]))
	}
	since := now.Add(-MergedWindow)
	for i := range d.merged {
		if pr := &d.merged[i]; pr.MergedAt != nil && !pr.MergedAt.Before(since) {
			out = append(out, c.Merged(pr))
		}
	}
	return out
}

// ReviewRequests are review requests sent from the tool: who, and when.
type ReviewRequests struct {
	Logins []string
	At     time.Time
}

// WithReviewRequests is the data as GitHub has it right after review requests of the
// PRs went out, before a refresh fetches them: those people requested too, at the time
// they were sent. d stays as it is.
func (d *Data) WithReviewRequests(requests map[int]ReviewRequests) *Data {
	if len(requests) == 0 {
		return d
	}
	out := *d
	out.prs = maps.Clone(d.prs)
	for n, rq := range requests {
		raw, ok := d.prs[n]
		if !ok {
			continue
		}
		pr := *raw
		pr.ReviewRequests.Nodes = slices.Clone(raw.ReviewRequests.Nodes)
		pr.TimelineItems.Nodes = slices.Clone(raw.TimelineItems.Nodes)
		for _, login := range rq.Logins {
			if !slices.ContainsFunc(pr.ReviewRequests.Nodes, func(rr github.ReviewRequest) bool {
				return rr.RequestedReviewer != nil && strings.EqualFold(rr.RequestedReviewer.Login, login)
			}) {
				pr.ReviewRequests.Nodes = append(pr.ReviewRequests.Nodes, github.ReviewRequest{RequestedReviewer: &github.Reviewer{Typename: "User", Login: login}})
			}
			pr.TimelineItems.Nodes = append(pr.TimelineItems.Nodes, github.TimelineItem{
				Typename: "ReviewRequestedEvent", CreatedAt: rq.At, RequestedReviewer: &github.Reviewer{Typename: "User", Login: login},
			})
		}
		out.prs[n] = &pr
	}
	return &out
}

// Show builds the detail of a fetched PR, as `show` would; false when it wasn't fetched
// in full (the merged ones come from the search).
func (d *Data) Show(cfg config.Config, now time.Time, number int) (model.PR, bool) {
	raw, ok := d.prs[number]
	if !ok {
		return model.PR{}, false
	}
	c := &classify.Classifier{Config: cfg, Viewer: d.Viewer, Now: now}
	return c.PR(raw, d.section[number]), true
}

// Filter drops the rows only --all shows (counting them per section) unless all, and
// with waitingOnMe every row whose move isn't mine.
func Filter(prs []model.PR, all, waitingOnMe bool) ([]model.PR, map[model.Section]model.Hidden) {
	var out []model.PR
	hidden := map[model.Section]model.Hidden{}
	for _, pr := range prs {
		switch {
		case waitingOnMe && pr.Next != model.NextMe:
		case pr.Hidden && !all:
			h := hidden[pr.Section]
			if pr.Draft {
				h.Drafts++
			} else {
				h.NotWaiting++
			}
			hidden[pr.Section] = h
		default:
			out = append(out, pr)
		}
	}
	return out, hidden
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
