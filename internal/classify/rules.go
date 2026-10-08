package classify

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// rule is one "whose move" rule (SPEC §5). The first matching rule of a section sets
// Next; every matching rule adds its reasons. A rule that yields (afterRuns, while a run is
// requested or running) or is a hint doesn't set Next, and its reasons come after all the
// others.
type rule struct {
	name   string
	next   model.NextAction
	hidden bool
	match  func(f *facts) []model.Reason
	// afterRuns: reviewers usually come after CI, so while a dry-run or safe-merge goes on,
	// the rule leaves the move to the others (rule 5, CI, unless an earlier one is mine).
	afterRuns bool
	// hint: the rule only informs, it never sets Next.
	hint bool
}

func applyRules(f *facts, rules []rule) {
	var yielded []model.Reason
	for _, r := range rules {
		reasons := r.match(f)
		if len(reasons) == 0 {
			continue
		}
		if r.hint || r.afterRuns && len(mineRunInProgress(f)) > 0 {
			yielded = append(yielded, reasons...)
			continue
		}
		if f.pr.Next == "" {
			f.pr.Next = r.next
			f.pr.Hidden = r.hidden
		}
		f.pr.Reasons = append(f.pr.Reasons, reasons...)
	}
	f.pr.Reasons = append(f.pr.Reasons, yielded...)
	if f.pr.Draft && f.pr.Section != model.SectionMine {
		f.pr.Hidden = true
	}
}

var mineRules = []rule{
	{name: "run failed", next: model.NextMe, match: mineRunFailed},
	{name: "conflicts", next: model.NextMe, match: mineConflicts},
	{name: "changes requested", next: model.NextMe, match: mineChangesRequested},
	{name: "re-request", next: model.NextMe, match: mineReRequest, afterRuns: true},
	{name: "owners unavailable", next: model.NextMe, match: mineOwnersUnavailable},
	{name: "assign reviewers", next: model.NextMe, match: mineAssignReviewers, afterRuns: true},
	{name: "unresolved thread", next: model.NextMe, match: mineUnresolvedThreads},
	{name: "new comment", next: model.NextMe, match: mineNewComment},
	{name: "run in progress", next: model.NextCI, match: mineRunInProgress},
	{name: "ready", next: model.NextMe, match: mineReady},
	{name: "waiting for reviewers", next: model.NextReviewers, match: mineWaiting},
	{name: "fallback", next: model.NextMe, match: mineFallback},
}

// releaseRules are Mine's on a release branch: no coordinator there, so no dry-run or
// safe-merge; the quality gates are the head commit's status contexts, started by a push
// to a release-run branch, and the release engineer merges. The reviewers don't wait for
// CI: the gates start on every push and run for hours.
var releaseRules = []rule{
	{name: "Aggregate failed", next: model.NextMe, match: releaseAggregateFailed},
	{name: "quality gate hints", hint: true, match: releaseGateHints},
	{name: "test-public failed", next: model.NextMe, match: releaseTestPublicFailed},
	{name: "command failed", hint: true, match: releaseCommandFailed},
	{name: "conflicts", next: model.NextMe, match: mineConflicts},
	{name: "no quality gates", next: model.NextMe, match: releaseNoGates},
	{name: "changes requested", next: model.NextMe, match: mineChangesRequested},
	{name: "re-request", next: model.NextMe, match: mineReRequest},
	{name: "owners unavailable", next: model.NextMe, match: mineOwnersUnavailable},
	{name: "assign reviewers", next: model.NextMe, match: mineAssignReviewers},
	{name: "unresolved thread", next: model.NextMe, match: mineUnresolvedThreads},
	{name: "new comment", next: model.NextMe, match: mineNewComment},
	{name: "Aggregate running", next: model.NextCI, match: releaseAggregateRunning},
	{name: "ready for the release engineer", next: model.NextRelease, match: releaseReady},
	{name: "waiting for reviewers", next: model.NextReviewers, match: releaseWaiting},
	{name: "fallback", next: model.NextMe, match: releaseFallback},
}

