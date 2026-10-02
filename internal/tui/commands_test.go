package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
)

// The first row is #90006: mine, its dry-run failed, code owners missing.
func TestPostAfterConfirming(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("D")
	if got := h.statusBar(); got != "Post /dry-run to #90006 (KT-990004: Example change)? [y/N]" {
		t.Errorf("prompt %q", got)
	}
	if len(h.posted) != 0 {
		t.Fatal("posted before y")
	}
	h.keys("y")
	if want := []string{"#90006 /dry-run"}; !slices.Equal(h.posted, want) {
		t.Fatalf("posted %q, want %q", h.posted, want)
	}
	// The run shows as requested right away, and isn't posted twice.
	h.contains("posted /dry-run to #90006", "dry-run requested just now")
	if pr, _ := h.m.current(); pr.DryRun.State != model.RunRequested || pr.DryRun.CommentURL != "https://github.com/JetBrains/kotlin/pull/90006#issuecomment-1" {
		t.Errorf("after the post: %+v", pr.DryRun)
	}
	h.keys("R")
	if !strings.Contains(h.statusBar(), "not posted: a dry-run is already requested on #90006") || h.m.confirm != nil {
		t.Errorf("a second dry-run: %q", h.statusBar())
	}
	// A refresh that started after the post shows what GitHub has: the fake has no new comment.
	h.clock = h.clock.Add(refreshCooldown)
	h.keys("r")
	if pr, _ := h.m.current(); pr.DryRun.State != model.RunFailed {
		t.Errorf("after the refresh: %+v", pr.DryRun)
	}
}

func TestConfirmationCancels(t *testing.T) {
	for _, key := range []string{"n", "enter", "esc", "Y", "q"} {
		h := newHarness(t, nil)
		h.start()
		h.keys("F", key)
		if len(h.posted) != 0 || h.quit || h.m.confirm != nil || !strings.Contains(h.statusBar(), "not posted") {
			t.Errorf("%s after the prompt: posted %q, quit %v, status bar %q", key, h.posted, h.quit, h.statusBar())
		}
	}
}

// A command that can't be posted now says why instead of asking.
func TestRefusals(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	for key, want := range map[string]string{
		"M": "not posted: #90006 isn't approved: code owners missing",
		"C": "not posted: no dry-run or safe-merge is requested or running on #90006",
	} {
		h.keys(key)
		if h.m.confirm != nil || !strings.Contains(h.statusBar(), want) {
			t.Errorf("%s: status bar %q, want %q", key, h.statusBar(), want)
		}
	}
	h.keys("a", "tab") // Review: #90001, someone else's draft
	h.keys("D")
	if !strings.Contains(h.statusBar(), "not posted: #90001 isn't yours (by alice_user)") {
		t.Errorf("someone else's PR: %q", h.statusBar())
	}
	h.keys("x")
	if h.m.menu != nil || !strings.Contains(h.statusBar(), "nothing to post on #90001 now (not posted: #90001 isn't yours") {
		t.Errorf("the menu on someone else's PR: %q", h.statusBar())
	}
	if len(h.posted) != 0 {
		t.Errorf("posted %q", h.posted)
	}
}

// The menu lists only what can be posted now; a command's key or enter picks one.
func TestActionsMenu(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("x")
	h.contains("Post on #90006", "D   /dry-run ", "R   /dry-run --retry", "F   /fixup", "O   /codeowners")
	h.lacks("/safe-merge", "/cancel-coordinator")
	h.keys("F")
	if h.m.menu != nil || h.m.confirm == nil || h.m.confirm.cmd != actions.Fixup {
		t.Fatalf("F in the menu: %+v", h.m.confirm)
	}
	h.keys("y")
	h.keys("x")
	h.lacks("F   /fixup") // just posted
	h.keys("down", "enter")
	if h.m.confirm == nil || h.m.confirm.cmd != actions.DryRunRetry {
		t.Errorf("down and enter: %+v", h.m.confirm)
	}
	h.keys("esc")
	h.keys("x", "esc")
	if h.m.menu != nil || h.m.confirm != nil {
		t.Error("esc didn't close the menu")
	}
	if want := []string{"#90006 /fixup"}; !slices.Equal(h.posted, want) {
		t.Errorf("posted %q, want %q", h.posted, want)
	}
}

func TestPostFromTheDetails(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("j", "enter", "O")
	if !strings.HasPrefix(h.statusBar(), "Post /codeowners to #90007") {
		t.Errorf("prompt %q", h.statusBar())
	}
	h.keys("y")
	if want := []string{"#90007 /codeowners"}; !slices.Equal(h.posted, want) || h.m.screen != screenDetail {
		t.Errorf("posted %q, screen %v", h.posted, h.m.screen)
	}
}

