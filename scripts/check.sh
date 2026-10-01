#!/usr/bin/env bash
# Runs every check a commit must pass, failing fast: formatting, vet, staticcheck,
# govulncheck, and the tests as CI runs them (no gh login, no config, no network).
# The tool versions are pinned as tool dependencies in go.mod.
set -euo pipefail
cd "$(dirname "$0")/.."

step() { printf '==> %s\n' "$*"; }

step gofmt
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  printf 'not gofmt-ed:\n%s\n' "$unformatted" >&2
  exit 1
fi

step go vet
go vet ./...

step staticcheck
go tool staticcheck ./...

step govulncheck
go tool govulncheck ./...

# go-gh also asks `gh auth token`, which reads the system keyring: GH_PATH points it at
# `false`. Any network access fails fast through the dead proxy.
step go test
env -u GH_TOKEN -u GITHUB_TOKEN -u GH_ENTERPRISE_TOKEN -u GITHUB_ENTERPRISE_TOKEN \
  GH_CONFIG_DIR="$(mktemp -d)" GH_PATH=/usr/bin/false HTTPS_PROXY=http://127.0.0.1:9 \
  go test -count=1 ./...