// Review shows only what waits on me: a personal request I haven't answered. Anything
// else, whatever the author did since, is hidden until I'm re-requested (`--all` shows it).
var reviewRules = []rule{
	{name: "release: ready", next: model.NextMe, match: reviewReleaseReady},
	{name: "release: CI", next: model.NextCI, hidden: true, match: reviewReleaseCI},
	{name: "release: author", next: model.NextAuthor, hidden: true, match: reviewReleaseAuthor},
	{name: "release: reviewers", next: model.NextReviewers, hidden: true, match: reviewReleaseReviewers},
	{name: "release: hints", hint: true, match: reviewReleaseHints},
	{name: "review requested", next: model.NextMe, match: reviewRequested},
	{name: "approved", next: model.NextDone, hidden: true, match: reviewApproved},
	{name: "not requested", next: model.NextAuthor, hidden: true, match: reviewNotRequested},
}

var teamRules = []rule{
	{name: "team requested", next: model.NextReviewers, match: teamRequested},
}

// Mine.

// 1. The latest run failed and isn't outdated (an error after the gate, like a failed
// merge, fails it whatever the gate's result), a command was rejected for a cause of
// mine, or a command got no response. A rejection is the outcome of its command, so it
// stays until a newer command or run supersedes it, even if the build it collided with
// finished later. One only reviewers resolve (missing code-owner approval) isn't my move:
// the other rules say whose it is.
func mineRunFailed(f *facts) []model.Reason { return runFailed(f, latest(f.pr.Runs)) }

func runFailed(f *facts, run model.Run) []model.Reason {
	switch {
	case run.NoResponse:
		return linked(fmt.Sprintf("%s requested %s, no response", run.Kind, model.Ago(f.c.Now, run.Started)), run.CommentURL)
	case run.State == model.RunRejected && ReviewersToFix(run.Reason):
		return nil
	case run.State == model.RunRejected:
		return linked(fmt.Sprintf("%s rejected: %s", run.Kind, run.Reason), run.CommentURL)
	case run.Outdated:
		return nil
	case run.Error != "":
		return linked(fmt.Sprintf("%s failed: %s", run.Kind, run.Error), run.CommentURL)
	case run.State == model.RunFailed:
		reason := fmt.Sprintf("%s failed %s", run.Kind, model.Ago(f.c.Now, run.Updated))
		if run.Reason != "" {
			reason += " (" + run.Reason + ")"
		}
		return linked(reason, run.BuildURL)
	}
	return nil
}

// 1a. GitHub found conflicts with the base branch, unless rule 1 already says so: a run
// rejected or failed for them.
func mineConflicts(f *facts) []model.Reason {
	if !f.pr.Conflicts {
		return nil
	}
	if run := latest(f.pr.Runs); len(mineRunFailed(f)) > 0 && ForConflicts(cmp.Or(run.Error, run.Reason)) {
		return nil
	}
	return linked("conflicts with "+cmp.Or(f.pr.Base, "the base branch")+", rebase", f.pr.URL)
}

// 2. A reviewer's latest opinionated review is CHANGES_REQUESTED, newer than the last push
// and than a pending re-request of them.
func mineChangesRequested(f *facts) []model.Reason {
	var logins []string
	var url string // the first of those reviews
	for _, r := range f.raw.LatestOpinionatedReviews.Nodes {
		if login := r.Author.LoginOrEmpty(); f.changesRequestedSincePush(login) {
			if len(logins) == 0 {
				url = r.URL
			}
			logins = append(logins, login)
		}
	}
	if len(logins) == 0 {
		return nil
	}
	return linked("changes requested by "+strings.Join(logins, ", "), url)
}

// 2a. A code owner is marked 🔄 and isn't re-requested yet. While a run goes on, after it
// (afterRuns).
func mineReRequest(f *facts) []model.Reason {
	var logins []string
	for _, rule := range f.missingRules() {
		if rule.Mark != model.MarkReRequest {
			continue
		}
		for _, a := range rule.Assignees {
			if !f.requestedUsers[strings.ToLower(a.Login)] && !sameLogin(a.Login, f.author) {
				logins = appendUnique(logins, a.Login)
			}
		}
	}
	if len(logins) == 0 {
		return nil
	}
	return linked("re-request review from "+strings.Join(logins, ", "), f.pr.URL)
}

