// Package github fetches pull requests through gh's GraphQL client. The types mirror
// the query in pr.graphql one to one; interpretation is left to the classify package.
package github

import "time"

type Actor struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

// Reviewer is a requested reviewer: a user (Login) or a team (Slug).
type Reviewer struct {
	Typename string `json:"__typename"`
	Login    string `json:"login,omitempty"`
	Slug     string `json:"slug,omitempty"`
}

type ReviewRequest struct {
	RequestedReviewer *Reviewer `json:"requestedReviewer"`
}

// ReviewState is a review's state (PullRequestReviewState).
type ReviewState string

const (
	ReviewApproved         ReviewState = "APPROVED"
	ReviewChangesRequested ReviewState = "CHANGES_REQUESTED"
	ReviewCommented        ReviewState = "COMMENTED"
	ReviewDismissed        ReviewState = "DISMISSED"
)

type Review struct {
	Author      *Actor      `json:"author"`
	State       ReviewState `json:"state"`
	Body        string      `json:"body,omitempty"`
	SubmittedAt time.Time   `json:"submittedAt"`
	URL         string      `json:"url,omitempty"`
	// Comments counts the thread comments submitted with the review.
	Comments *Count `json:"comments,omitempty"`
}

type Count struct {
	TotalCount int `json:"totalCount"`
}

type ThreadComment struct {
	Author    *Actor    `json:"author"`
	Body      string    `json:"body,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	URL       string    `json:"url,omitempty"`
}

type ReviewThread struct {
	IsResolved   bool                        `json:"isResolved"`
	IsOutdated   bool                        `json:"isOutdated"`
	Path         string                      `json:"path"`
	FirstComment Nodes[ThreadComment]        `json:"firstComment"`
	Comments     CountedNodes[ThreadComment] `json:"comments"`
}

type Reaction struct {
	User *Actor `json:"user"`
}

type Comment struct {
	Author      *Actor    `json:"author"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	IsMinimized bool      `json:"isMinimized"`
	// AuthorAssociation is OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR, NONE, …
	AuthorAssociation string          `json:"authorAssociation,omitempty"`
	URL               string          `json:"url,omitempty"`
	Reactions         Nodes[Reaction] `json:"reactions"`
}

