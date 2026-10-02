package classify

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

func TestRunLifecycle(t *testing.T) {
	tests := []struct {
		name  string
		pr    *prBuilder
		want  model.RunState
		check func(t *testing.T, run model.Run)
	}{
		{
			name: "command without reaction is requested",
			pr:   newPR(me).says(me, "/dry-run", at(115)),
			want: model.RunRequested,
			check: func(t *testing.T, run model.Run) {
				if run.NoResponse {
					t.Error("5 minutes is within requestedTimeout")
				}
			},
		},
		{
			name: "no reaction and no reply after requestedTimeout",
			pr:   newPR(me).says(me, "/dry-run", at(105)),
			want: model.RunRequested,
			check: func(t *testing.T, run model.Run) {
				if !run.NoResponse {
					t.Error("want NoResponse after 15 minutes")
				}
			},
		},
		{
			name: "🚀 from the bot means accepted",
			pr:   newPR(me).says(me, "/dry-run", at(100), rocket),
			want: model.RunAccepted,
		},
		{
			name: "a 🚀 from a person doesn't count",
			pr: newPR(me).says(me, "/safe-merge", at(118), func(c *github.Comment) {
				c.Reactions.Nodes = append(c.Reactions.Nodes, github.Reaction{User: user("alice_user")})
			}),
			want: model.RunRequested,
		},
		{
			name: "gate comment without a result is running",
			pr:   newPR(me).says(me, "/dry-run", at(10), rocket).gate(model.DryRun, 100, "", at(12)),
			want: model.RunRunning,
			check: func(t *testing.T, run model.Run) {
				if run.BuildURL != "https://buildserver.labs.intellij.net/build/100" || !run.Started.Equal(at(12)) {
					t.Errorf("run = %+v", run)
				}
			},
		},
		{
			name: "edited gate comment with a result",
			pr:   newPR(me).says(me, "/dry-run", at(10), rocket).gate(model.DryRun, 100, "passed", at(12), edited(at(70))),
			want: model.RunPassed,
			check: func(t *testing.T, run model.Run) {
				if !run.Updated.Equal(at(70)) {
					t.Errorf("updated = %v, want the edit time", run.Updated)
				}
			},
		},
		{
			name: "Command rejected answers the pending command",
			pr:   newPR(me).says(me, "/safe-merge", at(10)).rejected("Missing code owners approval", at(11)),
			want: model.RunRejected,
			check: func(t *testing.T, run model.Run) {
				if run.Kind != model.SafeMerge || run.Reason != "Missing code owners approval" {
					t.Errorf("run = %+v", run)
				}
			},
		},
		{
			name: "/cancel-coordinator cancels the running build",
			pr: newPR(me).says(me, "/dry-run", at(10), rocket).gate(model.DryRun, 100, "", at(12)).
				says(me, "/cancel-coordinator", at(30), rocket),
			want: model.RunCancelled,
		},
		{
			name: "a gate run without a result before a newer run was superseded",
			pr: newPR(me).gate(model.DryRun, 100, "", at(12)).
				gate(model.DryRun, 101, "passed", at(40)),
			want: model.RunPassed,
			check: func(t *testing.T, run model.Run) {
				if run.BuildURL != "https://buildserver.labs.intellij.net/build/101" {
					t.Errorf("latest = %+v", run)
				}
			},
		},
		{
			name: "a run triggered before the last push is outdated",
			pr:   newPR(me).gate(model.DryRun, 100, "passed", at(-30)),
			want: model.RunPassed,
			check: func(t *testing.T, run model.Run) {
				if !run.Outdated {
					t.Error("want outdated")
				}
			},
		},
		{
			name: "a pending command isn't outdated",
			pr:   newPR(me).says(me, "/dry-run", at(-5), rocket),
			want: model.RunAccepted,
			check: func(t *testing.T, run model.Run) {
				if run.Outdated {
					t.Error("a command that hasn't started yet builds the current head")
				}
			},
		},
		{
			name: "a failure reply without the Command rejected prefix",
			pr:   newPR(me).says(me, "/dry-run --force", at(100)).botReply("Unrecognized parameters: force", at(101)),
			want: model.RunRejected,
			check: func(t *testing.T, run model.Run) {
				if run.Reason != "Unrecognized parameters: force" {
					t.Errorf("reason = %q", run.Reason)
				}
			},
		},
		{
			name: "a failure reply answers the command without 🚀",
			pr: newPR(me).says(me, "/dry-run", at(100), rocket).says(me, "/fixup", at(101)).
				botReply("Autosquash aborted: Conflict detected in file(s): compiler/A.kt. Please rebase manually.", at(102)),
			want: model.RunAccepted,
		},
		{
			name: "success replies of other commands answer nothing",
			pr: newPR(me).says(me, "/dry-run", at(100)).says(me, "/test-private", at(101), rocket).
				botReply("Private aggregate run triggered at https://buildserver.labs.intellij.net/build/7 — use this link to monitor results.", at(102)).
				rejected("Command cannot be used on release branches.", at(103)),
			want: model.RunRejected,
			check: func(t *testing.T, run model.Run) {
				if run.Reason != "Command cannot be used on release branches." {
					t.Errorf("reason = %q", run.Reason)
				}
			},
		},
		{
			name: "/review is for another bot and waits for nothing",
			pr: newPR(me).says(me, "/dry-run", at(100)).says(me, "/review", at(101)).
				rejected("A Coordinator build is already in progress for this pull request.", at(102)),
			want: model.RunRejected,
		},
		{
			name: "an unparsable command gets its own reply",
			pr: newPR(me).says(me, "/dry-run --retry", at(100), rocket).says(me, "/dry-runx", at(101)).
				botReply("Unable to parse the issued command.", at(102)),
			want: model.RunAccepted,
		},
		{
			name: "commands from accounts without write access are ignored",
			pr:   newPR(me).says("alice_user", "/dry-run", at(100), association("CONTRIBUTOR")),
			want: model.RunNone,
		},
		{
			name: "commands from collaborators count",
			pr:   newPR(me).says("alice_user", "/dry-run", at(118), association("COLLABORATOR")),
			want: model.RunRequested,
		},
		{
			name: "/cancel-coordinator cancels a queued build",
			pr:   newPR(me).says(me, "/dry-run", at(100), rocket).says(me, "/cancel-coordinator", at(105), rocket),
			want: model.RunCancelled,
		},
		{
			name: "a /cancel-coordinator the bot didn't dispatch cancels nothing",
			pr: newPR(me).says(me, "/dry-run", at(10), rocket).gate(model.DryRun, 100, "", at(12)).
				says(me, "/cancel-coordinator", at(30)).rejected("Pull request is not open.", at(31)),
			want: model.RunRunning,
		},
		{
			name: "a minimized gate comment without a result was superseded",
			pr:   newPR(me).gate(model.DryRun, 100, "", at(12), minimized),
			want: model.RunCancelled,
			check: func(t *testing.T, run model.Run) {
				if run.Reason != "superseded" {
					t.Errorf("reason = %q", run.Reason)
				}
			},
		},
		{
			name: "runs from an ultimate Merge-Request are ignored",
			pr: newPR(me).comment(user("KotlinBuild"),
				gateBody(model.DryRun, 100, "")+"\nThe quality gate was triggered by Safe-Merge of the [corresponding Merge-Request](https://jetbrains.team/p/ij/reviews/1/timeline) in ultimate.", at(10)),
			want: model.RunNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := tt.pr.mine()
			run := pr.DryRun
			if run.State == model.RunNone {
				run = pr.SafeMerge
			}
			if run.State != tt.want {
				t.Fatalf("state = %s, want %s (runs %+v)", run.State, tt.want, pr.Runs)
			}
			if tt.check != nil {
				tt.check(t, run)
			}
		})
	}
}