// 2b. A missing code-owner rule where every listed owner who hasn't reviewed is ⏳.
func mineOwnersUnavailable(f *facts) []model.Reason {
	var reasons []model.Reason
	for _, rule := range f.missingRules() {
		candidates := 0
		available := false
		for _, o := range rule.Owners {
			if sameLogin(o.Login, f.author) || f.reviewed(o.Login) {
				continue
			}
			candidates++
			available = available || !o.Unavailable
		}
		if candidates > 0 && !available {
			reasons = append(reasons, model.Reason{Text: "all owners for " + rulePath(rule) + " unavailable"})
		}
	}
	return reasons
}

// 2c (after a run going on, like 2a). A missing code-owner rule nobody was asked for: the
// bot shows UNASSIGNED, it has
// owners besides me, and none of them, nor a team of it, is requested now (the table
// lags behind a request).
func mineAssignReviewers(f *facts) []model.Reason {
	var paths []string
	for _, rule := range f.missingRules() {
		if len(rule.Assignees) > 0 || rule.Mark == model.MarkReRequest || !f.ownedByOthers(rule) || f.requested(rule) {
			continue
		}
		paths = appendUnique(paths, rulePath(rule))
	}
	if len(paths) == 0 {
		return nil
	}
	return linked("assign reviewers for "+strings.Join(paths, ", "), f.pr.URL)
}

// ownedByOthers: the rule has an owner who isn't the author.
func (f *facts) ownedByOthers(rule model.CodeOwnerRule) bool {
	return slices.ContainsFunc(rule.Owners, func(o model.Owner) bool { return !sameLogin(o.Login, f.author) })
}

// requested: an owner of the rule, or a team of it, is requested now.
func (f *facts) requested(rule model.CodeOwnerRule) bool {
	for _, o := range rule.Owners {
		if f.requestedUsers[strings.ToLower(o.Login)] {
			return true
		}
	}
	return slices.ContainsFunc(rule.Teams, func(team string) bool {
		return slices.ContainsFunc(f.requestedTeams, func(t string) bool { return strings.EqualFold(t, team) })
	})
}

// 3. An unresolved, non-outdated thread whose last comment isn't mine, and whose author
// wasn't re-requested since (still pending).
func mineUnresolvedThreads(f *facts) []model.Reason {
	var logins []string
	var newest github.ThreadComment // the last comment of the newest such thread
	for i, t := range f.raw.ReviewThreads.Nodes {
		if login, ok := f.awaitsMyReply(i); ok {
			logins = append(logins, login)
			if last := t.Comments.Nodes[len(t.Comments.Nodes)-1]; last.CreatedAt.After(newest.CreatedAt) {
				newest = last
			}
		}
	}
	if len(logins) == 0 {
		return nil
	}
	return linked(fmt.Sprintf("%s from %s", plural(len(logins), "unresolved thread"), strings.Join(unique(logins), ", ")), newest.URL)
}

// 4. A non-bot comment from someone else after my last activity. Comments in threads
// that rule 3 already reports don't count again, nor does a comment its author followed
// with an approval (their last word) or that I answered with a re-request of them.
func mineNewComment(f *facts) []model.Reason {
	since := f.myLastActivity()
	var latest event
	for _, e := range f.events {
		if e.thread >= 0 {
			if _, reported := f.awaitsMyReply(e.thread); reported {
				continue
			}
		}
		if f.approvedSince(e.login, e.at) || f.reRequestedSince(e.login, e.at) {
			continue
		}
		if !sameLogin(e.login, f.me) && e.at.After(since) && e.at.After(latest.at) {
			latest = e
		}
	}
	if latest.login == "" {
		return nil
	}
	return linked(fmt.Sprintf("new %s from %s %s", latest.what, latest.login, model.Ago(f.c.Now, latest.at)), latest.url)
}

// 5. A run is requested or running.
func mineRunInProgress(f *facts) []model.Reason {
	var reasons []model.Reason
	for _, run := range []model.Run{f.pr.SafeMerge, f.pr.DryRun} {
		if inProgress(run) {
			reason := fmt.Sprintf("%s %s %s", run.Kind, run.State, model.Ago(f.c.Now, run.Started))
			if run.Reason != "" {
				reason += " (" + run.Reason + ")"
			}
			reasons = append(reasons, model.Reason{Text: reason, URL: run.Link()})
		}
	}
	return reasons
}

