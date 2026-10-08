package classify

import (
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

const (
	aggregate    = "Aggregate (2.5.0) (2.5.0)"
	userProjects = "K2 All Projects (External) (User Projects)"
)

// The code-owner rules of a release branch: bob_user's subsystem, and `*`, the release
// team's, whose approval the release engineer gives right before merging.
var firRow = ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"bob_user"}, mark: "✅", assignees: []string{"bob_user"}}

func releaseRow(mark string, members []string, assignees ...string) ownerRow {
	return ownerRow{path: "*", team: "kotlin-release", members: members, mark: mark, assignees: assignees}
}

var engineers = []string{"olivia_user", "grace_user"}

// A PR of mine into a release branch with all but the release team's approval: what's left
// once the Aggregate passes is the release engineer's.
func releasePR() *prBuilder {
	return newPR(me).release().ownersCheck("FAILURE", firRow, releaseRow("❌", engineers, "olivia_user")).
		request("olivia_user").reviewed("bob_user", "APPROVED", at(30))
}

// One the release engineer approved too, with its gates green.
func releaseReadyPR() *prBuilder {
	return newPR(me).release().ownersGreen().reviewed("alice_user", "APPROVED", at(30)).
		status(aggregate, "SUCCESS", at(60)).status(userProjects, "SUCCESS", at(70))
}