func TestRunsFromFixtures(t *testing.T) {
	c := fixtureClassifier(me)
	t.Run("90004: dry-runs, a rejection, a failed retry and the passed safe-merge", func(t *testing.T) {
		pr := c.Show(loadFixture(t, "90004").Repository.PullRequest)
		var states []model.RunState
		for _, r := range pr.Runs {
			states = append(states, model.RunState(string(r.Kind)+" "+string(r.State)))
		}
		want := []model.RunState{"safe-merge passed", "safe-merge failed", "safe-merge rejected", "dry-run passed", "dry-run passed"}
		if len(states) != len(want) {
			t.Fatalf("runs = %v, want %v", states, want)
		}
		for i := range want {
			if states[i] != want[i] {
				t.Fatalf("runs = %v, want %v", states, want)
			}
		}
		if pr.Runs[1].Reason != "retry 1/1" {
			t.Errorf("failed safe-merge reason = %q", pr.Runs[1].Reason)
		}
	})
	t.Run("90007: the rejected duplicate is the latest dry-run, the run it collided with stays in history", func(t *testing.T) {
		pr := c.Show(loadFixture(t, "90007").Repository.PullRequest)
		if pr.DryRun.State != model.RunRejected || !strings.HasPrefix(pr.DryRun.Reason, "A Coordinator build is already in progress") {
			t.Errorf("dry-run = %+v, want the rejection", pr.DryRun)
		}
		if len(pr.Runs) < 2 || pr.Runs[1].State != model.RunPassed {
			t.Errorf("history = %+v, want the passed run after the rejection", pr.Runs)
		}
	})
	t.Run("90008: cancelled, failed twice, then running", func(t *testing.T) {
		pr := c.Show(loadFixture(t, "90008").Repository.PullRequest)
		var states []model.RunState
		for _, r := range pr.Runs {
			states = append(states, r.State)
		}
		want := []model.RunState{model.RunRunning, model.RunFailed, model.RunFailed, model.RunCancelled}
		if !slices.Equal(states, want) {
			t.Errorf("runs = %v, want %v", states, want)
		}
	})
	t.Run("90001: the ultimate-triggered runs are skipped", func(t *testing.T) {
		pr := c.Show(loadFixture(t, "90001").Repository.PullRequest)
		for _, r := range pr.Runs {
			if r.BuildURL == "https://buildserver.labs.intellij.net/build/1072415381" {
				t.Errorf("external run included: %+v", r)
			}
		}
		if pr.DryRun.State != model.RunPassed {
			t.Errorf("dry-run = %s, want passed", pr.DryRun.State)
		}
	})
	t.Run("requestedTimeout comes from the config", func(t *testing.T) {
		c := testClassifier()
		c.Config.RequestedTimeout = config.Duration(time.Hour)
		pr := c.PR(newPR(me).says(me, "/dry-run", at(105)).build(), model.SectionMine)
		if pr.DryRun.NoResponse {
			t.Error("15 minutes is within a 1h timeout")
		}
	})
}