// 6. Code owners green, at least one approval, nothing running: ready to /safe-merge.
// A missing or outdated green dry-run doesn't block it, but adds a hint.
func mineReady(f *facts) []model.Reason {
	if f.pr.CodeOwners.State != model.CodeOwnersOK || f.pr.Approvals == 0 || f.inProgress() {
		return nil
	}
	reasons := texts("ready to /safe-merge")
	if dr := f.pr.DryRun; dr.State != model.RunPassed || dr.Outdated {
		reasons = append(reasons, model.Reason{Text: "no fresh dry-run"})
	}
	return reasons
}

// 7. Pending review requests or missing code owners.
func mineWaiting(f *facts) []model.Reason {
	var names []string
	for _, rr := range f.raw.ReviewRequests.Nodes {
		switch r := rr.RequestedReviewer; {
		case r == nil:
		case r.Slug != "":
			names = appendUnique(names, r.Slug)
		case r.Login != "":
			names = appendUnique(names, r.Login)
		}
	}
	for _, rule := range f.missingRules() {
		if rule.Mark == model.MarkReRequest {
			continue // the move is mine, see 2a
		}
		// UNASSIGNED rules are mine (2c), or wait on a request listed above.
		for _, a := range rule.Assignees {
			if !f.changesRequestedSincePush(a.Login) { // the move is mine, see 2
				names = appendUnique(names, a.Login)
			}
		}
	}
	if len(names) == 0 && f.pr.CodeOwners.State == model.CodeOwnersMissing && mineAssignReviewers(f) == nil {
		names = append(names, "code owners")
	}
	if len(names) == 0 {
		return nil
	}
	return texts("waiting: " + strings.Join(names, ", "))
}

// 8. None of the above.
func mineFallback(f *facts) []model.Reason {
	if f.pr.Next != "" {
		return nil
	}
	switch {
	case f.pr.Approvals == 0:
		return texts("no approvals and no pending requests")
	case f.pr.DryRun.State == model.RunNone:
		return texts("no dry-run yet")
	case f.pr.CodeOwners.State == model.CodeOwnersUnknown:
		return texts("code owners unknown, try /codeowners")
	}
	return texts("check the PR")
}

// Mine on a release branch.

// R1. The release Aggregate failed, and I haven't answered (the author, on someone else's):
// a failure unrelated to the change happens there, and a comment saying so is the answer
// the release engineer needs; a /test-public reruns it. A push answers it too: the new
// head commit has its own gates. The other gates (the User Projects) don't block the
// merge: the release engineers merge with them red, nobody answering.
func releaseAggregateFailed(f *facts) []model.Reason {
	if agg, ok := f.aggregate(); ok && failedGate(agg.State) && f.answer(agg) == "" {
		return linked("quality gate failed: "+agg.Context, agg.TargetURL)
	}
	return nil
}

// R1, hints: the Aggregate's failure I answered, the other gates' failures, and the
// other gates still running.
func releaseGateHints(f *facts) []model.Reason {
	var reasons []model.Reason
	for _, g := range f.gates() {
		aggregate := model.IsReleaseAggregate(g.Context)
		switch answer := f.answer(g); {
		case failedGate(g.State) && aggregate && answer != "":
			reasons = append(reasons, linked("quality gate failed: "+g.Context+" ("+answer+")", g.TargetURL)...)
		case failedGate(g.State) && !aggregate:
			reasons = append(reasons, linked("quality gate failed: "+g.Context+" (not blocking)", g.TargetURL)...)
		case runningGate(g.State) && !aggregate:
			reasons = append(reasons, linked("quality gate running: "+g.Context+" (not blocking)", g.TargetURL)...)
		}
	}
	return reasons
}

// answer is how the author answered the gate's state since it got it: "commented since",
// "/test-public since" for the Aggregate, or "" when they didn't.
func (f *facts) answer(g github.CheckContext) string {
	switch {
	case f.commentedSince(g.CreatedAt):
		return "commented since"
	case model.IsReleaseAggregate(g.Context) && slices.ContainsFunc(f.pr.Runs, func(r model.Run) bool {
		return r.Kind == model.TestPublic && r.Started.After(g.CreatedAt)
	}):
		return "/test-public since"
	}
	return ""
}

