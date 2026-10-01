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
`myLastActivity` is the latest of: last push, my last comment, my last review, my last thread reply.

**Mine:** the first rule that matches sets `Next`. Every rule that matches is added to `Reasons`.
1. **Me:** the latest dry-run or safe-merge failed (and isn't outdated), was rejected, or got no response
   → "safe-merge failed 2h ago", "dry-run rejected: <reason>".
2. **Me:** a reviewer's latest opinionated review is CHANGES_REQUESTED and newer than my last push.
2a. **Me:** a code owner is marked `🔄` (commented and needs a re-request) → "re-request review from alice_user"; one
    reason lists every such owner: "re-request review from alice_user, bob_user".
2b. **Me:** a missing code-owner rule where every listed owner who hasn't reviewed is `⏳` → "all owners for /analysis/ unavailable".
3. **Me:** an unresolved, non-outdated thread whose last comment isn't mine.
4. **Me:** a non-bot comment from someone else after `myLastActivity`. One event gives one reason: comments in threads
   that rule 3 reports, and reviews that consist of thread comments, don't count again.
5. **CI:** a run is requested or running.
6. **Me (ready):** `Code Owners Approval` is green, there's at least one approval, and no run is requested or running → "ready to /safe-merge".
   A missing or outdated green dry-run doesn't block this (CI isn't a gate), but it's added as a hint: "no fresh dry-run".
7. **Reviewers:** pending requests or missing code owners → "waiting: alice_user, bob_user".
8. **Me:** none of the above, for example no dry-run yet → "no dry-run yet".

**Review:** only a request brings a PR to me. Author pushes, author replies and thread replies don't: they arrive by email.
`show` still lists the threads, for information.
1. **Me:** requested from me personally, and I have no review after the latest request event. This covers re-requests.
2. **Done:** I approved. Hidden until I'm re-requested, whatever happened since.
3. **Author:** anything else, e.g. I commented or requested changes and wasn't re-requested. Hidden unless `--all`.

Every rule lives in one `classify` package, with a test per rule.

## 6. CLI

```
gh kotlin-prs                      # TUI on a TTY, otherwise `list` (§12; until v0.2.0 always `list`)
gh kotlin-prs list [--mine|--review] [--waiting-on-me] [--all] [--no-teams] [--no-merged]
                   [--format table|json|swiftbar] [--max-age DURATION]
gh kotlin-prs show <number> [--max-age DURATION]  # details: reviewers, run history, threads, reasons
gh kotlin-prs config [path|init [--force]]  # effective config with sources, its path, a commented template
gh kotlin-prs dry-run <number> [--retry] [--yes]
gh kotlin-prs safe-merge <number> [--yes]
gh kotlin-prs cancel <number> [--yes]       # /cancel-coordinator
gh kotlin-prs fixup <number> [--yes]        # /fixup
gh kotlin-prs codeowners <number> [--yes]   # /codeowners
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

## 7. TUI (bubbletea + bubbles + lipgloss + huh)

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
  `?` help with the symbol legend, `q` quit; `ctrl+c` always quits. Config `keys` rebinds them by action
  (`config.Actions`: up, down, first, last, pageUp, pageDown, nextSection, previousSection, details, back, filter, all,
  open, build, copy, refresh, help, quit): a key or a list replaces that action's keys. An unknown action, an action
  without keys, or a key bound twice is a config error. Inside the filter, enter and esc are fixed. From v0.3.0 (§8): `d` dry-run, `D` dry-run --retry, `m` safe-merge,
  `x` cancel coordinator, `f` fixup, `c` codeowners.
- **Refresh:** in the background every `refresh` (3m) and on `r`. The UI never blocks while a fetch runs; a spinner
  shows it. One fetch at a time: `r` during a refresh only notes "already refreshing", and the timer waits for it. `r`
  within 5s of a successful refresh notes "refreshed 2s ago" instead; after a failed one it retries at once. Rows are classified again every 30s between refreshes, so their ages keep moving.
- **Actions:** see §8. After one is posted, the row shows `requested` straight away and the next refreshes reconcile it.

## 8. Actions and safety

- An action posts a regular PR comment (`POST /repos/{repo}/issues/{n}/comments`, never a review comment), so it's visible to everyone.
- v1 actions: `/dry-run`, `/dry-run --retry`, `/safe-merge`, `/cancel-coordinator`, `/fixup`, `/codeowners`.
  `/safe-squash-merge`, `/test-public` and `/cherry-pick` aren't in v1, because they need extra input or report differently.
- `/cancel-coordinator` is offered only while a run is requested or running. `/codeowners` is offered when the check is missing or failing.
- It always asks for confirmation: a `huh` dialog in the TUI, a y/N prompt in the CLI. `--yes` skips the prompt (CLI only).
- Actions are allowed only on your own PRs.
- An action is refused when one of the same kind is already requested or running, and when the PR is a draft.
- Read commands never write anything.

## 9. Config

`~/.config/gh-kotlin-prs/config.yml` (`$XDG_CONFIG_HOME`), or `--config PATH`, or `$GH_KOTLIN_PRS_CONFIG`. Every key is
optional, and the defaults are for Kotlin. `config` prints each value with its source (default, file, flag); `config init`
writes every key commented out, so later default changes still apply.

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
keys: {}                  # TUI keys by action, e.g. {copy: c, refresh: [r, R]}; `?` lists the defaults
```

## 10. Layout, tests, release

```
main.go
internal/github/    queries, fetch, raw types
internal/cache/     raw responses with their fetch time (§13)
internal/classify/  bot parsing, rules → model
internal/listing/   fetch the sections, classify them with any clock, --all / --waiting-on-me
internal/model/
internal/render/    table, json, swiftbar
internal/tui/
internal/actions/
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
- **Later, to be decided after v0.3.0:** SwiftBar output (`--format swiftbar`), a daemon (background refresh and
  notifications without the TUI, reusing §13 and §14), an MCP mode, snoozing PRs.

## 12. CLI and TUI

- From v0.2.0, a bare `gh kotlin-prs` opens the TUI when stdin and stdout are terminals and prints `list` otherwise, or
  with `--format json`. `list` and `show` stay plain output for scripts, pipes, JSON and SwiftBar. Changing what the bare
  command does is user-visible: CHANGELOG. The TUI ignores `--max-age` (every refresh fetches) and `--debug` (stderr is
  the screen).
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

- In the TUI from v0.2.0, later in the daemon. They come from diffing consecutive refreshes (against the last notified
  state of §13, so a restart doesn't repeat them).
- Events, each configurable on or off: a run finished, failed or was rejected; a PR became my move; changes were
  requested; a review was requested from me; a PR of mine was merged.
- Delivery, configurable, several at once:
  - terminal escape notifications: OSC 9, OSC 777 (`notify;title;body`) or kitty's OSC 99; `auto` picks one by
    `$TERM_PROGRAM` / `$TERM` and sends nothing where none is known to work;
  - the terminal bell;
  - `notify.command`: a user command that gets the event as JSON on stdin, with `{title}`, `{body}` and `{url}`
    placeholders for its arguments, so terminal-notifier, osascript, ntfy or anything else works.
- One notifier: the daemon reuses the TUI's.

## 15. Open questions

None for now.
