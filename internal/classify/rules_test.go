package classify

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

type ruleCase struct {
	name string
	pr   *prBuilder
	next model.NextAction
	// reason must be the primary reason; extra reasons must appear anywhere in Reasons.
	reason string
	extra  []string
	// absent reasons must not appear (as substrings).
	absent []string
	hidden bool
}

func runRuleCases(t *testing.T, section model.Section, cases []ruleCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			pr := testClassifier().PR(tt.pr.build(), section)
			if pr.Next != tt.next {
				t.Errorf("next = %s, want %s (reasons %q)", pr.Next, tt.next, pr.Texts())
			}
			if tt.reason != "" && pr.Primary() != tt.reason {
				t.Errorf("primary = %q, want %q (all %q)", pr.Primary(), tt.reason, pr.Texts())
			}
			for _, r := range tt.extra {
				if !slices.Contains(pr.Texts(), r) {
					t.Errorf("reasons %q lack %q", pr.Texts(), r)
				}
			}
			for _, r := range tt.absent {
				for _, got := range pr.Texts() {
					if strings.Contains(got, r) {
						t.Errorf("reasons %q contain %q", pr.Texts(), r)
					}
				}
			}
			if pr.Hidden != tt.hidden {
				t.Errorf("hidden = %v, want %v", pr.Hidden, tt.hidden)
			}
		})
	}
}

// A PR of mine that is otherwise ready to merge: green code owners and one approval.
func readyPR() *prBuilder {
	return newPR(me).ownersGreen().reviewed("alice_user", "APPROVED", at(30))
}

func TestMineRule1RunFailed(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "failed dry-run",
			pr:   readyPR().says(me, "/dry-run", at(40), rocket).gate(model.DryRun, 100, "failed", at(42), edited(at(60))),
			next: model.NextMe, reason: "dry-run failed 1h ago",
			extra: []string{"ready to /safe-merge", "no fresh dry-run"},
		},
		{
			name: "a passed safe-merge that failed to merge",
			pr:   readyPR().says(me, "/safe-merge", at(90), rocket).gate(model.SafeMerge, 100, "merge failed", at(91), edited(at(110))),
			next: model.NextMe, reason: "safe-merge failed: rebase-merge failed: Pull Request has merge conflicts",
			extra: []string{"ready to /safe-merge", "no fresh dry-run"},
		},
		{
			name: "rejected safe-merge",
			pr:   readyPR().says(me, "/safe-merge", at(100)).rejected("GitHub has not yet finished checking for conflicts with the base branch.", at(101)),
			next: model.NextMe, reason: "safe-merge rejected: GitHub has not yet finished checking for conflicts with the base branch.",
		},
		{
			name: "no response to a command",
			pr:   readyPR().says(me, "/safe-merge", at(100)),
			next: model.NextMe, reason: "safe-merge requested 20m ago, no response",
		},
		{
			name: "an outdated failure doesn't count",
			pr:   readyPR().gate(model.DryRun, 100, "failed", at(-30)),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"failed"},
		},
		{
			name: "a rejection is the outcome of its command, even while another run is in progress",
			pr: readyPR().says(me, "/dry-run", at(40), rocket).gate(model.DryRun, 100, "", at(42)).
				says(me, "/safe-merge", at(50)).rejected("A Coordinator build is already in progress for this pull request.", at(51)),
			next: model.NextMe, reason: "safe-merge rejected: A Coordinator build is already in progress for this pull request.",
			extra: []string{"dry-run running 1h ago"},
		},
		{
			name: "a rejection stays after the build it collided with finished",
			pr: readyPR().says(me, "/dry-run", at(40), rocket).gate(model.DryRun, 100, "passed", at(42), edited(at(90))).
				says(me, "/dry-run", at(50)).rejected("A Coordinator build is already in progress for this pull request.", at(51)),
			next: model.NextMe, reason: "dry-run rejected: A Coordinator build is already in progress for this pull request.",
		},
		{
			name: "a rejection survives a push",
			pr:   readyPR().says(me, "/safe-merge", at(-20)).rejected("Pull request has conflicts with the base branch that must be resolved before merging.", at(-19)),
			next: model.NextMe, reason: "safe-merge rejected: Pull request has conflicts with the base branch that must be resolved before merging.",
		},
		{
			// Not minimized: only reviewers resolve the cause, so the move follows the other
			// rules; the run stays in the history.
			name: "rejected for missing code-owner approval",
			pr: newPR(me).ownersRed().request("bob_user").
				says(me, "/dry-run", at(10), rocket).gate(model.DryRun, 100, "passed", at(12)).
				says(me, "/safe-merge", at(61)).rejected("Missing code owners approval - verify the check 'Code Owners Approval' is green.", at(62)),
			next: model.NextReviewers, reason: "waiting: bob_user",
			absent: []string{"rejected"},
		},
		{
			name: "rejected for a fixup! commit",
			pr: readyPR().says(me, "/safe-merge", at(61)).
				rejected("Commit 'fixup!' is currently [not supported by GitHub](https://example.org) - please try `/fixup` command or `/safe-squash-merge`", at(62)),
			next: model.NextMe, reason: "safe-merge rejected: Commit 'fixup!' is currently [not supported by GitHub](https://example.org) - please try `/fixup` command or `/safe-squash-merge`",
		},
		{
			name: "rejected for an unknown reason",
			pr:   readyPR().says(me, "/safe-merge", at(61)).rejected("Something new.", at(62)),
			next: model.NextMe, reason: "safe-merge rejected: Something new.",
		},
		{
			name: "a newer command supersedes the rejection",
			pr: readyPR().says(me, "/safe-merge", at(10)).rejected("Missing code owners approval", at(11)).
				says(me, "/safe-merge", at(100), rocket),
			next: model.NextCI, reason: "safe-merge accepted 20m ago",
			absent: []string{"rejected"},
		},
		{
			name: "a later passed run replaces the failure",
			pr: readyPR().gate(model.DryRun, 100, "failed", at(10)).
				gate(model.DryRun, 101, "passed", at(50)),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"failed", "no fresh dry-run"},
		},
	})
}

