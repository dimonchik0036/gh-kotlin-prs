package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// assignment is a PR of the fixtures' viewer touching four subsystems, three of them
// nobody was asked for; judy_user owns two of those.
func assignment() model.PR {
	return model.PR{Number: 90010, Author: "dimonchik0036", Section: model.SectionMine, LastPush: time.Unix(2000, 0),
		CodeOwners: model.CodeOwnersStatus{State: model.CodeOwnersMissing, Check: "FAILURE", Rules: []model.CodeOwnerRule{
			{Paths: []string{"/analysis/"}, Mark: model.MarkNoReview, Owners: []model.Owner{{Login: "bob_user"}, {Login: "dave_user"}, {Login: "dimonchik0036"}}},
			{Paths: []string{"/compiler/fir/", "/compiler/frontend.common.jvm/", "/core/descriptors.jvm/", "/core/deserialization.common.jvm/"},
				Mark: model.MarkApproved, Assignees: []model.Assignee{{Login: "frank_user", Final: true}},
				Owners: []model.Owner{{Login: "erin_user"}, {Login: "frank_user"}}},
			{Paths: []string{"/compiler/testData/codegen/asmLike/"}, Mark: model.MarkNoReview, Owners: []model.Owner{
				{Login: "judy_user"}, {Login: "kim_user", Unavailable: true}, {Login: "mia_user", Role: "QA"}}},
			{Paths: []string{"/core/descriptors.runtime/"}, Mark: model.MarkNoReview, Owners: []model.Owner{
				{Login: "nora_user"}, {Login: "judy_user"}}},
		}},
		Reviewers: []model.Reviewer{
			{Login: "dave_user", State: model.ReviewerCommented},
			{Login: "frank_user", State: model.ReviewerApproved, Final: true, CodeOwner: true},
		},
	}
}

// The tree opens the rows that need me, closes the rest; picking someone picks them in
// every row they own; the footer counts the rows covered.
func TestPickerModel(t *testing.T) {
	p := newPicker(assignment())
	var open []bool
	for _, r := range p.rows {
		open = append(open, r.open)
	}
	if !slices.Equal(open, []bool{true, false, true, true}) || len(p.picked) != 0 {
		t.Fatalf("open %v, picked %q", open, p.picked)
	}
	if n := len(p.items()); n != 4+2+3+2 {
		t.Errorf("%d items", n)
	}
	if p.covered() != 1 {
		t.Errorf("covered %d, want the approved row", p.covered())
	}
	// judy_user, the first candidate of asmLike: header 0, bob, dave, header 1, header 2, judy.
	p.cursor = 5
	if it := p.items()[p.cursor]; p.rows[it.row].Candidates[it.candidate].Login != "judy_user" {
		t.Fatalf("item %+v", it)
	}
	p.toggle()
	if !slices.Equal(p.pickedIn(2), []string{"judy_user"}) || !slices.Equal(p.pickedIn(3), []string{"judy_user"}) || p.covered() != 3 {
		t.Errorf("judy_user: %q, %q, covered %d", p.pickedIn(2), p.pickedIn(3), p.covered())
	}
	p.toggle()
	if len(p.picked) != 0 || p.covered() != 1 {
		t.Errorf("unpicked: %q, covered %d", p.picked, p.covered())
	}
	// Closing moves the cursor to the row's header; space on a header opens it.
	p.setOpen(false)
	if it := p.items()[p.cursor]; it != (pickerItem{row: 2, candidate: -1}) || p.rows[2].open {
		t.Errorf("closed: cursor on %+v, open %v", it, p.rows[2].open)
	}
	p.toggle()
	if !p.rows[2].open || len(p.picked) != 0 {
		t.Errorf("space on a header: open %v, picked %q", p.rows[2].open, p.picked)
	}
	p.cursor = 3 // the approved row's header
	p.setOpen(true)
	if !p.rows[1].open || len(p.items()) != 4+2+2+3+2 {
		t.Errorf("expanded the approved row: %d items", len(p.items()))
	}
	p.move(-100)
	p.move(1000)
	if p.cursor != len(p.items())-1 {
		t.Errorf("cursor %d", p.cursor)
	}
}

// A re-request starts with its default picks, and their rows open.
func TestPickerDefaults(t *testing.T) {
	pr := assignment()
	pr.CodeOwners.Rules[0].Mark, pr.CodeOwners.Rules[0].Assignees = model.MarkReRequest, []model.Assignee{{Login: "dave_user"}}
	pr.CodeOwners.Rules[3].Mark, pr.CodeOwners.Rules[3].Assignees = model.MarkChangesRequested, []model.Assignee{{Login: "nora_user"}}
	pr.Reviewers = append(pr.Reviewers, model.Reviewer{Login: "nora_user", State: model.ReviewerChangesRequested, At: time.Unix(1000, 0)})
	p := newPicker(pr)
	if !slices.Equal(p.picked, []string{"dave_user", "nora_user"}) || !p.rows[0].open || !p.rows[3].open {
		t.Errorf("picked %q, open %v %v", p.picked, p.rows[0].open, p.rows[3].open)
	}
}

