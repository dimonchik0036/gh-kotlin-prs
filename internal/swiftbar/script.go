package swiftbar

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ScriptOptions are what the plugin script bakes in: SwiftBar runs it with a bare
// environment, so it needs absolute paths.
type ScriptOptions struct {
	// GH is gh's absolute path.
	GH string
	// Config is a config file to pass on, "" for the default one.
	Config string
	// MaxAge lets a run answer from the cache when that's this fresh.
	MaxAge time.Duration
	// Version is the tool's version that writes the script, for SwiftBar's plugin details.
	Version string
}

// Script is the plugin script. Without arguments it prints the menu; SwiftBar runs it
// with "copy <url>" and "refresh" (a live fetch, after which SwiftBar runs it again).
// GH_KOTLIN_PRS_GH tells the menu which gh opens the interactive view.
func Script(o ScriptOptions) string {
	var b strings.Builder
	// SwiftBar's plugin details show the xbar tags; the schedule is the file name's.
	writef(&b, `#!/bin/bash
# <xbar.title>gh kotlin-prs</xbar.title>
# <xbar.version>%s</xbar.version>
# <xbar.author>dimonchik0036</xbar.author>
# <xbar.author.github>dimonchik0036</xbar.author.github>
# <xbar.desc>Your PRs: whose move it is, their dry-runs, safe-merges and reviews.</xbar.desc>
# <xbar.dependencies>gh</xbar.dependencies>
# <xbar.about>https://github.com/dimonchik0036/gh-kotlin-prs</xbar.about>
# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
# <swiftbar.hideAbout>true</swiftbar.hideAbout>
#
# Written by "gh kotlin-prs swiftbar install"; "gh kotlin-prs swiftbar script" prints it.
`, strings.NewReplacer("<", "", ">", "", "\n", " ").Replace(o.Version))
	writef(&b, "export PATH=%s\n", Quote(filepath.Dir(o.GH)+":/usr/bin:/bin:/usr/sbin:/sbin"))
	writef(&b, "export GH_KOTLIN_PRS_GH=%s\n", Quote(o.GH))
	if o.Config != "" {
		writef(&b, "export GH_KOTLIN_PRS_CONFIG=%s\n", Quote(o.Config))
	}
	writef(&b, `gh=%s

case "${1:-}" in
  copy) printf '%%s' "$2" | pbcopy; exit ;;
  refresh) exec "$gh" kotlin-prs list --format swiftbar --max-age 0 > /dev/null ;;
esac
exec "$gh" kotlin-prs list --format swiftbar --max-age %s
`, Quote(o.GH), o.MaxAge)
	return b.String()
}

// FileName is the plugin's file name: SwiftBar reads the refresh interval from it
// (kotlin-prs.2m.sh). The interval is whole seconds, minutes or hours.
func FileName(interval time.Duration) (string, error) {
	for _, u := range []struct {
		d    time.Duration
		name string
	}{{time.Hour, "h"}, {time.Minute, "m"}, {time.Second, "s"}} {
		if interval >= u.d && interval%u.d == 0 {
			return fmt.Sprintf("kotlin-prs.%d%s.sh", interval/u.d, u.name), nil
		}
	}
	return "", fmt.Errorf("--interval %s isn't a whole number of seconds", interval)
}

// IsPlugin reports whether a file name is one of FileName's.
func IsPlugin(name string) bool {
	return strings.HasPrefix(name, "kotlin-prs.") && strings.HasSuffix(name, ".sh")
}

// Quote is s for a POSIX shell: as it is when that's safe, else single-quoted.
func Quote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:=@%+-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writef is fmt.Fprintf into a strings.Builder, whose writes never fail.
func writef(b *strings.Builder, format string, args ...any) {
	_, _ = fmt.Fprintf(b, format, args...)
}