func TestMineRule1aConflicts(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "conflicting",
			pr:   readyPR().mergeable("CONFLICTING"),
			next: model.NextMe, reason: "conflicts with master, rebase",
		},
		{
			name: "conflicting, base unknown",
			pr: func() *prBuilder {
				b := readyPR().mergeable("CONFLICTING")
				b.pr.BaseRefName = ""
				return b
			}(),
			next: model.NextMe, reason: "conflicts with the base branch, rebase",
		},
		{
			name: "mergeable",
			pr:   readyPR().mergeable("MERGEABLE"),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"conflicts"},
		},
		{
			name: "GitHub still checking",
			pr:   readyPR().mergeable("UNKNOWN"),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"conflicts"},
		},
		{
			name: "no mergeable field (older fixtures)",
			pr:   readyPR(),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"conflicts"},
		},
		{
			name: "outranks a running dry-run and requested changes",
			pr: readyPR().mergeable("CONFLICTING").reviewed("bob_user", "CHANGES_REQUESTED", at(35)).
				says(me, "/dry-run", at(100), rocket).gate(model.DryRun, 100, "", at(101)),
			next: model.NextMe, reason: "conflicts with master, rebase",
			extra: []string{"dry-run running 19m ago"},
		},
		{
			name: "a rejection for the conflicts says it once",
			pr: readyPR().mergeable("CONFLICTING").says(me, "/safe-merge", at(100)).
				rejected("Pull request has conflicts with the base branch that must be resolved before merging.", at(101)),
			next: model.NextMe, reason: "safe-merge rejected: Pull request has conflicts with the base branch that must be resolved before merging.",
			absent: []string{"conflicts with master"},
		},
		{
			name: "a failed merge for the conflicts says it once",
			pr: readyPR().mergeable("CONFLICTING").says(me, "/safe-merge", at(90), rocket).
				gate(model.SafeMerge, 100, "merge failed", at(91), edited(at(110))),
			next: model.NextMe, reason: "safe-merge failed: rebase-merge failed: Pull Request has merge conflicts",
			absent: []string{"conflicts with master"},
		},
		{
			name: "a rejection for another cause keeps both",
			pr: readyPR().mergeable("CONFLICTING").says(me, "/safe-merge", at(100)).
				rejected("GitHub has not yet finished checking for conflicts with the base branch.", at(101)),
			next: model.NextMe, reason: "safe-merge rejected: GitHub has not yet finished checking for conflicts with the base branch.",
			extra: []string{"conflicts with master, rebase"},
		},
	})
}

// A passed dry-run, then /cancel-coordinator, /safe-merge and the bot's rejection, all three
// minimized by the author: no rejected run, the reviewers' move.
func TestMineMinimizedRejection(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "minimized commands and their rejection",
			pr: newPR(me).ownersRed().request("bob_user").
				says(me, "/dry-run", at(10), rocket).gate(model.DryRun, 100, "passed", at(12)).
				says(me, "/cancel-coordinator", at(60), rocket, minimized).says(me, "/safe-merge", at(61), minimized).
				rejected("Missing code owners approval - verify the check 'Code Owners Approval' is green.", at(62), minimized),
			next: model.NextReviewers, reason: "waiting: bob_user",
			absent: []string{"rejected"},
		},
	})
}

// A rejection only reviewers resolve stays in the runs, just not as my move.
func TestReviewersRejectionStaysARun(t *testing.T) {
	pr := newPR(me).ownersRed().request("bob_user").says(me, "/safe-merge", at(61)).
		rejected("Missing code owners approval - verify the check 'Code Owners Approval' is green.", at(62)).mine()
	if pr.SafeMerge.State != model.RunRejected || len(pr.Runs) != 1 || pr.Next != model.NextReviewers {
		t.Errorf("safe-merge %+v, %d runs, next %s", pr.SafeMerge, len(pr.Runs), pr.Next)
	}
}

func TestMineRule2ChangesRequested(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "changes requested after the last push",
			pr:   newPR(me).ownersRed().reviewed("bob_user", "CHANGES_REQUESTED", at(30)),
			next: model.NextMe, reason: "changes requested by bob_user",
		},
		{
			name: "pushed after the changes were requested",
			pr:   newPR(me).ownersRed().reviewed("bob_user", "CHANGES_REQUESTED", at(-30)),
			next: model.NextReviewers, reason: "waiting: code owners",
			absent: []string{"changes requested"},
		},
		{
			name: "a later approval replaces the request",
			pr: newPR(me).ownersGreen().reviewed("bob_user", "CHANGES_REQUESTED", at(10)).
				reviewed("bob_user", "APPROVED", at(20)),
			next: model.NextMe, reason: "ready to /safe-merge",
		},
	})
}

