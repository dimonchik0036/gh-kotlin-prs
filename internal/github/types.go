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

type Review struct {
	Author      *Actor    `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
	URL         string    `json:"url,omitempty"`
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

// CheckContext is a CheckRun (Name, Conclusion, Status, Title, Summary) or a StatusContext (Context, State).
type CheckContext struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
	Status     string `json:"status,omitempty"`
	DetailsURL string `json:"detailsUrl,omitempty"`
	Title      string `json:"title,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Context    string `json:"context,omitempty"`
	State      string `json:"state,omitempty"`
	TargetURL  string `json:"targetUrl,omitempty"`
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

type PullRequest struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	IsDraft   bool       `json:"isDraft"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	MergedAt  *time.Time `json:"mergedAt"`
	Author    *Actor     `json:"author"`
	// Body is the description, fetched only where it's a bot's: a cherry-pick's names
	// the original PR and its author.
	Body string `json:"body,omitempty"`
	// Assignees: on a bot's PR, who answers for it.
	Assignees   Nodes[Actor] `json:"assignees"`
	HeadRefName string       `json:"headRefName"`
	HeadRefOid  string       `json:"headRefOid"`
	BaseRefName string       `json:"baseRefName,omitempty"`
	// Mergeable is MERGEABLE, CONFLICTING or UNKNOWN (GitHub still computing it; also
	// when missing, in fixtures older than the field).
	Mergeable                string                   `json:"mergeable,omitempty"`
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
