package classify

import (
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

func TestParseGate(t *testing.T) {
	tests := []struct {
		name, pr, build string
		want            GateReport
	}{
		{
			name: "dry-run passed", pr: "90005", build: "build/1076451184",
			want: GateReport{Kind: model.DryRun, State: model.RunPassed, BuildURL: "https://buildserver.labs.intellij.net/build/1076451184"},
		},
		{
			name: "dry-run failed", pr: "90006", build: "build/1076982024",
			want: GateReport{Kind: model.DryRun, State: model.RunFailed, BuildURL: "https://buildserver.labs.intellij.net/build/1076982024"},
		},
		{
			name: "dry-run failed after a cancelled run", pr: "90008", build: "build/1077216029",
			want: GateReport{Kind: model.DryRun, State: model.RunFailed, BuildURL: "https://buildserver.labs.intellij.net/build/1077216029"},
		},
		{
			name: "dry-run without a result (cancelled)", pr: "90008", build: "build/1076717920",
			want: GateReport{Kind: model.DryRun, State: model.RunRunning, BuildURL: "https://buildserver.labs.intellij.net/build/1076717920"},
		},
		{
			name: "safe-merge passed", pr: "90004", build: "build/1076441911",
			want: GateReport{Kind: model.SafeMerge, State: model.RunPassed, BuildURL: "https://buildserver.labs.intellij.net/build/1076441911"},
		},
		{
			name: "safe-merge failed after a retry", pr: "90004", build: "build/1076214625",
			want: GateReport{Kind: model.SafeMerge, State: model.RunFailed, BuildURL: "https://buildserver.labs.intellij.net/build/1076214625", Reason: "retry 1/1"},
		},
		{
			name: "safe-merge passed and merged", pr: "90002", build: "build/1077354565",
			want: GateReport{Kind: model.SafeMerge, State: model.RunPassed, BuildURL: "https://buildserver.labs.intellij.net/build/1077354565"},
		},
		{
			name: "started by an ultimate Merge-Request", pr: "90001", build: "build/1072415381",
			want: GateReport{Kind: model.DryRun, State: model.RunRunning, BuildURL: "https://buildserver.labs.intellij.net/build/1072415381", External: true},
		},
		{
			name: "this PR's run mentioning the ultimate Merge-Request", pr: "90001", build: "build/1072585435",
			want: GateReport{Kind: model.DryRun, State: model.RunPassed, BuildURL: "https://buildserver.labs.intellij.net/build/1072585435"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseGate(commentBody(t, tt.pr, "KotlinBuild", tt.build))
			if !ok {
				t.Fatal("not recognized as a gate comment")
			}
			if got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// The bot appends an error to a passed gate's comment when it can't act on the result,
// like a safe-merge whose gate passed but whose merge failed.
func TestParseGateError(t *testing.T) {
	const mergeConflicts = "\n\n---\n\nError: Failed to merge PR #90004: rebase-merge failed: Pull Request has merge conflicts"
	tests := []struct {
		name, body string
		want       GateReport
	}{
		{
			name: "safe-merge passed, then failed to merge",
			body: commentBody(t, "90004", "KotlinBuild", "build/1076441911") + mergeConflicts,
			want: GateReport{Kind: model.SafeMerge, State: model.RunFailed, BuildURL: "https://buildserver.labs.intellij.net/build/1076441911",
				Error: "rebase-merge failed: Pull Request has merge conflicts"},
		},
		{
			name: "dry-run passed with an error",
			body: commentBody(t, "90005", "KotlinBuild", "build/1076451184") + "\n\n---\n\nError: Something new.\n",
			want: GateReport{Kind: model.DryRun, State: model.RunFailed, BuildURL: "https://buildserver.labs.intellij.net/build/1076451184",
				Error: "Something new."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseGate(tt.body)
			if !ok || got != tt.want {
				t.Errorf("got %+v, %v\nwant %+v", got, ok, tt.want)
			}
		})
	}
}

func TestParseGateRunningRetry(t *testing.T) {
	// A retry in flight: the failed-run comment of 90004 before the bot appended the result.
	body := commentBody(t, "90004", "KotlinBuild", "build/1076214625")
	body = body[:len(body)-len("\n\n---\n\nQuality gate failed. See https://buildserver.labs.intellij.net/build/1076214625 to get full insight.")]
	got, ok := ParseGate(body)
	if !ok || got.State != model.RunRunning || got.Reason != "retry 1/1" {
		t.Errorf("got %+v, %v; want running with retry 1/1", got, ok)
	}
}

func TestParseGateIgnoresOtherComments(t *testing.T) {
	for _, body := range []string{
		commentBody(t, "90005", "kotlin-safemerge", "CODE_OWNERS_REVIEW_COMMENT"),
		commentBody(t, "90004", "kotlin-safemerge", "Command rejected"),
		"/dry-run",
	} {
		if got, ok := ParseGate(body); ok {
			t.Errorf("ParseGate(%.40q) = %+v, want not a gate comment", body, got)
		}
	}
}

func TestParseRejection(t *testing.T) {
	tests := []struct{ pr, want string }{
		{"90004", "Missing code owners approval - verify the check 'Code Owners Approval' is green."},
		{"90007", "A Coordinator build is already in progress for this pull request. Please wait for it to finish or cancel it with `/cancel-coordinator`"},
	}
	for _, tt := range tests {
		got, ok := ParseRejection(commentBody(t, tt.pr, "kotlin-safemerge", "Command rejected"))
		if !ok || got != tt.want {
			t.Errorf("pr-%s: got %q, %v; want %q", tt.pr, got, ok, tt.want)
		}
	}
	if _, ok := ParseRejection(commentBody(t, "90005", "kotlin-safemerge", "CODE_OWNERS_REVIEW_COMMENT")); ok {
		t.Error("the code-owners comment is not a rejection")
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		body string
		want Command
		ok   bool
	}{
		{"/dry-run", CmdDryRun, true},
		{"/dry-run --retry", CmdDryRun, true},
		{"  /safe-merge\n", CmdSafeMerge, true},
		{"/safe-merge --fixup=false", CmdSafeMerge, true},
		{"/safe-squash-merge --title=\"Fix it\" --message='Body'", CmdSafeSquashMerge, true},
		{"/cancel-coordinator ", CmdCancel, true},
		{"/test-public", CmdTestPublic, true},
		{"/test-private", CmdTestPrivate, true},
		{"/codeowners", CmdCodeOwners, true},
		{"/fixup", CmdFixup, true},
		{"/cherry-pick --target=2.3.20", CmdCherryPick, true},
		{"/review", CmdReview, true},
		{"/dry-run\nwith a note below", CmdDryRun, true},
		// Not bot commands.
		{"/final", "", false},
		{"/dry-runner", "", false},
		{"/dry-run\t--retry", "", false},
		{"please /dry-run", "", false},
		{"Comment text.", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseCommand(tt.body)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseCommand(%q) = %q, %v; want %q, %v", tt.body, got, ok, tt.want, tt.ok)
		}
	}
	if !IsCommand("/final") || IsCommand("Comment text.") {
		t.Error("IsCommand: /final is a slash command, prose is not")
	}
	for cmd, want := range map[Command]model.RunKind{CmdDryRun: model.DryRun, CmdSafeMerge: model.SafeMerge, CmdSafeSquashMerge: model.SafeMerge} {
		if kind, ok := cmd.RunKind(); !ok || kind != want {
			t.Errorf("%s runs a %v, want %v", cmd, kind, want)
		}
	}
	for _, cmd := range []Command{CmdTestPublic, CmdTestPrivate, CmdCodeOwners, CmdFixup, CmdCancel, CmdCherryPick, CmdReview} {
		if _, ok := cmd.RunKind(); ok {
			t.Errorf("%s starts no dry-run or safe-merge", cmd)
		}
	}
}

// Every reply the bot posts when a command fails, as it appears on PRs.
func TestParseFailure(t *testing.T) {
	tests := []struct{ body, want string }{
		{"Command rejected: Pull request is not open.", "Pull request is not open."},
		{"Command rejected: This command cannot be issued on a draft pull request.", "This command cannot be issued on a draft pull request."},
		{"Command rejected: Missing code owners approval - verify the check 'Code Owners Approval' is green.", "Missing code owners approval - verify the check 'Code Owners Approval' is green."},
		{"Command rejected: Command cannot be used on release branches.", "Command cannot be used on release branches."},
		{"Command rejected: GitHub has not yet finished checking for conflicts with the base branch.", "GitHub has not yet finished checking for conflicts with the base branch."},
		{"Command rejected: Pull request has conflicts with the base branch that must be resolved before merging.", "Pull request has conflicts with the base branch that must be resolved before merging."},
		{"Command rejected: A Coordinator build is already in progress for this pull request. Please wait for it to finish or cancel it with `/cancel-coordinator`", "A Coordinator build is already in progress for this pull request. Please wait for it to finish or cancel it with `/cancel-coordinator`"},
		{"Command rejected: Commit 'fixup!' is currently [not supported by GitHub](https://github.com/orgs/community/discussions/40804) - please try `/fixup` command or `/safe-squash-merge`", "Commit 'fixup!' is currently [not supported by GitHub](https://github.com/orgs/community/discussions/40804) - please try `/fixup` command or `/safe-squash-merge`"},
		{"Command rejected: Found orphaned fixup! commit(s) with no matching target commit on the branch:\n- `fixup! Example change`", "Found orphaned fixup! commit(s) with no matching target commit on the branch:\n- `fixup! Example change`"},
		{"Command rejected: Only JetBrains organization members can issue this command.", "Only JetBrains organization members can issue this command."},
		{"Command rejected: Missing required parameter --target.", "Missing required parameter --target."},
		{"Unrecognized parameters: force", "Unrecognized parameters: force"},
		{"Unable to parse the issued command.", "Unable to parse the issued command."},
		{"Couldn't reach TeamCity - please try again in a few minutes.", "Couldn't reach TeamCity - please try again in a few minutes."},
		{"Failed to process command due to an unexpected exception.", "Failed to process command due to an unexpected exception."},
		{"Autosquash aborted: Conflict detected in file(s): compiler/A.kt. Please rebase manually.", "Autosquash aborted: Conflict detected in file(s): compiler/A.kt. Please rebase manually."},
	}
	for _, tt := range tests {
		got, ok := ParseFailure(tt.body)
		if !ok || got != tt.want {
			t.Errorf("ParseFailure(%.50q) = %q, %v; want %q", tt.body, got, ok, tt.want)
		}
	}
	for _, body := range []string{
		commentBody(t, "90005", "kotlin-safemerge", codeOwnersMarker),
		"Private aggregate run triggered at https://buildserver.labs.intellij.net/build/1 — use this link to monitor results.",
		"Cherry-pick to `2.3.20` triggered at https://buildserver.labs.intellij.net/build/2",
	} {
		if reason, ok := ParseFailure(body); ok {
			t.Errorf("ParseFailure(%.50q) = %q, want no failure", body, reason)
		}
	}
}