func TestFailedPost(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.postErr = errors.New("post /dry-run to #90006: HTTP 502: Bad Gateway")
	h.keys("D", "y")
	if !strings.Contains(h.statusBar(), "✗ couldn't post /dry-run to #90006: post /dry-run to #90006: HTTP 502") {
		t.Errorf("status bar %q", h.statusBar())
	}
	if pr, _ := h.m.current(); pr.DryRun.State != model.RunFailed {
		t.Errorf("a failed post marked the run: %+v", pr.DryRun)
	}
}

// Without a way to post (and not in demo mode), nothing asks.
func TestNoPoster(t *testing.T) {
	h := newHarness(t, func(_ *harness, opts *Options) { opts.Post, opts.RequestReview = nil, nil })
	h.start()
	h.keys("D")
	if h.m.confirm != nil || !strings.Contains(h.statusBar(), "not posted: nothing to post with") {
		t.Errorf("status bar %q", h.statusBar())
	}
	h.keys("x")
	if h.m.menu != nil || !strings.Contains(h.statusBar(), "nothing to post with") {
		t.Errorf("menu: status bar %q", h.statusBar())
	}
}

// Demo mode pretends a send: nothing is called, the note says so, and the PR shows the
// command or the review as requested until the next refresh.
func TestDemoPretends(t *testing.T) {
	h := newHarness(t, func(h *harness, opts *Options) {
		opts.Demo = true
		opts.Post = func(context.Context, int, string) (string, error) { t.Error("demo mode posted"); return "", nil }
		opts.RequestReview = func(context.Context, int, []string) error { t.Error("demo mode requested a review"); return nil }
	})
	h.start()
	h.keys("D", "y")
	if pr, _ := h.m.current(); pr.DryRun.State != model.RunRequested || !strings.Contains(h.statusBar(), "demo: not sent: posted /dry-run to #90006") {
		t.Errorf("dry-run %+v, status bar %q", pr.DryRun, h.statusBar())
	}
	h.keys("A", "right", "down", "space", "enter", "y")
	pr, _ := h.m.current()
	if !slices.ContainsFunc(pr.Reviewers, func(r model.Reviewer) bool { return r.Login == "bob_user" && r.Requested }) ||
		!strings.Contains(h.statusBar(), "demo: not sent: requested a review of #90006 from bob_user") {
		t.Errorf("reviewers %+v, status bar %q", pr.Reviewers, h.statusBar())
	}
	h.clock = h.clock.Add(refreshCooldown)
	h.keys("r")
	if pr, _ := h.m.current(); pr.DryRun.State != model.RunFailed || slices.ContainsFunc(pr.Reviewers, func(r model.Reviewer) bool { return r.Login == "bob_user" }) {
		t.Errorf("after the refresh: %+v, %+v", pr.DryRun, pr.Reviewers)
	}
}

// My own post making a PR my move isn't news; other events still are.
func TestNoNotificationForOwnPosts(t *testing.T) {
	var ran [][]string
	h := newHarness(t, func(_ *harness, opts *Options) {
		cfg, err := config.Parse([]byte("notify: {terminal: none, command: [hook, '{title}']}\n"))
		if err != nil {
			t.Fatal(err)
		}
		opts.Notifier = notify.New(cfg.Notify, nil, func(_ context.Context, argv []string, _ []byte) error {
			ran = append(ran, argv)
			return nil
		})
	})
	h.m.diff = func(_, _ []model.PR) []notify.Event {
		return []notify.Event{{Kind: notify.MyMove, Number: 90006, Title: "#90006: your move"}, {Kind: notify.RunFailed, Number: 90007, Title: "#90007 dry-run failed"}}
	}
	h.start()
	h.keys("F", "y")
	h.clock = h.clock.Add(time.Minute)
	h.keys("r")
	if want := [][]string{{"hook", "#90007 dry-run failed"}}; len(ran) != 1 || !slices.Equal(ran[0], want[0]) {
		t.Errorf("notified %q, want %q", ran, want)
	}
	h.clock = h.clock.Add(time.Minute)
	h.keys("r")
	if len(ran) != 3 {
		t.Errorf("the next refresh, without a post: %q", ran)
	}
}

