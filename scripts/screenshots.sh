#!/usr/bin/env bash
# Regenerates the README screenshots in docs/images/ from the fixtures (demo mode):
# colors and hyperlinks on, Unicode icons, the fixtures' clock and viewer, no network.
# freeze renders the SVG; scripts/screenshots trims its embedded font to the glyphs used.
# Same fixtures and code → same bytes.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
out="$root/docs/images"
mkdir -p "$out"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

go build -o "$tmp/gh-kotlin-prs" .

# render <name> <args...>: the output of `gh-kotlin-prs <args...>` as docs/images/<name>.svg.
render() {
  local name="$1"
  shift
  env -u NO_COLOR -u COLORTERM CLICOLOR_FORCE=1 TERM=xterm-256color \
    GH_KOTLIN_PRS_DEMO="$root/testdata/raw" GH_KOTLIN_PRS_CONFIG="$tmp/no-config.yml" \
    "$tmp/gh-kotlin-prs" "$@" --icons unicode --hyperlinks always > "$tmp/$name.ansi"
  go run github.com/charmbracelet/freeze@v0.2.2 --language ansi --window \
    -o "$tmp/$name.svg" < "$tmp/$name.ansi" > /dev/null
  (cd "$root/scripts/screenshots" && go run . "$tmp/$name.svg" "$out/$name.svg")
  echo "wrote docs/images/$name.svg ($(wc -c < "$out/$name.svg" | tr -d ' ') bytes)"
}

render list list --all
render show show 90006
