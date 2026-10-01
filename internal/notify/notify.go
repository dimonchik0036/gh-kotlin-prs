// Package notify turns two consecutive snapshots of the list into events (Diff) and
// delivers them: as terminal notifications (OSC 9, OSC 777 or kitty's OSC 99), the bell,
// and a user command. It knows nothing of the TUI: the escapes come back as a string
// for the caller to write, and the command runs where the caller wants, so a daemon can
// reuse it.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// Kinds of events, as config.NotifyEvents names them.
const (
	RunPassed        = "runPassed"
	RunFailed        = "runFailed"
	RunRejected      = "runRejected"
	MyMove           = "myMove"
	ChangesRequested = "changesRequested"
	ReviewRequested  = "reviewRequested"
	Merged           = "merged"
)

// Event is one notification; its JSON is what the command gets on stdin.
type Event struct {
	Kind    string `json:"kind"`
	Number  int    `json:"number"`
	PRTitle string `json:"prTitle"`
	// Title and Body are the notification's text: "#90006 dry-run failed", the PR's title.
	Title string `json:"title"`
	Body  string `json:"body"`
	// URL is the page to open: the build, the bot's reply, the PR.
	URL string `json:"url"`
}

// Diff is what changed from prev to next, two live snapshots of the same sections (the
// hidden rows included). A PR new in next only counts for reviewRequested and merged:
// a PR opened since isn't news. When a run event or new changes explain a PR becoming
// my move, myMove isn't repeated.
func Diff(prev, next []model.PR) []Event {
	before := map[int]model.PR{}
	for _, pr := range prev {
		before[pr.Number] = pr
	}
	var events []Event
	for _, pr := range next {
		old, seen := before[pr.Number]
		switch pr.Section {
		case model.SectionMerged:
			if !seen || old.Section != model.SectionMerged {
				events = append(events, event(Merged, pr, fmt.Sprintf("#%d merged", pr.Number), pr.Title, pr.URL))
			}
		case model.SectionReview:
			if pr.Next == model.NextMe && (!seen || old.Next != model.NextMe) {
				events = append(events, event(ReviewRequested, pr, fmt.Sprintf("#%d: review requested", pr.Number), pr.Title+" by "+pr.Author, pr.URL))
			}
		case model.SectionMine:
			if !seen || old.Section != model.SectionMine {
				continue
			}
			explained := false
			for _, e := range append(runEvents(old.DryRun, pr.DryRun, pr), runEvents(old.SafeMerge, pr.SafeMerge, pr)...) {
				events, explained = append(events, e), explained || e.Kind != RunPassed
			}
			for _, r := range pr.Reviewers {
				if r.State != model.ReviewerChangesRequested || slices.ContainsFunc(old.Reviewers, func(o model.Reviewer) bool {
					return o.Login == r.Login && o.State == model.ReviewerChangesRequested && o.At.Equal(r.At)
				}) {
					continue
				}
				events = append(events, event(ChangesRequested, pr, fmt.Sprintf("#%d: changes requested by %s", pr.Number, r.Login), pr.Title, pr.URL))
				explained = true
			}
			if pr.Next == model.NextMe && old.Next != model.NextMe && !explained {
				reason := pr.PrimaryReason()
				events = append(events, event(MyMove, pr, fmt.Sprintf("#%d: your move", pr.Number), reason.Text, cmpOr(reason.URL, pr.URL)))
			}
		}
	}
	return events
}

// runEvents: the run finished (passed, failed, rejected) since the last snapshot, a new
// run or the old one. An outdated run is older than the last push: no news.
func runEvents(old, run model.Run, pr model.PR) []Event {
	kind := map[model.RunState]string{model.RunPassed: RunPassed, model.RunFailed: RunFailed, model.RunRejected: RunRejected}[run.State]
	if kind == "" || run.Outdated || (old.State == run.State && sameRun(old, run)) {
		return nil
	}
	title := fmt.Sprintf("#%d %s %s", pr.Number, run.Kind, run.State)
	body := pr.Title
	if run.State == model.RunRejected && run.Reason != "" {
		body = run.Reason
	}
	return []Event{event(kind, pr, title, body, cmpOr(run.Link(), pr.URL))}
}

// sameRun: the same command or gate comment, so the same run.
func sameRun(a, b model.Run) bool {
	return a.Started.Equal(b.Started) && a.CommentURL == b.CommentURL && a.BuildURL == b.BuildURL
}

