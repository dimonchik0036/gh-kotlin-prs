#!/usr/bin/env bash
# Records the README demo, docs/images/demo.gif, from docs/demo.tape: the interactive view on
# the fixtures (demo mode: their clock and viewer, the default config, no network).
# It needs VHS (https://github.com/charmbracelet/vhs) with ttyd and ffmpeg, e.g. `brew install vhs`.
# The recording plays the same keys every time; the GIF's frames differ only in timing.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
if ! command -v vhs > /dev/null; then
  echo "error: vhs is not installed (https://github.com/charmbracelet/vhs, e.g. brew install vhs)" >&2
  exit 1
fi
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

go build -o "$tmp/gh-kotlin-prs" .

# The tape also writes its frames as text, to check them below.
perl -pe 's|^(Output docs/images/demo\.gif)$|$1\nOutput "'"$tmp"'/demo.txt"|' docs/demo.tape > "$tmp/demo.tape"
if ! PATH="$tmp:$PATH" vhs "$tmp/demo.tape" > "$tmp/vhs.log" 2>&1; then
  cat "$tmp/vhs.log" >&2
  exit 1
fi

# The recording shows only the fixtures: no internal hosts, only the fake PR numbers 900xx.
if grep -qiE 'buildserver|intellij\.net|jetbrains\.team|youtrack' "$tmp/demo.txt"; then
  echo "error: the recording names an internal host" >&2
  exit 1
fi
others="$(grep -oE '#[0-9]+' "$tmp/demo.txt" | grep -vE '^#900[0-9][0-9]$' || true)"
if [ -n "$others" ]; then
  echo "error: the recording shows PR numbers that aren't the fixtures': $(echo $others)" >&2
  exit 1
fi
echo "wrote docs/images/demo.gif ($(wc -c < docs/images/demo.gif | tr -d ' ') bytes)"
