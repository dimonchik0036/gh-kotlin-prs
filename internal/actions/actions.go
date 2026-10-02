// Package actions are the bot commands the tool can post on a PR, and when it may: the
// local checks before anything is posted. Posting is internal/github's (PostComment).
package actions

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// Command is a bot command as the tool posts it.
type Command struct {
	// Name is the command's name for `run`: "dry-run-retry".
	Name string
	// Action is the TUI key action that posts it (config.Actions): "dryRunRetry".
	Action string
	// Text is the comment posted, exactly: "/dry-run --retry".
	Text string
	// Doc says what it does.
	Doc string
}

// The commands, in menu order.
var (
	DryRun = Command{Name: "dry-run", Action: "dryRun", Text: "/dry-run",
		Doc: "run the checks on the PR rebased onto the latest master, without merging"}
	DryRunRetry = Command{Name: "dry-run-retry", Action: "dryRunRetry", Text: "/dry-run --retry",
		Doc: "a dry-run that restarts once on a failure"}
	SafeMerge = Command{Name: "safe-merge", Action: "safeMerge", Text: "/safe-merge",
		Doc: "run the checks, then rebase-merge into master (fixup! commits are squashed first)"}
	CancelCoordinator = Command{Name: "cancel-coordinator", Action: "cancelCoordinator", Text: "/cancel-coordinator",
		Doc: "cancel the running dry-run or safe-merge"}
	Fixup = Command{Name: "fixup", Action: "fixup", Text: "/fixup",
		Doc: "squash fixup! commits into their targets and force-push the branch, without the checks"}
	CodeOwners = Command{Name: "codeowners", Action: "codeowners", Text: "/codeowners",
		Doc: "re-run the code-owners check and refresh the bot's table"}
)

// Commands lists every command.
var Commands = []Command{DryRun, DryRunRetry, SafeMerge, CancelCoordinator, Fixup, CodeOwners}

// Find returns the command named name.
func Find(name string) (Command, bool) {
	for _, c := range Commands {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// Names lists the commands' names, for usage messages.
func Names() string {
	names := make([]string, len(Commands))
	for i, c := range Commands {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// Kind is the run a command starts: dry-run or safe-merge, "" for the others.
func (c Command) Kind() model.RunKind {
	switch c {
	case DryRun, DryRunRetry:
		return model.DryRun
	case SafeMerge:
		return model.SafeMerge
	}
	return ""
}

// ErrNotPosted wraps every refusal: Check's reason why a command can't be posted now.
var ErrNotPosted = errors.New("not posted")

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotPosted, fmt.Sprintf(format, args...))
}

// Check says why the command can't be posted on the PR by viewer now, or nil when it can:
// only on the viewer's own open PRs, a dry-run or safe-merge only while no run is
// requested or running (the bot runs one at a time), a safe-merge only on a PR that
// isn't a draft and is approved, /cancel-coordinator only while a run is requested or
// running, and
// /codeowners only while its check is missing or failing. A request the bot never
// answered ("no response") doesn't count as running: it may be posted again.
func Check(c Command, pr model.PR, viewer string) error {
	if err := CheckOwnOpen(pr, viewer); err != nil {
		return err
	}
	running, active := activeRun(pr)
	switch c {
	case DryRun, DryRunRetry, SafeMerge:
		if active {
			return refuse("a %s is already %s on #%d", running.Kind, running.State, pr.Number)
		}
		switch {
		case c != SafeMerge:
		case pr.Draft:
			return refuse("#%d is a draft", pr.Number)
		case !Approved(pr):
			return refuse("#%d isn't approved: %s", pr.Number, missingApproval(pr))
		}
	case CancelCoordinator:
		if !active {
			return refuse("no dry-run or safe-merge is requested or running on #%d", pr.Number)
		}
	case CodeOwners:
		if pr.CodeOwners.Check == "SUCCESS" {
			return refuse("the code-owners check of #%d is already green", pr.Number)
		}
	}
	return nil
}

// CheckOwnOpen refuses a PR that isn't the viewer's or isn't open.
func CheckOwnOpen(pr model.PR, viewer string) error {
	switch {
	case !strings.EqualFold(pr.Author, viewer):
		return refuse("#%d isn't yours (by %s)", pr.Number, pr.Author)
	case !pr.MergedAt.IsZero():
		return refuse("#%d is merged", pr.Number)
	case pr.Closed:
		return refuse("#%d is closed", pr.Number)
	}
	return nil
}

// Available are the commands Check allows on the PR now, in menu order.
func Available(pr model.PR, viewer string) []Command {
	var out []Command
	for _, c := range Commands {
		if Check(c, pr, viewer) == nil {
			out = append(out, c)
		}
	}
	return out
}

// Approved reports whether the PR has an approval and a green code-owners check.
func Approved(pr model.PR) bool {
	return pr.Approvals > 0 && pr.CodeOwners.State == model.CodeOwnersOK
}

func missingApproval(pr model.PR) string {
	if pr.Approvals == 0 {
		return "no approval yet"
	}
	return "code owners missing"
}

// activeRun is the dry-run or safe-merge requested or running, if any; a request
// without a response doesn't count.
func activeRun(pr model.PR) (model.Run, bool) {
	for _, r := range []model.Run{pr.SafeMerge, pr.DryRun} {
		if r.State.InProgress() && !r.NoResponse {
			return r, true
		}
	}
	return model.Run{}, false
}

// Requested is pr as it looks right after c was posted at at, until the next refresh
// sees the comment: a dry-run or safe-merge requested by the comment at url, the move
// CI's. Other commands start no run: pr stays as it is.
func Requested(c Command, pr model.PR, url string, at time.Time) model.PR {
	kind := c.Kind()
	if kind == "" {
		return pr
	}
	run := model.Run{Kind: kind, State: model.RunRequested, Started: at, Updated: at, CommentURL: url}
	if kind == model.DryRun {
		pr.DryRun = run
	} else {
		pr.SafeMerge = run
	}
	pr.Runs = append([]model.Run{run}, pr.Runs...)
	pr.Next = model.NextCI
	pr.Reasons = append([]model.Reason{{Text: string(kind) + " requested just now", URL: url}}, pr.Reasons...)
	return pr
}
