# gh-kotlin-prs: spec (draft)

`gh kotlin-prs` is a `gh` extension in Go. It shows the open PRs in JetBrains/kotlin that you're involved in: their quality-gate
status, where the review stands, and who has the next move. It can trigger `/dry-run` and `/safe-merge`. A personal alias keeps typing short:
`gh alias set kp kotlin-prs`.

Repo: `dimonchik0036/gh-kotlin-prs` (public). Kotlin specifics (repo, bot logins, comment patterns) are config defaults, not hard-coded.

## 1. Scope

Two sections:
- **Mine:** `is:pr is:open author:@me`.
- **Review:** PRs where you're a reviewer, from three searches merged and deduplicated:
  - `user-review-requested:@me`: requested from you personally;
  - `reviewed-by:@me -author:@me`: you reviewed earlier, and GitHub drops you from `review-requested` after a review;
  - `review-requested:@me` minus the personal ones: the request went to a team you're in (for example `kotlin-analysis-api`), not to you.
    GitHub's round-robin auto-assignment usually swaps the team for one member, so these are rare.

Then, at the end, two optional groups. Both are shown by default, hidden when empty, and can be switched off:
- **Team requests:** the team-only requests above (`--no-teams`, TUI `t`).
- **Recently merged:** your PRs merged in the last 24h, confirming that a safe-merge landed (`--no-merged`, TUI `M`).
  The search is `is:pr is:merged author:@me merged:>=<UTC day of now-24h>`, the same query all day (so it can be
  cached, §13), and the result is cut to the last 24h with the current clock.

Drafts are shown in Mine (marked) and hidden in Review unless `--all`.

Review lists only what waits on you (§5). Everything else is hidden, and the section ends with one faint line
such as `+4 reviews not waiting on you, 1 draft (--all)`.

## 2. Data