// A pending review request to X made after X's activity hands that activity's move to X:
// I answered (often with a push), now it's their turn.
func TestMineReRequestHandsTheMove(t *testing.T) {
	// An unresolved thread from bob_user, a force-push, then a re-request still pending.
	thread := func() *prBuilder {
		return newPR(me).thread(false, false, by("bob_user", at(-120))).forcePush(at(50))
	}
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "a thread, a push, then a re-request",
			pr:   thread().request("bob_user").requestEvent("bob_user", at(60)),
			next: model.NextReviewers, reason: "waiting: bob_user",
			absent: []string{"unresolved thread", "new thread comment"},
		},
		{
			name: "the request is older than the thread's last comment",
			pr:   newPR(me).thread(false, false, by("bob_user", at(70))).request("bob_user").requestEvent("bob_user", at(60)),
			next: model.NextMe, reason: "1 unresolved thread from bob_user",
		},
		{
			name: "the request was removed",
			pr:   thread().requestEvent("bob_user", at(60)),
			next: model.NextMe, reason: "1 unresolved thread from bob_user",
		},
		{
			name: "someone else's re-request",
			pr:   thread().request("carol_user").requestEvent("carol_user", at(60)),
			next: model.NextMe, reason: "1 unresolved thread from bob_user",
		},
		{
			name: "a new comment, then a re-request",
			pr:   newPR(me).says("bob_user", "Is this ready?", at(30)).request("bob_user").requestEvent("bob_user", at(40)),
			next: model.NextReviewers, reason: "waiting: bob_user",
			absent: []string{"new comment"},
		},
		{
			name: "a new comment after the re-request",
			pr:   newPR(me).says("bob_user", "One more thing", at(50)).request("bob_user").requestEvent("bob_user", at(40)),
			next: model.NextMe, reason: "new comment from bob_user 1h ago",
		},
		{
			name: "changes requested, then a re-request without a push",
			pr: newPR(me).ownersRed().reviewed("bob_user", "CHANGES_REQUESTED", at(30)).
				request("bob_user").requestEvent("bob_user", at(40)),
			next: model.NextReviewers, reason: "waiting: bob_user",
			absent: []string{"changes requested"},
		},
		{
			name: "changes requested after the re-request",
			pr: newPR(me).ownersRed().reviewed("bob_user", "CHANGES_REQUESTED", at(50)).
				request("bob_user").requestEvent("bob_user", at(40)),
			next: model.NextMe, reason: "changes requested by bob_user",
		},
	})
	// The thread still shows in the details.
	if pr := thread().request("bob_user").requestEvent("bob_user", at(60)).mine(); pr.UnresolvedThreads != 1 || len(pr.Threads) != 1 {
		t.Errorf("threads %d, %+v", pr.UnresolvedThreads, pr.Threads)
	}
}

func TestMineRule2aReRequest(t *testing.T) {
	commented := ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"dave_user", "erin_user"}, mark: "🔄", assignees: []string{"dave_user"}}
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "🔄 code owner",
			pr:   newPR(me).ownersRed().owners(commented).reviewed("dave_user", "COMMENTED", at(20)),
			next: model.NextMe, reason: "re-request review from dave_user",
		},
		{
			name: "one reason for every 🔄 owner",
			pr: newPR(me).ownersRed().owners(commented,
				ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{"erin_user", "frank_user"}, mark: "🔄", assignees: []string{"erin_user", "dave_user"}}).
				reviewed("dave_user", "COMMENTED", at(20)).reviewed("erin_user", "COMMENTED", at(20)),
			next: model.NextMe, reason: "re-request review from dave_user, erin_user",
		},
		{
			name: "already re-requested",
			pr:   newPR(me).ownersRed().owners(commented).reviewed("dave_user", "COMMENTED", at(-20)).request("dave_user"),
			next: model.NextReviewers, reason: "waiting: dave_user",
			absent: []string{"re-request"},
		},
	})
}

