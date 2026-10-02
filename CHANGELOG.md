# Changelog

All notable user-visible changes. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [RELEASING.md](RELEASING.md). The JSON output has its own `"version"`, bumped only on
breaking JSON changes and noted here.

## Unreleased

## v0.6.1 — 2026-10-02

A fix for the interactive view. The JSON output is unchanged (`"version": 1`).

- The selected row of the interactive view keeps its links: its PR number, issue IDs, DR / SM cells and reason
  are clickable like the other rows'.

## v0.6.0 — 2026-10-02

Requesting reviews from code owners, fixes to whose move a PR is, and a faster menu-bar plugin. The JSON output only
gains fields, so it stays `"version": 1`. After upgrading, update the menu-bar plugin through its menu's "Update the
plugin script" item, or with `gh kotlin-prs swiftbar update`.

### Review requests

- `gh kotlin-prs run <number> request-review [login...]` requests a review of a PR of yours from its code owners, in
  one request: the logins given, or by default the ones to re-request (they commented and weren't re-requested, or
  requested changes before your last push). Only people of the bot's code-owners table, never a team; without logins
  to go on, it lists them per rule (#1, #2).
- In the interactive view, `A` (config `requestReview`) opens a tree of the PR's code-owner rules to pick the people
  to request a review from, the rules that need you open; `enter` asks y/N, then sends one request for everyone
  picked. The status bar hints `A re-request` / `A assign reviewers` on a PR that needs it, the `x` menu lists it,
  and `--pr <number> --post request-review` opens it on that PR (#1, #2).
- The menu-bar plugin offers "Re-request review from …" on a PR of yours with people to re-request, and "Assign
  reviewers…" on one with a code-owner rule nobody was asked for; both ask in the terminal first (#1, #2).
- The code-owners table is read by its links: a person is whoever the profile link names, so full names the bot may
  add next to logins don't break it, and the review picker shows them (`judy_user (Judy Doe)`).

### Whose move it is

- A PR of yours with a code-owner rule nobody was asked to review (the bot's `UNASSIGNED`) is your move: "assign
  reviewers for /analysis/", instead of "waiting: owners of /analysis/". A review request the bot's table doesn't
  show yet counts (#2).
- While a dry-run or safe-merge of yours is requested or running, "re-request review…" and "assign reviewers…" wait
  for it: the PR is CI's, with them as secondary lines, and your move once the run passed.
- Re-requesting a review hands the move to the reviewer: an unresolved thread, a new comment or requested changes from
  someone you re-requested since no longer make a PR of yours your move; it's "waiting: <them>" (#3).
- A safe-merge whose quality gate passed but whose merge failed (the bot's `Error:` line, e.g. "rebase-merge failed:
  Pull Request has merge conflicts") showed as passed and "ready to /safe-merge". Any `Error:` after the gate's result
  now fails the run: your move, "safe-merge failed: <error>", notified as a failure with the error. The JSON run has
  it as `error`.
- A PR of yours that GitHub reports as conflicting with its base branch is your move: "conflicts with master,
  rebase", right after a failed run, and said once when the bot rejected a command for it. `/dry-run` and
  `/safe-merge` are refused locally on it ("has conflicts with the base branch; rebase first"). The JSON PR has
  `base` and `conflicts`.
- A safe-merge or dry-run rejected for missing code-owner approval no longer makes a PR of yours your move: only
  reviewers resolve that, so it's "waiting: …" like before the command. The rejection still notifies and shows in
  the run history; rejections you can fix (conflicts, `fixup!` commits, a draft, …) stay your move.
- A command you minimized on the PR (resolved, e.g. with the PR Helper extension) no longer counts as a run, nor does
  the bot's reply to it; a minimized rejection of a visible command drops that rejected run.

### Menu-bar plugin

- The plugin runs every 30s and fetches every 3m: `swiftbar install` and `swiftbar script` take `--interval` (how
  often SwiftBar runs it, 30s) and `--max-age` (how old cached data a run takes, 3m) instead of one interval of 3m. A
  run picks up within 30s what the interactive view fetched or a command changed.
- A command posted from `run` or the interactive view drops the PR's cached details, so the plugin's next run fetches
  what the bot made of it.
- The plugin notifies of what changed whenever it gets newer data, also from the cache the interactive view wrote,
  not only on its own live fetches; nothing is lost or told twice.
- The plugin script drops its version line, which went stale with every upgrade. `gh kotlin-prs swiftbar update`
  rewrites the installed plugin with this version's script, keeping its settings; a plugin of v0.5.2 or older moves
  to the new timing, its old interval becoming the max-age. The menu offers it ("Update the plugin script") while the
  installed script is older.

### Other

- Review rows show the PR's author after the title (cut at 12 characters), in `list`, the interactive view and the
  menu-bar plugin, whose Review submenus start with "by <login> · pushed 2h ago".
- `Y` (config `copyBranch`) copies the selected PR's branch name in the interactive view, and the menu-bar plugin's
  PR submenu has "Copy branch" after "Copy link".
- Demo mode (`GH_KOTLIN_PRS_DEMO`) takes a list of fixture directories, and its interactive view pretends a post or a
  review request instead of refusing it. The README shows requesting a review in a recording.
- A GitHub release's notes are its version's section of this changelog, with a link comparing it to the previous one.

## v0.5.2 — 2026-10-02

A fix for when a PR of yours counts as your move. The JSON output is unchanged (`"version": 1`).

- A PR of yours no longer shows "new comment from …" as your move for a comment its author followed with an
  approval, or one you already answered with a bot command such as `/dry-run`: those count as their last word and as
  your activity. A running dry-run then shows as waiting on CI.

## v0.5.1 — 2026-10-02

A fix for `run`'s question. The JSON output is unchanged (`"version": 1`).

- `run`'s `[y/N]` question ignores terminal replies that reach it ahead of the answer (gh asks the terminal for its
  background before it starts the extension), so they can no longer turn a `y` into "not posted: cancelled".

## v0.5.0 — 2026-10-02

A switch for the menu-bar plugin's notifications, and clearer docs on which notification settings apply where. The
JSON output is unchanged (`"version": 1`).

- `notify.swiftbar: false` turns off the menu-bar plugin's SwiftBar notifications; `notify.command` still runs from it,
  as it does from the interactive view whatever `notify.terminal` and `notify.bell` are. The docs and `config` say
  which `notify` keys apply to the interactive view, to the plugin, or to both.

## v0.4.0 — 2026-10-02

The menu bar: a SwiftBar plugin that shows your PRs, notifies of changes, and opens the interactive view on a PR or a
command. The JSON output is unchanged (`"version": 1`).

- A menu-bar plugin for SwiftBar: `gh kotlin-prs swiftbar install`. The menu bar shows how many PRs wait on you (red
  when a run of yours failed); the menu lists the sections like `list`, and per PR why it's your move, its runs,
  reviewers and code owners, links, and the commands you could post, which open the interactive view to ask. It
  notifies of what changed through SwiftBar; a click on a notification opens its PR in the interactive view.
  `swiftbar script` prints the plugin for a manual install. It opens the interactive view in the terminal set in
  SwiftBar's settings, e.g. Ghostty.
- `list --format swiftbar` prints the plugin's menu.
- `gh kotlin-prs --pr <number>` opens the interactive view on that PR's details; `--post <command>` with it also asks
  to post that command there. Nothing is posted without `y`.
- A config file named by `--config` or `$GH_KOTLIN_PRS_CONFIG` must exist: a typo in the path is now an error instead
  of silently giving the defaults (the menu-bar plugin shows it in its menu). The default location may still have no
  file; `config`, `config path` and `config init` work either way.

## v0.3.0 — 2026-10-02

The bot's commands, from the CLI and the interactive view: dry-run, safe-merge and the rest, each shown and confirmed
before anything is posted, and refused when it can't work. Posting them is the only thing the tool writes to GitHub.
The JSON output is unchanged (`"version": 1`).

- `gh kotlin-prs run <number> <command>` posts a bot command on a PR of yours: `dry-run`, `dry-run-retry`,
  `safe-merge`, `cancel-coordinator`, `fixup` or `codeowners`. It shows the PR and the exact comment and asks first
  (`--yes` skips that), and refuses what the bot would refuse or what makes no sense now, saying why.
- The interactive view posts the same commands: `D` dry-run, `R` dry-run --retry, `M` safe-merge, `C`
  cancel-coordinator, `F` fixup, `O` codeowners, or `x` for a menu of the ones that make sense for the PR now. Each
  asks first, and the PR shows the run as requested right away.
- The help in the interactive view scrolls.
- Config `keys`: `R` is now taken by dry-run --retry; a config that binds `R` to another action needs another key.

## v0.2.0 — 2026-10-01

An interactive view with background refresh and notifications, and a cache. On a terminal, `gh kotlin-prs` now opens
the interactive view; in a pipe it still prints `list`. The JSON output is unchanged (`"version": 1`).

### Interactive view
- `gh kotlin-prs` on a terminal shows the rows of `list` with a selection; `enter` shows the selected PR as `show`
  does, `esc` goes back. The section flags (`--mine`, `--review`, `--all`, …) apply.
- Keys: `j`/`k` or the arrows, `tab` for the next section, `/` to filter, `a` to toggle `--all`, `o` to open the PR,
  `b` its newest build, `y` to copy its URL, `r` to refresh, `?` for help and the symbols, `q` to quit.
- It refreshes in the background every 3 minutes and on `r`, one refresh at a time, and the ages ("failed 2h ago")
  keep moving in between. The status bar says when the data was fetched and when the next refresh is due, keeps the
  data on screen when a refresh fails, and warns when the API budget runs low.
- It starts at once from the cache when the last fetch is at most 30 minutes old, then refreshes.

### Notifications
- While the interactive view runs: a dry-run or safe-merge of yours passed, failed or was rejected, a PR became your
  move, changes were requested, your review was requested, a PR of yours was merged. Only what changes between two
  refreshes, never what was already there.
- As terminal notifications in iTerm2, WezTerm, Ghostty, kitty, foot and rxvt-unicode, the bell, or a command of
  yours that gets `{title}`, `{body}` and `{url}` and the event as JSON.

### Cache
- Every fetch is kept in `~/.cache/gh-kotlin-prs/` (`$XDG_CACHE_HOME`). `list --max-age 5m` and `show --max-age 5m`
  answer from it while it's younger than that; ages are still computed from the current time. By default they fetch
  every time, as before. The cache is safe to delete, and old entries go after 3 days.

### Configuration
- `refresh` (3m) and `startupMaxAge` (30m) for the interactive view; `refresh` must be positive.
- `keys` rebinds the interactive view's keys by action, e.g. `keys: {copy: c, refresh: [r, R]}`; a key bound to two
  actions is an error.
- `notify` picks the events and the channels: `events`, `terminal` (`auto`, `osc9`, `osc777`, `osc99`, `none`),
  `bell`, `command` and `timeout`.

### Output changes
- `gh kotlin-prs` without a subcommand opens the interactive view on a terminal; `gh kotlin-prs list` is the plain
  output everywhere.
- `config` and `config init` show `keys` and `notify` as blocks, a line and a source per sub-key; the output of
  `config` reads back as the same config.
- `--debug` prints each query's cost under its name (`Sections`, `PullRequests`, `PullRequest`), or the age of the
  cache entry that answered it.

## v0.1.0 — 2026-10-01

The first release: a read-only view of your open JetBrains/kotlin PRs and the reviews waiting on you.

### Commands
- `gh kotlin-prs list` (or just `gh kotlin-prs`): your PRs and review requests in the sections Mine, Review,
  Team requests and Recently merged (24h). Filters: `--mine`, `--review`, `--waiting-on-me`, `--all`, `--no-teams`,
  `--no-merged`.
- `gh kotlin-prs show <number>`: one PR in detail: every reason, reviewers, code-owner rules, the dry-run and
  safe-merge history, unresolved threads and checks.
- `gh kotlin-prs config`: the effective config and where each value comes from; `config path` prints the file in use,
  `config init` writes a commented starter file.

### What it shows
- Whose move it is on each PR (yours, CI's, the reviewers' or the author's) and why, e.g. "dry-run failed 2h ago" or
  "changes requested by alice_user".
- Review lists only PRs where your review is requested and not given yet; `--all` shows the rest and drafts.
- The latest dry-run and safe-merge from the bots' comments: requested, no response, accepted, running, passed,
  failed, rejected with the bot's reason, or cancelled. Runs from before the last push are marked outdated.
- Approvals and the code-owners verdict; `show` lists each code-owner rule with its owners, who reviewed it, and who
  approved as final or is unavailable.
- Issues from `^KT-123 Fixed`, `^KT-123 Obsolete` and `^KT-123` commit trailers, the branch name and the title.

### Output
- A table, or JSON with `--format json` (`"version": 1`).
- Unicode symbols, or plain ASCII with `--icons ascii`; `--help` has the legend.
- Clickable PR numbers, issues, runs, reasons, reviewers and threads in terminals that support hyperlinks;
  `--hyperlinks auto|always|never`.

### Configuration
- An optional `~/.config/gh-kotlin-prs/config.yml` (or `--config PATH`, `$GH_KOTLIN_PRS_CONFIG`): the repository and
  bots, which team requests to show, the issue projects and issue URL, icons and hyperlinks. Every key has a default.
