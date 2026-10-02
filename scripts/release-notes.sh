#!/usr/bin/env bash
# Prints a version's notes for its GitHub release, from CHANGELOG.md: the lines between
# its "## <version> — <date>" heading and the next "## " heading, without the heading
# and the blank lines around them. With --repo owner/name, a last line compares the
# version with the one below it ("**Full Changelog**: …/compare/<previous>...<version>").
# Fails when the version has no section, or an empty one. $CHANGELOG names another file.
#
#   scripts/release-notes.sh v0.5.2
#   scripts/release-notes.sh --repo dimonchik0036/gh-kotlin-prs v0.5.2
set -euo pipefail

repo=""
if [ "${1:-}" = --repo ]; then
  repo="${2:?--repo needs owner/name}"
  shift 2
fi
if [ $# -ne 1 ] || [[ "$1" != v* ]]; then
  echo "usage: $0 [--repo owner/name] v<version>" >&2
  exit 2
fi
version="$1"
changelog="${CHANGELOG:-$(dirname "$0")/../CHANGELOG.md}"

# The section's lines, from the first one with text: $(...) drops the trailing blank ones.
notes="$(awk -v v="$version" '/^## /{ if (inside) exit; inside = ($2 == v); next } inside' "$changelog" | sed '/./,$!d')"
if [ -z "${notes//[[:space:]]/}" ]; then
  echo "error: $changelog has no notes for $version" >&2
  exit 1
fi
printf '%s\n' "$notes"
if [ -n "$repo" ]; then
  previous="$(awk -v v="$version" '/^## /{ if (seen) { print $2; exit } seen = ($2 == v) }' "$changelog")"
  if [ -n "$previous" ]; then
    printf '\n**Full Changelog**: https://github.com/%s/compare/%s...%s\n' "$repo" "$previous" "$version"
  fi
fi
