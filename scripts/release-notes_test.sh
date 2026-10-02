#!/usr/bin/env bash
# Tests scripts/release-notes.sh: on CHANGELOG.md, every released version has notes and
# Unreleased is never a release; on a made-up changelog, the edges.
set -euo pipefail
cd "$(dirname "$0")/.."
notes=scripts/release-notes.sh
fail() { echo "release-notes: $*" >&2; exit 1; }

versions="$(awk '/^## v/{ print $2 }' CHANGELOG.md)"
[ -n "$versions" ] || fail "no released version in CHANGELOG.md"
for v in $versions; do
  out="$("$notes" --repo dimonchik0036/gh-kotlin-prs "$v")" || fail "$v: no notes"
  case "$out" in "## "*|*$'\n## '*) fail "$v: another heading in its notes" ;; esac
done
"$notes" Unreleased > /dev/null 2>&1 && fail "Unreleased printed as a release"
"$notes" v0.0.0-missing > /dev/null 2>&1 && fail "a missing version printed"

dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT
cat > "$dir/CHANGELOG.md" <<'MD'
# Changelog

## Unreleased

- Not yet.

## v1.1.0 — 2026-10-03


Summary.

- A change.

## v1.0.1 — 2026-10-02

## v1.0.0 — 2026-10-01

- The first.
MD
check() {
  local want="$1"; shift
  local got
  got="$(CHANGELOG="$dir/CHANGELOG.md" "$notes" "$@")" || fail "$*: failed"
  [ "$got" = "$want" ] || fail "$*: got"$'\n'"$got"$'\n'"want"$'\n'"$want"
}
check $'Summary.\n\n- A change.' v1.1.0
check $'Summary.\n\n- A change.\n\n**Full Changelog**: https://github.com/o/r/compare/v1.0.1...v1.1.0' --repo o/r v1.1.0
check '- The first.' --repo o/r v1.0.0
CHANGELOG="$dir/CHANGELOG.md" "$notes" v1.0.1 > /dev/null 2>&1 && fail "an empty section printed"
CHANGELOG="$dir/CHANGELOG.md" "$notes" v1.0 > /dev/null 2>&1 && fail "a version prefix matched"
echo "release notes: ok"
