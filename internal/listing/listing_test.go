package listing

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/demo"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

var allSections = []model.Section{model.SectionMine, model.SectionReview, model.SectionTeams, model.SectionMerged}

func fetch(t *testing.T, sections []model.Section) (*Data, time.Time) {
	t.Helper()
	fixtures, err := demo.Load("../../testdata/raw")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Fetch(context.Background(), fixtures, config.Default(), fixtures.Now(), sections)
	if err != nil {
		t.Fatal(err)
	}
	return d, fixtures.Now()
}

func bySection(prs []model.PR) map[model.Section][]int {
	out := map[model.Section][]int{}
	for _, pr := range prs {
		out[pr.Section] = append(out[pr.Section], pr.Number)
	}
	return out
}

// The same data classified later ages: reasons move on, merged PRs leave the window.
func TestClassifyWithLaterClock(t *testing.T) {
	d, now := fetch(t, allSections)
	cfg := config.Default()
	before := d.Classify(cfg, now)
	if got := bySection(before)[model.SectionMerged]; len(got) == 0 {
		t.Fatalf("no merged rows at the fixtures' clock: %v", bySection(before))
	}
	later := d.Classify(cfg, now.Add(2*24*time.Hour))
	if got := bySection(later)[model.SectionMerged]; len(got) != 0 {
		t.Errorf("merged rows two days later: %v", got)
	}
	changed := false
	for _, pr := range later {
		i := slices.IndexFunc(before, func(b model.PR) bool { return b.Number == pr.Number })
		changed = changed || (i >= 0 && before[i].Primary() != pr.Primary())
	}
	if !changed {
		t.Error("no reason changed with the clock")
	}
}

func TestFetchOnlyShownSections(t *testing.T) {
	d, now := fetch(t, []model.Section{model.SectionMine})
	for s, numbers := range bySection(d.Classify(config.Default(), now)) {
		if s != model.SectionMine {
			t.Errorf("%s rows %v without asking for them", s, numbers)
		}
	}
}

func TestFilter(t *testing.T) {
	prs := []model.PR{
		{Number: 1, Section: model.SectionMine, Next: model.NextMe},
		{Number: 2, Section: model.SectionReview, Next: model.NextAuthor, Hidden: true},
		{Number: 3, Section: model.SectionReview, Next: model.NextMe, Hidden: true, Draft: true},
		{Number: 4, Section: model.SectionReview, Next: model.NextMe},
	}
	numbers := func(prs []model.PR) []int {
		var out []int
		for _, pr := range prs {
			out = append(out, pr.Number)
		}
		return out
	}
	shown, hidden := Filter(prs, false, false)
	if !slices.Equal(numbers(shown), []int{1, 4}) || hidden[model.SectionReview] != (model.Hidden{NotWaiting: 1, Drafts: 1}) {
		t.Errorf("default: %v, hidden %v", numbers(shown), hidden)
	}
	if shown, hidden := Filter(prs, true, false); len(shown) != 4 || len(hidden) != 0 {
		t.Errorf("--all: %v, hidden %v", numbers(shown), hidden)
	}
	if shown, _ := Filter(prs, true, true); !slices.Equal(numbers(shown), []int{1, 3, 4}) {
		t.Errorf("--all --waiting-on-me: %v", numbers(shown))
	}
}

func TestShow(t *testing.T) {
	d, now := fetch(t, allSections)
	prs := d.Classify(config.Default(), now)
	for _, row := range prs {
		pr, ok := d.Show(config.Default(), now, row.Number)
		if row.Section == model.SectionMerged {
			if ok {
				t.Errorf("#%d: a detail from the merged search", row.Number)
			}
			continue
		}
		if !ok || pr.Number != row.Number || pr.Primary() != row.Primary() {
			t.Errorf("#%d: %v, %q, want %q", row.Number, ok, pr.Primary(), row.Primary())
		}
	}
}

// Cached rebuilds the last snapshot from the cache while it's young enough, falling back
// to the previous day's merged search after midnight UTC.
func TestCached(t *testing.T) {
	fixtures, err := demo.Load("../../testdata/raw")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	clock := time.Date(2026, 10, 1, 23, 50, 0, 0, time.UTC)
	c := &cache.Client{Next: fixtures, Dir: t.TempDir(), Account: "test", Now: func() time.Time { return clock }}
	ctx := context.Background()
	if _, ok := Cached(ctx, c, cfg, clock, allSections, 30*time.Minute); ok {
		t.Fatal("a snapshot from an empty cache")
	}
	if _, err := Fetch(ctx, c, cfg, clock, allSections); err != nil {
		t.Fatal(err)
	}
	fetchedAt := clock

	clock = fetchedAt.Add(10 * time.Minute) // 00:00 UTC: the merged search has a new key
	d, ok := Cached(ctx, c, cfg, clock, allSections, 30*time.Minute)
	if !ok || !d.FetchedAt.Equal(fetchedAt) || len(d.Classify(cfg, fixtures.Now())) == 0 {
		t.Fatalf("after midnight: %v, %+v", ok, d)
	}
	// Mine alone makes another details query: the details of every section stand in.
	mine, ok := Cached(ctx, c, cfg, clock, []model.Section{model.SectionMine}, 30*time.Minute)
	if !ok {
		t.Fatal("no snapshot for Mine")
	}
	if got := bySection(mine.Classify(cfg, fixtures.Now())); len(got) != 1 || len(got[model.SectionMine]) == 0 {
		t.Errorf("Mine from the details of every section: %v", got)
	}
	clock = fetchedAt.Add(31 * time.Minute)
	if _, ok := Cached(ctx, c, cfg, clock, allSections, 30*time.Minute); ok {
		t.Error("a snapshot older than maxAge")
	}
	if d, ok := Cached(ctx, c, cfg, clock, allSections, 0); !ok || !d.FetchedAt.Equal(fetchedAt) {
		t.Errorf("any age: %v", ok)
	}
}

// WithReviewRequests classifies as if the requests were in: a request of erin_user, the
// assignee of #90006's /analysis/, is there already, bob_user's is new; d isn't touched.
func TestWithReviewRequests(t *testing.T) {
	d, now := fetch(t, allSections)
	cfg := config.Default()
	requested := func(prs []model.PR) []string {
		i := slices.IndexFunc(prs, func(pr model.PR) bool { return pr.Number == 90006 })
		var out []string
		for _, r := range prs[i].Reviewers {
			if r.Requested {
				out = append(out, r.Login)
			}
		}
		return out
	}
	with := d.WithReviewRequests(map[int]ReviewRequests{90006: {Logins: []string{"Erin_User", "bob_user"}, At: now}, 1: {Logins: []string{"bob_user"}}})
	if got := requested(with.Classify(cfg, now)); !slices.Equal(got, []string{"erin_user", "bob_user"}) {
		t.Errorf("requested %q", got)
	}
	if got := requested(d.Classify(cfg, now)); !slices.Equal(got, []string{"erin_user"}) {
		t.Errorf("the data changed: %q", got)
	}
	// The request's time is in the timeline, for the rules that hand the move over.
	events := with.prs[90006].TimelineItems.Nodes
	if last := events[len(events)-1]; last.Typename != "ReviewRequestedEvent" || last.RequestedReviewer.Login != "bob_user" || !last.CreatedAt.Equal(now) {
		t.Errorf("timeline %+v", last)
	}
	if len(d.prs[90006].TimelineItems.Nodes) != len(events)-2 {
		t.Error("the data's timeline changed")
	}
	if d.WithReviewRequests(nil) != d {
		t.Error("a copy for nothing")
	}
}
