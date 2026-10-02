package config

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// NotifyEvents are the events the TUI and the menu-bar plugin can notify about
// (internal/notify), with what each means.
var NotifyEvents = []struct{ Name, Doc string }{
	{"runPassed", "a dry-run or safe-merge of yours passed"},
	{"runFailed", "a dry-run or safe-merge of yours failed"},
	{"runRejected", "the bot rejected a /dry-run or /safe-merge of yours"},
	{"myMove", "a PR of yours became your move"},
	{"changesRequested", "a reviewer requested changes on a PR of yours"},
	{"reviewRequested", "your review was requested"},
	{"merged", "a PR of yours was merged"},
}

// Terminal notification protocols: OSC 9 (iTerm2, WezTerm, Ghostty), OSC 777 (foot,
// rxvt-unicode), kitty's OSC 99; auto picks one by the terminal, none sends nothing.
var notifyTerminals = []string{"auto", "osc9", "osc777", "osc99", "none"}

// Notify configures the notifications of the TUI and the menu-bar plugin. Events, Command
// and Timeout apply to both; each has its own channels besides the command, which runs
// whatever they are: the TUI a terminal notification and the bell, the plugin SwiftBar's.
type Notify struct {
	Events   []string `yaml:"events"`
	Terminal string   `yaml:"terminal"`
	Bell     bool     `yaml:"bell"`
	// Swiftbar is the plugin's notification through SwiftBar (AppleScript outside it).
	Swiftbar bool `yaml:"swiftbar"`
	// Command is run per event, its arguments' {title}, {body} and {url} replaced, the
	// event as JSON on stdin. Empty runs nothing.
	Command []string `yaml:"command"`
	Timeout Duration `yaml:"timeout"`
}

// DefaultNotify notifies of every event through the terminal, when it's known to support
// that, and through SwiftBar.
func DefaultNotify() Notify {
	events := make([]string, len(NotifyEvents))
	for i, e := range NotifyEvents {
		events[i] = e.Name
	}
	return Notify{Events: events, Terminal: "auto", Swiftbar: true, Command: []string{}, Timeout: Duration(10 * time.Second)}
}

func (n *Notify) validate() error {
	for i, e := range n.Events {
		if !slices.ContainsFunc(NotifyEvents, func(k struct{ Name, Doc string }) bool { return k.Name == e }) {
			return fmt.Errorf("config: notify: unknown event %q", e)
		}
		if slices.Contains(n.Events[:i], e) {
			return fmt.Errorf("config: notify: event %q listed twice", e)
		}
	}
	if !slices.Contains(notifyTerminals, n.Terminal) {
		return fmt.Errorf("config: notify: terminal must be one of %s, got %q", strings.Join(notifyTerminals, ", "), n.Terminal)
	}
	if len(n.Command) > 0 && strings.TrimSpace(n.Command[0]) == "" {
		return fmt.Errorf("config: notify: command must start with a program")
	}
	if n.Timeout <= 0 {
		return fmt.Errorf("config: notify: timeout must be positive, got %s", time.Duration(n.Timeout))
	}
	return nil
}