func TestMineRule2cAssignReviewers(t *testing.T) {
	unassigned := ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{"alice_user", "carol_user"}, mark: "❌"}
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "nobody was asked for a rule",
			pr:   newPR(me).ownersRed().owners(unassigned),
			next: model.NextMe, reason: "assign reviewers for /analysis/",
			absent: []string{"waiting"},
		},
		{
			name: "one reason for every such rule",
			pr: newPR(me).ownersRed().owners(unassigned,
				ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"dave_user"}, mark: "❌", assignees: []string{"dave_user"}},
				ownerRow{path: "/core/descriptors.runtime/", team: "kotlin-libraries", members: []string{"erin_user ⏳", "frank_user (QA)"}, mark: "❌"}).
				request("dave_user"),
			next: model.NextMe, reason: "assign reviewers for /analysis/, /core/descriptors.runtime/",
			extra: []string{"waiting: dave_user"},
		},
		{
			name: "the table lags behind a request of an owner",
			pr:   newPR(me).ownersRed().owners(unassigned).request("carol_user"),
			next: model.NextReviewers, reason: "waiting: carol_user",
			absent: []string{"assign reviewers"},
		},
		{
			name: "a team of the rule is requested",
			pr:   newPR(me).ownersRed().owners(unassigned).requestTeam("kotlin-analysis-api"),
			next: model.NextReviewers, reason: "waiting: kotlin-analysis-api",
			absent: []string{"assign reviewers"},
		},
		{
			name: "only I own it",
			pr:   newPR(me).ownersRed().owners(ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{me}, mark: "❌"}),
			next: model.NextReviewers, reason: "waiting: code owners",
			absent: []string{"assign reviewers"},
		},
		{
			name: "a rule without owners",
			pr:   newPR(me).ownersRed().owners(ownerRow{path: "/plugins/parcelize/", mark: "❌"}),
			next: model.NextReviewers, reason: "waiting: code owners",
			absent: []string{"assign reviewers"},
		},
	})
}

// Reviewers usually come after CI: while a dry-run or safe-merge goes on, 2a and 2c leave
// the move to it (their reasons come after); once it passed, they're mine.
func TestMineReviewersAfterRuns(t *testing.T) {
	unassigned := ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{"alice_user", "carol_user"}, mark: "❌"}
	commented := ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"dave_user", "erin_user"}, mark: "🔄", assignees: []string{"dave_user"}}
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "unassigned, a dry-run running",
			pr:   newPR(me).ownersRed().owners(unassigned).says(me, "/dry-run", at(100), rocket).gate(model.DryRun, 100, "", at(110)),
			next: model.NextCI, reason: "dry-run running 10m ago",
			extra: []string{"assign reviewers for /analysis/"},
		},
		{
			name: "unassigned, the dry-run passed",
			pr:   newPR(me).ownersRed().owners(unassigned).says(me, "/dry-run", at(100), rocket).gate(model.DryRun, 100, "passed", at(110)),
			next: model.NextMe, reason: "assign reviewers for /analysis/",
		},
		{
			name: "🔄, a safe-merge requested",
			pr: newPR(me).ownersRed().owners(commented).reviewed("dave_user", "COMMENTED", at(20)).
				says(me, "/safe-merge", at(115)),
			next: model.NextCI, reason: "safe-merge requested 5m ago",
			extra: []string{"re-request review from dave_user"},
		},
		{
			name: "a failed run is rule 1's anyway",
			pr:   newPR(me).ownersRed().owners(unassigned).says(me, "/dry-run", at(100), rocket).gate(model.DryRun, 100, "failed", at(110)),
			next: model.NextMe, reason: "dry-run failed 10m ago",
			extra: []string{"assign reviewers for /analysis/"},
		},
	})
}

func TestMineRule2bOwnersUnavailable(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "every owner who hasn't reviewed is ⏳",
			pr: newPR(me).ownersRed().owners(ownerRow{
				path: "/analysis/", team: "kotlin-analysis-api",
				members: []string{"alice_user ⏳", me, "carol_user ⏳", "frank_user"}, mark: "❌",
			}).reviewed("frank_user", "COMMENTED", at(10)),
			next: model.NextMe, reason: "all owners for /analysis/ unavailable",
		},
		{
			name: "someone is available",
			pr: newPR(me).ownersRed().owners(ownerRow{
				path: "/analysis/", team: "kotlin-analysis-api",
				members: []string{"alice_user ⏳", "carol_user"}, mark: "❌",
			}),
			next: model.NextMe, reason: "assign reviewers for /analysis/",
			absent: []string{"unavailable"},
		},
	})
}

func TestMineRule3UnresolvedThreads(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "last comment from a reviewer",
			pr:   readyPR().thread(false, false, by("bob_user", at(-10))).thread(false, false, by("bob_user", at(-20)), by(me, at(-5)), by("bob_user", at(-1))),
			next: model.NextMe, reason: "2 unresolved threads from bob_user",
		},
		{
			name: "my reply is the last one",
			pr:   readyPR().thread(false, false, by("bob_user", at(-10)), by(me, at(-5))),
			next: model.NextMe, reason: "ready to /safe-merge",
		},
		{
			name: "resolved and outdated threads are ignored",
			pr:   readyPR().thread(true, false, by("bob_user", at(-10))).thread(false, true, by("bob_user", at(-10))),
			next: model.NextMe, reason: "ready to /safe-merge",
		},
	})
}

