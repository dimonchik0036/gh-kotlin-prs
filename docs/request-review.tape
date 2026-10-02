# The README recording of a review request (docs/images/request-review.gif), on the fixtures
# plus the derived #90010 (testdata/demo): a PR of yours with code-owner rules nobody was
# asked for. Demo mode sends nothing. scripts/screenshots.sh runs it like docs/demo.tape.
#
#   vhs docs/request-review.tape

Output docs/images/request-review.gif

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

Env GH_KOTLIN_PRS_DEMO "testdata/raw:testdata/demo"
# The default config: no file at the default location (an explicit missing one is an error).
Env GH_KOTLIN_PRS_CONFIG ""
Env XDG_CONFIG_HOME "/nonexistent"
Hide
Type `PS1='$ '; gh() { gh-kotlin-prs "${@:2}"; }; clear`
Enter
Sleep 300ms
Show

# #90010 is the first row: a re-request and two rules nobody was asked for.
Type "gh kotlin-prs"
Sleep 400ms
Enter
Sleep 2s

# The tree: /analysis/ open with dave_user picked (to re-request), the approved rule
# closed, the two unassigned ones open.
Type "A"
Sleep 2.5s

# judy_user owns both unassigned rules: one pick covers them.
Down 7
Sleep 800ms
Space
Sleep 1.8s

# Collapse that rule and open it again.
Left
Sleep 1s
Right
Sleep 1.2s

# Send: the PR waits on them now (demo mode: nothing sent).
Enter
Sleep 2.2s
Enter
Sleep 1.5s
Down 8
Sleep 2s
Type "q"
Sleep 600ms
