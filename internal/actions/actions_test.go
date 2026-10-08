package actions

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

const me = "alice_user"

// ready is an open PR of mine, approved, no run going on.
func ready() model.PR {
	return model.PR{Number: 7, Author: "Alice_User", Section: model.SectionMine, Approvals: 1,
		DryRun:     model.Run{Kind: model.DryRun, State: model.RunPassed},
		SafeMerge:  model.Run{Kind: model.SafeMerge, State: model.RunNone},
		CodeOwners: model.CodeOwnersStatus{State: model.CodeOwnersOK, Check: "SUCCESS"}}
}

func names(cs []Command) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name      string
		edit      func(pr *model.PR)
		available []string
		reason    string // of a refused /dry-run, or of /safe-merge when dry-run is available
	}{
		{"ready", func(*model.PR) {}, []string{"dry-run", "dry-run-retry", "safe-merge", "fixup", "test-public"}, ""},
		{"not mine", func(pr *model.PR) { pr.Author = "bob_user" }, nil, "#7 isn't yours (by bob_user)"},
		{"merged", func(pr *model.PR) { pr.MergedAt = time.Unix(1, 0) }, nil, "#7 is merged"},
		{"closed", func(pr *model.PR) { pr.Closed = true }, nil, "#7 is closed"},
		{"draft", func(pr *model.PR) { pr.Draft = true }, []string{"dry-run", "dry-run-retry", "fixup", "test-public"}, "#7 is a draft"},
		{"a draft with a dry-run running", func(pr *model.PR) { pr.Draft, pr.DryRun.State = true, model.RunRunning },
			[]string{"cancel-coordinator", "fixup", "test-public"}, "a dry-run is already running on #7"},
		{"a draft with code owners missing", func(pr *model.PR) {
			pr.Draft, pr.CodeOwners = true, model.CodeOwnersStatus{State: model.CodeOwnersMissing, Check: "FAILURE"}
		}, []string{"dry-run", "dry-run-retry", "fixup", "codeowners", "test-public"}, "#7 is a draft"},
		{"a dry-run running", func(pr *model.PR) { pr.DryRun.State = model.RunRunning },
			[]string{"cancel-coordinator", "fixup", "test-public"}, "a dry-run is already running on #7"},
		{"a safe-merge requested", func(pr *model.PR) { pr.SafeMerge.State = model.RunRequested },
			[]string{"cancel-coordinator", "fixup", "test-public"}, "a safe-merge is already requested on #7"},
		{"accepted", func(pr *model.PR) { pr.DryRun.State = model.RunAccepted },
			[]string{"cancel-coordinator", "fixup", "test-public"}, "a dry-run is already accepted on #7"},
		{"requested without a response", func(pr *model.PR) { pr.DryRun.State, pr.DryRun.NoResponse = model.RunRequested, true },
			[]string{"dry-run", "dry-run-retry", "safe-merge", "fixup", "test-public"}, ""},
		{"no approval", func(pr *model.PR) { pr.Approvals = 0 },
			[]string{"dry-run", "dry-run-retry", "fixup", "test-public"}, "#7 isn't approved: no approval yet"},
		{"code owners missing", func(pr *model.PR) {
			pr.CodeOwners = model.CodeOwnersStatus{State: model.CodeOwnersMissing, Check: "FAILURE"}
		},
			[]string{"dry-run", "dry-run-retry", "fixup", "codeowners", "test-public"}, "#7 isn't approved: code owners missing"},
		{"conflicts", func(pr *model.PR) { pr.Conflicts = true },
			[]string{"fixup", "test-public"}, "#7 has conflicts with the base branch; rebase first"},
		{"a release branch", func(pr *model.PR) { pr.Release, pr.Base = true, "2.5.0" },
			[]string{"fixup", "test-public"}, "#7 merges into the release branch 2.5.0, where the bot runs no dry-run"},
		{"no code-owners check", func(pr *model.PR) { pr.CodeOwners = model.CodeOwnersStatus{State: model.CodeOwnersUnknown} },
			[]string{"dry-run", "dry-run-retry", "fixup", "codeowners", "test-public"}, "#7 isn't approved: code owners missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := ready()
			tt.edit(&pr)
			if got := names(Available(pr, me)); !slices.Equal(got, tt.available) {
				t.Errorf("available %v, want %v", got, tt.available)
			}
			refused := DryRun
			if Check(DryRun, pr, me) == nil {
				refused = SafeMerge
			}
			err := Check(refused, pr, me)
			switch {
			case tt.reason == "" && err != nil && refused == SafeMerge:
				t.Errorf("safe-merge refused: %v", err)
			case tt.reason != "" && (err == nil || !errors.Is(err, ErrNotPosted) || !strings.HasSuffix(err.Error(), ": "+tt.reason)):
				t.Errorf("%s refused with %v, want %q", refused.Name, err, tt.reason)
			}
		})
	}
	if err := Check(CancelCoordinator, ready(), me); err == nil || !strings.Contains(err.Error(), "no dry-run or safe-merge is requested or running") {
		t.Errorf("cancel without a run: %v", err)
	}
	if err := Check(CodeOwners, ready(), me); err == nil || !strings.Contains(err.Error(), "already green") {
		t.Errorf("codeowners with a green check: %v", err)
	}
}

