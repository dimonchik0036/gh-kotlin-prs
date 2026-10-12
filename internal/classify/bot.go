package classify

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// The bot protocol of JetBrains/kotlin (SPEC §3). Every pattern the bots are
// matched against lives here.
var (
	dryRunBanner = "**THIS IS A DRY RUN**"
	gateStarted  = regexp.MustCompile(`Quality gate is triggered at (\S+)`)
	// "Quality gate finished successfully ✅." (without ✅ before 2026-10-12), or after a
	// failure "[Quality gate](<url>) finished successfully (recovered via [retry](<url>))."
	// (or via [auxiliary quality gate](<url>)).
	gatePassed = regexp.MustCompile(`Quality gate(?:]\(\S+?\))? finished successfully`)
	// "Quality gate failed ❌. See <url> to get full insight." (without ❌ before 2026-10-12).
	gateFailed = regexp.MustCompile(`Quality gate failed\b`)
	gateRetry  = regexp.MustCompile(`Triggered a \[retry attempt]\((\S+?)\) №(\d+) out of (\d+)`)
	// The coordinator posts one comment per run and appends each of its messages to it.
	gateSeparator = "\n\n---\n\n"
	// An error after the result: the bot couldn't finish the run's job, like the merge.
	gateError = regexp.MustCompile(`(?m)^Error:[ \t]*(\S.*?)\s*$`)
	// Errors that only say the coordinator gave up on a call to GitHub or TeamCity: after
	// its retries, or on an error it doesn't retry, like a 4xx.
	gateGenericErrors = []string{"Failed all ", "Unretryable error occurred"}
	gateGenericError  = "the coordinator gave up on a GitHub or TeamCity call"
	// The end of the coordinator's messages for a run it couldn't start (ParseAbort).
	gateAbort    = "trigger Safe-Merge again."
	markdownLink = regexp.MustCompile(`\[([^]]*)]\([^)\s]*\)`)
	// The PR an error names says nothing on the PR's own page.
	gateErrorPR = regexp.MustCompile(`^Failed to merge PR #\d+: `)
	// Runs started by a Safe-Merge in the ultimate repository rather than by a command on this PR.
	gateExternal = "The quality gate was triggered by Safe-Merge of the"

	commandRejected = regexp.MustCompile(`(?s)^Command rejected:\s*(.+)$`)
	// Rejection reasons only reviewers resolve; any other one is the author's to fix:
	// conflicts, fixup!/amend!/squash! commits, a draft, a coordinator build, the unknown.
	reviewersRejections = []string{"Missing code owners approval"}
	// Rejections and errors for conflicts with the base branch: "Pull request has conflicts
	// with the base branch …", "rebase-merge failed: Pull Request has merge conflicts",
	// "… can't be rebased onto master due to conflicts.", "Either new conflicts appeared while
	// the quality gate was running …".
	conflictsReason  = regexp.MustCompile(`(?i)\b(?:has (?:merge )?conflicts|due to conflicts|new conflicts appeared)\b`)
	codeOwnersMarker = "<!-- CODE_OWNERS_REVIEW_COMMENT -->"
	// Replies of the code-owners bot that report success rather than a failure.
	successReplies = []string{"Private aggregate run triggered at ", "Cherry-pick to `"}

	slashCommand = regexp.MustCompile(`^/[a-z-]+(?:\s|$)`)

	// The body of a PR /cherry-pick opened: the original PR and its author's mention.
	// Real logins have no underscore; synthetic test logins do.
	cherryPickBody = regexp.MustCompile(`^Original pull request: (https://github\.com/[^/\s]+/[^/\s]+/pull/(\d+)) by @([A-Za-z0-9_](?:[A-Za-z0-9_-]*[A-Za-z0-9_])?)$`)
)

// ParseCherryPick reads the first line of a cherry-pick's body, as the bot writes it:
// "Original pull request: https://github.com/JetBrains/kotlin/pull/123 by @alice".
// It returns the original PR and its author's login.
func ParseCherryPick(body string) (model.Original, string, bool) {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	m := cherryPickBody.FindStringSubmatch(strings.TrimSpace(first))
	if m == nil {
		return model.Original{}, "", false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return model.Original{}, "", false
	}
	return model.Original{Number: n, URL: m[1]}, m[3], true
}

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
	// Error is the gate's `Error:` line, which fails even a passed run.
	Error string
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
	// The bot adds a line per attempt and keeps the earlier ones: the last is the latest.
	if all := gateRetry.FindAllStringSubmatch(body, -1); all != nil {
		m := all[len(all)-1]
		r.Reason = "retry " + m[2] + "/" + m[3]
	}
	switch {
	case gatePassed.MatchString(body):
		r.State = model.RunPassed
	case gateFailed.MatchString(body):
		r.State = model.RunFailed
	}
	if reason, ok := gateFailure(body); ok {
		r.State, r.Error = model.RunFailed, reason
	}
	return r, true
}

// ParseAbort reads a KotlinBuild comment the coordinator posted for a run it couldn't
// start: no build, so no gate line. Like "Changes in the corresponding Merge-Request can't
// be rebased onto master due to conflicts. … trigger Safe-Merge again." with the failed git
// command as `Error: …`, "ultimate contains a branch named …, but an open Merge-Request/PR
// is not found. … then trigger Safe-Merge again." with no error, or an `Error: …` alone. It
// returns the reason, read like a gate's error.
func ParseAbort(body string) (string, bool) {
	if gateStarted.MatchString(body) {
		return "", false
	}
	if reason, ok := gateFailure(body); ok {
		return reason, true
	}
	if first, _, _ := strings.Cut(body, gateSeparator); strings.Contains(first, gateAbort) {
		return firstSentence(first), true
	}
	return "", false
}

// gateFailure is the reason of the coordinator's `Error:` line. A message it posted right
// before the error says it best ("Either new conflicts appeared while the quality gate was
// running …", where the error is the failed git command); otherwise the error itself.
func gateFailure(body string) (string, bool) {
	parts := strings.Split(body, gateSeparator)
	for i, part := range parts {
		m := gateError.FindStringSubmatch(part)
		if m == nil {
			continue
		}
		if i > 0 && isCoordinatorMessage(parts[i-1]) {
			return firstSentence(parts[i-1]), true
		}
		reason := gateErrorPR.ReplaceAllString(m[1], "")
		if slices.ContainsFunc(gateGenericErrors, func(prefix string) bool { return strings.HasPrefix(reason, prefix) }) {
			reason = gateGenericError
		}
		return reason, true
	}
	return "", false
}

// isCoordinatorMessage: a part of the gate comment that is none of the gate's own lines.
func isCoordinatorMessage(part string) bool {
	return strings.TrimSpace(part) != "" && !strings.Contains(part, dryRunBanner) &&
		!slices.ContainsFunc([]*regexp.Regexp{gateStarted, gateRetry, gatePassed, gateFailed, gateError},
			func(re *regexp.Regexp) bool { return re.MatchString(part) })
}

// firstSentence is a message's first sentence as plain text: its links become their text.
func firstSentence(message string) string {
	text := strings.Join(strings.Fields(markdownLink.ReplaceAllString(message, "$1")), " ")
	if i := strings.Index(text, ". "); i >= 0 {
		return text[:i+1]
	}
	return text
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

// ForConflicts reports whether a rejection's reason or a gate's error is the PR's
// conflicts with the base branch, which rule 1a reports anyway.
func ForConflicts(reason string) bool {
	return conflictsReason.MatchString(reason)
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
