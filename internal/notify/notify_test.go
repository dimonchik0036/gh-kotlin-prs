package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/demo"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

var allSections = []model.Section{model.SectionMine, model.SectionReview, model.SectionTeams, model.SectionMerged}

// snapshot is the fixtures classified at their clock, every section, hidden rows included.
func snapshot(t *testing.T) []model.PR {
	t.Helper()
	fixtures, err := demo.Load("../../testdata/raw")
	if err != nil {
		t.Fatal(err)
	}
	d, err := listing.Fetch(context.Background(), fixtures, config.Default(), fixtures.Now(), allSections)
	if err != nil {
		t.Fatal(err)
	}
	return d.Classify(config.Default(), fixtures.Now())
}

// edit returns a copy of prs with f applied to PR number (dropped when f returns false).
func edit(prs []model.PR, number int, f func(pr *model.PR) bool) []model.PR {
	var out []model.PR
	for _, pr := range prs {
		pr.Reviewers = slices.Clone(pr.Reviewers)
		if pr.Number == number && !f(&pr) {
			continue
		}
		out = append(out, pr)
	}
	return out
}

func find(prs []model.PR, number int) model.PR {
	for _, pr := range prs {
		if pr.Number == number {
			return pr
		}
	}
	return model.PR{}
}

func kinds(events []Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, fmt.Sprintf("%s #%d", e.Kind, e.Number))
	}
	slices.Sort(out)
	return out
}

func TestDiffUnchanged(t *testing.T) {
	prs := snapshot(t)
	if events := Diff(prs, prs); len(events) != 0 {
		t.Errorf("events between equal snapshots: %v", kinds(events))
	}
}

// The fixtures as they were a little earlier: each change is one event.
func TestDiffFixtures(t *testing.T) {
	next := snapshot(t)
	prev := next
	// #90006: the failed dry-run was still running, and it was the reviewers' move.
	prev = edit(prev, 90006, func(pr *model.PR) bool {
		pr.DryRun.State, pr.Next = model.RunRunning, model.NextReviewers
		return true
	})
	// #90007: the rejected dry-run was just requested.
	prev = edit(prev, 90007, func(pr *model.PR) bool {
		pr.DryRun = model.Run{Kind: model.DryRun, State: model.RunRequested, CommentURL: pr.URL + "#issuecomment-1"}
		return true
	})
	// #90004 was still open.
	prev = edit(prev, 90004, func(pr *model.PR) bool {
		pr.Section, pr.Next = model.SectionMine, model.NextReviewers
		return true
	})
	// #90001's review request is new.
	prev = edit(prev, 90001, func(*model.PR) bool { return false })
	// #90005 is a new PR of mine: no news.
	prev = edit(prev, 90005, func(*model.PR) bool { return false })

	events := Diff(prev, next)
	want := []string{"merged #90004", "reviewRequested #90001", "runFailed #90006", "runRejected #90007"}
	if got := kinds(events); !slices.Equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	failed, rejected := find(next, 90006), find(next, 90007)
	for _, e := range events {
		switch e.Kind {
		case RunFailed:
			if e.Title != "#90006 dry-run failed" || e.URL != failed.DryRun.BuildURL || e.Body != failed.Title {
				t.Errorf("runFailed %+v", e)
			}
		case RunRejected:
			if e.Title != "#90007 dry-run rejected" || e.Body != rejected.DryRun.Reason || e.URL != rejected.DryRun.Link() {
				t.Errorf("runRejected %+v", e)
			}
		}
	}
}