func TestPickerScreen(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.m.picker = newPicker(assignment())
	h.m.picker.cursor = 5
	h.keys("space")
	h.contains(
		"Request review on #90010  ↑↓ move ∙ space pick ∙ ←→ collapse/expand ∙ enter send ∙ esc cancel",
		"▾ /analysis/                           unassigned",
		"    [ ] dave_user      commented",
		"▸ /compiler/fir/ +3                    ✓ frank_user",
		"▾ /compiler/testData/codegen/asmLike/  → judy_user",
		"    [x] judy_user      also /core/descriptors.runtime/",
		"    [ ] mia_user (QA)",
		"    [ ] kim_user ⏳",
		"▾ /core/descriptors.runtime/           → judy_user",
		"    [x] judy_user      also /compiler/testData/codegen/asmLike/",
		"  3 of 4 subsystems covered ∙ will request: judy_user",
	)
	h.lacks("erin_user", "dimonchik0036")
	// The tree scrolls with the cursor on a short screen.
	h.send(tea.WindowSizeMsg{Width: 120, Height: 8})
	h.keys("down", "down", "down", "down", "down", "down")
	h.contains("[x] judy_user      also /compiler/testData/codegen/asmLike/", "3 of 4 subsystems covered")
	h.lacks("/analysis/")
}

// A on #90006: pick an owner, send; the request goes out once, the PR shows them
// requested until a refresh, and a second request waits for it.
func TestRequestReview(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("A")
	h.contains("Request review on #90006", "▸ /analysis/", "requested: erin_user", "▸ /compiler/fir/ +1", "✓ trent_user",
		"2 of 2 subsystems covered ∙ nobody picked")
	h.keys("enter")
	if h.m.picker == nil || !strings.Contains(h.statusBar(), "not posted: nobody picked to request a review of #90006 from") {
		t.Errorf("enter with nothing picked: %q", h.statusBar())
	}
	h.keys("right", "down", "space", "enter")
	if want := []string{"#90006 review bob_user"}; !slices.Equal(h.posted, want) || h.m.picker != nil {
		t.Fatalf("posted %q", h.posted)
	}
	h.contains("requested a review of #90006 from bob_user")
	pr, _ := h.m.current()
	if i := slices.IndexFunc(pr.Reviewers, func(r model.Reviewer) bool { return r.Login == "bob_user" }); i < 0 || !pr.Reviewers[i].Requested {
		t.Errorf("bob_user isn't shown as requested: %+v", pr.Reviewers)
	}
	h.keys("A", "right", "down", "down", "space", "enter")
	if !strings.Contains(h.statusBar(), "not posted: a review of #90006 was just requested; the next refresh shows it") || len(h.posted) != 1 {
		t.Errorf("a second request: %q, posted %q", h.statusBar(), h.posted)
	}
	h.keys("esc")
	h.clock = h.clock.Add(refreshCooldown)
	h.keys("r")
	if pr, _ := h.m.current(); slices.ContainsFunc(pr.Reviewers, func(r model.Reviewer) bool { return r.Login == "bob_user" }) {
		t.Errorf("after the refresh, the fake has no request: %+v", pr.Reviewers)
	}
	h.keys("A", "esc")
	if h.m.picker != nil || !strings.Contains(h.statusBar(), "no review requested") {
		t.Errorf("esc: %q", h.statusBar())
	}
}

// A first assignment shown as requested: rule 2c no longer says it's my move.
func TestRequestReviewAssigns(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.m.posts[90006] = []post{{reviewers: []string{"carol_user"}, at: h.clock}}
	h.m.reclassify()
	pr, _ := h.m.current()
	if !slices.ContainsFunc(pr.Reviewers, func(r model.Reviewer) bool { return r.Login == "carol_user" && r.Requested }) ||
		!strings.Contains(strings.Join(pr.Texts(), "; "), "waiting: erin_user, carol_user") {
		t.Errorf("reasons %q, reviewers %+v", pr.Texts(), pr.Reviewers)
	}
}

func TestRequestReviewRefusals(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.postErr = errors.New("request a review of #90006 from bob_user: HTTP 422: reviews may only be requested from collaborators")
	h.keys("A", "right", "down", "space", "enter")
	if !strings.Contains(h.statusBar(), "✗ couldn't request a review of #90006 from bob_user: request a review") {
		t.Errorf("failed request: %q", h.statusBar())
	}
	h.keys("a", "tab", "A") // Review: #90001, someone else's
	if h.m.picker != nil || !strings.Contains(h.statusBar(), "not posted: #90001 isn't yours (by alice_user)") {
		t.Errorf("someone else's PR: %q", h.statusBar())
	}
	demo := newHarness(t, func(_ *harness, opts *Options) { opts.RequestReview = nil })
	demo.start()
	demo.keys("A", "right", "down", "space", "enter")
	if !strings.Contains(demo.statusBar(), "not posted: demo mode never posts") || len(demo.posted) != 0 {
		t.Errorf("demo mode: %q", demo.statusBar())
	}
}

// --pr N --post request-review opens the picker on the details after a live refresh.
func TestStartReview(t *testing.T) {
	h := newHarness(t, func(h *harness, opts *Options) {
		opts.Initial = h.cached(5 * time.Minute)
		opts.Start, opts.StartReview = 90006, true
	})
	cmd := h.m.Init()
	if h.m.picker != nil {
		t.Fatal("opened on cached data")
	}
	h.run(cmd)
	if h.m.picker == nil || h.m.picker.pr.Number != 90006 {
		t.Fatalf("no picker after the live refresh: %q", h.statusBar())
	}
	h.keys("esc")
	if h.m.screen != screenDetail {
		t.Errorf("screen %v after closing the picker", h.m.screen)
	}
}