// R1'. My latest /test-public got no response or was rejected.
func releaseTestPublicFailed(f *facts) []model.Reason {
	switch run := latestOfKind(f.pr.Runs, model.TestPublic); {
	case run.NoResponse:
		return linked(fmt.Sprintf("%s requested %s, no response", run.Kind, model.Ago(f.c.Now, run.Started)), run.CommentURL)
	case run.State == model.RunRejected:
		return linked(fmt.Sprintf("%s rejected: %s", run.Kind, run.Reason), run.CommentURL)
	}
	return nil
}

// R1”. A dry-run or safe-merge typed by hand: rejected, it only informs.
func releaseCommandFailed(f *facts) []model.Reason {
	coordinator := slices.DeleteFunc(slices.Clone(f.pr.Runs), func(r model.Run) bool { return r.Kind == model.TestPublic })
	return runFailed(f, latest(coordinator))
}

// R1b. No gate, and the branch isn't one whose pushes run them: none will start.
func releaseNoGates(f *facts) []model.Reason {
	if len(f.gates()) > 0 || f.c.Config.ReleaseRunBranch(f.pr.Branch, f.pr.Base) {
		return nil
	}
	prefixes := f.c.Config.ReleaseRunPrefixesOf(f.pr.Base)
	if len(prefixes) == 0 {
		return nil
	}
	return linked("no quality gates: the branch doesn't start with "+strings.Join(prefixes, " or "), f.pr.URL)
}

// R5. A /test-public waits for the Aggregate, the Aggregate runs, or it hasn't reported
// since the push yet.
func releaseAggregateRunning(f *facts) []model.Reason {
	var reasons []model.Reason
	if run, ok := f.testPublicWaiting(); ok {
		reasons = linked(fmt.Sprintf("%s %s %s", run.Kind, run.State, model.Ago(f.c.Now, run.Started)), run.CommentURL)
	}
	agg, ok := f.aggregate()
	switch {
	case ok && runningGate(agg.State):
		reasons = append(reasons, linked("quality gate running: "+agg.Context, agg.TargetURL)...)
	case !ok && (len(f.gates()) > 0 || f.c.Config.ReleaseRunBranch(f.pr.Branch, f.pr.Base)):
		reasons = append(reasons, texts("the Aggregate hasn't started, pushed "+model.Ago(f.c.Now, f.lastPush))...)
	}
	return reasons
}

// R6. What's left is the release engineer's (readyForRelease): they approve, as code
// owners of the release team's rule, and merge.
func releaseReady(f *facts) []model.Reason {
	if !f.readyForRelease() {
		return nil
	}
	if f.pr.CodeOwners.State == model.CodeOwnersOK {
		return texts("approved, waiting for the release engineer to merge")
	}
	if names := f.releaseEngineers(); len(names) > 0 {
		return texts("waiting for the release engineer: " + strings.Join(names, ", "))
	}
	return texts("waiting for the release engineer")
}

// R7. As on master, unless R6 says it: the release engineer is the reviewer left.
func releaseWaiting(f *facts) []model.Reason {
	if f.readyForRelease() {
		return nil
	}
	return mineWaiting(f)
}

// R8. None of the above.
func releaseFallback(f *facts) []model.Reason {
	if f.pr.Next != "" {
		return nil
	}
	switch {
	case f.pr.Approvals == 0:
		return texts("no approvals and no pending requests")
	case f.pr.CodeOwners.State == model.CodeOwnersUnknown:
		return texts("code owners unknown, try /codeowners")
	}
	return texts("check the PR")
}

// readyForRelease: a release branch's PR is the release engineer's to approve and merge:
// the Aggregate passed (or failed and the author answered) with no /test-public waiting,
// someone approved, no changes are requested since the push, no conflicts, and every
// code-owner rule is approved but the release team's (approving it is their part: the
// check stays red until they do).
func (f *facts) readyForRelease() bool {
	agg, ok := f.aggregate()
	passed := ok && (agg.State == "SUCCESS" || failedGate(agg.State) && f.answer(agg) != "")
	if _, waiting := f.testPublicWaiting(); !passed || waiting || f.pr.Approvals == 0 || f.pr.Conflicts {
		return false
	}
	for _, r := range f.raw.LatestOpinionatedReviews.Nodes {
		if f.changesRequestedSincePush(r.Author.LoginOrEmpty()) {
			return false
		}
	}
	switch f.pr.CodeOwners.State {
	case model.CodeOwnersOK:
		return true
	case model.CodeOwnersMissing:
		missing := f.pr.CodeOwners.Missing()
		return len(missing) > 0 && !slices.ContainsFunc(missing, func(r model.CodeOwnerRule) bool { return !f.releaseRule(r) })
	}
	return false
}