func TestDiffMine(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	base := model.PR{Number: 7, Title: "Example change", URL: "https://example.org/pull/7", Section: model.SectionMine, Next: model.NextReviewers,
		DryRun:    model.Run{Kind: model.DryRun, State: model.RunPassed, Started: at, BuildURL: "https://ci.example.org/build/1"},
		SafeMerge: model.Run{Kind: model.SafeMerge, State: model.RunNone},
		Reviewers: []model.Reviewer{{Login: "alice_user", State: model.ReviewerPending}}}
	change := func(f func(pr *model.PR)) []model.PR {
		pr := base
		pr.Reviewers = slices.Clone(base.Reviewers)
		f(&pr)
		return []model.PR{pr}
	}
	tests := []struct {
		name string
		next []model.PR
		want []string
	}{
		{"a new run passed", change(func(pr *model.PR) {
			pr.DryRun.Started, pr.DryRun.BuildURL = at.Add(time.Hour), "https://ci.example.org/build/2"
		}), []string{"runPassed #7"}},
		{"an outdated run", change(func(pr *model.PR) {
			pr.DryRun.Started, pr.DryRun.State, pr.DryRun.Outdated = at.Add(time.Hour), model.RunFailed, true
		}), nil},
		{"the move became mine", change(func(pr *model.PR) {
			pr.Next, pr.Reasons = model.NextMe, []model.Reason{{Text: "new comment from bob_user", URL: "https://example.org/pull/7#c1"}}
		}), []string{"myMove #7"}},
		{"changes requested, the move mine", change(func(pr *model.PR) {
			pr.Next, pr.Reviewers[0].State, pr.Reviewers[0].At = model.NextMe, model.ReviewerChangesRequested, at
		}), []string{"changesRequested #7"}},
		{"a safe-merge failed", change(func(pr *model.PR) {
			pr.Next, pr.SafeMerge = model.NextMe, model.Run{Kind: model.SafeMerge, State: model.RunFailed, Started: at, CommentURL: "https://example.org/pull/7#c2"}
		}), []string{"runFailed #7"}},
		{"the move stayed mine", change(func(pr *model.PR) { pr.Next = model.NextReviewers }), nil},
	}
	for _, tt := range tests {
		if got := kinds(Diff([]model.PR{base}, tt.next)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	moved := Diff([]model.PR{base}, tests[2].next)[0]
	if moved.Title != "#7: your move" || moved.Body != "new comment from bob_user" || moved.URL != "https://example.org/pull/7#c1" {
		t.Errorf("myMove %+v", moved)
	}
}

func TestDetect(t *testing.T) {
	for _, c := range []struct {
		env  []string
		want string
	}{
		{[]string{"TERM_PROGRAM=iTerm.app", "TERM=xterm-256color"}, "osc9"},
		{[]string{"TERM_PROGRAM=WezTerm"}, "osc9"},
		{[]string{"TERM=xterm-ghostty"}, "osc9"},
		{[]string{"TERM=xterm-kitty"}, "osc99"},
		{[]string{"TERM=foot"}, "osc777"},
		{[]string{"TERM=rxvt-unicode-256color"}, "osc777"},
		{[]string{"TERM_PROGRAM=Apple_Terminal", "TERM=xterm-256color"}, ""},
		{[]string{"TERM_PROGRAM=vscode"}, ""},
		{[]string{"TERM_PROGRAM=iTerm.app", "TMUX=/tmp/tmux-1/default,1,0"}, ""},
		{[]string{"TERM=screen-256color"}, ""},
		{nil, ""},
	} {
		if got := Detect(c.env); got != c.want {
			t.Errorf("Detect(%q) = %q, want %q", c.env, got, c.want)
		}
	}
}

func notifier(t *testing.T, yaml string, environ []string, run Runner) *Notifier {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg.Notify, environ, run)
}

var ev = Event{Kind: RunFailed, Number: 7, PRTitle: "Example; change", Title: "#7 dry-run failed", Body: "Example; change\x1b]8;;x\a", URL: "https://ci.example.org/build/1"}

func TestEscapes(t *testing.T) {
	for _, c := range []struct{ yaml, want string }{
		{"notify: {terminal: osc9}", "\x1b]9;#7 dry-run failed: Example; change ]8;;x \a"},
		{"notify: {terminal: osc777}", "\x1b]777;notify;#7 dry-run failed;Example, change ]8,,x \a"},
		{"notify: {terminal: osc99}", "\x1b]99;i=1:d=0;#7 dry-run failed\a\x1b]99;i=1:p=body;Example; change ]8;;x \a"},
		{"notify: {terminal: none, bell: true}", "\a"},
		{"notify: {terminal: osc9, bell: true}", "\x1b]9;#7 dry-run failed: Example; change ]8;;x \a\a"},
		{"notify: {terminal: none}", ""},
	} {
		if got := notifier(t, c.yaml, nil, nil).Escapes([]Event{ev}); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.yaml, got, c.want)
		}
	}
	if got := notifier(t, "notify: {terminal: auto}", []string{"TERM=xterm-kitty"}, nil).Escapes([]Event{ev, ev}); strings.Count(got, "i=2:") != 2 {
		t.Errorf("kitty notifications share an id: %q", got)
	}
}

func TestEnabled(t *testing.T) {
	n := notifier(t, "notify: {events: [merged, runFailed]}", nil, nil)
	got := n.Enabled([]Event{{Kind: Merged}, {Kind: MyMove}, {Kind: RunFailed}})
	if len(got) != 2 || got[0].Kind != Merged || got[1].Kind != RunFailed {
		t.Errorf("enabled %+v", got)
	}
}

// The command gets the placeholders filled in, the event on stdin, and a deadline.
func TestCommand(t *testing.T) {
	var argv []string
	var stdin []byte
	var deadline bool
	run := func(ctx context.Context, a []string, in []byte) error {
		argv, stdin = a, in
		_, deadline = ctx.Deadline()
		return nil
	}
	n := notifier(t, "notify: {command: [notify-send, --app-name=kp, '{title}', '{body} ({url})'], timeout: 3s}", nil, run)
	if !n.HasCommand() {
		t.Fatal("no command")
	}
	if err := n.Run(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if want := []string{"notify-send", "--app-name=kp", ev.Title, ev.Body + " (" + ev.URL + ")"}; !slices.Equal(argv, want) || !deadline {
		t.Errorf("argv %q, deadline %v", argv, deadline)
	}
	var got Event
	if err := json.Unmarshal(stdin, &got); err != nil || got != ev {
		t.Errorf("stdin %s: %v", stdin, err)
	}
	if err := notifier(t, "", nil, func(context.Context, []string, []byte) error { return errors.New("ran") }).Run(context.Background(), ev); err != nil {
		t.Errorf("no command configured, yet: %v", err)
	}
}

func TestExec(t *testing.T) {
	ctx := context.Background()
	if err := Exec(ctx, []string{"sh", "-c", `read -r line; [ "$line" = hello ]`}, []byte("hello\n")); err != nil {
		t.Errorf("stdin: %v", err)
	}
	if err := Exec(ctx, []string{"sh", "-c", "echo oops >&2; exit 3"}, nil); err == nil || !strings.Contains(err.Error(), "oops") {
		t.Errorf("a failure: %v", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := Exec(ctx, []string{"sleep", "5"}, nil); err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("the timeout: %v after %v", err, time.Since(start))
	}
}

// The kinds are the config's event names.
func TestKindsMatchConfig(t *testing.T) {
	var names []string
	for _, e := range config.NotifyEvents {
		names = append(names, e.Name)
	}
	if want := []string{RunPassed, RunFailed, RunRejected, MyMove, ChangesRequested, ReviewRequested, Merged}; !slices.Equal(names, want) {
		t.Errorf("config.NotifyEvents %q, want %q", names, want)
	}
}