func TestMineRule4NewComment(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "comment after my last push",
			pr:   readyPR().says("bob_user", "Is this ready?", at(60)),
			next: model.NextMe, reason: "new comment from bob_user 1h ago",
		},
		{
			name: "I replied after it",
			pr:   readyPR().says("bob_user", "Is this ready?", at(60)).says(me, "Yes", at(70)),
			next: model.NextMe, reason: "ready to /safe-merge",
		},
		{
			name: "bots, commands and minimized comments don't count",
			pr: readyPR().comment(botActor("kodee-bot"), "Hi! It looks like there are no references to any YT issues", at(60)).
				says("bob_user", "/dry-run", at(61), rocket).gate(model.DryRun, 100, "passed", at(62)).
				says("alice_user", "nit", at(63), minimized),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"new comment"},
		},
		{
			name: "a commenting review counts",
			pr:   readyPR().reviewed("carol_user", "COMMENTED", at(60)),
			next: model.NextMe, reason: "new review from carol_user 1h ago",
		},
		{
			name: "a thread comment that rule 3 reports counts once",
			pr:   readyPR().thread(false, false, by("bob_user", at(60))),
			next: model.NextMe, reason: "1 unresolved thread from bob_user",
			absent: []string{"new thread comment"},
		},
		{
			name: "a review is represented by its thread comments",
			pr:   readyPR().reviewedWith("bob_user", "COMMENTED", at(61), 1).thread(false, false, by("bob_user", at(60))),
			next: model.NextMe, reason: "1 unresolved thread from bob_user",
			absent: []string{"new review", "new thread comment"},
		},
		{
			name: "a comment in a resolved thread is still new",
			pr:   readyPR().thread(true, false, by("bob_user", at(60))),
			next: model.NextMe, reason: "new thread comment from bob_user 1h ago",
		},
		{
			name: "my bot command after it is my activity",
			pr: readyPR().says("bob_user", "One test failed but it's unrelated", at(60)).
				says(me, "/dry-run", at(70), rocket).gate(model.DryRun, 100, "passed", at(80)),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"new comment"},
		},
		{
			name: "someone else's command isn't my activity",
			pr: readyPR().says("bob_user", "Is this ready?", at(60)).
				says("carol_user", "/dry-run", at(70), rocket).gate(model.DryRun, 100, "passed", at(80)),
			next: model.NextMe, reason: "new comment from bob_user 1h ago",
		},
		{
			name: "the commenter approved after the comment",
			pr:   readyPR().says("bob_user", "One nit, otherwise fine", at(60)).reviewed("bob_user", "APPROVED", at(61)),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"new comment"},
		},
		{
			name: "the reviewer approved after a commenting review",
			pr:   readyPR().reviewed("carol_user", "COMMENTED", at(60)).reviewed("carol_user", "APPROVED", at(61)),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"new review"},
		},
		{
			name: "the reviewer approved after a comment in a resolved thread",
			pr:   readyPR().thread(true, false, by("bob_user", at(60))).reviewed("bob_user", "APPROVED", at(61)),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"new thread comment"},
		},
		{
			name: "an approval doesn't hide a later comment",
			pr:   readyPR().reviewed("bob_user", "APPROVED", at(50)).says("bob_user", "One more thing", at(60)),
			next: model.NextMe, reason: "new comment from bob_user 1h ago",
		},
		{
			name: "someone else's approval doesn't hide it",
			pr:   readyPR().says("bob_user", "Is this ready?", at(60)).reviewed("carol_user", "APPROVED", at(61)),
			next: model.NextMe, reason: "new comment from bob_user 1h ago",
		},
		{
			name: "an approval doesn't hide an unresolved thread",
			pr:   readyPR().thread(false, false, by("bob_user", at(60))).reviewed("bob_user", "APPROVED", at(61)),
			next: model.NextMe, reason: "1 unresolved thread from bob_user",
		},
		{
			// "One test failed but it's unrelated", an approval 20s later, then my /dry-run,
			// still running.
			name: "a comment, its approval, then my running dry-run",
			pr: readyPR().says("bob_user", "One test failed but it's definitely unrelated", at(60)).
				reviewed("bob_user", "APPROVED", at(60).Add(20*time.Second)).
				says(me, "/dry-run", at(100), rocket).gate(model.DryRun, 100, "", at(101)),
			next: model.NextCI, reason: "dry-run running 19m ago",
			absent: []string{"new comment"},
		},
	})
}

func TestMineRule5RunInProgress(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "running",
			pr:   readyPR().says(me, "/safe-merge", at(100), rocket).gate(model.SafeMerge, 100, "", at(110)),
			next: model.NextCI, reason: "safe-merge running 10m ago",
		},
		{
			name: "running a retry",
			pr: readyPR().gate(model.DryRun, 100, "", at(110)).
				comment(user("KotlinBuild"), gateBody(model.DryRun, 101, "")+"\n---\n\nTriggered a [retry attempt](https://buildserver.labs.intellij.net/build/102) №1 out of 1.", at(111)),
			next: model.NextCI, reason: "dry-run running 9m ago (retry 1/1)",
		},
		{
			name: "accepted",
			pr:   readyPR().says(me, "/dry-run", at(100), rocket),
			next: model.NextCI, reason: "dry-run accepted 20m ago",
		},
		{
			name: "requested just now",
			pr:   readyPR().says(me, "/dry-run", at(118)),
			next: model.NextCI, reason: "dry-run requested 2m ago",
		},
	})
}

