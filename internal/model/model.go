// Package model is the classified view of a PR. Its JSON form (plus "version") is
// the contract for every UI.
package model

import "time"

// Version of the JSON output.
const Version = 1

type Section string

const (
	SectionMine   Section = "mine"
	SectionReview Section = "review"
	SectionTeams  Section = "teams"
	SectionMerged Section = "merged"
)

type RunKind string

const (
	DryRun    RunKind = "dry-run"
	SafeMerge RunKind = "safe-merge"
)

type RunState string

const (
	RunNone RunState = "none"
	// RunRequested is a command comment with no reaction and no reply yet.
	RunRequested RunState = "requested"
	// RunAccepted means the automation reacted with 🚀, but the gate hasn't reported yet.
	RunAccepted RunState = "accepted"
	RunRunning  RunState = "running"
	RunPassed   RunState = "passed"
	RunFailed   RunState = "failed"
	RunRejected RunState = "rejected"
	// RunCancelled is a run without a result followed by a /cancel-coordinator or a newer run.
	RunCancelled RunState = "cancelled"
)

// Link is the page of a run: its build, or the comment behind it before a build exists.
func (r Run) Link() string {
	if r.BuildURL != "" {
		return r.BuildURL
	}
	return r.CommentURL
}

// InProgress reports whether the run still waits for the automation.
func (s RunState) InProgress() bool {
	return s == RunRequested || s == RunAccepted || s == RunRunning
}

type Run struct {
	Kind     RunKind  `json:"kind"`
	State    RunState `json:"state"`
	Reason   string   `json:"reason,omitempty"`
	BuildURL string   `json:"buildUrl,omitempty"`
	// Started is when the gate reported the run, or when the command was posted if it hasn't yet.
	Started time.Time `json:"started,omitzero"`
	Updated time.Time `json:"updated,omitzero"`
	// Outdated: triggered before the last push.
	Outdated bool `json:"outdated,omitempty"`
	// NoResponse: requested longer than requestedTimeout ago with no reaction or reply.
	NoResponse bool `json:"noResponse,omitempty"`
	// Minimized: the gate comment was hidden ("resolved") on GitHub.
	Minimized bool `json:"minimized,omitempty"`
	// CommentURL is the comment behind the run: the gate's report, the bot's rejection,
	// or the command while nothing answered it.
	CommentURL string `json:"commentUrl,omitempty"`
}

type IssueSource string

const (
	// IssueTrailer is a `^KT-123` line in a commit message.
	IssueTrailer IssueSource = "trailer"
	IssueBranch  IssueSource = "branch"
	IssueTitle   IssueSource = "title"
)

type IssueResolution string

const (
	IssueFixed    IssueResolution = "fixed"
	IssueObsolete IssueResolution = "obsolete"
	// IssueRelated is a bare trailer, or an ID in the branch or title.
	IssueRelated IssueResolution = ""
)

type Issue struct {
	ID         string          `json:"id"`
	URL        string          `json:"url,omitempty"`
	Source     IssueSource     `json:"source"`
	Resolution IssueResolution `json:"resolution,omitempty"`
}

// PrimaryIssue is the first issue's ID, or "".
func (p PR) PrimaryIssue() string {
	if len(p.Issues) == 0 {
		return ""
	}
	return p.Issues[0].ID
}

type ReviewerState string

const (
	ReviewerPending          ReviewerState = "pending"
	ReviewerApproved         ReviewerState = "approved"
	ReviewerChangesRequested ReviewerState = "changes_requested"
	ReviewerCommented        ReviewerState = "commented"
	ReviewerDismissed        ReviewerState = "dismissed"
)

type Reviewer struct {
	Login string `json:"login,omitempty"`
	// Team is set for a request to a team; Login is empty then.
	Team  string        `json:"team,omitempty"`
	State ReviewerState `json:"state"`
	// Requested: a review request is currently pending.
	Requested bool `json:"requested,omitempty"`
	// Final: approved with /final (🔒).
	Final bool `json:"final,omitempty"`
	// Unavailable: marked ⏳ by the code-owners bot.
	Unavailable bool `json:"unavailable,omitempty"`
	// ReRequest: marked 🔄, i.e. commented and waits for a re-request.
	ReRequest bool      `json:"reRequest,omitempty"`
	At        time.Time `json:"at,omitzero"`
	CodeOwner bool      `json:"codeOwner,omitempty"`
}

type CodeOwnersState string

const (
	CodeOwnersOK      CodeOwnersState = "ok"
	CodeOwnersMissing CodeOwnersState = "missing"
	CodeOwnersUnknown CodeOwnersState = "unknown"
)

// ApprovalMark is the per-rule mark in the code-owners table.
type ApprovalMark string

const (
	MarkNoReview         ApprovalMark = "no_review"         // ❌
	MarkReRequest        ApprovalMark = "re_request"        // 🔄
	MarkChangesRequested ApprovalMark = "changes_requested" // 🔴
	MarkApproved         ApprovalMark = "approved"          // ✅
	MarkUnknown          ApprovalMark = "unknown"
)