// Every command has its key action, bound by default to its capital letter.
func TestCommandKeys(t *testing.T) {
	defaults := config.DefaultKeys()
	for c, key := range map[actions.Command]string{
		actions.DryRun: "D", actions.DryRunRetry: "R", actions.SafeMerge: "M",
		actions.CancelCoordinator: "C", actions.Fixup: "F", actions.CodeOwners: "O",
	} {
		if got := defaults[c.Action]; !slices.Equal(got, []string{key}) {
			t.Errorf("%s: keys %q, want %q", c.Action, got, key)
		}
	}
	if got := defaults["actions"]; !slices.Equal(got, []string{"x"}) {
		t.Errorf("actions: keys %q", got)
	}
}

// A draft takes every command but /safe-merge.
func TestDraftCommands(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	draft := func(edit func(pr *model.PR)) {
		for i := range h.m.prs {
			if h.m.prs[i].Number == 90006 {
				h.m.prs[i].Draft = true
				edit(&h.m.prs[i])
			}
		}
		h.m.rebuild()
	}
	draft(func(*model.PR) {})
	h.keys("x")
	h.contains("Post on #90006", "D   /dry-run ", "R   /dry-run --retry", "F   /fixup", "O   /codeowners")
	h.lacks("/safe-merge", "/cancel-coordinator")
	h.keys("esc", "M")
	if h.m.confirm != nil || !strings.Contains(h.statusBar(), "not posted: #90006 is a draft") {
		t.Errorf("M on a draft: %q", h.statusBar())
	}
	draft(func(pr *model.PR) { pr.DryRun.State = model.RunRunning })
	h.keys("x")
	h.contains("C   /cancel-coordinator", "F   /fixup")
	h.lacks("/safe-merge", "D   /dry-run ")
	h.keys("C", "y")
	if want := []string{"#90006 /cancel-coordinator"}; !slices.Equal(h.posted, want) {
		t.Errorf("posted %q, want %q", h.posted, want)
	}
}

// --pr opens the PR's details at once; --post asks only after a live refresh.
func TestStartOnAPR(t *testing.T) {
	fixup := actions.Fixup
	h := newHarness(t, func(h *harness, opts *Options) {
		opts.Initial = h.cached(5 * time.Minute)
		opts.Start, opts.StartCommand = 90007, &fixup
	})
	cmd := h.m.Init()
	h.contains("#90007 KT-990001: Example change", "esc back")
	if h.m.confirm != nil {
		t.Fatal("asked on cached data")
	}
	h.run(cmd)
	if got := h.statusBar(); !strings.HasPrefix(got, "Post /fixup to #90007") {
		t.Fatalf("after the live refresh: %q", got)
	}
	h.keys("y")
	if want := []string{"#90007 /fixup"}; !slices.Equal(h.posted, want) {
		t.Errorf("posted %q", h.posted)
	}
	h.keys("esc")
	h.contains("Mine (3)")
}

func TestStartWithoutCache(t *testing.T) {
	retry := actions.DryRunRetry
	h := newHarness(t, func(_ *harness, opts *Options) { opts.Start, opts.StartCommand = 90006, &retry })
	h.start()
	h.contains("#90006 KT-990004: Example change")
	if got := h.statusBar(); !strings.HasPrefix(got, "Post /dry-run --retry to #90006") {
		t.Errorf("status bar %q", got)
	}
	h.keys("n")
	if len(h.posted) != 0 {
		t.Errorf("posted %q", h.posted)
	}
}

func TestStartRefusesOrMisses(t *testing.T) {
	merge := actions.SafeMerge
	h := newHarness(t, func(_ *harness, opts *Options) { opts.Start, opts.StartCommand = 90006, &merge })
	h.start()
	if h.m.confirm != nil || !strings.Contains(h.statusBar(), "not posted: #90006 isn't approved") {
		t.Errorf("an unavailable command: %q", h.statusBar())
	}
	h = newHarness(t, func(_ *harness, opts *Options) { opts.Start, opts.StartCommand = 12345, &merge })
	h.start()
	if h.m.screen != screenList || h.m.confirm != nil || !strings.Contains(h.statusBar(), "#12345 isn't in the list") {
		t.Errorf("a PR not in the list: screen %v, %q", h.m.screen, h.statusBar())
	}
	// Leaving the details before the live refresh drops the question.
	fixup := actions.Fixup
	h = newHarness(t, func(h *harness, opts *Options) {
		opts.Initial = h.cached(time.Minute)
		opts.Start, opts.StartCommand = 90006, &fixup
	})
	cmd := h.m.Init()
	h.keys("esc")
	h.run(cmd)
	if h.m.confirm != nil {
		t.Error("asked after leaving the details")
	}
}
