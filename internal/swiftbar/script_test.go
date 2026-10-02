package swiftbar

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScript(t *testing.T) {
	s := Script(ScriptOptions{GH: "/opt/homebrew/bin/gh", Config: "/Users/alice_user/my config.yml", MaxAge: time.Minute})
	golden(t, "swiftbar-script.sh", s)
	// SwiftBar 2.1.1's PluginMetadata reads each tag between "<xbar.<key>>" and its close.
	for key, want := range map[string]string{"title": "gh kotlin-prs", "author": "dimonchik0036",
		"author.github": "dimonchik0036", "dependencies": "gh", "about": "https://github.com/dimonchik0036/gh-kotlin-prs"} {
		_, rest, _ := strings.Cut(s, "<xbar."+key+">")
		if got, _, _ := strings.Cut(rest, "</xbar."+key+">"); got != want {
			t.Errorf("xbar.%s is %q, want %q", key, got, want)
		}
	}
	if strings.Contains(s, "schedule>") || strings.Contains(s, "version>") {
		t.Error("a schedule besides the file name's, or a version that goes stale")
	}
	for _, want := range []string{
		"#!/bin/bash\n",
		"# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>\n",
		"# <swiftbar.hideAbout>true</swiftbar.hideAbout>\n",
		"export PATH=/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin\n",
		"export GH_KOTLIN_PRS_CONFIG='/Users/alice_user/my config.yml'\n",
		"export GH_KOTLIN_PRS_GH=/opt/homebrew/bin/gh\n",
		"export GH_KOTLIN_PRS_SCRIPT=2\n",
		`refresh) exec "$gh" kotlin-prs list --format swiftbar --max-age 0 > /dev/null ;;`,
		"exec \"$gh\" kotlin-prs list --format swiftbar --max-age 1m0s\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the script lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(Script(ScriptOptions{GH: "/usr/local/bin/gh", MaxAge: time.Minute}), "GH_KOTLIN_PRS_CONFIG") {
		t.Error("a config without one")
	}
	path := filepath.Join(t.TempDir(), "kotlin-prs.2m.sh")
	if err := os.WriteFile(path, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Errorf("bash -n: %v\n%s", err, out)
	}
}

// The script's own arguments: copy puts the URL on the clipboard, the rest run gh.
func TestScriptRuns(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "gh")
	log := filepath.Join(dir, "log")
	if err := os.WriteFile(fake, []byte("#!/bin/bash\necho \"$*\" >> "+Quote(log)+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "kotlin-prs.2m.sh")
	if err := os.WriteFile(script, []byte(Script(ScriptOptions{GH: fake, MaxAge: time.Minute})), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{}, {"refresh"}} {
		if out, err := exec.Command(script, args...).CombinedOutput(); err != nil {
			t.Fatalf("%q: %v\n%s", args, err, out)
		}
	}
	data, _ := os.ReadFile(log)
	want := "kotlin-prs list --format swiftbar --max-age 1m0s\nkotlin-prs list --format swiftbar --max-age 0\n"
	if string(data) != want {
		t.Errorf("gh ran with\n%s\nwant\n%s", data, want)
	}
}

func TestFileName(t *testing.T) {
	for d, want := range map[time.Duration]string{2 * time.Minute: "kotlin-prs.2m.sh", 90 * time.Second: "kotlin-prs.90s.sh", time.Hour: "kotlin-prs.1h.sh", 30 * time.Second: "kotlin-prs.30s.sh"} {
		if got, err := FileName(d); err != nil || got != want || !IsPlugin(got) {
			t.Errorf("FileName(%s) = %q, %v", d, got, err)
		}
	}
	for _, d := range []time.Duration{0, 1500 * time.Millisecond, -time.Minute} {
		if got, err := FileName(d); err == nil {
			t.Errorf("FileName(%s) = %q", d, got)
		}
	}
	if IsPlugin("other.1m.sh") {
		t.Error("another plugin")
	}
}

func TestQuote(t *testing.T) {
	for s, want := range map[string]string{"gh": "gh", "/a/b-c_d.e": "/a/b-c_d.e", "a b": "'a b'", "it's": `'it'\''s'`, "": "''", "$HOME": "'$HOME'"} {
		if got := Quote(s); got != want {
			t.Errorf("Quote(%q) = %s, want %s", s, got, want)
		}
	}
}

// ReadScript reads back what Script wrote, and what v0.4.0 to v0.5.2 wrote (format 1:
// a version line, no GH_KOTLIN_PRS_SCRIPT, --max-age half the interval).
func TestReadScript(t *testing.T) {
	data, err := os.ReadFile("../../testdata/swiftbar-script-format1.sh")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadScript(string(data))
	if want := (Installed{Format: 1, GH: "/opt/homebrew/bin/gh", Config: "/Users/alice_user/my config.yml", MaxAge: 90 * time.Second}); err != nil || got != want {
		t.Errorf("format 1: %+v, %v, want %+v", got, err, want)
	}
	o := ScriptOptions{GH: "/Users/alice_user/my tools/gh", Config: "/Users/alice_user/it's.yml", MaxAge: 3 * time.Minute}
	got, err = ReadScript(Script(o))
	if want := (Installed{Format: ScriptFormat, GH: o.GH, Config: o.Config, MaxAge: o.MaxAge}); err != nil || got != want {
		t.Errorf("round trip: %+v, %v, want %+v", got, err, want)
	}
	if got, err := ReadScript(Script(ScriptOptions{GH: "/opt/homebrew/bin/gh", MaxAge: time.Minute})); err != nil || got.Config != "" {
		t.Errorf("no config: %+v, %v", got, err)
	}
	for text, want := range map[string]error{
		"#!/bin/bash\necho 'my own plugin'\n": ErrNotOurs,
		"":                                    ErrNotOurs,
	} {
		if _, err := ReadScript(text); !errors.Is(err, want) {
			t.Errorf("%q: %v", text, err)
		}
	}
	broken := strings.Replace(Script(o), "export GH_KOTLIN_PRS_SCRIPT=2", "export GH_KOTLIN_PRS_SCRIPT=two", 1)
	if _, err := ReadScript(broken); err == nil || errors.Is(err, ErrNotOurs) {
		t.Errorf("a broken format: %v", err)
	}
}

func TestUnquote(t *testing.T) {
	for _, s := range []string{"/opt/homebrew/bin/gh", "/a b/c", "it's", "''", "a'b'c", ""} {
		if got := Unquote(Quote(s)); got != s {
			t.Errorf("Unquote(Quote(%q)) = %q", s, got)
		}
	}
}

func TestInterval(t *testing.T) {
	for name, want := range map[string]time.Duration{"kotlin-prs.30s.sh": 30 * time.Second, "kotlin-prs.3m.sh": 3 * time.Minute,
		"kotlin-prs.1h.sh": time.Hour, "kotlin-prs.sh": 0, "kotlin-prs.0s.sh": 0, "other.3m.sh": 0, "kotlin-prs.3m.py": 0} {
		if got, ok := Interval(name); got != want || ok != (want != 0) {
			t.Errorf("%s: %s, %v", name, got, ok)
		}
	}
	for _, d := range []time.Duration{30 * time.Second, 3 * time.Minute, time.Hour} {
		name, _ := FileName(d)
		if got, ok := Interval(name); !ok || got != d {
			t.Errorf("%s: %s", name, got)
		}
	}
}
