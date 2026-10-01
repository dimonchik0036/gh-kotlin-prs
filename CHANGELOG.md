# Changelog

All notable user-visible changes. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [RELEASING.md](RELEASING.md). The JSON output has its own `"version"`, bumped only on
breaking JSON changes and noted here.

## Unreleased

- `gh kotlin-prs` on a terminal opens an interactive view: the rows of `list` with a selection, the `show` detail on
  `enter`, a filter (`/`), `--all` on `a`, open the PR (`o`) or its build (`b`), copy its URL (`y`), and a refresh
  every `refresh` (3m) or on `r` (one at a time, and not again within 5s unless the last one failed), with a status
  bar. It starts from the cache when that's at most `startupMaxAge` old.
  In a pipe or with `--format json`, it prints `list` as before.
- Config: `keys` rebinds the interactive view's keys by action, e.g. `keys: {copy: c, refresh: [r, R]}`.
- Every fetch is cached in `~/.cache/gh-kotlin-prs/` (`$XDG_CACHE_HOME`); `list --max-age 5m` and `show --max-age 5m`
  answer from it while it's younger than that. Ages are still computed from the current time. By default they
  always fetch, as before.
- `--debug` prints each query's cost under its name (`Sections`, `PullRequests`, `PullRequest`), or the age of the
  cache entry that answered it.
- Config: `startupMaxAge` (default 30m), the oldest cached data the TUI starts from; `refresh` must be positive.

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