// A minimized command issues no run, and the bot's reply to it goes with it; a minimized
// rejection drops the rejected run. The bot's own minimizing of superseded gate comments
// is TestRunLifecycle's.
func TestMinimizedCommands(t *testing.T) {
	for _, tt := range []struct {
		name      string
		pr        *prBuilder
		dryRun    model.RunState
		safeMerge model.RunState
	}{
		{"a minimized command", newPR(me).says(me, "/safe-merge", at(100), minimized), model.RunNone, model.RunNone},
		{"the reply to a minimized command doesn't answer an earlier one",
			newPR(me).says(me, "/dry-run", at(100)).says(me, "/safe-merge", at(101), minimized).rejected("Missing code owners approval", at(102)),
			model.RunRequested, model.RunNone},
		{"a minimized reply to a minimized command",
			newPR(me).says(me, "/safe-merge", at(100), minimized).rejected("Missing code owners approval", at(102), minimized),
			model.RunNone, model.RunNone},
		{"a minimized rejection of a visible command",
			newPR(me).says(me, "/safe-merge", at(100)).rejected("Missing code owners approval", at(102), minimized),
			model.RunNone, model.RunNone},
		{"a visible rejection still counts", newPR(me).says(me, "/safe-merge", at(100)).rejected("Missing code owners approval", at(102)),
			model.RunNone, model.RunRejected},
		{"a minimized dispatched cancel cancels nothing",
			newPR(me).says(me, "/dry-run", at(10), rocket).gate(model.DryRun, 100, "", at(12)).says(me, "/cancel-coordinator", at(20), rocket, minimized),
			model.RunRunning, model.RunNone},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pr := tt.pr.mine()
			if pr.DryRun.State != tt.dryRun || pr.SafeMerge.State != tt.safeMerge {
				t.Errorf("dry-run %s, safe-merge %s; want %s, %s (runs %+v)", pr.DryRun.State, pr.SafeMerge.State, tt.dryRun, tt.safeMerge, pr.Runs)
			}
		})
	}
}
