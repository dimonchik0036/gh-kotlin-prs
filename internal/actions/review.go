package actions

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// RequestReview is the `run` command that requests a review from code owners, also
// --post's and the TUI picker's name for it. It isn't a comment: internal/github's
// RequestReviewers sends it.
const RequestReview = "request-review"

// Subsystem is a row of the bot's code-owners table as the review picker shows it: the
// people who may be asked for its review.
type Subsystem struct {
	// Path is the row's first path; More counts its other paths.
	Path string
	More int
	// Candidates are the owners besides the author: in table order, then the (QA) and
	// (PM) members, then the unavailable ones (⏳).
	Candidates []Candidate
	// Status is what the row has now: "unassigned", "requested: bob_user" (just
	// "requested" for a request the table doesn't show yet), "re-request: bob_user",
	// "changes requested: bob_user", "✓ bob_user".
	Status string
	// NeedsMe: nobody was asked for it (UNASSIGNED, nobody requested since), or an owner
	// waits for a re-request (🔄). The picker opens these rows.
	NeedsMe bool
	// Covered: approved, or an owner of it is requested now.
	Covered bool
}

// Candidate is someone a review may be requested from.
type Candidate struct {
	Login string
	// Name is the display name the bot shows, when it does.
	Name string
	// Role is "QA" or "PM" when the bot marks the member so.
	Role        string
	Unavailable bool
	// Activity is what they did on the PR: "approved", "changes requested", "commented",
	// then "requested" while a request is pending.
	Activity []string
	// Also are the first paths of the other subsystems they own.
	Also []string
}

// Hint is the candidate's activity and their other subsystems, for one line.
func (c Candidate) Hint() string {
	hint := slices.Clone(c.Activity)
	if len(c.Also) > 0 {
		hint = append(hint, "also "+strings.Join(c.Also, ", "))
	}
	return strings.Join(hint, ", ")
}

// Subsystems are the rows of pr's code-owners table that have someone to ask: a rule
// without owners (#NO_OWNERS), or owned only by the author, isn't one.
func Subsystems(pr model.PR) []Subsystem {
	reviewers := map[string]model.Reviewer{}
	for _, r := range pr.Reviewers {
		if r.Login != "" {
			reviewers[strings.ToLower(r.Login)] = r
		}
	}
	var out []Subsystem
	for _, rule := range pr.CodeOwners.Rules {
		s := Subsystem{Path: first(rule.Paths), More: max(0, len(rule.Paths)-1)}
		requested := false
		for _, o := range rule.Owners {
			if strings.EqualFold(o.Login, pr.Author) {
				continue
			}
			c := Candidate{Login: o.Login, Name: o.Name, Role: o.Role, Unavailable: o.Unavailable}
			if r, ok := reviewers[strings.ToLower(o.Login)]; ok {
				c.Activity = activity(r)
				requested = requested || r.Requested
			}
			s.Candidates = append(s.Candidates, c)
		}
		if len(s.Candidates) == 0 {
			continue
		}
		slices.SortStableFunc(s.Candidates, func(a, b Candidate) int { return cmp.Compare(rank(a), rank(b)) })
		s.Status, s.NeedsMe = status(rule, requested, pr.Author)
		s.Covered = rule.Approved() || requested
		out = append(out, s)
	}
	for i := range out {
		for k, c := range out[i].Candidates {
			for j, other := range out {
				if j != i && other.has(c.Login) {
					out[i].Candidates[k].Also = append(out[i].Candidates[k].Also, other.Path)
				}
			}
		}
	}
	return out
}

func (s Subsystem) has(login string) bool {
	return slices.ContainsFunc(s.Candidates, func(c Candidate) bool { return strings.EqualFold(c.Login, login) })
}

// rank orders a row's candidates: everyone else, then (QA) and (PM), then ⏳.
func rank(c Candidate) int {
	switch {
	case c.Unavailable:
		return 2
	case c.Role != "":
		return 1
	}
	return 0
}

