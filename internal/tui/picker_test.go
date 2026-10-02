package tui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
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
	if len(h.posted) != 0 || h.statusBar() != "Request a review of #90006 from bob_user? [y/N]" {
		t.Fatalf("enter didn't ask: posted %q, status bar %q", h.posted, h.statusBar())
	}
	// Anything but y goes back to the picks, the back action included.
	h.keys("n")
	if h.m.picker == nil || h.m.picker.confirming || !slices.Equal(h.m.picker.picked, []string{"bob_user"}) {
		t.Fatalf("n: picker %+v", h.m.picker)
	}
	h.keys("enter", "backspace")
	if h.m.picker == nil || len(h.posted) != 0 {
		t.Fatal("backspace at the question closed the picker or sent")
	}
	h.keys("enter", "y")
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
	h.keys("A", "backspace")
	if h.m.picker != nil {
		t.Error("backspace, the other back key, didn't close the picker")
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
	h.keys("A", "right", "down", "space", "enter", "y")
	if !strings.Contains(h.statusBar(), "✗ couldn't request a review of #90006 from bob_user: request a review") {
		t.Errorf("failed request: %q", h.statusBar())
	}
	h.keys("a", "tab", "A") // Review: #90001, someone else's
	if h.m.picker != nil || !strings.Contains(h.statusBar(), "not posted: #90001 isn't yours (by alice_user)") {
		t.Errorf("someone else's PR: %q", h.statusBar())
	}
	none := newHarness(t, func(_ *harness, opts *Options) { opts.RequestReview = nil })
	none.start()
	none.keys("A", "right", "down", "space", "enter")
	if !strings.Contains(none.statusBar(), "not posted: nothing to request reviews with") || len(none.posted) != 0 {
		t.Errorf("no way to request: %q", none.statusBar())
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

// docs/request-review.tape's keys on the fixtures plus the derived #90010, in demo mode:
// what the recording shows.
func TestRequestReviewRecording(t *testing.T) {
	h := newHarnessOn(t, "../../testdata/raw"+string(filepath.ListSeparator)+"../../testdata/demo", func(_ *harness, opts *Options) {
		opts.Demo, opts.Post, opts.RequestReview = true, nil, nil
	})
	h.start()
	if pr, _ := h.m.current(); pr.Number != 90010 || pr.Primary() != "re-request review from dave_user" {
		t.Fatalf("the first row is #%d: %q", pr.Number, pr.Primary())
	}
	if !strings.Contains(h.statusBar(), "A request review ∙ enter details") {
		t.Errorf("no review hint: %q", h.statusBar())
	}
	h.keys("A")
	h.contains("▾ /analysis/                           → dave_user", "    [x] dave_user          commented", "▸ /compiler/fir/ +3", "✓ trent_user",
		"▾ /compiler/testData/codegen/asmLike/  unassigned", "▾ /core/descriptors.runtime/           unassigned",
		"▾ /plugins/parcelize/                  unassigned", "2 of 5 subsystems covered ∙ will request: dave_user")
	// judy_user covers two rows; the third unassigned one stays open.
	for range 7 {
		h.keys("down")
	}
	h.keys("space")
	h.contains("4 of 5 subsystems covered ∙ will request: dave_user, judy_user", "▾ /plugins/parcelize/                  unassigned")
	if strings.Count(h.screen(), "→ judy_user") != 2 {
		t.Errorf("judy_user isn't picked in both rows:\n%s", h.screen())
	}
	h.keys("left")
	h.lacks("    [ ] laura_user")
	h.keys("right")
	h.contains("    [ ] laura_user")
	for range 10 {
		h.keys("down")
	}
	h.keys("space")
	h.contains("▾ /plugins/parcelize/                  → peggy_user", "5 of 5 subsystems covered ∙ will request: dave_user, judy_user, peggy_user")
	h.keys("enter")
	if got := h.statusBar(); got != "Request a review of #90010 from dave_user, judy_user, peggy_user? [y/N]" {
		t.Errorf("the question: %q", got)
	}
	h.keys("y")
	h.contains("demo: not sent: requested a review of #90010 from dave_user, judy_user, peggy_user")
	if pr, _ := h.m.current(); pr.Primary() != "waiting: dave_user, judy_user, peggy_user" {
		t.Errorf("after the request: %q", pr.Texts())
	}
	if strings.Contains(h.statusBar(), "A request review") {
		t.Errorf("the hint stays: %q", h.statusBar())
	}
	h.keys("enter")
	h.contains("#90010 KT-990011: Example change")
}

// The status bar hints the review key only on a PR with someone to re-request (2a) or a
// rule to assign (2c), naming which.
func TestReviewHint(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	if bar := h.statusBar(); strings.Contains(bar, "A ") || !strings.Contains(bar, "enter details") {
		t.Errorf("#90006 has neither: %q", bar)
	}
	i := slices.IndexFunc(h.m.prs, func(pr model.PR) bool { return pr.Number == h.m.selected })
	for reasons, want := range map[string]string{
		"re-request review from bob_user":                                 "A re-request ∙ enter details",
		"assign reviewers for /analysis/":                                 "A assign reviewers ∙ enter details",
		"re-request review from bob_user|assign reviewers for /analysis/": "A request review ∙ enter details",
		// While a run goes on, the reasons come after it: the hint still shows.
		"dry-run running 10m ago|assign reviewers for /analysis/": "A assign reviewers ∙ enter details",
	} {
		h.m.prs[i].Reasons = nil
		for _, r := range strings.Split(reasons, "|") {
			h.m.prs[i].Reasons = append(h.m.prs[i].Reasons, model.Reason{Text: r})
		}
		h.m.rebuild()
		if bar := h.statusBar(); !strings.Contains(bar, want) {
			t.Errorf("%q: status bar %q, want %q", reasons, bar, want)
		}
		h.keys("enter")
		if bar := h.statusBar(); !strings.Contains(bar, strings.Replace(want, "enter details", "esc back", 1)) {
			t.Errorf("%q in the details: status bar %q", reasons, bar)
		}
		h.keys("esc")
	}
}

// "request review…" ends the commands menu on my open PRs: its key or enter opens the
// picker; someone else's PR doesn't list it.
func TestMenuRequestReview(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("x")
	h.contains("O   /codeowners       re-run", "A   request review…   pick code owners to request a review from")
	h.keys("A")
	if h.m.menu != nil || h.m.picker == nil {
		t.Fatalf("A in the menu: menu %v, picker %v", h.m.menu != nil, h.m.picker != nil)
	}
	h.keys("esc", "x")
	for range h.m.menu.size() {
		h.keys("down")
	}
	h.keys("enter")
	if h.m.picker == nil {
		t.Error("enter on the last line didn't open the picker")
	}
	h.keys("esc", "a", "tab", "x") // Review: #90001, someone else's
	if h.m.menu != nil || strings.Contains(h.screen(), "request review…") {
		t.Errorf("someone else's PR: %q", h.statusBar())
	}
}

// The picker and its question follow the back action's keys, not a fixed esc.
func TestPickerBackRebound(t *testing.T) {
	h := newHarness(t, func(_ *harness, opts *Options) {
		cfg, err := config.Parse([]byte("keys: {back: Z}\n"))
		if err != nil {
			t.Fatal(err)
		}
		opts.Config = cfg
	})
	h.start()
	h.keys("A", "esc")
	if h.m.picker == nil {
		t.Fatal("esc closed the picker with back rebound to Z")
	}
	h.contains("Z cancel")
	h.keys("Z")
	if h.m.picker != nil {
		t.Error("Z, the back key, didn't close the picker")
	}
}

// A display name from the bot's table shows after the login.
func TestCandidateName(t *testing.T) {
	for _, tt := range []struct {
		c    actions.Candidate
		want string
	}{
		{actions.Candidate{Login: "judy_user"}, "judy_user"},
		{actions.Candidate{Login: "judy_user", Name: "Judy Doe", Role: "QA", Unavailable: true}, "judy_user (Judy Doe) (QA) ⏳"},
	} {
		if got := candidateName(tt.c); got != tt.want {
			t.Errorf("%+v: %q, want %q", tt.c, got, tt.want)
		}
	}
}