func TestReleaseRules(t *testing.T) {
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "the Aggregate passed: the release engineer's",
			pr:   releasePR().status(aggregate, "SUCCESS", at(60)),
			next: model.NextRelease, reason: "waiting for the release engineer: olivia_user",
			absent: []string{"waiting: ", "safe-merge", "dry-run"},
		},
		{
			name: "they approved, not merged yet",
			pr:   releaseReadyPR(),
			next: model.NextRelease, reason: "approved, waiting for the release engineer to merge",
		},
		{
			name: "the User Projects don't block",
			pr:   releasePR().status(aggregate, "SUCCESS", at(60)).status(userProjects, "FAILURE", at(70)).status("K2 All Projects (Internal) (K2 User Projects)", "PENDING", at(10)),
			next: model.NextRelease, reason: "waiting for the release engineer: olivia_user",
			extra: []string{"quality gate failed: " + userProjects + " (not blocking)", "quality gate running: K2 All Projects (Internal) (K2 User Projects) (not blocking)"},
		},
		{
			name: "a failed Aggregate is mine",
			pr:   releasePR().status(aggregate, "FAILURE", at(60)).status(userProjects, "FAILURE", at(70)),
			next: model.NextMe, reason: "quality gate failed: " + aggregate,
			extra:  []string{"quality gate failed: " + userProjects + " (not blocking)", "waiting: olivia_user"},
			absent: []string{"release engineer"},
		},
		{
			name: "an errored one too",
			pr:   releasePR().status(aggregate, "ERROR", at(60)),
			next: model.NextMe, reason: "quality gate failed: " + aggregate,
		},
		{
			name: "a comment after the failure answers it",
			pr:   releasePR().status(aggregate, "FAILURE", at(60)).says(me, "The failure is unrelated.", at(90)),
			next: model.NextRelease, reason: "waiting for the release engineer: olivia_user",
			extra: []string{"quality gate failed: " + aggregate + " (commented since)"},
		},
		{
			name: "a comment before it doesn't",
			pr:   releasePR().says(me, "Rebased.", at(50)).status(aggregate, "FAILURE", at(60)),
			next: model.NextMe, reason: "quality gate failed: " + aggregate,
		},
		{
			name: "the Aggregate running is CI's",
			pr:   releasePR().status(aggregate, "PENDING", at(10)).status(userProjects, "SUCCESS", at(70)),
			next: model.NextCI, reason: "quality gate running: " + aggregate,
			extra:  []string{"waiting: olivia_user"},
			absent: []string{"release engineer"},
		},
		{
			name: "no Aggregate yet after a push to a release-run branch",
			pr:   releasePR(),
			next: model.NextCI, reason: "the Aggregate hasn't started, pushed 2h ago",
		},
		{
			name: "only the User Projects reported",
			pr:   releasePR().status(userProjects, "PENDING", at(10)),
			next: model.NextCI, reason: "the Aggregate hasn't started, pushed 2h ago",
		},
		{
			name: "rrrn/ runs it too",
			pr:   releasePR().branch("rrrn/2.5.0/perf/fix"),
			next: model.NextCI, reason: "the Aggregate hasn't started, pushed 2h ago",
		},
		{
			name: "no gate will start on another branch",
			pr:   releasePR().branch(me + "/fix"),
			next: model.NextMe, reason: "no quality gates: the branch doesn't start with rrr/2.5.0/ or rrrn/2.5.0/",
		},
		{
			name: "gates started by hand on another branch count",
			pr:   releasePR().branch(me+"/fix").status(aggregate, "SUCCESS", at(60)),
			next: model.NextRelease, reason: "waiting for the release engineer: olivia_user",
		},
		{
			name: "assigning reviewers doesn't wait for the Aggregate",
			pr: newPR(me).release().status(aggregate, "PENDING", at(10)).ownersCheck("FAILURE",
				ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"bob_user"}, mark: "❌"}, releaseRow("❌", engineers, "olivia_user")).
				request("olivia_user"),
			next: model.NextMe, reason: "assign reviewers for /compiler/fir/",
			extra: []string{"quality gate running: " + aggregate},
		},
		{
			name: "the other code owners first",
			pr: newPR(me).release().status(aggregate, "SUCCESS", at(60)).ownersCheck("FAILURE",
				ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"bob_user"}, mark: "❌", assignees: []string{"bob_user"}}, releaseRow("❌", engineers, "olivia_user")).
				request("bob_user", "olivia_user").reviewed("carol_user", "APPROVED", at(30)),
			next: model.NextReviewers, reason: "waiting: bob_user, olivia_user",
		},
		{
			name: "changes requested",
			pr:   releasePR().status(aggregate, "SUCCESS", at(60)).reviewed("carol_user", "CHANGES_REQUESTED", at(40)),
			next: model.NextMe, reason: "changes requested by carol_user",
			absent: []string{"release engineer"},
		},
		{
			name: "conflicts",
			pr:   releasePR().status(aggregate, "SUCCESS", at(60)).mergeable("CONFLICTING"),
			next: model.NextMe, reason: "conflicts with 2.5.0, rebase",
			absent: []string{"release engineer"},
		},
		{
			name: "a rejected dry-run only informs",
			pr: releasePR().status(aggregate, "SUCCESS", at(60)).says(me, "/dry-run", at(100)).
				rejected("Command cannot be used on release branches.", at(101)),
			next: model.NextRelease, reason: "waiting for the release engineer: olivia_user",
			extra: []string{"dry-run rejected: Command cannot be used on release branches."},
		},
		{
			name: "no approval",
			pr:   newPR(me).release().ownersGreen().status(aggregate, "SUCCESS", at(60)),
			next: model.NextMe, reason: "no approvals and no pending requests",
			absent: []string{"dry-run"},
		},
	})
}

// A /test-public waits for the bot, then for the Aggregate, whose report ends it.
func TestReleaseTestPublic(t *testing.T) {
	failed := func() *prBuilder { return releasePR().status(aggregate, "FAILURE", at(60)) }
	runRuleCases(t, model.SectionMine, []ruleCase{
		{
			name: "requested after a failure: it answers it",
			pr:   failed().says(me, "/test-public", at(118)),
			next: model.NextCI, reason: "test-public requested 2m ago",
			extra:  []string{"quality gate failed: " + aggregate + " (/test-public since)"},
			absent: []string{"release engineer"},
		},
		{
			name: "accepted",
			pr:   failed().says(me, "/test-public", at(100), rocket),
			next: model.NextCI, reason: "test-public accepted 20m ago",
		},
		{
			name: "no response",
			pr:   failed().says(me, "/test-public", at(100)),
			next: model.NextMe, reason: "test-public requested 20m ago, no response",
		},
		{
			name: "rejected",
			pr:   failed().says(me, "/test-public", at(100)).botReply("Failed to process the command.", at(101)),
			next: model.NextMe, reason: "test-public rejected: Failed to process the command.",
		},
		{
			name: "the Aggregate runs again",
			pr:   releasePR().says(me, "/test-public", at(100), rocket).status(aggregate, "PENDING", at(105)),
			next: model.NextCI, reason: "quality gate running: " + aggregate,
			absent: []string{"test-public accepted"},
		},
		{
			name: "and fails again",
			pr:   releasePR().says(me, "/test-public", at(100), rocket).status(aggregate, "FAILURE", at(115)),
			next: model.NextMe, reason: "quality gate failed: " + aggregate,
		},
		{
			name: "or passes",
			pr:   releasePR().says(me, "/test-public", at(100), rocket).status(aggregate, "SUCCESS", at(115)),
			next: model.NextRelease, reason: "waiting for the release engineer: olivia_user",
		},
	})
}

