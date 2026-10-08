#!/usr/bin/env bash
# Records the README recordings, docs/images/<name>.gif from docs/<name>.tape: demo (the
# interactive view on the fixtures) and request-review (a review request on the derived
# #90010 of testdata/demo), in demo mode: the fixtures' clock and viewer, the default
# config, no network, nothing sent.
# It needs VHS (https://github.com/charmbracelet/vhs) with ttyd and ffmpeg, e.g. `brew install vhs`.
# A recording plays the same keys every time; the GIF's frames differ only in timing.
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

# What each recording must show besides the list: the help's newest keys and a Review row's
# author; the review hint, a name, a pick covering two rows, the question, the pretended send.
shows() {
  case "$1" in
    demo) echo "post /test-public, after asking|pick code owners to request a review from|copy its branch name|Example change          kevin2_user  DR" ;;
    request-review) echo "A request review|judy_user (Judy User)|4 of 5 subsystems covered|Request a review of #90010 from dave_user, judy_user, peggy_user? [y/N]|demo: not sent: requested a review of #90010" ;;
  esac
}

for name in demo request-review; do
  # The tape also writes its frames as text, to check them below.
  perl -pe 's|^(Output docs/images/'"$name"'\.gif)$|$1\nOutput "'"$tmp/$name"'.txt"|' "docs/$name.tape" > "$tmp/$name.tape"
  if ! PATH="$tmp:$PATH" vhs "$tmp/$name.tape" > "$tmp/vhs.log" 2>&1; then
    cat "$tmp/vhs.log" >&2
    exit 1
  fi

  # It shows the list: a binary that failed at once leaves just the prompt.
  if ! grep -q 'Mine (' "$tmp/$name.txt"; then
    echo "error: docs/images/$name.gif never shows the list; its last screen:" >&2
    tail -n 30 "$tmp/$name.txt" >&2
    exit 1
  fi

  IFS='|' read -ra wants <<< "$(shows "$name")"
  for want in "${wants[@]}"; do
    if ! grep -qF -- "$want" "$tmp/$name.txt"; then
      echo "error: docs/images/$name.gif never shows \"$want\"" >&2
      exit 1
    fi
  done

  # The recording shows only the fixtures: no internal hosts, only the fake PR numbers 900xx.
  if grep -qiE 'buildserver|intellij\.net|jetbrains\.team|youtrack' "$tmp/$name.txt"; then
    echo "error: docs/images/$name.gif names an internal host" >&2
    exit 1
  fi
  others="$(grep -oE '#[0-9]+' "$tmp/$name.txt" | grep -vE '^#900[0-9][0-9]$' || true)"
  if [ -n "$others" ]; then
    echo "error: docs/images/$name.gif shows PR numbers that aren't the fixtures': $(echo $others)" >&2
    exit 1
  fi
  echo "wrote docs/images/$name.gif ($(wc -c < "docs/images/$name.gif" | tr -d ' ') bytes)"
done