func TestMineRule6Ready(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "with a fresh green dry-run",
			pr:   readyPR().gate(model.DryRun, 100, "passed", at(10)),
			next: model.NextMe, reason: "ready to /safe-merge",
			absent: []string{"no fresh dry-run"},
		},
		{
			name: "no dry-run is only a hint",
			pr:   readyPR(),
			next: model.NextMe, reason: "ready to /safe-merge",
			extra: []string{"no fresh dry-run"},
		},
		{
			name: "an outdated dry-run is only a hint",
			pr:   readyPR().gate(model.DryRun, 100, "passed", at(-10)),
			next: model.NextMe, reason: "ready to /safe-merge",
			extra: []string{"no fresh dry-run"},
		},
		{
			name: "the check, not the table, decides",
			pr: readyPR().owners(ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{"alice_user"}, mark: "❌", assignees: []string{"alice_user"}}).
				request("grace_user"),
			next: model.NextMe, reason: "ready to /safe-merge",
			extra: []string{"waiting: grace_user"},
		},
		{
			name: "an approval is required",
			pr:   newPR(me).ownersGreen().request("alice_user"),
			next: model.NextReviewers, reason: "waiting: alice_user",
		},
	})
}

func TestMineRule7Waiting(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "pending user and team requests",
			pr:   newPR(me).ownersRed().request("alice_user", "bob_user").requestTeam("kotlin-analysis-api"),
			next: model.NextReviewers, reason: "waiting: alice_user, bob_user, kotlin-analysis-api",
		},
		{
			name: "missing code owners",
			pr: newPR(me).ownersRed().reviewed("carol_user", "APPROVED", at(10)).owners(
				ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{"carol_user"}, mark: "✅", assignees: []string{"carol_user 🔒"}},
				ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"bob_user"}, mark: "❌", assignees: []string{"bob_user"}},
				ownerRow{path: "/plugins/parcelize/", mark: "❌"},
			),
			next: model.NextReviewers, reason: "waiting: bob_user",
		},
		{
			name: "a failing check without details",
			pr:   newPR(me).ownersRed().reviewed("carol_user", "APPROVED", at(10)),
			next: model.NextReviewers, reason: "waiting: code owners",
		},
	})
}

func TestMineRule8Fallback(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "no reviewers at all",
			pr:   newPR(me),
			next: model.NextMe, reason: "no approvals and no pending requests",
		},
		{
			name: "approved, code owners unknown, no dry-run",
			pr:   newPR(me).reviewed("alice_user", "APPROVED", at(10)),
			next: model.NextMe, reason: "no dry-run yet",
		},
	})
}

func TestReviewRule1Requested(t *testing.T) {
	runRuleCases(t, model.SectionReview, []ruleCase{
		{
			name: "requested from me",
			pr:   newPR("alice_user").request(me).requestEvent(me, at(60)),
			next: model.NextMe, reason: "review requested 1h ago",
		},
		{
			name: "re-requested after my review",
			pr:   newPR("alice_user").reviewed(me, "CHANGES_REQUESTED", at(30)).request(me).requestEvent(me, at(60)),
			next: model.NextMe, reason: "review requested 1h ago",
		},
		{
			name: "request event older than my review",
			pr:   newPR("alice_user").request(me).requestEvent(me, at(20)).reviewed(me, "COMMENTED", at(30)),
			next: model.NextAuthor, reason: "commented 1h ago, not re-requested", hidden: true,
		},
		{
			name: "a team request isn't personal",
			pr:   newPR("alice_user").requestTeam("kotlin-analysis-api"),
			next: model.NextAuthor, reason: "not requested from you", hidden: true,
		},
	})
}

func TestReviewRule2Approved(t *testing.T) {
	runRuleCases(t, model.SectionReview, []ruleCase{
		{
			name: "nothing since my approval",
			pr:   newPR("alice_user").reviewed(me, "APPROVED", at(30)),
			next: model.NextDone, reason: "approved 1h ago", hidden: true,
		},
		{
			name: "pushes don't bring it back",
			pr:   newPR("alice_user").reviewed(me, "APPROVED", at(30)).forcePush(at(60)),
			next: model.NextDone, reason: "approved 1h ago; author pushed since", hidden: true,
		},
		{
			name: "thread replies don't bring it back",
			pr: newPR("alice_user").reviewed(me, "APPROVED", at(30)).
				thread(false, false, by(me, at(30)), by("carol_user", at(50))),
			next: model.NextDone, reason: "approved 1h ago", hidden: true,
		},
	})
}

func TestReviewRule3NotRequested(t *testing.T) {
	runRuleCases(t, model.SectionReview, []ruleCase{
		{
			name: "changes requested, nothing since",
			pr:   newPR("alice_user").reviewed(me, "CHANGES_REQUESTED", at(30)),
			next: model.NextAuthor, reason: "changes requested 1h ago, not re-requested", hidden: true,
		},
		{
			name: "author pushed after my changes request",
			pr:   newPR("alice_user").reviewed(me, "CHANGES_REQUESTED", at(-30)),
			next: model.NextAuthor, reason: "changes requested 2h ago, not re-requested; author pushed since", hidden: true,
		},
		{
			name: "author replied after my comments",
			pr:   newPR("alice_user").reviewed(me, "COMMENTED", at(30)).says("alice_user", "Done", at(40)),
			next: model.NextAuthor, reason: "commented 1h ago, not re-requested; author replied since", hidden: true,
		},
		{
			name: "author replied in my thread",
			pr: newPR("alice_user").reviewed(me, "COMMENTED", at(30)).
				thread(false, false, by(me, at(30)), by("alice_user", at(40))),
			next: model.NextAuthor, reason: "commented 1h ago, not re-requested; author replied since", hidden: true,
		},
		{
			name: "my approval was dismissed",
			pr:   newPR("alice_user").reviewed(me, "DISMISSED", at(-30)),
			next: model.NextAuthor, reason: "review dismissed, not re-requested; author pushed since", hidden: true,
		},
		{
			name: "the author's bot command isn't a reply",
			pr:   newPR("alice_user").reviewed(me, "COMMENTED", at(30)).says("alice_user", "/dry-run", at(40), rocket),
			next: model.NextAuthor, reason: "commented 1h ago, not re-requested", hidden: true,
			absent: []string{"author replied"},
		},
		{
			name: "a bot comment isn't the author's",
			pr:   newPR("alice_user").reviewed(me, "COMMENTED", at(30)).gate(model.DryRun, 100, "passed", at(40)),
			next: model.NextAuthor, reason: "commented 1h ago, not re-requested", hidden: true,
		},
	})
}

