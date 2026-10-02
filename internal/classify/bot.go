package classify

import (
	"regexp"
	"slices"
	"strings"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// The bot protocol of JetBrains/kotlin (SPEC §3). Every pattern the bots are
// matched against lives here.
var (
	dryRunBanner = "**THIS IS A DRY RUN**"
	gateStarted  = regexp.MustCompile(`Quality gate is triggered at (\S+)`)
	gatePassed   = "Quality gate finished successfully."
	gateFailed   = regexp.MustCompile(`Quality gate failed\.(?: See (\S+))?`)
	gateRetry    = regexp.MustCompile(`Triggered a \[retry attempt]\((\S+?)\) №(\d+) out of (\d+)`)
	// Runs started by a Safe-Merge in the ultimate repository rather than by a command on this PR.
	gateExternal = "The quality gate was triggered by Safe-Merge of the"

	commandRejected = regexp.MustCompile(`(?s)^Command rejected:\s*(.+)$`)
	// Rejection reasons only reviewers resolve; any other one is the author's to fix:
	// conflicts, fixup!/amend!/squash! commits, a draft, a coordinator build, the unknown.
	reviewersRejections = []string{"Missing code owners approval"}
	codeOwnersMarker    = "<!-- CODE_OWNERS_REVIEW_COMMENT -->"
	// Replies of the code-owners bot that report success rather than a failure.
	successReplies = []string{"Private aggregate run triggered at ", "Cherry-pick to `"}

	slashCommand = regexp.MustCompile(`^/[a-z-]+(?:\s|$)`)
)

// Command is a command of the code-owners bot, without the slash.
type Command string

// The commands listed in the bot's "PR commands for maintainers" table.
const (
	CmdSafeMerge       Command = "safe-merge"
	CmdSafeSquashMerge Command = "safe-squash-merge"
	CmdDryRun          Command = "dry-run"
	CmdTestPublic      Command = "test-public"
	CmdTestPrivate     Command = "test-private"
	CmdCodeOwners      Command = "codeowners"
	CmdFixup           Command = "fixup"
	CmdCancel          Command = "cancel-coordinator"
	CmdCherryPick      Command = "cherry-pick"
	// CmdReview is answered by another bot: kotlin-safemerge neither reacts nor replies.
	CmdReview Command = "review"
)

var commands = []Command{
	CmdSafeMerge, CmdSafeSquashMerge, CmdDryRun, CmdTestPublic, CmdTestPrivate,
	CmdCodeOwners, CmdFixup, CmdCancel, CmdCherryPick, CmdReview,
}

// ParseCommand returns the bot command a comment issues: its first line is the
// command alone or the command followed by a space and parameters.
func ParseCommand(body string) (Command, bool) {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	first = strings.TrimRight(first, "\r")
	for _, c := range commands {
		if first == "/"+string(c) || strings.HasPrefix(first, "/"+string(c)+" ") {
			return c, true
		}
	}
	return "", false
}

// attemptsCommand reports whether a comment's first line starts like a bot command
// even if it doesn't parse ("/dry-runx"): the bot answers those with "Unable to parse
// the issued command.".
func attemptsCommand(body string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return slices.ContainsFunc(commands, func(c Command) bool { return strings.HasPrefix(first, "/"+string(c)) })
}

// IsCommand reports whether a comment starts with any slash command (/dry-run, /final, …),
// which doesn't count as "someone commented".
func IsCommand(body string) bool {
	return slashCommand.MatchString(strings.TrimSpace(body))
}

// RunKind maps a command to the run it starts.
func (c Command) RunKind() (model.RunKind, bool) {
	switch c {
	case CmdDryRun:
		return model.DryRun, true
	case CmdSafeMerge, CmdSafeSquashMerge:
		return model.SafeMerge, true
	}
	return "", false
}

// GateReport is a parsed quality-gate comment.
type GateReport struct {
	Kind     model.RunKind
	State    model.RunState // Running, Passed or Failed
	BuildURL string
	Reason   string
	// External: triggered from an ultimate Merge-Request, not by this PR.
	External bool
}

// ParseGate parses a KotlinBuild quality-gate comment.
func ParseGate(body string) (GateReport, bool) {
	started := gateStarted.FindStringSubmatch(body)
	if started == nil {
		return GateReport{}, false
	}
	r := GateReport{
		Kind:     model.SafeMerge,
		State:    model.RunRunning,
		BuildURL: started[1],
		External: strings.Contains(body, gateExternal),
	}
	if strings.Contains(body, dryRunBanner) {
		r.Kind = model.DryRun
	}
	if m := gateRetry.FindStringSubmatch(body); m != nil {
		r.Reason = "retry " + m[2] + "/" + m[3]
	}
	switch {
	case strings.Contains(body, gatePassed):
		r.State = model.RunPassed
	case gateFailed.MatchString(body):
		r.State = model.RunFailed
	}
	return r, true
}

// ParseRejection returns the reason of a `Command rejected:` reply. Some reasons span
// several lines (a list of commits).
func ParseRejection(body string) (string, bool) {
	m := commandRejected.FindStringSubmatch(strings.TrimSpace(body))
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// ReviewersToFix reports whether a rejection's reason is one only reviewers resolve, like
// "Missing code owners approval - verify the check 'Code Owners Approval' is green.".
func ReviewersToFix(reason string) bool {
	return slices.ContainsFunc(reviewersRejections, func(prefix string) bool { return strings.HasPrefix(reason, prefix) })
}

// ParseFailure returns the reason of a reply that reports a command's failure: a
// `Command rejected:` reply, or any other text reply of the bot, like "Unrecognized
// parameters: …" or "Couldn't reach TeamCity - please try again in a few minutes.".
// The code-owners comment and the success replies of /test-private and /cherry-pick
// are not failures.
func ParseFailure(body string) (string, bool) {
	if reason, ok := ParseRejection(body); ok {
		return reason, true
	}
	if IsCodeOwnersComment(body) || isSuccessReply(body) {
		return "", false
	}
	return strings.TrimSpace(body), true
}

func isSuccessReply(body string) bool {
	return slices.ContainsFunc(successReplies, func(prefix string) bool { return strings.HasPrefix(body, prefix) })
}

// IsCodeOwnersComment reports whether a comment is the code-owners table.
func IsCodeOwnersComment(body string) bool {
	return strings.Contains(body, codeOwnersMarker)
}
