package classify

import (
	"slices"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

type pendingCommand struct {
	cmd Command
	// kind is empty for commands that start no dry-run or safe-merge.
	kind     model.RunKind
	at       time.Time
	accepted bool
	url      string
	// dismissed: the author minimized the command, which issues no run; it still waits,
	// so that the bot's reply to it goes with it.
	dismissed bool
}

// runs reconstructs the dry-run and safe-merge history from the PR comments, newest first.
//
// Walking the comments in order, every bot command waits for its outcome. The bot adds
// 🚀 once it has dispatched a command; when it can't, it replies instead (`Command
// rejected: …` or another failure text) and adds no reaction. A reply therefore answers
// the latest waiting command without 🚀. A dry-run or safe-merge then ends with a gate
// comment of its kind. Commands still waiting at the end are requested (no 🚀 yet) or
// accepted (🚀, the build is queued).
//
// A gate comment without a result is running only while it is the newest one, isn't
// minimized (the bot hides older gate comments when it starts a new build) and no
// dispatched `/cancel-coordinator` came after it.
//
// A command its author minimized issues nothing, and the bot's reply to it goes with it;
// a minimized rejection drops the rejected run: the author dismissed it.
func (c *Classifier) runs(comments []github.Comment, lastPush time.Time) []model.Run {
	comments = slices.Clone(comments)
	slices.SortStableFunc(comments, func(a, b github.Comment) int { return a.CreatedAt.Compare(b.CreatedAt) })

	var runs []model.Run
	var pending []pendingCommand
	for _, cm := range comments {
		login := cm.Author.LoginOrEmpty()
		switch {
		case sameLogin(login, c.Config.GateBot):
			gate, ok := ParseGate(cm.Body)
			if !ok || gate.External {
				continue
			}
			pending = slices.DeleteFunc(pending, func(p pendingCommand) bool { return p.kind == gate.Kind })
			runs = append(runs, model.Run{
				Kind:       gate.Kind,
				State:      gate.State,
				Reason:     gate.Reason,
				BuildURL:   gate.BuildURL,
				Started:    cm.CreatedAt,
				Updated:    cm.UpdatedAt,
				Minimized:  cm.IsMinimized,
				CommentURL: cm.URL,
			})
		case sameLogin(login, c.Config.OwnersBot):
			if isSuccessReply(cm.Body) {
				continue
			}
			reason, ok := ParseFailure(cm.Body)
			if !ok {
				continue
			}
			i := answeredCommand(pending)
			if i < 0 {
				continue
			}
			cmd := pending[i]
			pending = slices.Delete(pending, i, i+1)
			if cmd.kind != "" && !cmd.dismissed && !cm.IsMinimized {
				runs = append(runs, model.Run{
					Kind:       cmd.kind,
					State:      model.RunRejected,
					Reason:     reason,
					Started:    cmd.at,
					Updated:    cm.CreatedAt,
					CommentURL: cm.URL,
				})
			}
		case c.isBot(cm.Author) || !hasWriteAccess(cm.AuthorAssociation):
			// The bot ignores commands from accounts without write access.
		default:
			cmd, ok := ParseCommand(cm.Body)
			if !ok {
				if attemptsCommand(cm.Body) {
					// Waits for the bot's "Unable to parse the issued command." reply.
					pending = append(pending, pendingCommand{at: cm.CreatedAt})
				}
				continue
			}
			if cmd == CmdReview {
				continue
			}
			p := pendingCommand{cmd: cmd, at: cm.CreatedAt, accepted: c.hasBotRocket(cm), url: cm.URL, dismissed: cm.IsMinimized}
			p.kind, _ = cmd.RunKind()
			if cmd == CmdCancel && p.accepted {
				// Dispatched, so no reply comes; a minimized one cancels nothing.
				if !p.dismissed {
					pending, runs = cancel(pending, runs, cm.CreatedAt)
				}
				continue
			}
			pending = append(pending, p)
		}
	}

	// The coordinator runs one build at a time, so only the newest gate run can still be running.
	for i := range runs {
		if runs[i].State != model.RunRunning {
			continue
		}
		if runs[i].Minimized {
			runs[i].State = model.RunCancelled
			runs[i].Reason = joinReason(runs[i].Reason, "superseded")
			continue
		}
		for _, later := range runs[i+1:] {
			if later.BuildURL != "" {
				runs[i].State = model.RunCancelled
				runs[i].Reason = joinReason(runs[i].Reason, "superseded")
				break
			}
		}
	}

	for _, p := range pending {
		if p.kind == "" || p.dismissed {
			continue
		}
		run := model.Run{Kind: p.kind, State: model.RunRequested, Started: p.at, Updated: p.at, CommentURL: p.url}
		if p.accepted {
			run.State = model.RunAccepted
		} else if c.Now.Sub(p.at) > time.Duration(c.Config.RequestedTimeout) {
			run.NoResponse = true
		}
		runs = append(runs, run)
	}

	// Only builds get outdated: a command that hasn't started one yet will build the
	// current head, and a rejection is feedback on the command, not on the code.
	for i := range runs {
		switch runs[i].State {
		case model.RunRequested, model.RunAccepted, model.RunRejected:
		default:
			runs[i].Outdated = runs[i].Started.Before(lastPush)
		}
	}
	slices.SortStableFunc(runs, func(a, b model.Run) int { return b.Started.Compare(a.Started) })
	return runs
}

// answeredCommand picks the waiting command a failure reply answers: the latest one
// without 🚀, since 🚀 means the bot dispatched it. Returns -1 if none waits.
func answeredCommand(pending []pendingCommand) int {
	for i := len(pending) - 1; i >= 0; i-- {
		if !pending[i].accepted {
			return i
		}
	}
	return len(pending) - 1
}

// cancel applies a dispatched /cancel-coordinator: the running build and any queued
// (accepted) dry-run or safe-merge before it are cancelled.
func cancel(pending []pendingCommand, runs []model.Run, at time.Time) ([]pendingCommand, []model.Run) {
	for i := range runs {
		if runs[i].State == model.RunRunning {
			runs[i].State = model.RunCancelled
			runs[i].Reason = joinReason(runs[i].Reason, "cancelled")
		}
	}
	pending = slices.DeleteFunc(pending, func(p pendingCommand) bool {
		if p.kind == "" || !p.accepted || p.at.After(at) {
			return false
		}
		runs = append(runs, model.Run{Kind: p.kind, State: model.RunCancelled, Reason: "cancelled", Started: p.at, Updated: at, CommentURL: p.url})
		return true
	})
	return pending, runs
}

// hasWriteAccess: owners, organization members and collaborators may issue commands.
// An empty association (not fetched) counts as access.
func hasWriteAccess(association string) bool {
	switch association {
	case "", "OWNER", "MEMBER", "COLLABORATOR":
		return true
	}
	return false
}

func (c *Classifier) hasBotRocket(cm github.Comment) bool {
	for _, r := range cm.Reactions.Nodes {
		if r.User != nil && c.Config.IsBot(r.User.Login) {
			return true
		}
	}
	return false
}

// latestOfKind is the newest run of that kind by start: a rejection starts at its
// command, so it stays the latest until a newer command or run of that kind.
func latestOfKind(runs []model.Run, kind model.RunKind) model.Run {
	r := latest(slices.DeleteFunc(slices.Clone(runs), func(r model.Run) bool { return r.Kind != kind }))
	r.Kind = kind
	return r
}

// latest is the run started last; runs are sorted newest first.
func latest(runs []model.Run) model.Run {
	if len(runs) == 0 {
		return model.Run{State: model.RunNone}
	}
	return runs[0]
}

func inProgress(r model.Run) bool { return r.State.InProgress() && !r.NoResponse }

func joinReason(a, b string) string {
	if a == "" {
		return b
	}
	return a + ", " + b
}