func activity(r model.Reviewer) []string {
	var out []string
	switch r.State {
	case model.ReviewerApproved:
		out = append(out, "approved")
	case model.ReviewerChangesRequested:
		out = append(out, "changes requested")
	case model.ReviewerCommented:
		out = append(out, "commented")
	}
	if r.Requested {
		out = append(out, "requested")
	}
	return out
}

// status is a row's Status and NeedsMe: requested says an owner of it is requested now,
// which the table may not show yet.
func status(rule model.CodeOwnerRule, requested bool, author string) (string, bool) {
	var logins []string
	for _, a := range rule.Assignees {
		if !strings.EqualFold(a.Login, author) {
			logins = append(logins, a.Login)
		}
	}
	who := strings.Join(logins, ", ")
	switch {
	case rule.Approved():
		return "✓ " + who, false
	case rule.Mark == model.MarkChangesRequested:
		return "changes requested: " + who, false
	case rule.Mark == model.MarkReRequest:
		return "re-request: " + who, !requested
	case len(logins) > 0:
		return "requested: " + who, false
	case requested:
		return "requested", false
	}
	return "unassigned", true
}

func first(paths []string) string {
	if len(paths) == 0 {
		return "?"
	}
	return paths[0]
}

// PreSelected are the people a re-request asks by default: the 🔄 owners not requested
// again yet, and the reviewers who requested changes before my last push, both among
// the candidates. Never someone who approved: a request doesn't dismiss an approval.
// Empty on a first assignment, which has no default.
func PreSelected(pr model.PR) []string {
	candidates := map[string]bool{}
	for _, s := range Subsystems(pr) {
		for _, c := range s.Candidates {
			candidates[strings.ToLower(c.Login)] = true
		}
	}
	reviewers := map[string]model.Reviewer{}
	for _, r := range pr.Reviewers {
		reviewers[strings.ToLower(r.Login)] = r
	}
	var out []string
	add := func(login string) {
		r := reviewers[strings.ToLower(login)]
		if candidates[strings.ToLower(login)] && !r.Requested && r.State != model.ReviewerApproved &&
			!slices.ContainsFunc(out, func(l string) bool { return strings.EqualFold(l, login) }) {
			out = append(out, login)
		}
	}
	for _, rule := range pr.CodeOwners.Rules {
		if rule.Mark == model.MarkReRequest {
			for _, a := range rule.Assignees {
				add(a.Login)
			}
		}
	}
	for _, r := range pr.Reviewers {
		if r.State == model.ReviewerChangesRequested && r.At.Before(pr.LastPush) {
			add(r.Login)
		}
	}
	return out
}

// CheckReviewRequest says why a review of pr can't be requested from logins by viewer
// now, or nil when it can: only on the viewer's own open PRs (drafts too), from at least
// one person, and only from candidates of its code-owners table.
func CheckReviewRequest(pr model.PR, viewer string, logins []string) error {
	if err := CheckOwnOpen(pr, viewer); err != nil {
		return err
	}
	if len(logins) == 0 {
		return refuse("nobody picked to request a review of #%d from", pr.Number)
	}
	subsystems := Subsystems(pr)
	var unknown []string
	for _, login := range logins {
		if !slices.ContainsFunc(subsystems, func(s Subsystem) bool { return s.has(login) }) {
			unknown = append(unknown, login)
		}
	}
	if len(unknown) > 0 {
		return refuse("%s %s no code owner of #%d to ask", strings.Join(unknown, ", "), plural(len(unknown), "is", "are"), pr.Number)
	}
	return nil
}

// CandidatesText lists the candidates per subsystem, one line each, for error messages.
func CandidatesText(subsystems []Subsystem) string {
	var b strings.Builder
	for _, s := range subsystems {
		logins := make([]string, len(s.Candidates))
		for i, c := range s.Candidates {
			logins[i] = c.Login
		}
		path := s.Path
		if s.More > 0 {
			path += fmt.Sprintf(" +%d", s.More)
		}
		b.WriteString(fmt.Sprintf("  %s (%s): %s\n", path, s.Status, strings.Join(logins, ", ")))
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