func TestReviewDraftsHidden(t *testing.T) {
	runRuleCases(t, model.SectionReview, []ruleCase{
		{
			name: "draft in Review",
			pr:   newPR("alice_user").draft().request(me),
			next: model.NextMe, reason: "review requested", hidden: true,
		},
	})
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "draft in Mine",
			pr:   newPR(me).draft(),
			next: model.NextMe, reason: "no approvals and no pending requests",
		},
	})
}

func TestTeamRequests(t *testing.T) {
	runRuleCases(t, model.SectionTeams, []ruleCase{
		{
			name: "requested from a team",
			pr:   newPR("alice_user").requestTeam("kotlin-analysis-api"),
			next: model.NextReviewers, reason: "team review requested: kotlin-analysis-api",
		},
	})
}

func TestReviewers(t *testing.T) {
	pr := newPR(me).ownersRed().
		request("alice_user").requestTeam("kotlin-analysis-api").
		reviewed("bob_user", "COMMENTED", at(10)).
		reviewed("carol_user", "APPROVED", at(20)).
		reviewed(me, "COMMENTED", at(25)).
		owners(
			ownerRow{path: "/analysis/", team: "kotlin-analysis-api", members: []string{"carol_user", "alice_user ⏳"}, mark: "✅", assignees: []string{"carol_user 🔒"}},
			ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"bob_user", "dave_user"}, mark: "🔄", assignees: []string{"bob_user"},
				names: map[string]string{"bob_user": "Bob User"}},
			ownerRow{path: "/compiler/ir/", team: "kotlin-backend", members: []string{"alice_user"}, mark: "❌", assignees: []string{"alice_user"},
				names: map[string]string{"alice_user": "Alice User"}},
		).mine()
	// Names come from the table, an owner's or an assignee's.
	want := []model.Reviewer{
		{Login: "alice_user", Name: "Alice User", State: model.ReviewerPending, Requested: true, CodeOwner: true, Unavailable: true},
		{Team: "kotlin-analysis-api", State: model.ReviewerPending, Requested: true},
		{Login: "bob_user", Name: "Bob User", State: model.ReviewerCommented, At: at(10), CodeOwner: true, ReRequest: true},
		{Login: "carol_user", State: model.ReviewerApproved, At: at(20), CodeOwner: true, Final: true},
	}
	if !slices.Equal(pr.Reviewers, want) {
		t.Errorf("reviewers:\n got %+v\nwant %+v", pr.Reviewers, want)
	}
	if pr.Approvals != 1 {
		t.Errorf("approvals = %d, want 1", pr.Approvals)
	}
}