type Owner struct {
	Login string `json:"login"`
	// Name is the display name the bot shows next to the login, when it does.
	Name string `json:"name,omitempty"`
	// Team is the innermost team listing this owner; empty for individual owners.
	Team        string `json:"team,omitempty"`
	Unavailable bool   `json:"unavailable,omitempty"`
	// Role is "QA" or "PM" when the bot marks the member so.
	Role string `json:"role,omitempty"`
}

// Assignee is a login from the Approval column: who approved, requested changes or is assigned.
type Assignee struct {
	Login string `json:"login"`
	// Name is the display name the bot shows next to the login, when it does.
	Name        string `json:"name,omitempty"`
	Final       bool   `json:"final,omitempty"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

type CodeOwnerRule struct {
	Paths  []string     `json:"paths"`
	Teams  []string     `json:"teams,omitempty"`
	Owners []Owner      `json:"owners,omitempty"`
	Mark   ApprovalMark `json:"mark"`
	// Assignees is empty when the bot shows UNASSIGNED.
	Assignees []Assignee `json:"assignees,omitempty"`
}

func (r CodeOwnerRule) Approved() bool { return r.Mark == MarkApproved }

type CodeOwnersStatus struct {
	State CodeOwnersState `json:"state"`
	// Check is the conclusion of the `Code Owners Approval` check, empty if absent.
	Check string `json:"check,omitempty"`
	// Rules is the bot's table; the ones not approved are what's missing.
	Rules []CodeOwnerRule `json:"rules,omitempty"`
}

// Missing returns the rules that aren't approved yet.
func (s CodeOwnersStatus) Missing() []CodeOwnerRule {
	var out []CodeOwnerRule
	for _, r := range s.Rules {
		if !r.Approved() {
			out = append(out, r)
		}
	}
	return out
}

type Thread struct {
	Path string `json:"path"`
	// URL is the thread's first comment.
	URL        string    `json:"url,omitempty"`
	Author     string    `json:"author"`
	FirstLine  string    `json:"firstLine"`
	LastAuthor string    `json:"lastAuthor"`
	LastAt     time.Time `json:"lastAt,omitzero"`
	Comments   int       `json:"comments"`
	Outdated   bool      `json:"outdated,omitempty"`
}

type Check struct {
	Name  string `json:"name"`
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
}

type NextAction string

const (
	NextMe        NextAction = "me"
	NextReviewers NextAction = "reviewers"
	NextCI        NextAction = "ci"
	NextAuthor    NextAction = "author"
	NextDone      NextAction = "done"
)

type PR struct {
	Number  int     `json:"number"`
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Author  string  `json:"author"`
	Branch  string  `json:"branch"`
	Draft   bool    `json:"draft,omitempty"`
	Section Section `json:"section"`
	// Issues are the referenced issues, the primary one first.
	Issues   []Issue   `json:"issues"`
	Updated  time.Time `json:"updated,omitzero"`
	LastPush time.Time `json:"lastPush,omitzero"`
	MergedAt time.Time `json:"mergedAt,omitzero"`
	// Closed: closed without being merged.
	Closed    bool `json:"closed,omitempty"`
	DryRun    Run  `json:"dryRun"`
	SafeMerge Run  `json:"safeMerge"`
	// Runs is the history, newest first.
	Runs       []Run            `json:"runs,omitempty"`
	Reviewers  []Reviewer       `json:"reviewers,omitempty"`
	Approvals  int              `json:"approvals"`
	CodeOwners CodeOwnersStatus `json:"codeOwners"`
	Checks     []Check          `json:"checks,omitempty"`
	// UnresolvedThreads counts unresolved threads, outdated ones included.
	UnresolvedThreads int        `json:"unresolvedThreads"`
	Threads           []Thread   `json:"threads,omitempty"`
	Next              NextAction `json:"next"`
	// Reasons explain Next; the first one is the primary reason.
	Reasons []Reason `json:"reasons"`
	// Hidden: shown only with --all (a done review, a draft in Review).
	Hidden bool `json:"hidden,omitempty"`
}

// Primary returns the main reason, or "".
func (p PR) Primary() string { return p.PrimaryReason().Text }

// PrimaryReason is the main reason, or the zero Reason.
func (p PR) PrimaryReason() Reason {
	if len(p.Reasons) == 0 {
		return Reason{}
	}
	return p.Reasons[0]
}

// Reason is one explanation of Next, with the page that shows it when known: a build,
// a bot reply, a review or a thread.
type Reason struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// Texts are the reasons' texts.
func (p PR) Texts() []string {
	texts := make([]string, len(p.Reasons))
	for i, r := range p.Reasons {
		texts[i] = r.Text
	}
	return texts
}

// Output is the top-level JSON document of `list --format json`.
type Output struct {
	Version     int       `json:"version"`
	Viewer      string    `json:"viewer"`
	GeneratedAt time.Time `json:"generatedAt"`
	PRs         []PR      `json:"prs"`
	// Hidden counts the PRs per section left out without --all.
	Hidden map[Section]Hidden `json:"hidden,omitempty"`
}

// Hidden counts the rows of a section that only --all shows.
type Hidden struct {
	// NotWaiting: rows that don't wait on the viewer, e.g. reviews already given.
	NotWaiting int `json:"notWaiting,omitempty"`
	// Drafts: drafts outside Mine.
	Drafts int `json:"drafts,omitempty"`
}

func (h Hidden) Total() int { return h.NotWaiting + h.Drafts }
