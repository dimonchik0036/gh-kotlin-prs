// Package classify turns raw GitHub data into the model: it parses the bots'
// comments (SPEC §3) and applies the "whose move" rules (SPEC §5).
package classify

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

const codeOwnersCheck = "Code Owners Approval"

type Classifier struct {
	Config config.Config
	// Viewer is the login of the current user.
	Viewer string
	// Now is the clock; every relative time is computed against it.
	Now time.Time
}

// PR classifies an open PR shown in the given section.
func (c *Classifier) PR(raw *github.PullRequest, section model.Section) model.PR {
	f := c.facts(raw, section)
	switch section {
	case model.SectionMine:
		applyRules(f, mineRules)
	case model.SectionTeams:
		applyRules(f, teamRules)
	default:
		applyRules(f, reviewRules)
	}
	return *f.pr
}

// Show classifies a single PR in the section it would be listed in. A merged or
// closed PR keeps its details, but the rules don't apply to it.
func (c *Classifier) Show(raw *github.PullRequest) model.PR {
	pr := c.PR(raw, SectionFor(raw, c.Viewer))
	switch raw.State {
	case "MERGED":
		merged := c.Merged(raw)
		pr.MergedAt, pr.Next, pr.Reasons, pr.Hidden = merged.MergedAt, model.NextDone, merged.Reasons, false
	case "CLOSED":
		pr.Next, pr.Reasons, pr.Hidden, pr.Closed = model.NextDone, []model.Reason{{Text: "closed"}}, false, true
	}
	return pr
}

// Merged builds the row of a recently merged PR.
func (c *Classifier) Merged(raw *github.PullRequest) model.PR {
	pr := c.basics(raw, model.SectionMerged)
	pr.DryRun = model.Run{Kind: model.DryRun, State: model.RunNone}
	pr.SafeMerge = model.Run{Kind: model.SafeMerge, State: model.RunNone}
	pr.CodeOwners = model.CodeOwnersStatus{State: model.CodeOwnersUnknown}
	pr.Next = model.NextDone
	if raw.MergedAt != nil {
		pr.MergedAt = *raw.MergedAt
		pr.Reasons = []model.Reason{{Text: "merged " + model.Ago(c.Now, *raw.MergedAt)}}
	} else {
		pr.Reasons = []model.Reason{{Text: "merged"}}
	}
	return pr
}

func (c *Classifier) basics(raw *github.PullRequest, section model.Section) model.PR {
	pr := model.PR{
		Number:    raw.Number,
		Title:     raw.Title,
		URL:       raw.URL,
		Author:    raw.Author.LoginOrEmpty(),
		Branch:    raw.HeadRefName,
		Base:      raw.BaseRefName,
		Draft:     raw.IsDraft,
		Conflicts: raw.Mergeable == "CONFLICTING",
		Section:   section,
		Updated:   raw.UpdatedAt,
	}
	if pr.URL == "" {
		pr.URL = github.PRURL(c.Config.Repo, raw.Number)
	}
	pr.Issues = c.issues(raw)
	return pr
}

// event is a non-bot activity: a comment, a thread comment or a review.
type event struct {
	login string
	at    time.Time
	// what: "comment", "review" or "thread comment", for reasons.
	what string
	// thread is the index of the review thread of a thread comment, else -1.
	thread int
	url    string
}

// facts is everything the rules look at.
type facts struct {
	c        *Classifier
	pr       *model.PR
	raw      *github.PullRequest
	me       string
	author   string
	lastPush time.Time
	// opinions: the latest APPROVED / CHANGES_REQUESTED / DISMISSED review per reviewer.
	opinions map[string]github.Review
	// reviews by people other than the author and bots, oldest first.
	reviews []github.Review
	// events: non-bot comments, thread comments and COMMENTED reviews, slash commands excluded.
	events []event
	// myLastCommand: my latest slash-command comment (/dry-run, /safe-merge, …), activity
	// of mine though not an event.
	myLastCommand  time.Time
	requestedUsers map[string]bool
	requestedTeams []string
	// requestedAt: the latest REVIEW_REQUESTED event per user, if in the fetched timeline.
	requestedAt map[string]time.Time
}

