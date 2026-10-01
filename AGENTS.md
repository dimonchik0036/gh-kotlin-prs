# Agent guide

`gh kotlin-prs` is a gh extension in Go. [SPEC.md](SPEC.md) is the spec (data, bot protocol, whose-move rules,
CLI); keep it in sync with behavior changes. [README.md](README.md) is for users.

## Build and check

Run `scripts/check.sh` before every commit; CI runs the same script. It runs gofmt, go vet, staticcheck,
govulncheck (both pinned as tool dependencies in go.mod) and the tests as CI runs them: no gh login, no config,
no network. To run it on every commit: `git config core.hooksPath .githooks`.

Workflow actions are pinned to full commit SHAs with the version as a comment (`@<sha> # v7.0.1`); Dependabot bumps them.

GoLand's inspections keep flagging the same things: don't shadow builtins (`real`, `len`, `new`, …), don't name
variables after imported packages (`width`, `ansi`, …), start doc comments with the identifier ("RunRequested is …"),
don't escape what needs no escape in regexps (`]` outside a class), write intentionally non-nil empty slices as
`make([]T, 0)`, use a value only after checking the error that comes with it, and give all of a type's methods
pointer receivers or all value receivers.

Tests never touch gh's auth, the user's config, the user's cache or the network: inject the client, config path,
cache dir and clock (`internal/cli` env, `classify.Classifier.Now`, `cache.Client`). `GH_KOTLIN_PRS_DEMO=<dir>` runs the CLI on the fixtures in `<dir>`
through the same seams (`internal/demo`), with their clock and viewer; `scripts/screenshots.sh` records the README
demo from it (`docs/demo.tape` with VHS, which it needs; it fails when a frame shows an internal host or a real PR
number). Goldens: `go test ./internal/classify ./internal/render -update`,
in that order, then review the diff.

## Fixtures

Only through `scripts/fetch-fixtures.sh`, which anonymizes everything it writes. Never commit raw GraphQL responses,
real logins (other than the repo owner's and the bots'), PR titles or human comment text.
`TestCommittedFixturesAreAnonymized` guards this.
PR and KT numbers, commit SHAs and thread paths are fake as well. The real → fake map is
`testdata/.fixture-map.json`, local and gitignored: never commit it, and never write real numbers into tests or docs.
Synthetic test data uses pseudonyms (alice_user, bob_user, …).
The tool relies only on what is publicly visible on JetBrains/kotlin PRs; don't add links to non-public pages.

## Commits

- Short imperative subject; a body only when the why isn't obvious.
- Logical commits, each one green on its own (`scripts/check.sh`).
- `origin/main` is published: add commits on top, never rewrite it.
- Every user-visible change adds a line to `## Unreleased` in [CHANGELOG.md](CHANGELOG.md) in the same commit.
- Releases follow [RELEASING.md](RELEASING.md).