// releaseRule: the code-owner rule is the release team's.
func (f *facts) releaseRule(r model.CodeOwnerRule) bool {
	team := f.c.Config.ReleaseTeam
	return team != "" && slices.ContainsFunc(r.Teams, func(t string) bool { return strings.EqualFold(t, team) })
}

// releaseEngineers are who the release team's missing rules wait on: their assignees.
func (f *facts) releaseEngineers() []string {
	var names []string
	for _, r := range f.pr.CodeOwners.Missing() {
		if f.releaseRule(r) {
			for _, a := range r.Assignees {
				names = appendUnique(names, a.Login)
			}
		}
	}
	return names
}

// gates are a release branch's quality gates: the status contexts of the head commit.
func (f *facts) gates() []github.CheckContext {
	var out []github.CheckContext
	for _, c := range f.raw.Commits.Nodes {
		if c.Commit.StatusCheckRollup == nil {
			continue
		}
		for _, ctx := range c.Commit.StatusCheckRollup.Contexts.Nodes {
			if ctx.Typename == "StatusContext" {
				out = append(out, ctx)
			}
		}
	}
	return out
}

// qualityGate is the Aggregate as one run for the row, the gate that blocks: failed while
// that isn't answered, a test-public waiting, running, failed (answered), passed, or none
// before it reports.
func (f *facts) qualityGate() model.Run {
	agg, ok := f.aggregate()
	of := func(state model.RunState) model.Run {
		return model.Run{Kind: model.QualityGate, State: state, BuildURL: agg.TargetURL, Started: agg.CreatedAt, Updated: agg.CreatedAt}
	}
	if ok && failedGate(agg.State) && f.answer(agg) == "" {
		return of(model.RunFailed)
	}
	if run, waiting := f.testPublicWaiting(); waiting {
		run.Kind = model.QualityGate
		return run
	}
	switch {
	case !ok:
		return model.Run{Kind: model.QualityGate, State: model.RunNone}
	case runningGate(agg.State):
		return of(model.RunRunning)
	case failedGate(agg.State):
		return of(model.RunFailed)
	case agg.State == "SUCCESS":
		return of(model.RunPassed)
	}
	return model.Run{Kind: model.QualityGate, State: model.RunNone}
}

// aggregate is the release Aggregate among the gates.
func (f *facts) aggregate() (github.CheckContext, bool) {
	for _, g := range f.gates() {
		if model.IsReleaseAggregate(g.Context) {
			return g, true
		}
	}
	return github.CheckContext{}, false
}

// testPublicWaiting is my latest /test-public while it waits for the bot or the Aggregate;
// one with no response doesn't wait any more.
func (f *facts) testPublicWaiting() (model.Run, bool) {
	run := latestOfKind(f.pr.Runs, model.TestPublic)
	return run, (run.State == model.RunRequested || run.State == model.RunAccepted) && !run.NoResponse
}

func failedGate(state string) bool { return state == "FAILURE" || state == "ERROR" }

func runningGate(state string) bool { return state == "PENDING" || state == "EXPECTED" }

// commentedSince: the author (whoever answers for the PR; me on mine) commented, replied in
// a thread or reviewed after t.
func (f *facts) commentedSince(t time.Time) bool {
	return slices.ContainsFunc(f.events, func(e event) bool { return sameLogin(e.login, f.author) && e.at.After(t) })
}

// Review.

// 1. Requested from me personally, and no review of mine after the latest request. Not a
// release engineer's request: the R- rules above say when it's theirs.
func reviewRequested(f *facts) []model.Reason {
	if !f.requestedUsers[strings.ToLower(f.me)] || f.releaseEngineer() {
		return nil
	}
	requested := f.requestedAt[strings.ToLower(f.me)]
	if last := f.myLastReview(); !last.IsZero() && !requested.IsZero() && last.After(requested) {
		return nil
	}
	if requested.IsZero() {
		return texts("review requested")
	}
	return texts("review requested " + model.Ago(f.c.Now, requested))
}

