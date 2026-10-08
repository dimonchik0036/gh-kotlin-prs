#!/usr/bin/env bash
# Regenerates the GraphQL fixtures testdata/raw/pr-<n>.json: fetches the given PRs plus
# every existing fixture, then anonymizes them all in one batch (scripts/anonymize), so
# pseudonyms agree across files. Raw responses only live in a temp dir.
#
# Fixture numbers are fake. testdata/.fixture-map.json (local, gitignored, never commit it)
# maps the real PR numbers to them; without it the existing fixtures can't be refetched,
# only new ones added.
#
#   scripts/fetch-fixtures.sh              # refresh the existing fixtures
#   scripts/fetch-fixtures.sh <pr-number>  # and add these real PRs
#   REPO=JetBrains/kotlin scripts/fetch-fixtures.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
repo="${REPO:-JetBrains/kotlin}"
out="$root/testdata/raw"
map="$root/testdata/.fixture-map.json"
mkdir -p "$out"

numbers=("$@")
if [ -f "$map" ]; then
  while read -r n; do numbers+=("$n"); done < <(jq -r '.prs | keys[]' "$map")
elif compgen -G "$out/pr-*.json" > /dev/null; then
  if [ ${#numbers[@]} -eq 0 ]; then
    echo "error: $map is missing." >&2
    echo "It maps the real PRs behind the committed fixtures and is kept out of git, so without it" >&2
    echo "they can't be refetched. You can still add new fixtures: $0 <pr-number>..." >&2
    exit 1
  fi
  echo "warning: $map is missing: only adding new fixtures, existing ones stay as they are" >&2
fi
if [ ${#numbers[@]} -eq 0 ]; then
  echo "usage: $0 <pr-number>..." >&2
  exit 2
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

query="query PullRequest(\$owner: String!, \$name: String!, \$number: Int!) {
  viewer { login }
  rateLimit { limit cost remaining resetAt }
  repository(owner: \$owner, name: \$name) { pullRequest(number: \$number) { ...PR body } }
}
$(cat "$root/internal/github/pr.graphql")"

for n in $(printf '%s\n' "${numbers[@]}" | sort -un); do
  gh api graphql \
    -F owner="${repo%%/*}" -F name="${repo#*/}" -F number="$n" -f query="$query" > "$tmp/pr-$n.json"
done

(cd "$root" && go run ./scripts/anonymize -map "$map" -out "$out" "$tmp"/pr-*.json)