func (c *Classifier) facts(raw *github.PullRequest, section model.Section) *facts {
	pr := c.basics(raw, section)
	f := &facts{
		c:              c,
		pr:             &pr,
		raw:            raw,
		me:             c.Viewer,
		author:         pr.Author,
		opinions:       map[string]github.Review{},
		requestedUsers: map[string]bool{},
		requestedAt:    map[string]time.Time{},
	}
	f.lastPush = lastPush(raw)
	pr.LastPush = f.lastPush

	pr.Runs = c.runs(raw.Comments.Nodes, f.lastPush)
	pr.DryRun = latestOfKind(pr.Runs, model.DryRun)
	pr.SafeMerge = latestOfKind(pr.Runs, model.SafeMerge)

	for _, rr := range raw.ReviewRequests.Nodes {
		switch r := rr.RequestedReviewer; {
		case r == nil:
		case r.Slug != "":
			f.requestedTeams = append(f.requestedTeams, r.Slug)
		case r.Login != "":
			f.requestedUsers[strings.ToLower(r.Login)] = true
		}
	}
	for _, item := range raw.TimelineItems.Nodes {
		if item.Typename == "ReviewRequestedEvent" && item.RequestedReviewer != nil && item.RequestedReviewer.Login != "" {
			login := strings.ToLower(item.RequestedReviewer.Login)
			if item.CreatedAt.After(f.requestedAt[login]) {
				f.requestedAt[login] = item.CreatedAt
			}
		}
	}
	for _, r := range raw.LatestOpinionatedReviews.Nodes {
		if login := r.Author.LoginOrEmpty(); login != "" && !c.isBot(r.Author) && !sameLogin(login, f.author) {
			f.opinions[strings.ToLower(login)] = r
		}
	}
	for _, r := range raw.Reviews.Nodes {
		if login := r.Author.LoginOrEmpty(); login != "" && !c.isBot(r.Author) {
			if !sameLogin(login, f.author) {
				f.reviews = append(f.reviews, r)
			}
			// A review with thread comments is represented by those comments.
			if r.State == "COMMENTED" && (r.Comments == nil || r.Comments.TotalCount == 0) {
				f.events = append(f.events, event{login: login, at: r.SubmittedAt, what: "review", thread: -1, url: r.URL})
			}
		}
	}
	slices.SortStableFunc(f.reviews, func(a, b github.Review) int { return a.SubmittedAt.Compare(b.SubmittedAt) })
	for _, cm := range raw.Comments.Nodes {
		if cm.IsMinimized || c.isBot(cm.Author) || cm.Author == nil {
			continue
		}
		if IsCommand(cm.Body) {
			if sameLogin(cm.Author.Login, f.me) && cm.CreatedAt.After(f.myLastCommand) {
				f.myLastCommand = cm.CreatedAt
			}
			continue
		}
		f.events = append(f.events, event{login: cm.Author.Login, at: cm.CreatedAt, what: "comment", thread: -1, url: cm.URL})
	}
	for i, t := range raw.ReviewThreads.Nodes {
		for _, tc := range t.Comments.Nodes {
			if tc.Author != nil && !c.isBot(tc.Author) {
				f.events = append(f.events, event{login: tc.Author.Login, at: tc.CreatedAt, what: "thread comment", thread: i, url: tc.URL})
			}
		}
	}

	pr.CodeOwners = c.codeOwners(raw)
	pr.Checks = checks(raw)
	pr.Reviewers = f.reviewers()
	for _, r := range f.opinions {
		if r.State == "APPROVED" {
			pr.Approvals++
		}
	}
	pr.Threads, pr.UnresolvedThreads = c.threads(raw)
	return f
}

