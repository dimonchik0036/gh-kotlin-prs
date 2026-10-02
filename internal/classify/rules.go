package classify

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// rule is one "whose move" rule (SPEC §5). The first matching rule of a section sets
// Next; every matching rule adds its reasons. A rule that yields (afterRuns, while a run is
// requested or running) doesn't set Next, and its reasons come after all the others.
type rule struct {
	name   string
	next   model.NextAction
	hidden bool
	match  func(f *facts) []model.Reason
	// afterRuns: reviewers usually come after CI, so while a dry-run or safe-merge goes on,
	// the rule leaves the move to the others (rule 5, CI, unless an earlier one is mine).
	afterRuns bool
}

func applyRules(f *facts, rules []rule) {
	var yielded []model.Reason
	for _, r := range rules {
		reasons := r.match(f)
		if len(reasons) == 0 {
			continue
		}
		if r.afterRuns && len(mineRunInProgress(f)) > 0 {
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

// Review shows only what waits on me: a personal request I haven't answered. Anything
// else, whatever the author did since, is hidden until I'm re-requested (`--all` shows it).
var reviewRules = []rule{
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
func mineRunFailed(f *facts) []model.Reason {
	switch run := latest(f.pr.Runs); {
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

// Review.

// 1. Requested from me personally, and no review of mine after the latest request.
func reviewRequested(f *facts) []model.Reason {
	if !f.requestedUsers[strings.ToLower(f.me)] {
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