func TestIssues(t *testing.T) {
	type want struct {
		id         string
		source     model.IssueSource
		resolution model.IssueResolution
	}
	tests := []struct {
		name          string
		branch, title string
		commits       []string
		want          []want
	}{
		{
			name: "branch then title", branch: "alice_user/KT-990001.some.change", title: "KT-990002: Change",
			want: []want{{"KT-990001", model.IssueBranch, ""}, {"KT-990002", model.IssueTitle, ""}},
		},
		{
			name: "lower case and other projects", branch: "rr/bob_user/kt-990003-lowercase", title: "[IDE] KTIJ-990001, KTI-990001",
			want: []want{{"KT-990003", model.IssueBranch, ""}, {"KTIJ-990001", model.IssueTitle, ""}, {"KTI-990001", model.IssueTitle, ""}},
		},
		{
			name: "trailers first: fixed, obsolete, related", branch: "carol_user/KT-990009.cleanup", title: "KT-990009: Change",
			commits: []string{
				"Clean up\n\n^KT-990005\n^KT-990004 Obsolete",
				"Fix it\n\n^KT-990001 Fixed\n^KT-990002 fixed",
			},
			want: []want{
				{"KT-990001", model.IssueTrailer, model.IssueFixed},
				{"KT-990002", model.IssueTrailer, model.IssueFixed},
				{"KT-990004", model.IssueTrailer, model.IssueObsolete},
				{"KT-990005", model.IssueTrailer, ""},
				{"KT-990009", model.IssueBranch, ""},
			},
		},
		{
			name: "a trailer's resolution wins, the branch doesn't add a duplicate", branch: "dave_user/KT-990001.fix", title: "KT-990001: Fix",
			commits: []string{"Prepare\n\n^KT-990001", "Fix\n\n^KT-990001 Fixed"},
			want:    []want{{"KT-990001", model.IssueTrailer, model.IssueFixed}},
		},
		{
			name: "trailers of every issue project", branch: "erin_user/KTI-990009-infra",
			commits: []string{
				"IDE side\n\n^KTIJ-990001 Fixed\n^kti-990002\n^KT-990003 Obsolete",
				"Follow-up\n\n^ktij-990004 fixed\n^KTI-990002 Obsolete",
			},
			want: []want{
				{"KTIJ-990001", model.IssueTrailer, model.IssueFixed},
				{"KTIJ-990004", model.IssueTrailer, model.IssueFixed},
				{"KTI-990002", model.IssueTrailer, model.IssueObsolete},
				{"KT-990003", model.IssueTrailer, model.IssueObsolete},
				{"KTI-990009", model.IssueBranch, ""},
			},
		},
		{
			name:    "only whole trailer lines count",
			commits: []string{"Mentions ^KT-990001 inline\n\nSee KT-990002.\n^KT-990003 is broken\n ^KT-990004\n^KT-990005 Verified"},
			want:    []want{{"KT-990005", model.IssueTrailer, ""}},
		},
		{name: "none", branch: "feature", title: "No issue"},
	}
	c := testClassifier()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newPR(me)
			b.pr.HeadRefName, b.pr.Title = tt.branch, tt.title
			for _, m := range tt.commits {
				b.commitMessage(m)
			}
			var got []want
			for _, issue := range c.PR(b.build(), model.SectionMine).Issues {
				got = append(got, want{issue.ID, issue.Source, issue.Resolution})
				if issue.URL != "https://youtrack.jetbrains.com/issue/"+issue.ID {
					t.Errorf("%s: url %q", issue.ID, issue.URL)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("issues:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestIssuesFollowConfig(t *testing.T) {
	c := testClassifier()
	c.Config.IssueProjects = []string{"ABC"}
	c.Config.IssueURL = "https://example.org/browse/{id}"
	b := newPR(me)
	b.pr.HeadRefName, b.pr.Title = "abc-7-change", "KT-990001: not this project"
	got := c.PR(b.build(), model.SectionMine).Issues
	if len(got) != 1 || got[0].ID != "ABC-7" || got[0].URL != "https://example.org/browse/ABC-7" {
		t.Errorf("issues = %+v", got)
	}
}

// Reasons link the page that shows them.
func TestReasonURLs(t *testing.T) {
	const pr = "https://github.com/JetBrains/kotlin/pull/1"
	tests := []struct {
		name string
		b    *prBuilder
		text string
		url  string
	}{
		{"failed run → build", readyPR().says(me, "/dry-run", at(40), rocket).gate(model.DryRun, 100, "failed", at(42), edited(at(60))),
			"dry-run failed 1h ago", "https://buildserver.labs.intellij.net/build/100"},
		{"rejection → the bot's reply", readyPR().says(me, "/safe-merge", at(100)).rejected("Pull request has conflicts with the base branch that must be resolved before merging.", at(101)),
			"safe-merge rejected: Pull request has conflicts with the base branch that must be resolved before merging.", pr + "#issuecomment-101"},
		{"no response → the command", readyPR().says(me, "/safe-merge", at(100)),
			"safe-merge requested 20m ago, no response", pr + "#issuecomment-100"},
		{"running → build", readyPR().says(me, "/dry-run", at(100), rocket).gate(model.DryRun, 100, "", at(110)),
			"dry-run running 10m ago", "https://buildserver.labs.intellij.net/build/100"},
		{"accepted → the command", readyPR().says(me, "/dry-run", at(100), rocket),
			"dry-run accepted 20m ago", pr + "#issuecomment-100"},
		{"changes requested → the review", newPR(me).ownersRed().reviewed("bob_user", "CHANGES_REQUESTED", at(30)),
			"changes requested by bob_user", pr + "#pullrequestreview-200"},
		{"re-request → the PR", newPR(me).ownersRed().owners(ownerRow{path: "/a/", team: "t", members: []string{"dave_user"}, mark: "🔄", assignees: []string{"dave_user"}}).reviewed("dave_user", "COMMENTED", at(-20)),
			"re-request review from dave_user", pr},
		{"threads → the newest one's last comment", readyPR().thread(false, false, by("bob_user", at(-10))).thread(false, false, by("bob_user", at(-20)), by(me, at(-5)), by("carol_user", at(-1))),
			"2 unresolved threads from bob_user, carol_user", pr + "#discussion_r312"},
		{"new comment → the comment", readyPR().says("bob_user", "Is this ready?", at(60)),
			"new comment from bob_user 1h ago", pr + "#issuecomment-100"},
		{"waiting → no link", newPR(me).ownersGreen().request("alice_user"), "waiting: alice_user", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.b.mine().Reasons
			i := slices.IndexFunc(got, func(r model.Reason) bool { return r.Text == tt.text })
			if i < 0 {
				t.Fatalf("no reason %q in %+v", tt.text, got)
			}
			if got[i].URL != tt.url {
				t.Errorf("%q links %q, want %q", tt.text, got[i].URL, tt.url)
			}
		})
	}
}