// A release engineer's review (on a release branch, a member of the release team): theirs
// is the last approval, and they merge right after it. So it's their move only once what's
// left is theirs (readyForRelease), requested or not; until then it waits, hidden, on CI,
// the author or the other reviewers, whatever a request says.

// R-1. Ready: approve and merge; the tool doesn't merge, GitHub's button does.
func reviewReleaseReady(f *facts) []model.Reason {
	if !f.releaseEngineer() || !f.readyForRelease() {
		return nil
	}
	if f.myOpinion() == "APPROVED" {
		return linked("approved, ready to merge", f.pr.URL)
	}
	return linked("ready to approve and merge", f.pr.URL)
}

// R-2. Not ready, the Aggregate (or a /test-public) still running.
func reviewReleaseCI(f *facts) []model.Reason {
	if !f.releaseEngineer() || f.readyForRelease() {
		return nil
	}
	return releaseAggregateRunning(f)
}

// R-3. Not ready, the author's to fix: a failed Aggregate they didn't answer, conflicts,
// changes requested.
func reviewReleaseAuthor(f *facts) []model.Reason {
	if !f.releaseEngineer() || f.readyForRelease() {
		return nil
	}
	return slices.Concat(releaseAggregateFailed(f), mineConflicts(f), mineChangesRequested(f))
}

// R-4. Not ready otherwise: the other reviewers.
func reviewReleaseReviewers(f *facts) []model.Reason {
	if !f.releaseEngineer() || f.readyForRelease() || f.pr.Next != "" {
		return nil
	}
	return texts("not ready to merge yet: waiting for the other reviewers")
}

// The gates' hints, for the release engineer to judge.
func reviewReleaseHints(f *facts) []model.Reason {
	if !f.releaseEngineer() {
		return nil
	}
	return releaseGateHints(f)
}

// releaseEngineer: the PR is into a release branch and I'm in its code owners' release team.
func (f *facts) releaseEngineer() bool {
	if !f.pr.Release {
		return false
	}
	return slices.ContainsFunc(f.pr.CodeOwners.Rules, func(r model.CodeOwnerRule) bool {
		return f.releaseRule(r) && slices.ContainsFunc(r.Owners, func(o model.Owner) bool { return sameLogin(o.Login, f.me) })
	})
}

// 2. I approved: done until a re-request, whatever happened since.
func reviewApproved(f *facts) []model.Reason {
	if f.myOpinion() != "APPROVED" {
		return nil
	}
	return texts(withActivity(f, "approved "+model.Ago(f.c.Now, f.myLastReview())))
}

// 3. Anything else waits for a re-request: replies and pushes arrive by email anyway.
func reviewNotRequested(f *facts) []model.Reason {
	if f.pr.Next != "" {
		return nil
	}
	var reason string
	switch f.myOpinion() {
	case "CHANGES_REQUESTED":
		reason = "changes requested " + model.Ago(f.c.Now, f.myLastReview())
	case "COMMENTED":
		reason = "commented " + model.Ago(f.c.Now, f.myLastReview())
	case "DISMISSED":
		reason = "review dismissed"
	default:
		return texts("not requested from you")
	}
	return texts(withActivity(f, reason+", not re-requested"))
}

// withActivity notes, for information only, that the author pushed or replied after my review.
func withActivity(f *facts, reason string) string {
	if what := f.authorActivitySince(f.myLastReview()); what != "" {
		return reason + "; " + what + " since"
	}
	return reason
}

// Team requests.

func teamRequested(f *facts) []model.Reason {
	if len(f.requestedTeams) == 0 {
		return texts("team review requested")
	}
	return texts("team review requested: " + strings.Join(f.requestedTeams, ", "))
}

// Helpers.

// texts are reasons without a page to link.
func texts(text ...string) []model.Reason {
	reasons := make([]model.Reason, len(text))
	for i, t := range text {
		reasons[i] = model.Reason{Text: t}
	}
	return reasons
}