// lastPush is the latest force-push or commit date. A rebase resets the committer
// date, so this is close enough to the actual push time.
func lastPush(raw *github.PullRequest) time.Time {
	var t time.Time
	later := func(u time.Time) {
		if u.After(t) {
			t = u
		}
	}
	for _, item := range raw.TimelineItems.Nodes {
		switch item.Typename {
		case "HeadRefForcePushedEvent":
			later(item.CreatedAt)
		case "PullRequestCommit":
			if item.Commit != nil {
				later(item.Commit.CommittedDate)
			}
		}
	}
	for _, c := range raw.Commits.Nodes {
		later(c.Commit.CommittedDate)
	}
	return t
}

func checkRuns(raw *github.PullRequest) []github.CheckContext {
	var out []github.CheckContext
	for _, c := range raw.Commits.Nodes {
		if c.Commit.StatusCheckRollup == nil {
			continue
		}
		for _, ctx := range c.Commit.StatusCheckRollup.Contexts.Nodes {
			if ctx.Typename == "CheckRun" {
				out = append(out, ctx)
			}
		}
	}
	return out
}

func checks(raw *github.PullRequest) []model.Check {
	var out []model.Check
	for _, c := range raw.Commits.Nodes {
		if c.Commit.StatusCheckRollup == nil {
			continue
		}
		for _, ctx := range c.Commit.StatusCheckRollup.Contexts.Nodes {
			switch ctx.Typename {
			case "CheckRun":
				state := ctx.Conclusion
				if state == "" {
					state = ctx.Status
				}
				out = append(out, model.Check{Name: ctx.Name, State: state, URL: ctx.DetailsURL})
			case "StatusContext":
				out = append(out, model.Check{Name: ctx.Context, State: ctx.State, URL: ctx.TargetURL})
			}
		}
	}
	return out
}

// codeOwners combines the `Code Owners Approval` check (the verdict) with the bot's
// table (the details). The bot resolves team hierarchies, so a green check wins over the table.
func (c *Classifier) codeOwners(raw *github.PullRequest) model.CodeOwnersStatus {
	var status model.CodeOwnersStatus
	found := false
	// The check's summary carries the same table as the comment, for the current head.
	// The comment is created once when the PR opens, so on a long PR it falls out of
	// the fetched comments; it's only the fallback.
	for _, ctx := range checkRuns(raw) {
		if ctx.Name != codeOwnersCheck {
			continue
		}
		status.Check = ctx.Conclusion
		if status.Check == "" {
			status.Check = ctx.Status
		}
		if rules, ok := ParseCodeOwners(ctx.Summary); ok {
			status.Rules, found = rules, true
		}
	}
	if !found {
		for _, cm := range raw.Comments.Nodes {
			if !sameLogin(cm.Author.LoginOrEmpty(), c.Config.OwnersBot) {
				continue
			}
			if rules, ok := ParseCodeOwners(cm.Body); ok {
				status.Rules, found = rules, true // updated in place; keep the last one
			}
		}
	}
	switch status.Check {
	case "SUCCESS":
		status.State = model.CodeOwnersOK
	case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
		status.State = model.CodeOwnersMissing
	case "":
		status.State = model.CodeOwnersUnknown
		if found {
			status.State = model.CodeOwnersOK
			if len(status.Missing()) > 0 {
				status.State = model.CodeOwnersMissing
			}
		}
	default: // QUEUED, IN_PROGRESS, PENDING, …
		status.State = model.CodeOwnersUnknown
	}
	return status
}

