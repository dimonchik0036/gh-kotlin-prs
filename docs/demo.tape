# The README recording of the interactive view (docs/images/demo.gif), on the fixtures:
# scripts/screenshots.sh runs it with gh-kotlin-prs on the PATH, from the repository root.
#
#   vhs docs/demo.tape

Output docs/images/demo.gif

Require gh-kotlin-prs

Set Shell bash
Set FontSize 14
Set Columns 110
Set Rows 28
Set Padding 16
Set Framerate 12
Set TypingSpeed 70ms
Set CursorBlink false
Set Theme { "name": "gh-kotlin-prs", "background": "#171717", "foreground": "#c4c4c4", "cursor": "#c4c4c4", "selection": "#3a3a3a", "black": "#171717", "red": "#D74E6F", "green": "#31BB71", "yellow": "#D3E561", "blue": "#8056FF", "magenta": "#ED61D7", "cyan": "#04D7D7", "white": "#c4c4c4", "brightBlack": "#4D4D4D", "brightRed": "#FE5F86", "brightGreen": "#00D787", "brightYellow": "#EBFF71", "brightBlue": "#9B79FF", "brightMagenta": "#FF7AEA", "brightCyan": "#00FEFE", "brightWhite": "#FAFAFA" }

# The fixtures instead of GitHub, the default config, and `gh kotlin-prs` for the binary.
Env GH_KOTLIN_PRS_DEMO "testdata/raw"
Env GH_KOTLIN_PRS_CONFIG "/nonexistent/gh-kotlin-prs.yml"
Hide
Type `PS1='$ '; gh() { gh-kotlin-prs "${@:2}"; }; clear`
Enter
Sleep 300ms
Show

Type "gh kotlin-prs"
Sleep 400ms
Enter
Sleep 1.5s

# Move the selection, open the details and scroll them, back.
Type "j"
Sleep 700ms
Type "j"
Sleep 900ms
Enter
Sleep 1.2s
Down 6
Sleep 1s
Escape
Sleep 800ms

# Filter the rows, clear the filter.
Type "/"
Sleep 300ms
Type@110ms "dry-run"
Sleep 1.3s
Escape
Sleep 700ms

# Also the reviews not waiting on you, then the help.
Type "a"
Sleep 1.6s
Type "?"
Sleep 2s
Type "q"
Sleep 600ms