// linked is a reason with the page that shows it.
func linked(text, url string) []model.Reason {
	return []model.Reason{{Text: text, URL: url}}
}

// awaitsMyReply: thread i is unresolved, not outdated, and its last comment is from
// someone else; returns that someone.
func (f *facts) awaitsMyReply(i int) (string, bool) {
	t := f.raw.ReviewThreads.Nodes[i]
	if t.IsResolved || t.IsOutdated || len(t.Comments.Nodes) == 0 {
		return "", false
	}
	last := t.Comments.Nodes[len(t.Comments.Nodes)-1]
	if last.Author == nil || f.c.isBot(last.Author) || sameLogin(last.Author.Login, f.me) || f.reRequestedSince(last.Author.Login, last.CreatedAt) {
		return "", false
	}
	return last.Author.Login, true
}

// reRequestedSince: a review request to login is pending and was made after t, so what
// login did before it is answered: the move is theirs. The request's time comes from
// the fetched timeline (its last items); without it, nothing is handed over.
func (f *facts) reRequestedSince(login string, t time.Time) bool {
	key := strings.ToLower(login)
	return f.requestedUsers[key] && f.requestedAt[key].After(t)
}

func (f *facts) inProgress() bool {
	return slices.ContainsFunc(f.pr.Runs, inProgress)
}

// missingRules: the code-owner rules still missing; none when the check is green.
func (f *facts) missingRules() []model.CodeOwnerRule {
	if f.pr.CodeOwners.State == model.CodeOwnersOK {
		return nil
	}
	return f.pr.CodeOwners.Missing()
}

// changesRequestedSincePush: login's latest opinion requests changes, newer than my last
// push and than a pending re-request of them.
func (f *facts) changesRequestedSincePush(login string) bool {
	r, ok := f.opinions[strings.ToLower(login)]
	return ok && r.State == "CHANGES_REQUESTED" && r.SubmittedAt.After(f.lastPush) && !f.reRequestedSince(login, r.SubmittedAt)
}

func (f *facts) reviewed(login string) bool {
	for _, r := range f.reviews {
		if sameLogin(r.Author.LoginOrEmpty(), login) {
			return true
		}
	}
	return false
}

// myLastActivity is the latest of the last push and my last comment, slash command,
// review or thread reply.
func (f *facts) myLastActivity() time.Time {
	t := f.lastPush
	if f.myLastCommand.After(t) {
		t = f.myLastCommand
	}
	for _, e := range f.events {
		if sameLogin(e.login, f.me) && e.at.After(t) {
			t = e.at
		}
	}
	return t
}

// approvedSince reports whether login submitted an APPROVED review at t or later.
func (f *facts) approvedSince(login string, t time.Time) bool {
	for _, r := range f.reviews {
		if r.State == "APPROVED" && sameLogin(r.Author.LoginOrEmpty(), login) && !r.SubmittedAt.Before(t) {
			return true
		}
	}
	return false
}

func (f *facts) myLastReview() time.Time {
	var t time.Time
	for _, r := range f.reviews {
		if sameLogin(r.Author.LoginOrEmpty(), f.me) && r.SubmittedAt.After(t) {
			t = r.SubmittedAt
		}
	}
	return t
}

// myOpinion is my latest opinionated review state, or COMMENTED if I only commented.
func (f *facts) myOpinion() string {
	if r, ok := f.opinions[strings.ToLower(f.me)]; ok {
		return r.State
	}
	if !f.myLastReview().IsZero() {
		return "COMMENTED"
	}
	return ""
}

// authorActivitySince returns "author pushed" or "author replied" if the author did so
// after t. The author's slash commands aren't replies to a reviewer.
func (f *facts) authorActivitySince(t time.Time) string {
	if f.lastPush.After(t) {
		return "author pushed"
	}
	for _, e := range f.events {
		if sameLogin(e.login, f.author) && e.at.After(t) {
			return "author replied"
		}
	}
	return ""
}

func rulePath(r model.CodeOwnerRule) string {
	if len(r.Paths) == 0 {
		return "?"
	}
	return r.Paths[0]
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return list
		}
	}
	return append(list, s)
}

func unique(list []string) []string {
	var out []string
	for _, s := range list {
		out = appendUnique(out, s)
	}
	return out
}