// The run takes the Aggregate's state and build once it reports after the command.
func TestTestPublicRun(t *testing.T) {
	pr := newPR(me).release().says(me, "/test-public", at(100), rocket).status(aggregate, "FAILURE", at(115)).mine()
	run := latestOfKind(pr.Runs, model.TestPublic)
	if run.State != model.RunFailed || run.BuildURL == "" || !run.Updated.Equal(at(115)) || run.Outdated {
		t.Errorf("run = %+v", run)
	}
	pushed := newPR(me).release().says(me, "/test-public", at(-30), rocket).status(aggregate, "SUCCESS", at(-10)).mine()
	if run := latestOfKind(pushed.Runs, model.TestPublic); run.State != model.RunPassed || !run.Outdated {
		t.Errorf("before the push: %+v", run)
	}
	// On master, /test-public runs the public Aggregate, which reports nowhere the tool reads.
	if master := newPR(me).says(me, "/test-public", at(100), rocket).mine(); len(master.Runs) != 0 {
		t.Errorf("master runs = %+v", master.Runs)
	}
}

// The row's QG is the Aggregate, the gate that blocks, as the rules see it.
func TestQualityGate(t *testing.T) {
	approved := func() *prBuilder { return newPR(me).release().ownersGreen().reviewed("alice_user", "APPROVED", at(30)) }
	for _, tt := range []struct {
		name  string
		pr    *prBuilder
		state model.RunState
		url   string // the Aggregate's build: 500 + its index among the checks, the code owners' first
	}{
		{"none", approved(), model.RunNone, ""},
		{"only the User Projects", approved().status(userProjects, "FAILURE", at(50)), model.RunNone, ""},
		{"passed, whatever the User Projects say", approved().status(userProjects, "FAILURE", at(50)).status(aggregate, "SUCCESS", at(60)), model.RunPassed, "502"},
		{"running", approved().status(aggregate, "PENDING", at(60)), model.RunRunning, "501"},
		{"failed", approved().status(aggregate, "FAILURE", at(60)), model.RunFailed, "501"},
		{"failed and answered", approved().status(aggregate, "FAILURE", at(60)).says(me, "Unrelated.", at(80)), model.RunFailed, "501"},
		{"a test-public waiting", approved().status(aggregate, "FAILURE", at(60)).says(me, "/test-public", at(100), rocket), model.RunAccepted, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			qg := tt.pr.mine().QualityGate
			if qg.Kind != model.QualityGate || qg.State != tt.state || tt.url != "" && !strings.HasSuffix(qg.BuildURL, "/"+tt.url) {
				t.Errorf("QualityGate = %+v, want %s", qg, tt.state)
			}
		})
	}
	if master := readyPR().mine(); master.QualityGate != (model.Run{}) {
		t.Errorf("on master: %+v", master.QualityGate)
	}
}

// Only the configured release branches take these rules.
func TestReleaseBranchFollowsConfig(t *testing.T) {
	c := testClassifier()
	if pr := c.PR(releaseReadyPR().build(), model.SectionMine); !pr.Release || pr.Next != model.NextRelease {
		t.Errorf("release %v, next %s, want a release PR", pr.Release, pr.Next)
	}
	c.Config.ReleaseBranches = ""
	if pr := c.PR(releaseReadyPR().build(), model.SectionMine); pr.Release || pr.Primary() != "ready to /safe-merge" {
		t.Errorf("release %v, %q, want a master-like PR", pr.Release, pr.Primary())
	}
}