// TimelineItem is a PullRequestCommit, HeadRefForcePushedEvent or ReviewRequestedEvent.
type TimelineItem struct {
	Typename          string    `json:"__typename"`
	Commit            *Commit   `json:"commit,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	RequestedReviewer *Reviewer `json:"requestedReviewer,omitempty"`
}

// CheckConclusion is how a check run ended (CheckConclusionState); empty while it runs.
type CheckConclusion string

const (
	CheckSuccess        CheckConclusion = "SUCCESS"
	CheckFailure        CheckConclusion = "FAILURE"
	CheckCancelled      CheckConclusion = "CANCELLED"
	CheckTimedOut       CheckConclusion = "TIMED_OUT"
	CheckActionRequired CheckConclusion = "ACTION_REQUIRED"
	CheckStartupFailure CheckConclusion = "STARTUP_FAILURE"
)

// Failed: the check run ended without passing, other than skipped or neutral.
func (c CheckConclusion) Failed() bool {
	switch c {
	case CheckFailure, CheckCancelled, CheckTimedOut, CheckActionRequired, CheckStartupFailure:
		return true
	}
	return false
}

// StatusState is a status context's state (StatusState).
type StatusState string

const (
	StatusSuccess  StatusState = "SUCCESS"
	StatusFailure  StatusState = "FAILURE"
	StatusError    StatusState = "ERROR"
	StatusPending  StatusState = "PENDING"
	StatusExpected StatusState = "EXPECTED"
)

// Failed: the status context reported a failure or an error.
func (s StatusState) Failed() bool { return s == StatusFailure || s == StatusError }

// Running: the status context is still to report, expected or pending.
func (s StatusState) Running() bool { return s == StatusPending || s == StatusExpected }

// CheckContext is a CheckRun (Name, Conclusion, Status, Title, Summary) or a StatusContext (Context, State).
type CheckContext struct {
	Typename   string          `json:"__typename"`
	Name       string          `json:"name,omitempty"`
	Conclusion CheckConclusion `json:"conclusion,omitempty"`
	// Status is a check run's progress (QUEUED, IN_PROGRESS, COMPLETED, …): only shown.
	Status     string      `json:"status,omitempty"`
	DetailsURL string      `json:"detailsUrl,omitempty"`
	Title      string      `json:"title,omitempty"`
	Summary    string      `json:"summary,omitempty"`
	Context    string      `json:"context,omitempty"`
	State      StatusState `json:"state,omitempty"`
	TargetURL  string      `json:"targetUrl,omitempty"`
	// CreatedAt is when a status context got its state.
	CreatedAt time.Time `json:"createdAt,omitzero"`
}

type StatusCheckRollup struct {
	Contexts Nodes[CheckContext] `json:"contexts"`
}

type Commit struct {
	CommittedDate     time.Time          `json:"committedDate"`
	Message           string             `json:"message,omitempty"`
	StatusCheckRollup *StatusCheckRollup `json:"statusCheckRollup,omitempty"`
}

type PullRequestCommit struct {
	Commit Commit `json:"commit"`
}

// PRState is a pull request's state (PullRequestState).
type PRState string

const (
	PROpen   PRState = "OPEN"
	PRClosed PRState = "CLOSED"
	PRMerged PRState = "MERGED"
)

// MergeableState is whether a pull request merges without conflicts (MergeableState).
type MergeableState string

const (
	Mergeable   MergeableState = "MERGEABLE"
	Conflicting MergeableState = "CONFLICTING"
	// MergeableUnknown: GitHub is still checking.
	MergeableUnknown MergeableState = "UNKNOWN"
)

// Known: GitHub has checked for conflicts. Not when the state is missing either, as in
// fixtures older than the field.
func (s MergeableState) Known() bool { return s == Mergeable || s == Conflicting }

type PullRequest struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	IsDraft   bool       `json:"isDraft"`
	State     PRState    `json:"state"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	MergedAt  *time.Time `json:"mergedAt"`
	Author    *Actor     `json:"author"`
	// Body is the description, fetched only where it's a bot's: a cherry-pick's names
	// the original PR and its author.
	Body string `json:"body,omitempty"`
	// Assignees: on a bot's PR, who answers for it.
	Assignees                Nodes[Actor]             `json:"assignees"`
	HeadRefName              string                   `json:"headRefName"`
	HeadRefOid               string                   `json:"headRefOid"`
	BaseRefName              string                   `json:"baseRefName,omitempty"`
	Mergeable                MergeableState           `json:"mergeable,omitempty"`
	ReviewRequests           Nodes[ReviewRequest]     `json:"reviewRequests"`
	LatestOpinionatedReviews Nodes[Review]            `json:"latestOpinionatedReviews"`
	Reviews                  Nodes[Review]            `json:"reviews"`
	ReviewThreads            Nodes[ReviewThread]      `json:"reviewThreads"`
	Comments                 Nodes[Comment]           `json:"comments"`
	TimelineItems            Nodes[TimelineItem]      `json:"timelineItems"`
	Commits                  Nodes[PullRequestCommit] `json:"commits"`
	// History is the last 30 commits, for the issue trailers in their messages.
	History Nodes[PullRequestCommit] `json:"history"`
}

type Nodes[T any] struct {
	Nodes []T `json:"nodes"`
}

type CountedNodes[T any] struct {
	TotalCount int `json:"totalCount"`
	Nodes      []T `json:"nodes"`
}

// RateLimit is GitHub's GraphQL budget: Limit points an hour, Remaining of them left
// until ResetAt, and what the request cost.
type RateLimit struct {
	Limit     int       `json:"limit"`
	Cost      int       `json:"cost"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"resetAt"`
}

// LoginOrEmpty returns the actor's login, or "" for a deleted account.
func (a *Actor) LoginOrEmpty() string {
	if a == nil {
		return ""
	}
	return a.Login
}