func TestFind(t *testing.T) {
	for _, c := range Commands {
		if got, ok := Find(c.Name); !ok || got != c {
			t.Errorf("Find(%q) = %+v, %v", c.Name, got, ok)
		}
		if !strings.HasPrefix(c.Text, "/") || strings.Contains(c.Text, "\n") {
			t.Errorf("%s posts %q", c.Name, c.Text)
		}
	}
	if _, ok := Find("safe-squash-merge"); ok {
		t.Error("an out-of-scope command")
	}
	if got := Names(); got != "dry-run, dry-run-retry, safe-merge, cancel-coordinator, fixup, codeowners, test-public" {
		t.Errorf("Names() = %s", got)
	}
}

func TestRequested(t *testing.T) {
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	pr := ready()
	pr.Next, pr.Reasons = model.NextMe, []model.Reason{{Text: "ready to /safe-merge"}}
	got := Requested(SafeMerge, pr, "https://example.org/pull/7#c9", at)
	if got.SafeMerge.State != model.RunRequested || !got.SafeMerge.Started.Equal(at) || got.SafeMerge.CommentURL != "https://example.org/pull/7#c9" ||
		got.Next != model.NextCI || got.Primary() != "safe-merge requested just now" || len(got.Runs) != 1 {
		t.Errorf("after /safe-merge: %+v", got)
	}
	if Check(SafeMerge, got, me) == nil || Check(CancelCoordinator, got, me) != nil {
		t.Error("the requested safe-merge doesn't count as running")
	}
	if pr.Primary() != "ready to /safe-merge" || len(pr.Runs) != 0 {
		t.Error("Requested changed its argument")
	}
	if fixed := Requested(Fixup, pr, "u", at); fixed.Next != pr.Next || len(fixed.Reasons) != 1 {
		t.Errorf("after /fixup: %+v", fixed)
	}
	if untracked := Requested(TestPublic, pr, "u", at); untracked.Next != pr.Next || len(untracked.Runs) != 0 {
		t.Errorf("after /test-public on master: %+v", untracked)
	}
	pr.Release = true
	tested := Requested(TestPublic, pr, "u", at)
	if tested.Next != model.NextCI || tested.Primary() != "test-public requested just now" || len(tested.Runs) != 1 ||
		tested.DryRun != pr.DryRun || tested.SafeMerge != pr.SafeMerge {
		t.Errorf("after /test-public: %+v", tested)
	}
	if Check(TestPublic, tested, me) == nil {
		t.Error("the requested test-public doesn't count as running")
	}
}

// /test-public only on a release branch, and not while it or the Aggregate runs.
func TestCheckTestPublic(t *testing.T) {
	release := func(edit func(pr *model.PR)) model.PR {
		pr := ready()
		pr.Release, pr.Base = true, "2.5.0"
		pr.Checks = []model.Check{{Name: "Aggregate (2.5.0) (2.5.0)", State: "FAILURE"}, {Name: "K2 All Projects (Internal) (K2 User Projects)", State: "PENDING"}}
		edit(&pr)
		return pr
	}
	for _, tt := range []struct {
		name   string
		pr     model.PR
		reason string
	}{
		// The public Aggregate reports nowhere: nothing to wait for, whatever the statuses say.
		{"master", func() model.PR {
			pr := ready()
			pr.Checks = []model.Check{{Name: "Aggregate (master) (Kotlin Dev)", State: "PENDING"}}
			return pr
		}(), ""},
		{"the Aggregate failed", release(func(*model.PR) {}), ""},
		{"the Aggregate runs", release(func(pr *model.PR) { pr.Checks[0].State = "PENDING" }), "Aggregate (2.5.0) (2.5.0) is already running on #7"},
		{"accepted", release(func(pr *model.PR) {
			pr.Runs = []model.Run{{Kind: model.TestPublic, State: model.RunAccepted}, {Kind: model.TestPublic, State: model.RunPassed}}
		}), "a test-public is already accepted on #7"},
		{"no response", release(func(pr *model.PR) {
			pr.Runs = []model.Run{{Kind: model.TestPublic, State: model.RunRequested, NoResponse: true}}
		}), ""},
		{"an older one passed", release(func(pr *model.PR) {
			pr.Runs = []model.Run{{Kind: model.DryRun, State: model.RunRejected}, {Kind: model.TestPublic, State: model.RunPassed}}
		}), ""},
		{"not mine", release(func(pr *model.PR) { pr.Author = "bob_user" }), "#7 isn't yours (by bob_user)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := Check(TestPublic, tt.pr, me)
			switch {
			case tt.reason == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tt.reason != "" && (err == nil || !strings.HasSuffix(err.Error(), ": "+tt.reason)):
				t.Errorf("err = %v, want %q", err, tt.reason)
			}
		})
	}
}