func event(kind string, pr model.PR, title, body, url string) Event {
	return Event{Kind: kind, Number: pr.Number, PRTitle: pr.Title, Title: title, Body: body, URL: url}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Runner runs a command with stdin; tests substitute it.
type Runner func(ctx context.Context, argv []string, stdin []byte) error

// Exec runs the command, its output discarded; the error includes the start of stderr.
func Exec(ctx context.Context, argv []string, stdin []byte) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if s := strings.TrimSpace(stderr.String()); s != "" {
			s, _, _ = strings.Cut(s, "\n")
			return fmt.Errorf("%w: %s", err, s)
		}
		return err
	}
	return nil
}

// Notifier delivers the events the config enables through its channels.
type Notifier struct {
	events []string
	// terminal is the resolved protocol: osc9, osc777, osc99, or "" for none.
	terminal string
	bell     bool
	command  []string
	timeout  time.Duration
	run      Runner
	// id numbers kitty's notifications, which are sent in parts.
	id int
}

// New resolves the config for the terminal in environ ("auto" picks by $TERM_PROGRAM
// and $TERM). run is nil for Exec.
func New(cfg config.Notify, environ []string, run Runner) *Notifier {
	if run == nil {
		run = Exec
	}
	terminal := cfg.Terminal
	switch terminal {
	case "auto":
		terminal = Detect(environ)
	case "none":
		terminal = ""
	}
	return &Notifier{events: cfg.Events, terminal: terminal, bell: cfg.Bell, command: cfg.Command, timeout: time.Duration(cfg.Timeout), run: run}
}

// Detect picks the terminal notification protocol by $TERM_PROGRAM and $TERM: "" where
// none is known to work (Terminal.app, VS Code, tmux and screen without passthrough).
func Detect(environ []string) string {
	env := map[string]string{}
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	term, program := env["TERM"], env["TERM_PROGRAM"]
	switch {
	case env["TMUX"] != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux"):
		return ""
	case program == "iTerm.app" || program == "WezTerm" || program == "ghostty" || term == "xterm-ghostty":
		return "osc9"
	case program == "kitty" || term == "xterm-kitty" || env["KITTY_WINDOW_ID"] != "":
		return "osc99"
	case strings.HasPrefix(term, "foot") || strings.HasPrefix(term, "rxvt-unicode"):
		return "osc777"
	}
	return ""
}

// Enabled keeps the events the config asks for.
func (n *Notifier) Enabled(events []Event) []Event {
	return slices.DeleteFunc(slices.Clone(events), func(e Event) bool { return !slices.Contains(n.events, e.Kind) })
}

// Escapes are the terminal notifications and bells for the events, to write to the
// terminal as they are; "" for none.
func (n *Notifier) Escapes(events []Event) string {
	var b strings.Builder
	for _, e := range events {
		title, body := clean(e.Title), clean(e.Body)
		switch n.terminal {
		case "osc9":
			b.WriteString(ansi.Notify(title + ": " + body))
		case "osc777":
			b.WriteString("\x1b]777;notify;" + strings.ReplaceAll(title, ";", ",") + ";" + strings.ReplaceAll(body, ";", ",") + "\x07")
		case "osc99":
			n.id++
			id := fmt.Sprintf("i=%d", n.id)
			b.WriteString(ansi.DesktopNotification(title, id, "d=0"))
			b.WriteString(ansi.DesktopNotification(body, id, "p=body"))
		}
		if n.bell {
			b.WriteString("\a")
		}
	}
	return b.String()
}

// clean drops what would end or garble an escape sequence: control characters.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
}

// HasCommand reports whether events run a command.
func (n *Notifier) HasCommand() bool { return len(n.command) > 0 }

// Run runs the command for one event, within the timeout.
func (n *Notifier) Run(ctx context.Context, e Event) error {
	if len(n.command) == 0 {
		return nil
	}
	stdin, err := json.Marshal(e)
	if err != nil {
		return err
	}
	argv := make([]string, len(n.command))
	r := strings.NewReplacer("{title}", e.Title, "{body}", e.Body, "{url}", e.URL)
	for i, arg := range n.command {
		argv[i] = r.Replace(arg)
	}
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	return n.run(ctx, argv, stdin)
}
