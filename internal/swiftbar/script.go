package swiftbar

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ScriptFormat is the version of the plugin script's text, which the script exports as
// GH_KOTLIN_PRS_SCRIPT for the menu to see (none was format 1, v0.4.0 to v0.5.2). Bump it
// on any change to the script's text: the menu then offers `swiftbar update`.
const ScriptFormat = 2

// ScriptOptions are what the plugin script bakes in: SwiftBar runs it with a bare
// environment, so it needs absolute paths.
type ScriptOptions struct {
	// GH is gh's absolute path.
	GH string
	// Config is a config file to pass on, "" for the default one.
	Config string
	// MaxAge lets a run answer from the cache when that's this fresh.
	MaxAge time.Duration
}

// Script is the plugin script. Without arguments it prints the menu; SwiftBar runs it
// with "copy <url>" and "refresh" (a live fetch, after which SwiftBar runs it again).
// GH_KOTLIN_PRS_GH tells the menu which gh opens the interactive view.
func Script(o ScriptOptions) string {
	var b strings.Builder
	// SwiftBar's plugin details show the xbar tags; the schedule is the file name's. No
	// version: it would go stale with every upgrade of the extension.
	b.WriteString(`#!/bin/bash
# <xbar.title>gh kotlin-prs</xbar.title>
# <xbar.author>dimonchik0036</xbar.author>
# <xbar.author.github>dimonchik0036</xbar.author.github>
# <xbar.desc>Your PRs: whose move it is, their dry-runs, safe-merges and reviews.</xbar.desc>
# <xbar.dependencies>gh</xbar.dependencies>
# <xbar.about>https://github.com/dimonchik0036/gh-kotlin-prs</xbar.about>
# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
# <swiftbar.hideAbout>true</swiftbar.hideAbout>
#
# Written by "gh kotlin-prs swiftbar install"; "gh kotlin-prs swiftbar script" prints it.
`)
	writef(&b, "export PATH=%s\n", Quote(filepath.Dir(o.GH)+":/usr/bin:/bin:/usr/sbin:/sbin"))
	writef(&b, "export GH_KOTLIN_PRS_SCRIPT=%d\n", ScriptFormat)
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

// Interval is the run interval a FileName says, false for another name.
func Interval(name string) (time.Duration, bool) {
	if !IsPlugin(name) {
		return 0, false
	}
	d, err := time.ParseDuration(strings.TrimSuffix(strings.TrimPrefix(name, "kotlin-prs."), ".sh"))
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// writtenBy starts the line every script Script writes has, of any format.
const writtenBy = `# Written by "gh kotlin-prs swiftbar install"`

// ErrNotOurs is ReadScript's error for a file Script didn't write.
var ErrNotOurs = errors.New(`not written by "gh kotlin-prs swiftbar install"`)

// Installed is what an installed plugin script says of itself.
type Installed struct {
	// Format is its GH_KOTLIN_PRS_SCRIPT, 1 without one.
	Format int
	// GH and Config are the paths it bakes in, Config "" for none.
	GH, Config string
	// MaxAge is its run's --max-age.
	MaxAge time.Duration
}

// ReadScript reads back what a script of any format bakes in.
func ReadScript(text string) (Installed, error) {
	if !strings.Contains(text, "\n"+writtenBy) {
		return Installed{}, ErrNotOurs
	}
	in := Installed{Format: 1}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "gh="):
			in.GH = Unquote(strings.TrimPrefix(line, "gh="))
		case strings.HasPrefix(line, "export GH_KOTLIN_PRS_CONFIG="):
			in.Config = Unquote(strings.TrimPrefix(line, "export GH_KOTLIN_PRS_CONFIG="))
		case strings.HasPrefix(line, "export GH_KOTLIN_PRS_SCRIPT="):
			n, err := strconv.Atoi(strings.TrimPrefix(line, "export GH_KOTLIN_PRS_SCRIPT="))
			if err != nil {
				return Installed{}, fmt.Errorf("its GH_KOTLIN_PRS_SCRIPT: %w", err)
			}
			in.Format = n
		case strings.HasPrefix(line, `exec "$gh" kotlin-prs list --format swiftbar --max-age `):
			d, err := time.ParseDuration(strings.TrimPrefix(line, `exec "$gh" kotlin-prs list --format swiftbar --max-age `))
			if err != nil {
				return Installed{}, fmt.Errorf("its --max-age: %w", err)
			}
			in.MaxAge = d
		}
	}
	if in.GH == "" {
		return Installed{}, errors.New("it names no gh")
	}
	return in, nil
}

// Unquote undoes Quote.
func Unquote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") {
		return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
	}
	return s
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