// A release engineer (me, in the release team owning `*`): their approval is the last one
// and they merge right after it, so the PR is theirs once what's left is theirs, requested
// or not; until then it waits, hidden, on CI, the author or the other reviewers.
func TestReleaseEngineer(t *testing.T) {
	mates := []string{me, "grace_user"}
	// alice_user's PR into a release branch, bob_user's subsystem approved, mine (`*`) requested from me.
	pr := func(fir ownerRow) *prBuilder {
		return newPR("alice_user").release().ownersCheck("FAILURE", fir, releaseRow("❌", mates, me)).request(me).reviewed("bob_user", "APPROVED", at(20))
	}
	ready := func() *prBuilder { return pr(firRow) }
	runRuleCases(t, model.SectionReview, []ruleCase{
		{
			name: "ready",
			pr:   ready().status(aggregate, "SUCCESS", at(60)),
			next: model.NextMe, reason: "ready to approve and merge",
			absent: []string{"review requested"},
		},
		{
			name: "approved, not merged yet",
			pr: newPR("alice_user").release().ownersCheck("SUCCESS", firRow, releaseRow("✅", mates, me)).
				reviewed("bob_user", "APPROVED", at(20)).reviewed(me, "APPROVED", at(80)).status(aggregate, "SUCCESS", at(60)),
			next: model.NextMe, reason: "approved, ready to merge",
		},
		{
			name: "the User Projects don't block",
			pr:   ready().status(aggregate, "SUCCESS", at(60)).status(userProjects, "FAILURE", at(70)),
			next: model.NextMe, reason: "ready to approve and merge",
			extra: []string{"quality gate failed: " + userProjects + " (not blocking)"},
		},
		{
			name: "requested while the Aggregate runs",
			pr:   ready().status(aggregate, "PENDING", at(10)),
			next: model.NextCI, reason: "quality gate running: " + aggregate, hidden: true,
		},
		{
			name: "a failed Aggregate the author hasn't answered",
			pr:   ready().status(aggregate, "FAILURE", at(60)),
			next: model.NextAuthor, reason: "quality gate failed: " + aggregate, hidden: true,
		},
		{
			name: "one the author answered",
			pr:   ready().status(aggregate, "FAILURE", at(60)).says("alice_user", "Unrelated to the change.", at(80)),
			next: model.NextMe, reason: "ready to approve and merge",
			extra: []string{"quality gate failed: " + aggregate + " (commented since)"},
		},
		{
			name: "my own comment doesn't answer it",
			pr:   ready().status(aggregate, "FAILURE", at(60)).says(me, "Is it related?", at(80)),
			next: model.NextAuthor, hidden: true,
		},
		{
			name: "the other code owners first",
			pr:   pr(ownerRow{path: "/compiler/fir/", team: "kotlin-frontend", members: []string{"bob_user"}, mark: "❌", assignees: []string{"carol_user"}}).status(aggregate, "SUCCESS", at(60)),
			next: model.NextReviewers, reason: "not ready to merge yet: waiting for the other reviewers", hidden: true,
		},
	})
	// Not in the release team: a request is a request, as on master.
	notMine := newPR("alice_user").release().ownersCheck("FAILURE", firRow, releaseRow("❌", engineers, "olivia_user")).
		request(me).status(aggregate, "PENDING", at(10))
	if got := testClassifier().PR(notMine.build(), model.SectionReview); got.Next != model.NextMe || got.Primary() != "review requested" {
		t.Errorf("not a release engineer: next %s, %q", got.Next, got.Texts())
	}
	// Without a release team, nobody is one.
	c := testClassifier()
	c.Config.ReleaseTeam = ""
	if got := c.PR(ready().status(aggregate, "PENDING", at(10)).build(), model.SectionReview); got.Primary() != "review requested" {
		t.Errorf("without a release team: next %s, %q", got.Next, got.Texts())
	}
}
