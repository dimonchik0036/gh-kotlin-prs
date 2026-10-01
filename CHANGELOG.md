# Changelog

All notable user-visible changes. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [RELEASING.md](RELEASING.md). The JSON output has its own `"version"`, bumped only on
breaking JSON changes and noted here.

## Unreleased

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