func (f *facts) reviewers() []model.Reviewer {
	var out []*model.Reviewer
	byLogin := map[string]*model.Reviewer{}
	get := func(login string) *model.Reviewer {
		key := strings.ToLower(login)
		if r, ok := byLogin[key]; ok {
			return r
		}
		r := &model.Reviewer{Login: login, State: model.ReviewerPending}
		byLogin[key] = r
		out = append(out, r)
		return r
	}
	for _, rr := range f.raw.ReviewRequests.Nodes {
		switch r := rr.RequestedReviewer; {
		case r == nil:
		case r.Slug != "":
			out = append(out, &model.Reviewer{Team: r.Slug, State: model.ReviewerPending, Requested: true})
		case r.Login != "":
			get(r.Login).Requested = true
		}
	}
	for _, rv := range f.reviews {
		r := get(rv.Author.Login)
		if r.State == model.ReviewerPending && rv.State == "COMMENTED" {
			r.State = model.ReviewerCommented
		}
		r.At = rv.SubmittedAt
	}
	for _, op := range f.raw.LatestOpinionatedReviews.Nodes {
		if _, ok := f.opinions[strings.ToLower(op.Author.LoginOrEmpty())]; !ok {
			continue
		}
		r := get(op.Author.Login)
		r.State = reviewerState(op.State)
		if op.SubmittedAt.After(r.At) {
			r.At = op.SubmittedAt
		}
	}
	for _, rule := range f.pr.CodeOwners.Rules {
		for _, o := range rule.Owners {
			if r, ok := byLogin[strings.ToLower(o.Login)]; ok {
				r.CodeOwner = true
				r.Unavailable = r.Unavailable || o.Unavailable
				r.Name = cmp.Or(r.Name, o.Name)
			}
		}
		for _, a := range rule.Assignees {
			if sameLogin(a.Login, f.author) {
				continue
			}
			r := get(a.Login)
			r.CodeOwner = true
			r.Final = r.Final || a.Final
			r.Unavailable = r.Unavailable || a.Unavailable
			r.Name = cmp.Or(r.Name, a.Name)
			r.ReRequest = r.ReRequest || rule.Mark == model.MarkReRequest
		}
	}
	reviewers := make([]model.Reviewer, len(out))
	for i, r := range out {
		reviewers[i] = *r
	}
	return reviewers
}

func reviewerState(state string) model.ReviewerState {
	switch state {
	case "APPROVED":
		return model.ReviewerApproved
	case "CHANGES_REQUESTED":
		return model.ReviewerChangesRequested
	case "DISMISSED":
		return model.ReviewerDismissed
	case "COMMENTED":
		return model.ReviewerCommented
	}
	return model.ReviewerPending
}

func (c *Classifier) threads(raw *github.PullRequest) ([]model.Thread, int) {
	var out []model.Thread
	for _, t := range raw.ReviewThreads.Nodes {
		if t.IsResolved {
			continue
		}
		th := model.Thread{Path: t.Path, Comments: t.Comments.TotalCount, Outdated: t.IsOutdated}
		if len(t.FirstComment.Nodes) > 0 {
			first := t.FirstComment.Nodes[0]
			th.Author = first.Author.LoginOrEmpty()
			th.FirstLine = firstLine(first.Body)
			th.URL = first.URL
		}
		if n := len(t.Comments.Nodes); n > 0 {
			last := t.Comments.Nodes[n-1]
			th.LastAuthor = last.Author.LoginOrEmpty()
			th.LastAt = last.CreatedAt
		}
		out = append(out, th)
	}
	return out, len(out)
}

func firstLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return strings.TrimSpace(line)
}

func (c *Classifier) isBot(a *github.Actor) bool {
	return a != nil && (a.Typename == "Bot" || c.Config.IsBot(a.Login))
}

func sameLogin(a, b string) bool { return a != "" && config.SameLogin(a, b) }

// SectionFor picks the section a single PR would be listed in, for `show`.
func SectionFor(pr *github.PullRequest, viewer string) model.Section {
	if sameLogin(pr.Author.LoginOrEmpty(), viewer) {
		return model.SectionMine
	}
	personal := false
	team := false
	for _, rr := range pr.ReviewRequests.Nodes {
		switch r := rr.RequestedReviewer; {
		case r == nil:
		case r.Slug != "":
			team = true
		case sameLogin(r.Login, viewer):
			personal = true
		}
	}
	reviewed := slices.ContainsFunc(pr.Reviews.Nodes, func(r github.Review) bool { return sameLogin(r.Author.LoginOrEmpty(), viewer) })
	if team && !personal && !reviewed {
		return model.SectionTeams
	}
	return model.SectionReview
}
