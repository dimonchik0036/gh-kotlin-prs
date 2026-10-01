# gh-kotlin-prs

A `gh` extension that shows the open [JetBrains/kotlin](https://github.com/JetBrains/kotlin) PRs you're involved in:
their dry-run / safe-merge status, where the review stands, and who has the next move. See [SPEC.md](SPEC.md).

![The interactive view: moving the selection, a PR's details, the filter, --all and the help](docs/images/demo.gif)

`gh kotlin-prs list` and `show` print the same rows and details as plain text. The recording uses the anonymized test
fixtures (`scripts/screenshots.sh`).

## Install

```sh
gh extension install dimonchik0036/gh-kotlin-prs
gh extension upgrade kotlin-prs   # later updates
gh alias set kp kotlin-prs        # optional short alias
```

After that, `gh kp` works in place of `gh kotlin-prs` in every command below, e.g. `gh kp`, `gh kp list`.

It needs `gh` logged in (`gh auth login`): the extension uses gh's authentication and never stores the token. For a specific
version, add `--pin vX.Y.Z` to the install.

## Usage

```sh
gh kotlin-prs                 # the interactive view on a terminal (? for its keys), `list` otherwise
gh kotlin-prs list            # Mine, Review, Team requests, Recently merged (24h)
gh kotlin-prs list --mine     # only your PRs (and the recently merged ones)
gh kotlin-prs list --review --waiting-on-me
gh kotlin-prs list --all      # also reviews not waiting on you and drafts
gh kotlin-prs list --no-teams --no-merged
gh kotlin-prs list --format json   # the model with "version": 1
gh kotlin-prs show <number>   # reviewers, code owners, runs, threads and all reasons
```

### Commands

`run` posts one of the bot's commands on a PR of yours, as a regular comment, after showing it and asking:

```sh
gh kotlin-prs run 90006 dry-run        # /dry-run; also dry-run-retry, safe-merge, cancel-coordinator, fixup, codeowners
gh kotlin-prs run 90006 safe-merge --yes
```

It refuses what the bot would refuse or what makes no sense now: someone else's PR, a closed one, a second
dry-run or safe-merge while one is requested or running, a safe-merge on a draft or before approval,
`cancel-coordinator` with nothing running, `codeowners` while its check is green. Without a terminal it needs
`--yes`. The interactive view posts the same commands (below). Posting them is the only thing the tool ever writes to
GitHub.

### Interactive view

On a terminal, `gh kotlin-prs` (with the same section flags as `list`) shows the same rows with a selection, refreshes
them in the background every 3 minutes (config `refresh`), and keeps the ages moving in between. It starts from the
cache when the last fetch is at most 30 minutes old (config `startupMaxAge`), else it waits for GitHub.

```
j/k, up/down  select            enter  details (esc: back)     o  open the PR       r  refresh now
tab           next section      /      filter                  b  open its build    ?  help and symbols
a             toggle --all      g/G    first / last            y  copy its URL      q  quit
x             commands for it: D dry-run, R dry-run --retry, M safe-merge, C cancel-coordinator, F fixup, O codeowners
```

A command asks `Post /dry-run to #90006 (…)? [y/N]` first; only `y` posts. The row then shows the run as requested
until the next refresh shows what the bot made of it.

`keys` in the config rebinds them by action (`keys: {copy: c, refresh: [r, F5]}`; `?` and `config` list the actions);
a key bound twice is an error. `r` does nothing while a refresh runs or for 5 seconds after one, except to retry a
failed one. The status bar says when the data was fetched and when the next refresh is due, keeps the last data on screen when a
refresh fails, and warns when less than a tenth of the hourly API budget is left. In a pipe, or with
`--format json`, the bare command prints `list` as before.

### Notifications

While it runs, the interactive view notifies you of what changed between two refreshes: a dry-run or safe-merge of
yours passed, failed or was rejected, a PR became your move, changes were requested, your review was requested, a PR
of yours was merged (config `notify.events`). Nothing is sent for what was already there when it started.

They go through the terminal where it's known to support that (iTerm2, WezTerm, Ghostty, kitty, foot, rxvt-unicode;
`notify.terminal`), optionally the bell (`notify.bell: true`), and any command you like: its arguments get `{title}`,
`{body}` and `{url}`, its stdin the event as JSON.

```yaml
notify:
  command: [terminal-notifier, -title, "{title}", -message, "{body}", -open, "{url}"]   # macOS
  # command: [notify-send, "{title}", "{body}"]                                         # Linux
```

`--debug` prints the GraphQL cost of each request to stderr, or the cache entry that answered it (not in the
interactive view).

Every fetch is kept in `~/.cache/gh-kotlin-prs/` (`$XDG_CACHE_HOME`), and `--max-age` lets `list` and `show` use it
instead of asking GitHub while it's young enough. The ages ("failed 2h ago") are still computed from the current time.
By default, or with `--max-age 0`, they always fetch:

```sh
gh kotlin-prs list --max-age 5m   # at most one fetch every 5 minutes, e.g. from a shell prompt or a status bar
```

The cache is safe to delete at any time; files not written for 3 days are dropped on their own.

On a terminal, PR numbers, issue IDs, the DR / SM cells and the reasons are clickable (OSC 8 hyperlinks): a reason
opens its build, the bot's reply, the review or the thread; `show` also links builds (as a short `build <id>`), logins and threads.
`--hyperlinks auto|always|never` (config `hyperlinks`) overrides that; JSON never has links.

A row reads:

```
❯  #90005  KT-990003 +1  Example change  DR ✓  SM -  0/2 ✗  2 threads  changes requested by alice_user
```

The first column is whose move it is, then the issue, the latest dry-run (`DR`) and safe-merge (`SM`), approvals out
of the people reviewing with the code-owners verdict, unresolved threads and the main reason. `show` lists every reason.

Issues come from `^KT-123 Fixed` / `^KT-123 Obsolete` / `^KT-123` trailers in the PR's commit messages, then the branch
name and the title. The row shows the primary one (fixed first) and how many more: `KT-990003 +1`; `show` lists them all.

Review lists only PRs where your review is requested and you haven't answered it yet; the rest is counted in a
`+4 reviews not waiting on you, 1 draft (--all)` line and shown with `--all`.

Symbols are single-width text characters, so the columns line up in any terminal (`--help` prints the same legend):

```
  next move:   ❯ yours, ⟳ CI, ∙ reviewers or author, ✓ done
  DR / SM:     - none, ? requested, ! no response, ⋯ accepted, ⟳ running, ✓ passed, ✗ failed, ⊘ rejected, ⨯ cancelled, ~ prefix: older than the last push
  approvals:   A/R approved/reviewing, ✓ code owners ok, ✗ code owners missing, ? unknown
```

With `--icons ascii` (or `icons: ascii` in the config):

```
  next move:   ! yours, > CI, . reviewers or author, + done
  DR / SM:     - none, ? requested, ! no response, ^ accepted, > running, + passed, x failed, / rejected, c cancelled, ~ prefix: older than the last push
  approvals:   A/R approved/reviewing, + code owners ok, x code owners missing, ? unknown
```

`show` spells out the code-owner bot's marks: `❌` → "no review" (or "no reviewer assigned"), `🔄` → "commented, review
not re-requested", `🔴` → "changes requested", `✅` → "approved", and `🔒` / `⏳` → "(final)" / "(unavailable)" after
the login.

Exit codes: 0 ok, 1 error, 2 bad usage.

## Config

`~/.config/gh-kotlin-prs/config.yml` (or `$XDG_CONFIG_HOME/gh-kotlin-prs/config.yml`); `--config PATH` or
`$GH_KOTLIN_PRS_CONFIG` point elsewhere. Every key is optional:

```sh
gh kotlin-prs config          # the effective config, each value marked default, file or flag
gh kotlin-prs config path     # the file in use
gh kotlin-prs config init     # write the file with every key and its default, commented out (--force to overwrite)
```


```yaml
repo: JetBrains/kotlin
bots: [KotlinBuild, kotlin-safemerge, kodee-bot]
gateBot: KotlinBuild
ownersBot: kotlin-safemerge
teams: []                 # only show team requests to these teams; empty means all
refresh: 3m
startupMaxAge: 30m        # the TUI starts from cached data at most this old
requestedTimeout: 10m
icons: unicode            # or ascii
issueProjects: [KT, KTIJ, KTI]   # issue IDs recognized in commit trailers, branch names and titles
issueURL: https://youtrack.jetbrains.com/issue/{id}
hyperlinks: auto          # always, never
keys: {}                  # TUI keys by action, e.g. {copy: c, refresh: [r, F5]}; `?` lists the defaults
notify:                   # TUI notifications; unset keys keep these defaults
  events: [runPassed, runFailed, runRejected, myMove, changesRequested, reviewRequested, merged]
  terminal: auto          # osc9, osc777, osc99, none
  bell: false
  command: []             # e.g. [terminal-notifier, -title, "{title}", -message, "{body}", -open, "{url}"]
  timeout: 10s
```

## Development

Build and install the working copy in place of a release (`gh extension remove kotlin-prs` first if one is installed):

```sh
go build && gh extension install .
```

Run `scripts/check.sh` before every commit; CI runs the same script. It checks formatting, `go vet`, staticcheck and
govulncheck (pinned as tool dependencies in `go.mod`), then runs the tests the way CI does: without your gh login,
your config or the network. To run it on every commit, enable the bundled hook: `git config core.hooksPath .githooks`.

```sh
scripts/check.sh
go test ./internal/classify ./internal/render -update   # rewrite goldens (in this order), review the diff
scripts/fetch-fixtures.sh <pr-number>                    # refresh testdata/raw, adding these PRs
```

`GH_KOTLIN_PRS_DEMO=testdata/raw gh kotlin-prs list` (or `show <fixture number>`) serves the fixtures instead of
GitHub, with the fixtures' clock and viewer: no login, no network. The README's recording comes from it:
`scripts/screenshots.sh` plays `docs/demo.tape` with [VHS](https://github.com/charmbracelet/vhs).

`testdata/raw` holds GraphQL responses, `testdata/golden` the model JSON
and table text they produce. `scripts/fetch-fixtures.sh` refetches every fixture and passes the batch through
`scripts/anonymize`: logins become pseudonyms (alice_user, bob_user, …) except yours and the bots', human text becomes
filler, and bot comments stay verbatim apart from the logins in them. Raw responses never leave a temp dir.
PR and KT numbers, commit SHAs, merge-request ids and thread paths are fake too. The real → fake map is
`testdata/.fixture-map.json`: local, gitignored, never commit it. Without it the script can't refetch the
existing fixtures, only add new ones.
After refreshing fixtures, regenerate the goldens and review them.

## Releasing

See [RELEASING.md](RELEASING.md); user-visible changes go to [CHANGELOG.md](CHANGELOG.md).

## License

Licensed under the [Apache License 2.0](LICENSE).