- Auth and transport: `go-gh` (`api.NewGraphQLClient` with gh's default host and its token), so the token is `gh`'s.
  The token is never stored; the responses are, in the cache (§13).
- Two GraphQL requests per refresh. The first runs every section's `search(type: ISSUE, query: …, first: 50)` and returns
  only PR numbers: search connections are charged by page size, so inlining the details would cost ~50 points per search.
  The second fetches the unique PRs by alias (`pr90005: pullRequest(number: 90005) { ...PR }`), ~1 point per PR. Per PR:
  - `number title url isDraft author headRefName headRefOid createdAt updatedAt`
  - `reviewRequests(first: 20) { requestedReviewer { ... on User { login } ... on Team { slug } } }`
  - `latestOpinionatedReviews(first: 20)` and `reviews(last: 30) { author state submittedAt comments { totalCount } }`
  - `reviewThreads(last: 50) { isResolved isOutdated path firstComment: comments(first: 1) { author body createdAt url } comments(last: 5) { author createdAt } }`
  - `comments(last: 40) { author body createdAt updatedAt }`. The bots edit their comments in place, so `updatedAt` matters.
  - `timelineItems(last: 5, itemTypes: [PULL_REQUEST_COMMIT, HEAD_REF_FORCE_PUSHED_EVENT, REVIEW_REQUESTED_EVENT])`, for the time of the last push and of the last review request.
  - `commits(last: 1) { commit { statusCheckRollup { contexts(first: 20) { ... on CheckRun { name conclusion status } ... on StatusContext { context state targetUrl } } } } }`
  - `history: commits(last: 30) { commit { message } }`, for the issue trailers (no measurable cost: ~40 points for 27 PRs before and after).
- Issues: `^PROJ-N` trailer lines in the commit messages (`^KT-123 Fixed`, `^KT-123 Obsolete`, bare `^KT-123` =
  related; several per commit), then IDs in the branch name, then in the title. The projects come from `issueProjects`
  (`KT`, `KTIJ`, `KTI`), matched case-insensitively and upper-cased. One entry per ID, a trailer's resolution wins;
  order: fixed trailers, obsolete, related, branch, title.
- Last push time: the latest `HeadRefForcePushedEvent`, else the `committedDate` of the last commit. A rebase resets the committer date, so this is close enough.
- Budget: about one point per PR plus one per refresh (~40 for 27 PRs), within 5000/h even with refreshes every minute. Add `--debug` to print the query cost, or the cache entry that answered instead (§13).

## 3. Bot protocol

KotlinBuild posts one comment per quality-gate run and **edits it** when the run ends:
- `**THIS IS A DRY RUN**` at the top marks a dry-run. Without it, the run is a safe-merge.
- `Quality gate is triggered at <tc-url>` gives the TeamCity build. `Commit: [abc1234](…)` is the merge-ref commit, not the head.
- `Triggered a [retry attempt](<url>) №N out of M.` means a retry is running.
- Result lines:
  - `Quality gate finished successfully.` → passed;
  - `Quality gate failed. See <url> …` → failed;
  - neither → running.

kotlin-safemerge posts the code-owners table and answers commands.

**Code owners.** The table is a comment marked with `<!-- CODE_OWNERS_REVIEW_COMMENT -->`, created when the PR opens and
edited in place. The same text is the summary of the `Code Owners Approval` check run on the head commit, titled
"All code owners have approved" (success) or "Waiting for code owner approvals" (failure); the check is always completed.
We read the check first: the comment is old, so on a busy PR it falls out of `comments(last: 40)`.
- One row per group of rules with the same owners: the rules (`<code>` paths, comma-separated, with zero-width spaces
  after `/`, `.`, `_` and `-`), the owners and the approval.
- Owners: individual users first, comma-separated, then teams. A team is a `<details>` with the team link in its
  `<summary>` and its members and nested teams as `<li>`, or a bare team link when it has neither. A member can be
  followed by `(QA)` or `(PM)`, and by `⏳` when unavailable. A rule without owners (`#NO_OWNERS`) has an empty cell and
  takes any reviewer's decision.
- Approval: a mark, then the people it refers to:
  - `❌` no decision: the owners currently requested as reviewers, or `UNASSIGNED`;
  - `🔄` an owner only commented and isn't requested again: they owe a decision and need a re-request;
  - `🔴` an owner requested changes (wins over the other marks of the row);
  - `✅` every rule of the row approved: the approvers, `🔒` after those whose approving review contains `/final`;
  - `⏳` after anyone listed who's unavailable.
- Refreshed on pushes, submitted and dismissed reviews, review requests and their removal, base retargeting,
  `/codeowners` and re-runs of the check. An unchanged table isn't re-posted.
- **The table and the check are the source of truth.** The bot resolves team hierarchies (an approval from a
  `kotlin-jvm` member satisfies `kotlin-compiler`), which we don't recompute.
- The comment also carries the "PR commands for maintainers" table and, for tooling, the same catalog as
  `<!-- KPR_COMMANDS:<base64 JSON> -->`. Nothing machine-readable reports a command's state; that comes from the replies below.

**Commands.** The bot reads a command from a regular PR comment whose first line is the command alone or followed by a space
and parameters (`--flag`, `--flag=false`, `--key=value`, `--key="quoted value"`). It ignores edited comments, comments
in reviews, and comments from accounts without write access (no reaction, no reply).
- Once it has dispatched a command it adds 🚀 to the comment. For `/dry-run` and `/safe-merge` that means the build is
  queued; at that moment it minimizes ("resolved") every earlier KotlinBuild comment, so a minimized gate comment
  without a result was superseded. KotlinBuild's own comment follows, usually within ~3 min.
- When it can't, it adds no reaction and posts one reply:
  - `Command rejected: <reason>`, for example "Pull request is not open.", "This command cannot be issued on a draft
    pull request.", "Missing code owners approval - verify the check 'Code Owners Approval' is green.", "Command cannot be
    used on release branches.", "GitHub has not yet finished checking for conflicts with the base branch.", "Pull request
    has conflicts with the base branch that must be resolved before merging.", "A Coordinator build is already in progress
    for this pull request. …", "Commit 'fixup!' is currently [not supported by GitHub](…) - please try `/fixup` command or
    `/safe-squash-merge`" (likewise `amend!` and `squash!`), "Found orphaned fixup! commit(s) …" (with a list of commits),
    "Only JetBrains organization members can issue this command.", "Missing required parameter --target.";
  - or a plain message: "Unable to parse the issued command." (e.g. `/dry-runx`), "Unrecognized parameters: …",
    "Autosquash aborted: …", "Couldn't reach TeamCity - please try again in a few minutes.", "Failed to process command
    due to an unexpected exception.".
- So a reply answers the latest command without 🚀. It's that command's outcome: a rejected dry-run or safe-merge,
  which stays the latest run of its kind until a newer command or run of that kind, even if a build it collided with
  finishes later. It's never outdated.
- `/test-private` replies "Private aggregate run triggered at <url> — use this link to monitor results.", `/cherry-pick`
  replies "Cherry-pick to `<ver>` triggered at <url>". `/review` belongs to another bot: kotlin-safemerge neither reacts
  nor replies.
- `/cancel-coordinator` gets only 🚀, even when nothing runs. The cancelled build's KotlinBuild comment never gets a result.

Our states:
- A `/dry-run` or `/safe-merge` with no reaction and no reply → **requested**; after 10 min → **requested, no response**.
- 🚀 but no KotlinBuild comment yet → **accepted**.
- A gate comment without a result is **running** only while it's the newest one, isn't minimized, and no dispatched
  `/cancel-coordinator` came after it; otherwise **cancelled**. A dispatched `/cancel-coordinator` also cancels an accepted run.
- Gate comments saying "The quality gate was triggered by Safe-Merge of the [corresponding Merge-Request] in ultimate"
  belong to an ultimate Merge-Request, not to this PR's commands, and are ignored.

Checks:
- `Code Owners Approval` (CheckRun): SUCCESS or FAILURE, with the table in its summary.
- `Safe-Merge Coordinator (Kotlin Dev)` (StatusContext).

Query: `comments { authorAssociation reactions(content: ROCKET, first: 5) { nodes { user { login } } } }` and
`... on CheckRun { name conclusion title summary }`.

A successful safe-merge merges the PR, so it moves to Recently merged.

| Command | Effect |
|---|---|
| `/dry-run [--retry]` | Safe-merge Coordinator in dry-run: full checks on top of the latest master. `--retry` reruns the failed build once, to rule out flakiness. Not allowed on release branches. |
| `/safe-merge [--fixup=false]` | Coordinator in rebase-and-merge mode. Autosquashes `fixup!` commits and force-pushes first (`--fixup`, on by default). Requires a green `Code Owners Approval`, no draft, no conflicts. |
| `/safe-squash-merge [--title=…] [--message=…]` | Coordinator in squash mode. The title and body default to the PR title and description. |
| `/cancel-coordinator` | Cancels the coordinator that's running (dry-run or safe-merge). |
| `/codeowners` | Re-runs the code-owners check, for when it failed unexpectedly or is missing. |
| `/fixup` | Autosquashes `fixup!` commits and force-pushes. Not supported for PRs from forks. |
| `/test-public` | Public Aggregate without rebasing (the release Aggregate on release branches). |
| `/test-private` | Private Aggregate without rebasing. JetBrains organization members only. |
| `/cherry-pick --target=<ver>` | Opens a PR from branch `rrr/<target>/<branch>`. Works on open and merged PRs. |
| `/review` | The auto code review, run by another bot. |

v1 only parses dry-run and safe-merge runs. How the bot reports the result of `/test-public` is still unknown, so it's out of scope for now.

**Staleness:** a run triggered before the last push is shown as `outdated`. The result is kept, but greyed out.

**Review dismissal**:
- The bot dismisses only approvals of owners whose files the new changes touch, with "New changes affect files owned by
  this reviewer. Re-review is required.", and requests their review again.
- A rebase, squash, commit split or message amend dismisses nothing, and `/final` reviews are never dismissed.
- Re-requesting a review only notifies the reviewer and doesn't invalidate an approval.
- So GitHub's review states (including `DISMISSED`) are authoritative, and we compute no staleness for approvals.

Top-level comments can be "resolved" (minimized) with the PR Helper browser extension. Minimized comments (`isMinimized`) are ignored by the rules.

Mandatory gates for merging: at least one approval, plus code-owner approvals. CI runs aren't required, because the safe-merge runs the full set of checks anyway.

## 4. Model

```go
type Run struct {
    Kind      RunKind   // DryRun | SafeMerge
    State     RunState  // None | Requested | Accepted | Running | Passed | Failed | Rejected | Cancelled
    Reason    string    // reject reason, retry info
    BuildURL  string
    Started   time.Time
    Updated   time.Time
    Outdated  bool      // triggered before the last push
}
type Reviewer struct {
    Login     string
    Team      string    // non-empty for team requests
    State     ReviewerState // Pending | Approved | ChangesRequested | Commented | Dismissed
    Final     bool      // 🔒
    Unavailable bool    // ⏳
    At        time.Time
    CodeOwner bool
}
type PR struct {
    Number, Title, URL, Author, Branch string
    Draft            bool
    Section          Section    // Mine | Review
    Issues           []Issue    // {ID, URL, Source: trailer|branch|title, Resolution: fixed|obsolete|""}, primary first
    LastPush         time.Time
    DryRun, SafeMerge Run       // latest of each kind
    Runs             []Run      // history, newest first
    Reviewers        []Reviewer
    CodeOwners       CodeOwnersStatus // OK | Missing(rules…) | Unknown
    UnresolvedThreads int
    Next             NextAction // Me | Reviewers | CI | Author | Done
    Reasons          []Reason   // {Text, URL}: human-readable, first = primary; URL = the page that shows it
}
```

The JSON output is this model plus `"version": 1`. Any UI reads it as its contract; the version is bumped only on a
breaking change (RELEASING.md).

## 5. "Whose move" rules

Bots (`KotlinBuild`, `kotlin-safemerge`, `*[bot]`) are ignored in every "someone commented" rule.
Slash-command comments (`/dry-run`, `/safe-merge`, …) are nobody's "comment": they're never a new comment and never an
author's reply to a review.
`myLastActivity` is the latest of: last push, my last comment, my last slash command, my last review, my last thread
reply.

**Mine:** the first rule that matches sets `Next`. Every rule that matches is added to `Reasons`.
1. **Me:** the latest dry-run or safe-merge failed (and isn't outdated), was rejected, or got no response
   → "safe-merge failed 2h ago", "dry-run rejected: <reason>".
2. **Me:** a reviewer's latest opinionated review is CHANGES_REQUESTED and newer than my last push.
2a. **Me:** a code owner is marked `🔄` (commented and needs a re-request) → "re-request review from alice_user"; one
    reason lists every such owner: "re-request review from alice_user, bob_user".
2b. **Me:** a missing code-owner rule where every listed owner who hasn't reviewed is `⏳` → "all owners for /analysis/ unavailable".
2c. **Me:** a missing code-owner rule nobody was asked for: the bot shows `UNASSIGNED`, the rule has owners besides me,
    and none of them, nor a team of it, is requested now (the table lags behind a request, so the live requests
    decide) → "assign reviewers for /analysis/"; one reason lists every such rule's first path. A rule without owners
    (`#NO_OWNERS`) isn't one: any reviewer's decision covers it.
3. **Me:** an unresolved, non-outdated thread whose last comment isn't mine.
4. **Me:** a non-bot comment from someone else after `myLastActivity`. One event gives one reason: comments in threads
   that rule 3 reports, and reviews that consist of thread comments, don't count again. A comment, commenting review
   or thread comment whose author submitted an APPROVED review at the same time or later doesn't count: the approval
   is their last word (an unresolved thread awaiting my reply is still rule 3).
5. **CI:** a run is requested or running.
6. **Me (ready):** `Code Owners Approval` is green, there's at least one approval, and no run is requested or running → "ready to /safe-merge".
   A missing or outdated green dry-run doesn't block this (CI isn't a gate), but it's added as a hint: "no fresh dry-run".
7. **Reviewers:** pending requests or missing code owners → "waiting: alice_user, bob_user": the requested people and
   teams, then the assignees of the missing rules (not the `🔄` ones of 2a, nor the ones of 2 who requested changes
   since my push), or "waiting: code owners" when the check fails and nothing else names anyone (and 2c doesn't fire).
8. **Me:** none of the above, for example no dry-run yet → "no dry-run yet".

**Review:** only a request brings a PR to me. Author pushes, author replies and thread replies don't: they arrive by email.
`show` still lists the threads, for information.
1. **Me:** requested from me personally, and I have no review after the latest request event. This covers re-requests.
2. **Done:** I approved. Hidden until I'm re-requested, whatever happened since.
3. **Author:** anything else, e.g. I commented or requested changes and wasn't re-requested. Hidden unless `--all`.

Every rule lives in one `classify` package, with a test per rule.

## 6. CLI

```
gh kotlin-prs [--pr <number> [--post <command>]]  # TUI on a TTY, otherwise `list` (§12)
gh kotlin-prs list [--mine|--review] [--waiting-on-me] [--all] [--no-teams] [--no-merged]
                   [--format table|json|swiftbar] [--max-age DURATION]
gh kotlin-prs show <number> [--max-age DURATION]  # details: reviewers, run history, threads, reasons
gh kotlin-prs config [path|init [--force]]  # effective config with sources, its path, a commented template
gh kotlin-prs run <number> <command> [--yes]  # post a bot command (§8): dry-run, dry-run-retry, safe-merge,
                                              # cancel-coordinator, fixup, codeowners
gh kotlin-prs run <number> request-review [login...] [--yes]  # request a review from code owners (§8)
gh kotlin-prs swiftbar install [--dir D] [--interval 3m] [--force]  # the menu-bar plugin (§15)
gh kotlin-prs swiftbar script [--interval 3m]  # its script, for a manual install
gh kotlin-prs open <number>        # browser
```

- Table rows: `❯ #90005  KT-990003 +1 <title⋯>  DR ✓  SM -  1/2 ✓  2 threads  <primary reason>`. Rows where `Next = Me` come first in each section.
  The issue column is the primary issue plus how many more; `show` lists them all: `KT-1 (fixed) ∙ KT-2 (related) ∙ KT-3 (branch)`.
  `1/2 ✓` is approvals out of the people reviewing, then the code-owners verdict.
- Symbols are single-width text characters only: no emoji, nothing East-Asian-ambiguous, since terminals draw those
  double-width and break the columns. `--icons ascii` (config `icons`) switches to pure ASCII. The legend is in `--help`
  and the README.
- `show` spells out the code-owner bot's marks instead of symbols. A rule line starts with ✓ / ✗ (ascii `+` / `x`),
  the people line under it says the mark: `❌` → `no review: erin_user` (`no reviewer assigned` without assignees),
  `🔄` → `commented, review not re-requested: alice_user, bob_user`, `🔴` → `changes requested: bob_user`, `✅` →
  `approved: trent_user (final), carol_user`, then `∙ owners: …`. `🔒` / `⏳` become `(final)` / `(unavailable)`
  after the login, `(final, unavailable)` if both. Reviewer flags are words too: `code owner, final`,
  `code owner, needs a re-request`, `unavailable`.
- Reasons link the page that shows them: a failed or running run → its build; a rejection or bot failure reply → that
  reply; "requested, no response" and "accepted" → the command; "changes requested by X" → that review; "re-request review
  from X" → the PR; unresolved threads → the newest one's last comment; a new comment → that comment. "waiting: …" has no link.
  The DR / SM cells link their run's build, or its comment before a build exists (a ⊘ links the rejection).
- Hyperlinks (OSC 8): the PR number links the PR, issue IDs link `issueURL`; in `show` also builds, reviewer logins and
  teams, and threads (their first comment). `--hyperlinks auto|always|never` (config `hyperlinks`): auto links only when
  stdout is a terminal, JSON never. Widths and truncation treat the escapes as zero-width; without a terminal, `always`
  keeps the links and drops the colors. Linked text is underlined and blue unless it has a color of its own; status
  symbols inside a link (the ✗ of `DR ✗`, a `~` prefix) stay clickable but keep exactly their own style.
- With links on, `show` prints no raw URLs: a build is a short label linked to it, `build <id>` from the URL's last
  path segment (the URL without scheme and host if that isn't a number), and the header drops the PR URL line, the
  `#N` being its link. With links off, both URLs stay in full: plain output is the only way to get them there.
- Exit codes: 0 ok, 1 error, 2 bad usage.

## 7. TUI (bubbletea + bubbles + lipgloss)

- **List screen:** the sections with the same rows as the table (the section flags of the bare command apply), the
  selected row in reverse video, plus a status bar showing the last refresh, errors and the rate limit (§12). On a
  narrow terminal the reason column narrows first, then the title, down to 20 and 16 columns; past that lines are cut.
- **Detail pane** (`enter`, `esc` back): the `show` view at the terminal's width, scrollable (`j`/`k`, `pgup`/`pgdown`).
  A merged row comes from the search, so its detail fetches the PR in full. It shows:
  - all reviewers with state and time;
  - code-owner rules with what's missing;
  - run history with TeamCity links;
  - unresolved threads (author, path, first line);
  - all reasons.
- **Keys:** `↑↓/jk` select (`g`/`G` first/last), `enter` details, `o` open the PR, `b` open its newest build (or the
  bot comment before a build exists), `y` copy its URL (system clipboard, else OSC 52), `r` refresh, `/` filter (over
  the cells' text; `enter` keeps it, `esc` clears it), `tab`/`shift+tab` next/previous section, `a` toggle `--all`,
  `?` help with the symbol legend (scrollable), `q` quit; `ctrl+c` always quits. The commands of §8, in the list and
  in the details: `D` dry-run, `R` dry-run --retry, `M` safe-merge, `C` cancel-coordinator, `F` fixup, `O` codeowners,
  and `x` for a menu of the ones that can be posted now. Config `keys` rebinds them by action (`config.Actions`: up,
  down, first, last, pageUp, pageDown, nextSection, previousSection, details, back, filter, all, open, build, copy,
  actions, dryRun, dryRunRetry, safeMerge, cancelCoordinator, fixup, codeowners, refresh, help, quit): a key or a list
  replaces that action's keys. An unknown action, an action without keys, or a key bound twice is a config error.
  Inside the filter, enter and esc are fixed.
- **Refresh:** in the background every `refresh` (3m) and on `r`. The UI never blocks while a fetch runs; a spinner
  shows it. One fetch at a time: `r` during a refresh only notes "already refreshing", and the timer waits for it. `r`
  within 5s of a successful refresh notes "refreshed 2s ago" instead; after a failed one it retries at once. Rows are classified again every 30s between refreshes, so their ages keep moving.
- **Actions:** see §8. A command's key asks `Post /safe-merge to #90006 (title⋯)? [y/N]` in the status bar: only `y`
  posts, any other key (enter and esc too) cancels with "not posted". A command that can't be posted now says why
  instead of asking. `x` lists the commands that can be posted on the PR now, each with its key; its key, or the
  movement keys and enter, picks one, which then asks the same way; any other key closes it.

## 8. Actions and safety

- An action posts one of the bot's commands as a regular PR comment with exactly the command's text
  (`POST /repos/{repo}/issues/{n}/comments` with gh's token, never a review comment), so everyone sees it, or requests
  a review (below). Those are the only writes the tool makes: `list`, `show`, `config` and the TUI's refreshes never
  write.
- The commands (`internal/actions`):

  | Name | Comment | What the bot does |
  |---|---|---|
  | `dry-run` | `/dry-run` | runs the checks on the PR rebased onto the latest master, without merging |
  | `dry-run-retry` | `/dry-run --retry` | the same, restarting once when it fails |
  | `safe-merge` | `/safe-merge` | the same checks, then a rebase-merge into master; `fixup!` commits are squashed first |
  | `cancel-coordinator` | `/cancel-coordinator` | cancels the dry-run or safe-merge build that's running |
  | `fixup` | `/fixup` | squashes `fixup!` commits into their targets and force-pushes the branch, no checks |
  | `codeowners` | `/codeowners` | re-runs the code-owners check and refreshes its table comment |

  Not in scope: `/safe-squash-merge`, `/cherry-pick`, `/test-public`, `/test-private`, `/review`, and `/safe-merge`'s
  `--fixup=false`: they need extra input or report differently.
- Checked before posting (`actions.Check`), each refusal with its reason:
  - only on your own PRs, open (not merged, not closed);
  - a dry-run or safe-merge only while no dry-run or safe-merge is requested or running: the bot runs one
    coordinator build per PR and rejects a second one ("A Coordinator build is already in progress"). A request the bot
    never answered ("no response") doesn't count, so it can be posted again;
  - `/safe-merge` only on a PR that isn't a draft and is approved: at least one approval and a green
    `Code Owners Approval`. A draft takes every other command;
  - `/cancel-coordinator` only while a dry-run or safe-merge is requested or running;
  - `/codeowners` only while its check is missing or failing.
  The bot still has the last word (conflicts, stacked PRs, `amend!` / `squash!` commits); its rejection shows as the
  run's state.
- CLI: `run <number> <command>` fetches the PR live, checks the command, prints the PR's number and title and the exact
  comment, and asks `Post this comment? [y/N]`: only `y` or `yes` posts. `--yes` skips the question; without a terminal
  and without `--yes` it refuses (exit 2) before fetching anything. It prints the new comment's URL. A refusal or a
  failed post exits 1. Escape sequences in the answer are dropped first: gh asks the terminal for its background
  (OSC 11, then CSI 6n) before it runs the extension, and reads the replies itself, but a late one (after its 5s wait)
  would reach the question's stdin ahead of the answer. The tool itself never queries the terminal.
- TUI: the keys and the menu of §7. A posted dry-run or safe-merge shows as requested straight away, until a refresh
  that started after the post shows what GitHub has; until then the same command isn't posted again on that PR. A
  failed post shows in the status bar. A PR becoming my move right after my own post isn't notified (§14).
- After a post, from `run` or the TUI, the PR's cached details are dropped (`github.ForgetPR`: its own entry and
  every details batch holding it), so the next fetch of it, the menu-bar plugin's next run say, is live and shows
  what the bot made of it. A refusal, a declined question or a failed post keeps them.
- Demo mode never posts.

**Review requests** (`request-review`, `internal/actions`), for a re-request after a round of review and for the first
assignment of a rule nobody was asked for (§5, 2a and 2c):
- One `POST /repos/{repo}/pulls/{n}/requested_reviewers` with `{"reviewers": [logins]}` per confirmation, all the picked
  people at once (one timeline event, one refresh of the bot's table). GitHub adds them to the requested reviewers and
  re-requests those who reviewed already; a 422 requests nobody, and is reported as it is. Never a team request.
- Only people of the bot's code-owners table can be asked: per row (a subsystem), its owners besides me, in table
  order, then the `(QA)` and `(PM)` members, then the `⏳` ones (still selectable). Rows without owners
  (`#NO_OWNERS`), or owned only by me, aren't offered. Each person has hints: what they did on the PR (`approved`,
  `changes requested`, `commented`, `requested`) and `also <path>` for the other rows they own.
- A row's status: `unassigned`, `requested: x` (or `requested` for a request the table doesn't show yet),
  `re-request: x` (`🔄`), `changes requested: x`, `✓ x`. It needs me when it's unassigned with nobody requested, or
  `🔄`; it's covered when it's approved or an owner of it is requested now.
- The default picks, for a re-request: the `🔄` owners not requested again yet, and the reviewers who requested changes
  before my last push, both among the table's people. Never an approver (a request doesn't dismiss an approval), and
  nobody whose approval was dismissed (the bot re-requests those itself). A first assignment has no default.
- Checks: only on my own open PRs, drafts included; at least one person; only the table's people.
- CLI: `run <number> request-review [login...]` asks the logins given, or the default picks, with the usual question
  (`Request this review? [y/N]`, `--yes`). Without logins and without default picks, or with a login the table doesn't
  have, it refuses with the candidates per row. After the request it drops the PR's cached details, so the next run
  fetches them.

## 9. Config

`~/.config/gh-kotlin-prs/config.yml` (`$XDG_CONFIG_HOME`), or `--config PATH`, or `$GH_KOTLIN_PRS_CONFIG`. Every key is
optional, and the defaults are for Kotlin. The default location may have no file (all defaults then), but a file named
by `--config` or `$GH_KOTLIN_PRS_CONFIG` must exist: else `list`, `show`, `run`, the TUI and `swiftbar install`/`script`
fail with `config: <path> (from --config) doesn't exist`, and the plugin shows that in its menu. `config` and
`config path` still answer (`config` marks the file not found), and `config init` creates it. `notify` serves the TUI
and the menu-bar plugin: `events`, `command` and `timeout` apply to both, `terminal` and `bell` to the TUI, `swiftbar`
to the plugin (§14). `config` prints each value with its source (default, file, flag) as YAML that reads back as the
same config: the maps (`keys`, `notify`) as blocks, a line and a source per sub-key, the source comments aligned per
block unless a line is longer than 60 columns. Values are quoted only where plain YAML wouldn't read back the same.
`config init` writes the same layout with every key commented out, so later default changes still apply.

```yaml
repo: JetBrains/kotlin
bots: [KotlinBuild, kotlin-safemerge, kodee-bot]
gateBot: KotlinBuild
ownersBot: kotlin-safemerge
teams: []                 # slugs for --teams
refresh: 3m
startupMaxAge: 30m        # the TUI starts from cached data at most this old
requestedTimeout: 10m
icons: unicode            # or ascii
issueProjects: [KT, KTIJ, KTI]
issueURL: https://youtrack.jetbrains.com/issue/{id}
hyperlinks: auto          # always, never
keys: {}                  # TUI keys by action, e.g. {copy: c, refresh: [r, F5]}; `?` lists the defaults
notify:                   # notifications; unset keys keep these defaults
  events: [runPassed, runFailed, runRejected, myMove, changesRequested, reviewRequested, merged]   # TUI and plugin
  terminal: auto          # TUI only: osc9, osc777, osc99, none
  bell: false             # TUI only
  swiftbar: true          # menu-bar plugin only: its notification through SwiftBar
  command: []             # TUI and plugin, e.g. [notify-send, "{title}", "{body}"]
  timeout: 10s            # TUI and plugin: for the command
```

## 10. Layout, tests, release

```
main.go
internal/github/    queries, fetch, raw types
internal/cache/     raw responses with their fetch time (§13)
internal/classify/  bot parsing, rules → model
internal/listing/   fetch the sections, classify them with any clock, --all / --waiting-on-me
internal/model/
internal/render/    table, json
internal/tui/
internal/actions/   the bot commands and when they may be posted
internal/notify/    events from two snapshots, terminal notifications, the command hook
internal/swiftbar/  the menu-bar plugin: its menu, script and notifications
testdata/           raw GraphQL responses + golden outputs
scripts/fetch-fixtures.sh
```

- Tests never touch the network: raw response → model (golden JSON) → table (golden text), plus one unit test per rule in §5.
  Fixtures are real responses passed through `scripts/anonymize` (pseudonyms, filler text, fake numbers; see the README).
- Scaffolded with `gh extension create --precompiled=go kotlin-prs`, which includes the `cli/gh-extension-precompile`
  release workflow. CI runs `scripts/check.sh`.
- Dev install: `go build && gh extension install .`

## 11. Roadmap

- **v0.1.0, read-only CLI:** everything above except the TUI and actions: `list`/`show` with table and JSON output,
  issues from trailers, hyperlinks, `--icons`, the config commands. The gate is real use: tune the rules of §5 on the
  maintainer's own PRs and reviews until the "whose move" column is right without second-guessing.
  Public-release checklist: license, a review of §3 (only publicly observable behavior), a privacy grep over the whole
  history (fixtures, tests, docs), the repo description and topics (`gh-extension`), README install from GitHub
  (`gh extension install dimonchik0036/gh-kotlin-prs`).
- **v0.2.0, TUI (read-only) + cache + notifications:** §7, §12, §13, §14.
- **v0.3.0, actions** in the CLI and the TUI, with confirmation (§8).
- **v0.4.0, the SwiftBar menu-bar plugin** (§15).
- **Later:** a daemon (background refresh and notifications without the TUI or the plugin, reusing §13 and §14), an
  MCP mode, snoozing PRs, shell completion. gh doesn't pass completion on to extensions (`gh __complete kotlin-prs ""`
  answers nothing, and aliases aren't resolved), so it needs a hook that wraps gh's own completion.

## 12. CLI and TUI

- From v0.2.0, a bare `gh kotlin-prs` opens the TUI when stdin and stdout are terminals and prints `list` otherwise, or
  with `--format json`. `list` and `show` stay plain output for scripts, pipes, JSON and SwiftBar. Changing what the bare
  command does is user-visible: CHANGELOG. The TUI ignores `--max-age` (every refresh fetches) and `--debug` (stderr is
  the screen).
- `gh kotlin-prs --pr N` opens the TUI on N's details (`esc` goes to the list); `--post <command>` with it also asks
  to post that command, the same question as its key, once a live refresh shows the PR's state (never on cached data,
  and only if the details are still shown). Only `y` posts. Both need a terminal.
- **Startup:** with a snapshot in the cache whose every part is at most `startupMaxAge` (30m) old, the TUI shows it at
  once and refreshes behind it; otherwise it shows "Loading PRs from GitHub⋯" until the first response. After midnight
  UTC the merged search has a new key, so it falls back to the previous day's (`listing.Cached`). When the search's PRs
  changed since their details were fetched (or another `list` flag fetched another set), it takes the newest details
  entry of the account and repo instead, still within `startupMaxAge`; PRs missing from it appear with the refresh.
  `startupMaxAge: 0` always waits for GitHub.
- **Status bar** (the last line): `updated 2m ago ∙ next refresh in 1m`; while refreshing `⠋ refreshing⋯ (cached 12m
  ago)`; after a failure the data stays on screen with `✗ refresh failed: <error> (updated 14m ago) ∙ r to retry`.
  The rate limit shows only when less than a tenth of it is left (`! API budget 312/5000, resets 18:00`), or as the
  error when a request was refused for it, and only from live responses, never the cache. Notes such as `copied <url>`
  stay for 4s; key hints are on the right. Single-width symbols only, as in the CLI: `⟳ ✗ !` rather than emoji.
- One rendering source: the TUI list rows and the `list` table come from the same row and cell code (same columns,
  symbols and reasons), and the TUI detail pane reuses the `show` renderer. The TUI only adds selection, the detail
  pane, the filter, the status bar and refresh. In `internal/render`: `Blocks` are the sections (title, sorted rows,
  the "nothing here" and `--all` notes), a `Row` is a PR as `Cell`s (column, plain text, link target, an optional
  max width cut with the ellipsis, rendered with or without links), `Lines` aligns rows into columns, and
  `DetailView` is the `show` text cut to a given width.
- Commands (§8): `run` and the TUI share the checks (`actions.Check`) and the post (`github.PostComment`). The TUI's
  status bar holds the confirmation (`Post … ? [y/N]`) and a failed post; `x` shows its menu over the bottom of the
  list or the details.

## 13. Cache and state

- **Cache:** the raw GraphQL responses with their fetch time in `$XDG_CACHE_HOME/gh-kotlin-prs/` (default
  `~/.cache/gh-kotlin-prs/`). Classification always re-runs with the current clock, so cached data still ages correctly
  ("failed 2h ago" keeps moving).
  - One file per query, `<operation>-<scope>-<hash>.json` (`Sections`, `PullRequests`, `PullRequest`): the scope hashes
    the account (gh's host and token, never stored), the operation and its variables (the repo, the merged cut-off
    day), the hash also the query text (the PR numbers of a details query). A file is `{"format": 1, "fetchedAt": …, "data": <the response's data as GitHub sent it>}`, mode 0600.
    The details of ~40 PRs are about 1 MB.
  - Every successful fetch writes its file atomically (a temp file renamed over it) and drops files not written for
    3 days. A failed fetch writes nothing.
  - An unreadable file (corrupt, another `format`, data that doesn't decode) is a miss: `--debug` notes it and the
    next fetch overwrites it. The cache never fails a command, and neither does a failed write.
  - Demo mode (`GH_KOTLIN_PRS_DEMO`) neither reads nor writes it.
  - `cache.Offline` answers queries from the cache only (any age, or up to a bound) and reports the oldest entry it
    used: the TUI starts from it (§12). For the operations in its `Fallback`, a miss takes the newest entry of the same
    scope by its stored `fetchedAt`, still within the bound.
- The CLI fetches every time by default; `--max-age DURATION` (`list` and `show`) lets it use a cache entry younger
  than that, per query: `list`'s search and its details are separate entries. `--max-age 0` always fetches. The TUI
  shows the cache at start and refreshes in the background.
- **State**, separately in `$XDG_STATE_HOME/gh-kotlin-prs/`: what was seen, snoozes, and the last notified status per PR.
  Deleting the cache never loses state.

## 14. Notifications

- In the TUI from v0.2.0, in the menu-bar plugin from v0.4.0 (§15), later in the daemon. In the TUI they come from
  diffing two consecutive live refreshes (`notify.Diff` over the classified rows, hidden ones included): never the first
  load, never the cache snapshot the TUI starts from, and a failed refresh keeps the last good one as the baseline. A
  restart starts a new baseline, so it repeats nothing but misses what changed while it was closed; keeping the last
  notified state (§13) is for later, with the daemon.
- Events (config `notify.events`, all on by default):
  - `runPassed`, `runFailed`, `runRejected`: the latest dry-run or safe-merge of my PR reached that state, a new run or
    the one seen before; not when it's outdated. The link is the build, or the bot's reply for a rejection, whose
    reason is the body;
  - `myMove`: a PR of mine became my move, unless a run failing or being rejected, or new changes requested, already
    say so;
  - `changesRequested`: a reviewer's latest review on my PR is newly CHANGES_REQUESTED;
  - `reviewRequested`: a PR in Review became my move (a new or repeated request);
  - `merged`: a PR of mine moved to Recently merged.
  A PR that's new in the snapshot only counts for `reviewRequested` and `merged`: a PR I just opened isn't news.
- Delivery, configurable, several at once. Each notifier has its own channels, and `notify.command` runs from both
  whatever they're set to:
  - `notify.terminal` (TUI only): OSC 9 (`osc9`), OSC 777 `notify;title;body` (`osc777`), kitty's OSC 99 (`osc99`), or
    `none`. `auto` (the default) picks by the terminal: iTerm2, WezTerm, Ghostty → OSC 9; kitty → OSC 99; foot,
    rxvt-unicode → OSC 777; nothing in Terminal.app, VS Code, tmux and screen (which need passthrough), or anything
    unknown. Written through bubbletea (`tea.Raw`): the escapes don't move the cursor, so the screen stays whole.
    Control characters in the text become spaces, `;` becomes `,` for OSC 777;
  - `notify.bell` (TUI only, off by default): a BEL per event;
  - `notify.swiftbar` (plugin only, on by default): SwiftBar's notification (§15);
  - `notify.command` (TUI and plugin): argv run per event, `{title}`, `{body}` and `{url}` replaced in its arguments and
    the event as JSON on stdin (`{"kind", "number", "prTitle", "title", "body", "url"}`), in the background with a
    timeout (`notify.timeout`, 10s). In the TUI a failure shows in the status bar for a few seconds; nothing ever waits
    for it. terminal-notifier, osascript, notify-send, ntfy or anything else works.
- The status bar names the first event of a refresh ("#90006 dry-run failed (+1 more)").
- One notifier: `internal/notify` knows nothing of the TUI (it returns the escapes as a string and runs the command
  where the caller says), so the daemon can reuse it.

## 15. Menu bar (SwiftBar)

- `list --format swiftbar` (the section flags of `list` apply) prints a [SwiftBar](https://github.com/swiftbar/SwiftBar)
  plugin's output (`internal/swiftbar`), from the same rows as `list`:
  - the title: the SF Symbol `arrow.triangle.pull`, template-rendered so it follows light and dark mode, and the number
    of PRs whose move is mine (none for 0). `⋯` while a dry-run or safe-merge of mine is requested or running; the
    symbol red while one of mine failed or was rejected (not outdated). `!` last when nothing could be fetched, or when
    a refresh failed and the cached data shown is older than twice the plugin's `--max-age` (it missed a refresh; one
    failed refresh alone doesn't mark it, so a blip doesn't flicker), e.g. `3 ⋯ !`; it doesn't change the color. The
    red is `sfconfig=<base64 {"renderingMode":"Palette","colors":["#FF3B30"]}>` (systemRed, readable on light and dark
    menu bars): SwiftBar 2.1.1 colors an `sfimage` only through `sfconfig`, and `sfcolor` only colors the symbols in
    an item's text, so `sfcolor` is ignored for the menu-bar icon;
  - the dropdown: "Your move: N ∙ updated 1m ago" (or the fetch error, with the cached data's age), the API budget when
    less than a tenth is left (live responses only), then the sections: Mine and Review at the top level, Team requests
    and Recently merged as submenus, each with its count and the `--all` note. A PR is one monospace line: who has
    the move, the number, the issue, the title cut at 33 (the submenu starts with all of it), the dry-run, the
    safe-merge and `list`'s reviews cell (`1/2 ✗`: approvals of the people reviewing, the code-owners mark), no thread
    count;
  - a PR's submenu: the whole title (gray, no action), "Details in the interactive view", whose move it is and every
    reason (linked, cut at 80 with the whole text as the tooltip), the runs (linked to their builds), the reviewers and
    the code-owner rules still missing, "Open on GitHub", "Copy link", and the commands `actions.Available` allows
    now. A command opens the interactive view on the PR with that command's question (`--pr N --post <command>`);
    nothing is ever posted from the menu. A click on the row itself opens the details too (hovering opens the submenu),
    and ⌥ shows the row's alternate, which opens the PR in the browser;
  - the footer: "Refresh now" (a live fetch, then SwiftBar runs the plugin again) and "Open the interactive view".
  - Item texts are neutralized for SwiftBar: `|` becomes `¦`, newlines spaces, a leading `-` gets a zero-width space,
    and user text has `emojize=false symbolize=false`; parameter values with blanks are quoted.
  - Every item with a submenu has an action: a PR row the details' (`bash=exec … --pr N terminal=true`; `href=.`
    outside SwiftBar, without the plugin), Team requests and Recently merged `href=.`, the href SwiftBar 2.1.1 skips on
    a click, so the click only closes the menu. SwiftBar 2.1.1 updates the menu in place, and when an item's line
    changes it patches the item: for an item without an action that clears the action AppKit gave it for its submenu,
    so the item turns grey and its submenu stays shut until a full rebuild (SwiftBar #512, fixed in 2.1.2-beta-1). An
    action of its own survives the patch, and AppKit runs it on a click on the item. The ⌥ alternate rows have their
    main row's text.
  - It never fails: an error shows in the menu (with the whole first line as the tooltip when the menu cuts it),
    with the last cached data (any age) when there is some, and exits 0. A bad config is such an error: the run fetches
    nothing and shows what the cache has for the default config. A bad flag still fails.
- The plugin script (`swiftbar script`, written by `swiftbar install` as `kotlin-prs.<interval>.sh`, mode 0755) bakes in
  gh's absolute path, a PATH with its folder, and the config file when `--config` or `$GH_KOTLIN_PRS_CONFIG` names one
  (which must exist), since SwiftBar runs it with a bare environment. It hides SwiftBar's "Run in Terminal" and
  "About" items. Without arguments it runs `list --format swiftbar --max-age <interval/2, at least 10s>`, so a fresh
  fetch of the TUI or the CLI answers; with `copy <url>` and `refresh` it serves those clicks of the menu.
  - Its xbar tags fill SwiftBar's plugin details, with the names SwiftBar 2.1.1's `PluginMetadata` reads: `title`,
    `version` (the `--version` of the tool that wrote it), `author` and `author.github` (dimonchik0036), `desc`,
    `dependencies` (gh) and `about` (the repo; not xbar's `abouturl`). No schedule: the file name has the interval.
- `swiftbar install` writes into SwiftBar's plugin folder (`defaults read com.ameba.SwiftBar PluginDirectory`) or
  `--dir`; it refuses when SwiftBar has no folder yet, and replaces an installed `kotlin-prs.*.sh` only with `--force`
  (removing the other intervals' files).
- Opening the interactive view from the menu: the items are `bash=exec param1=<gh> param2=kotlin-prs param3=--pr
  param4=N [param5=--post param6=<command>] terminal=true`. SwiftBar opens a new tab in the terminal of its Settings →
  Advanced → Terminal (Terminal, iTerm or Ghostty) and types `export <its SWIFTBAR_*/OS_* variables>; <bash> <params>`
  into the user's shell, unquoted, with no PATH of its own, so the line reads `…; exec /opt/homebrew/bin/gh kotlin-prs
  --pr 90006`. `exec` replaces the tab's shell, so the tab closes when the interactive view quits. gh's path is the
  plugin script's (`GH_KOTLIN_PRS_GH`), the gh the plugin itself runs. There's no config of our own for it.
- Notifications: a plugin run that fetched every query live (no cache hit) diffs its rows with the last live run's,
  kept in the cache dir (`swiftbar-baseline.json`), with `notify.Diff` and the `notify.events` of §14. The first run
  only stores its rows. Each event goes to `notify.command`, and unless `notify.swiftbar` is false to SwiftBar's
  `swiftbar://notify?plugin=…&title=…&body=…&bash=exec&param1=<gh>&param2=kotlin-prs&param3=--pr&param4=N&terminal=true`
  (`osascript display notification`, which no click opens anything from, when SwiftBar isn't the caller). Every event
  is about a PR, and a click opens the interactive view on its details, the way the menu's items do; one about no PR
  would carry `href=` and open its page. With `notify.swiftbar: false` the run still diffs, runs the command and
  stores its rows, so turning SwiftBar's notification back on replays nothing.
  - SwiftBar 2.1.1 keeps all the URL's parameters for the click and reads them as an item's: joined as `key=value`
    in no fixed order, a value with a space quoted in `'`, then parsed like a menu line. An apostrophe in a quoted
    title would end it there and lose `bash` and its params, so the title and body never have a space: SwiftBar shows
    each `+` in them as a space, so spaces go as a raw `+`, the text's own `+` as `＋` (U+FF0B), other blanks as a
    space, and a leading quote gets a zero-width space.
  - A click finds the plugin that posted the notification by its file's path, so notifications from before a
    reinstall with another interval (another file name) open nothing.
  - On macOS 26, SwiftBar 2.1.1 also shows its menu-bar recovery alert ("SwiftBar is already running") after every
    notification click and every `open` of a `swiftbar://` URL: macOS sends a reopen event after them (SwiftBar #535,
    fixed in 2.1.2-beta-4 by #562). The click's action still runs.
  - Who notifies: the plugin through SwiftBar (`notify.swiftbar`, false for nothing), the TUI through the terminal
    (`notify.terminal`, `none` for nothing, and `notify.bell`), and `notify.command` (empty by default) from both,
    whatever the channels are. `notify.events` filters all of them.
    They don't coordinate: the same change may be notified by each.

## 16. Open questions

None for now.
