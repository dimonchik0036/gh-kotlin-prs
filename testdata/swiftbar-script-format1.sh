#!/bin/bash
# <xbar.title>gh kotlin-prs</xbar.title>
# <xbar.version>v0.5.2</xbar.version>
# <xbar.author>dimonchik0036</xbar.author>
# <xbar.author.github>dimonchik0036</xbar.author.github>
# <xbar.desc>Your PRs: whose move it is, their dry-runs, safe-merges and reviews.</xbar.desc>
# <xbar.dependencies>gh</xbar.dependencies>
# <xbar.about>https://github.com/dimonchik0036/gh-kotlin-prs</xbar.about>
# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
# <swiftbar.hideAbout>true</swiftbar.hideAbout>
#
# Written by "gh kotlin-prs swiftbar install"; "gh kotlin-prs swiftbar script" prints it.
export PATH=/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin
export GH_KOTLIN_PRS_GH=/opt/homebrew/bin/gh
export GH_KOTLIN_PRS_CONFIG='/Users/alice_user/my config.yml'
gh=/opt/homebrew/bin/gh

case "${1:-}" in
  copy) printf '%s' "$2" | pbcopy; exit ;;
  refresh) exec "$gh" kotlin-prs list --format swiftbar --max-age 0 > /dev/null ;;
esac
exec "$gh" kotlin-prs list --format swiftbar --max-age 1m30s
