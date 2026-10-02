package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/width"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/demo"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/render"
)

var allSections = []model.Section{model.SectionMine, model.SectionReview, model.SectionTeams, model.SectionMerged}

// harness runs the model on the fixtures without a terminal: messages go straight to
// Update, commands run synchronously, and the clock only moves when a test moves it.
type harness struct {
	t        *testing.T
	m        *Model
	fixtures *demo.Client
	clock    time.Time
	fetches  int
	fetchErr error
	rate     github.RateLimit
	opened   []string
	copied   []string
	// posted are the comments posted, "#number text", and the review requests, "#number
	// review login, login"; postErr fails them.
	posted  []string
	postErr error
	quit    bool
	// raw is what the TUI wrote to the terminal past the screen: notifications.
	raw []string
}

func newHarness(t *testing.T, configure func(h *harness, opts *Options)) *harness {
	t.Helper()
	return newHarnessOn(t, "../../testdata/raw", configure)
}

// newHarnessOn is newHarness on the fixtures of dirs (demo.Load).
func newHarnessOn(t *testing.T, dirs string, configure func(h *harness, opts *Options)) *harness {
	t.Helper()
	fixtures, err := demo.Load(dirs)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, fixtures: fixtures, clock: fixtures.Now()}
	cfg := config.Default()
	opts := Options{
		Config: cfg,
		Render: render.Options{Icons: render.Unicode, Org: "JetBrains", Sections: allSections},
		Fetch: func(ctx context.Context) (*listing.Data, error) {
			h.fetches++
			if h.fetchErr != nil {
				return nil, h.fetchErr
			}
			d, err := listing.Fetch(ctx, fixtures, cfg, h.clock, allSections)
			if d != nil {
				d.RateLimit = h.rate
			}
			return d, err
		},
		FetchPR: func(ctx context.Context, number int) (*github.PRResponse, error) {
			return github.FetchPR(ctx, fixtures, "JetBrains", "kotlin", number)
		},
		Now:  func() time.Time { return h.clock },
		Open: func(url string) error { h.opened = append(h.opened, url); return nil },
		Copy: func(text string) error { h.copied = append(h.copied, text); return nil },
		Post: func(_ context.Context, number int, text string) (string, error) {
			if h.postErr != nil {
				return "", h.postErr
			}
			h.posted = append(h.posted, fmt.Sprintf("#%d %s", number, text))
			return fmt.Sprintf("https://github.com/JetBrains/kotlin/pull/%d#issuecomment-%d", number, len(h.posted)), nil
		},
		RequestReview: func(_ context.Context, number int, logins []string) error {
			if h.postErr != nil {
				return h.postErr
			}
			h.posted = append(h.posted, fmt.Sprintf("#%d review %s", number, strings.Join(logins, ", ")))
			return nil
		},
	}
	if configure != nil {
		configure(h, &opts)
	}
	h.m = New(context.Background(), opts)
	h.m.schedule = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	h.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	return h
}

// cached is the fixtures' data as a cache snapshot fetched age ago.
func (h *harness) cached(age time.Duration) *listing.Data {
	d, err := listing.Fetch(context.Background(), h.fixtures, config.Default(), h.clock, allSections)
	if err != nil {
		h.t.Fatal(err)
	}
	d.FetchedAt = h.clock.Add(-age)
	return d
}

func (h *harness) send(msg tea.Msg) {
	_, cmd := h.m.Update(msg)
	h.run(cmd)
}

// run executes a command and feeds the TUI's own messages back. Anything else (the OSC 52
// request of the clipboard fallback) is for the terminal.
func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			h.run(c)
		}
	case tea.QuitMsg:
		h.quit = true
	case tea.RawMsg:
		h.raw = append(h.raw, msg.Msg.(string))
	case fetchedMsg, detailMsg, noteMsg, copyMsg, postedMsg, reviewRequestedMsg:
		h.send(msg)
	}
}

func (h *harness) start() { h.run(h.m.Init()) }

var keyCodes = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp, "down": tea.KeyDown,
	"backspace": tea.KeyBackspace, "space": tea.KeySpace, "left": tea.KeyLeft, "right": tea.KeyRight,
}

func (h *harness) keys(keys ...string) {
	for _, k := range keys {
		switch {
		case k == "shift+tab":
			h.send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		case keyCodes[k] != 0:
			h.send(tea.KeyPressMsg{Code: keyCodes[k]})
		default:
			for _, r := range k {
				h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

// screen is the visible text, without styles.
func (h *harness) screen() string { return ansi.Strip(h.m.content()) }

func (h *harness) contains(want ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range want {
		if !strings.Contains(s, w) {
			h.t.Errorf("the screen lacks %q:\n%s", w, s)
		}
	}
}

func (h *harness) lacks(unwanted ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range unwanted {
		if strings.Contains(s, w) {
			h.t.Errorf("the screen has %q:\n%s", w, s)
		}
	}
}

// statusBar is the last line of the screen.
func (h *harness) statusBar() string {
	lines := strings.Split(h.screen(), "\n")
	return lines[len(lines)-1]
}

func TestStartupWithoutCache(t *testing.T) {
	h := newHarness(t, nil)
	h.contains("Loading PRs from GitHub")
	h.start()
	if h.fetches != 1 {
		t.Fatalf("%d fetches", h.fetches)
	}
	h.contains("Mine (3)", "#90006", "Recently merged (24h) (1)")
	if got := h.statusBar(); !strings.HasPrefix(got, "updated just now ∙ next refresh in 3m") || !strings.HasSuffix(got, "? help ∙ q quit") {
		t.Errorf("status bar %q", got)
	}
}

// A cached snapshot shows at once; the refresh runs behind it.
func TestStartupFromCache(t *testing.T) {
	h := newHarness(t, func(h *harness, opts *Options) { opts.Initial = h.cached(12 * time.Minute) })
	h.contains("Mine (3)", "#90006")
	cmd := h.m.Init()
	if got := h.statusBar(); !strings.Contains(got, "refreshing⋯ (cached 12m ago)") {
		t.Errorf("while refreshing: %q", got)
	}
	h.run(cmd)
	if got := h.statusBar(); !strings.HasPrefix(got, "updated just now") {
		t.Errorf("after the refresh: %q", got)
	}
}

func TestLoadFailure(t *testing.T) {
	h := newHarness(t, nil)
	h.fetchErr = errors.New("authentication token not found for host github.com")
	h.start()
	h.contains("✗ couldn't load your PRs: authentication token not found", "r to retry, q to quit")
	h.fetchErr = nil
	h.keys("r")
	h.contains("Mine (3)")
}

// A failed refresh keeps the data on screen and says how old it is.
func TestRefreshFailureKeepsData(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.clock = h.clock.Add(14 * time.Minute)
	h.fetchErr = errors.New("HTTP 502: Bad Gateway (https://api.github.com/graphql)\nmore detail")
	h.keys("r")
	h.contains("#90006")
	if got := h.statusBar(); !strings.HasPrefix(got, "✗ refresh failed: HTTP 502: Bad Gateway (https://api.github.com/graphql) (updated 14m ago) ∙ r to retry") {
		t.Errorf("status bar %q", got)
	}
	h.fetchErr = nil
	h.keys("r")
	if got := h.statusBar(); !strings.HasPrefix(got, "updated just now") {
		t.Errorf("after a retry: %q", got)
	}
}

func TestRateLimit(t *testing.T) {
	resets := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)
	h := newHarness(t, func(h *harness, opts *Options) {
		opts.Initial = h.cached(time.Minute)
		opts.Initial.RateLimit = github.RateLimit{Limit: 5000, Remaining: 3, ResetAt: resets}
	})
	if got := h.statusBar(); strings.Contains(got, "API") {
		t.Errorf("a warning from the cache: %q", got)
	}
	h.rate = github.RateLimit{Limit: 5000, Remaining: 4000, ResetAt: resets}
	h.start()
	if got := h.statusBar(); strings.Contains(got, "API") {
		t.Errorf("a warning with 80%% left: %q", got)
	}
	h.rate.Remaining = 312
	h.clock = h.clock.Add(refreshCooldown)
	h.keys("r")
	want := "! API budget 312/5000, resets " + resets.In(h.clock.Location()).Format("15:04")
	if got := h.statusBar(); !strings.Contains(got, want) {
		t.Errorf("status bar %q, want %q", got, want)
	}
	h.fetchErr = errors.New("graphql: api rate limit exceeded for user id 1") // GitHub's words, lower-cased
	h.clock = h.clock.Add(refreshCooldown)
	h.keys("r")
	if got := h.statusBar(); !strings.HasPrefix(got, "✗ refresh failed: API rate limit reached, resets "+resets.In(h.clock.Location()).Format("15:04")+" (updated just now)") {
		t.Errorf("status bar %q", got)
	}
}

// Refreshes run every `refresh`; in between, the rows are classified again so ages move.
func TestBackgroundRefresh(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.clock = h.clock.Add(2 * time.Minute)
	h.send(tickMsg{})
	if h.fetches != 1 {
		t.Errorf("%d fetches after 2m", h.fetches)
	}
	if got := h.statusBar(); !strings.HasPrefix(got, "updated 2m ago ∙ next refresh in 1m") {
		t.Errorf("status bar %q", got)
	}
	h.clock = h.clock.Add(time.Minute)
	h.send(tickMsg{})
	if h.fetches != 2 {
		t.Errorf("%d fetches after 3m", h.fetches)
	}
}

func TestAgesMoveBetweenRefreshes(t *testing.T) {
	h := newHarness(t, func(_ *harness, opts *Options) { opts.Config.Refresh = config.Duration(24 * time.Hour) })
	h.start()
	h.contains("dry-run failed 7h ago")
	h.clock = h.clock.Add(time.Hour)
	h.send(tickMsg{})
	h.contains("dry-run failed 8h ago")
	if h.fetches != 1 {
		t.Errorf("%d fetches", h.fetches)
	}
}

func selectedLine(t *testing.T, h *harness) string {
	t.Helper()
	return ansi.Strip(styledSelectedLine(t, h))
}

// styledSelectedLine is the selected line with its escapes.
func styledSelectedLine(t *testing.T, h *harness) string {
	t.Helper()
	for _, line := range strings.Split(h.m.content(), "\n") {
		if strings.Contains(line, "\x1b[7m") {
			return line
		}
	}
	t.Fatalf("no selected line:\n%s", h.screen())
	return ""
}

// lineWith is the line of the screen, with its escapes, that shows text.
func lineWith(t *testing.T, h *harness, text string) string {
	t.Helper()
	for _, line := range strings.Split(h.m.content(), "\n") {
		if strings.Contains(ansi.Strip(line), text) {
			return line
		}
	}
	t.Fatalf("no line with %q:\n%s", text, h.screen())
	return ""
}

func TestSelection(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	if got := selectedLine(t, h); !strings.Contains(got, "#90006") {
		t.Errorf("first selection %q", got)
	}
	h.keys("j")
	if got := selectedLine(t, h); !strings.Contains(got, "#90007") {
		t.Errorf("after j %q", got)
	}
	h.keys("tab")
	if got := selectedLine(t, h); !strings.Contains(got, "#90004") {
		t.Errorf("after tab, past the empty Review, the merged row: %q", got)
	}
	h.keys("tab")
	if got := selectedLine(t, h); !strings.Contains(got, "#90006") {
		t.Errorf("tab wraps around: %q", got)
	}
	h.keys("shift+tab", "j", "G")
	if got := selectedLine(t, h); !strings.Contains(got, "#90004") {
		t.Errorf("G, the last row: %q", got)
	}
	h.clock = h.clock.Add(refreshCooldown)
	h.keys("r")
	if h.fetches != 2 {
		t.Errorf("%d fetches", h.fetches)
	}
	if got := selectedLine(t, h); !strings.Contains(got, "#90004") {
		t.Errorf("a refresh moved the selection: %q", got)
	}
}

// The selected row keeps the links it has unselected, in the selection's style alone, and
// spans the screen; without hyperlinks it has none.
func TestSelectedRowLinks(t *testing.T) {
	selection := strings.Split(styleSelected.Render("x"), "x")
	for _, on := range []bool{true, false} {
		h := newHarness(t, func(_ *harness, opts *Options) { opts.Render.Hyperlinks = on })
		h.start()
		pr, _ := h.m.current()
		line := styledSelectedLine(t, h)
		h.keys("j")
		unselected := render.StripStyles(lineWith(t, h, fmt.Sprintf("#%d", pr.Number)))
		if !strings.HasPrefix(line, selection[0]) || !strings.HasSuffix(line, selection[1]) {
			t.Fatalf("links %v: not in the selection's style: %q", on, line)
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(line, selection[0]), selection[1])
		if render.StripStyles(inner) != inner {
			t.Errorf("links %v: styles inside the selection: %q", on, line)
		}
		if rest, ok := strings.CutPrefix(inner, unselected); !ok || strings.TrimRight(rest, " ") != "" {
			t.Errorf("links %v: the selected row is %q, unselected %q", on, inner, unselected)
		}
		if w := ansi.StringWidth(line); w != 120 {
			t.Errorf("links %v: the selection is %d wide", on, w)
		}
		if has := strings.Contains(line, ansi.SetHyperlink(pr.URL)); has != on {
			t.Errorf("links %v: the PR's link in the selected row: %v: %q", on, has, line)
		}
		if !on && strings.Contains(line, "\x1b]8;") {
			t.Errorf("a hyperlink without hyperlinks: %q", line)
		}
	}
}

func TestFilterAndAll(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.contains("+2 reviews not waiting on you, 1 draft (a to show)")
	h.keys("a")
	h.contains("Review (3)", "#90001")
	h.lacks("not waiting on you")
	h.keys("a", "/", "990004")
	h.contains("/ 990004", "#90006")
	h.lacks("#90007", "#90005")
	h.keys("enter", "j")
	if got := selectedLine(t, h); !strings.Contains(got, "#90006") {
		t.Errorf("the only match isn't selected: %q", got)
	}
	h.keys("/", "nothing like this")
	h.contains("no PR matches the filter")
	h.keys("esc")
	h.lacks("/ nothing")
	h.contains("#90007", "#90005")
}

func TestDetail(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("enter")
	h.contains("#90006 KT-990004: Example change", "Next: me", "Reviewers", "Code owners: missing", "esc back")
	h.keys("o", "b", "y")
	pr, _ := h.m.current()
	if !slices.Equal(h.opened, []string{pr.URL, pr.Runs[0].Link()}) || !slices.Equal(h.copied, []string{pr.URL}) {
		t.Errorf("opened %q, copied %q", h.opened, h.copied)
	}
	if got := h.statusBar(); !strings.HasPrefix(got, "copied "+pr.URL) && !strings.Contains(got, "copied "+pr.URL) {
		t.Errorf("status bar %q", got)
	}
	h.keys("Y")
	if !slices.Equal(h.copied, []string{pr.URL, "topic/KT-990004-example"}) || !strings.Contains(h.statusBar(), "copied topic/KT-990004-example") {
		t.Errorf("the branch: copied %q, status bar %q", h.copied, h.statusBar())
	}
	h.keys("esc")
	h.contains("Mine (3)")
}

// A merged row comes from the search; its detail fetches the PR in full.
func TestMergedDetail(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("G", "enter")
	h.contains("#90004", "Code owners", "Runs")
	if pr, _ := h.m.detailPR(); pr.MergedAt.IsZero() || pr.Next != model.NextDone {
		t.Errorf("merged detail %+v", pr)
	}
}

// Narrow terminals narrow the reason, then the title column; nothing is wider than the
// screen.
func TestResize(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 30}, {Width: 80, Height: 12}, {Width: 30, Height: 5}} {
		h.send(size)
		lines := strings.Split(h.m.content(), "\n")
		if len(lines) != size.Height {
			t.Errorf("%d lines on a %d-line screen", len(lines), size.Height)
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > size.Width {
				t.Errorf("width %d: line %d is %d wide: %q", size.Width, i, w, ansi.Strip(line))
			}
		}
	}
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	var rejected string
	for _, line := range strings.Split(h.screen(), "\n") {
		if strings.Contains(line, "#90007") {
			rejected = strings.TrimRight(line, " ")
		}
	}
	if !strings.Contains(rejected, "dry-run rejected: A Coord") || !strings.HasSuffix(rejected, render.Unicode.Ellipsis) || ansi.StringWidth(rejected) != 100 {
		t.Errorf("the reason isn't narrowed to fit 100 columns: %q", rejected)
	}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 12})
	h.keys("G")
	h.contains("#90004")
}

func TestHelpAndQuit(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("?")
	h.contains("Keys", "tab              the first row of the next section", "D                post /dry-run on it, after asking")
	h.keys("G") // the help scrolls
	h.contains("Symbols", "next move:")
	h.keys("esc")
	h.contains("Mine (3)")
	h.keys("q")
	if !h.quit {
		t.Error("q didn't quit")
	}
}

// Every symbol on screen is single-width, as in the CLI.
func TestScreensAreSingleWidth(t *testing.T) {
	for _, icons := range render.IconSets {
		h := newHarness(t, func(_ *harness, opts *Options) { opts.Render.Icons = icons })
		screens := []string{h.screen()}
		h.start()
		h.m.refreshing = true
		screens = append(screens, h.screen())
		h.m.refreshing = false
		h.keys("?")
		screens = append(screens, h.screen())
		h.keys("esc", "enter")
		screens = append(screens, h.screen())
		for _, s := range screens {
			for _, r := range s {
				if r >= 0x80 && (icons.Name == "ascii" || width.LookupRune(r).Kind() != width.Neutral || ansi.StringWidth(string(r)) != 1) {
					t.Errorf("%s: %U (%c) may render double-width", icons.Name, r, r)
				}
			}
		}
	}
}

// One fetch at a time: the refresh key and the timer wait for the running one.
func TestRefreshInFlight(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.clock = h.clock.Add(time.Minute)
	if cmd := h.m.refreshKey(); cmd == nil || !h.m.refreshing {
		t.Fatal("no refresh")
	}
	h.keys("r")
	if got := h.statusBar(); !strings.Contains(got, "refreshing⋯ (updated just now) ∙ already refreshing") {
		t.Errorf("status bar %q", got)
	}
	h.clock = h.clock.Add(5 * time.Minute)
	h.send(tickMsg{})
	if h.fetches != 1 {
		t.Errorf("%d fetches while one is running, want none", h.fetches-1)
	}
}

// Right after a refresh the key waits 5s, unless the refresh failed: a retry is immediate.
func TestRefreshCooldown(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.clock = h.clock.Add(2 * time.Second)
	h.keys("r")
	if h.fetches != 1 || !strings.Contains(h.statusBar(), "refreshed 2s ago") {
		t.Errorf("%d fetches, status bar %q", h.fetches, h.statusBar())
	}
	h.clock = h.clock.Add(3 * time.Second)
	h.keys("r")
	if h.fetches != 2 {
		t.Errorf("%d fetches 5s after a refresh", h.fetches)
	}
	h.fetchErr = errors.New("HTTP 502")
	h.clock = h.clock.Add(refreshCooldown)
	h.keys("r")
	h.fetchErr = nil
	h.keys("r")
	if h.fetches != 4 || !strings.HasPrefix(h.statusBar(), "updated just now") {
		t.Errorf("a retry right after a failure: %d fetches, status bar %q", h.fetches, h.statusBar())
	}
}

// The config's keys replace the defaults, in the keys, the hints and the help.
func TestCustomKeys(t *testing.T) {
	h := newHarness(t, func(_ *harness, opts *Options) {
		keys, err := config.Parse([]byte("keys: {copy: c, refresh: [e, F5], down: [down, n]}\n"))
		if err != nil {
			t.Fatal(err)
		}
		opts.Config.Keys = keys.Keys
	})
	h.start()
	h.keys("n", "c", "y")
	pr, _ := h.m.current()
	if pr.Number != 90007 || !slices.Equal(h.copied, []string{pr.URL}) {
		t.Errorf("selected #%d, copied %q", pr.Number, h.copied)
	}
	h.clock = h.clock.Add(refreshCooldown)
	h.keys("r")
	if h.fetches != 1 {
		t.Errorf("the default refresh key still works")
	}
	h.keys("e")
	h.m.note = ""
	if h.fetches != 2 || !strings.Contains(h.statusBar(), "e refresh") {
		t.Errorf("%d fetches, status bar %q", h.fetches, h.statusBar())
	}
	h.keys("?")
	h.contains("e / F5", "c                copy its URL", "down / n")
}

// Update scrolls the list to keep the selection, and its section's title on its first
// row, on screen; View only reads the state.
func TestScroll(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.send(tea.WindowSizeMsg{Width: 120, Height: 5})
	h.keys("G")
	h.contains("#90004")
	h.lacks("Mine (3)")
	offset := h.m.offset
	if first, second := h.m.content(), h.m.content(); first != second || h.m.offset != offset {
		t.Errorf("View changed the state: offset %d, then %d", offset, h.m.offset)
	}
	h.keys("g")
	h.contains("Mine (3)", "#90006")
	h.lacks("#90004")
}

// Notifications come from two live refreshes in a row, never from the first load or the
// cache: the terminal gets them, the command runs per event, the status bar names them.
// notify.swiftbar is the plugin's channel: off, it changes nothing here.
func TestNotifications(t *testing.T) {
	var ran [][]string
	fail := false
	run := func(_ context.Context, argv []string, _ []byte) error {
		ran = append(ran, argv)
		if fail {
			return errors.New("exit status 1: boom")
		}
		return nil
	}
	events := []notify.Event{
		{Kind: notify.RunFailed, Number: 7, Title: "#7 dry-run failed", Body: "Example change", URL: "https://ci.example.org/build/1"},
		{Kind: notify.Merged, Number: 8, Title: "#8 merged", Body: "Other change", URL: "https://example.org/pull/8"},
		{Kind: notify.MyMove, Number: 9, Title: "#9: your move", Body: "new comment"},
	}
	h := newHarness(t, func(h *harness, opts *Options) {
		opts.Initial = h.cached(time.Minute)
		cfg, err := config.Parse([]byte("notify: {terminal: osc9, swiftbar: false, events: [runFailed, merged], command: [hook, '{title}']}\n"))
		if err != nil {
			t.Fatal(err)
		}
		opts.Notifier = notify.New(cfg.Notify, nil, run)
	})
	diffs := 0
	h.m.diff = func(prev, next []model.PR) []notify.Event {
		diffs++
		if len(prev) == 0 || len(next) == 0 {
			t.Error("an empty snapshot")
		}
		return events
	}
	h.start()
	if diffs != 0 || len(h.raw) != 0 || len(ran) != 0 {
		t.Fatalf("the first live refresh after the cache notified: %d diffs, %q, %q", diffs, h.raw, ran)
	}
	h.clock = h.clock.Add(time.Minute)
	h.keys("r")
	if diffs != 1 {
		t.Fatalf("%d diffs", diffs)
	}
	if want := []string{"\x1b]9;#7 dry-run failed: Example change\a\x1b]9;#8 merged: Other change\a"}; !slices.Equal(h.raw, want) {
		t.Errorf("terminal %q, want %q", h.raw, want)
	}
	if want := [][]string{{"hook", "#7 dry-run failed"}, {"hook", "#8 merged"}}; fmt.Sprint(ran) != fmt.Sprint(want) {
		t.Errorf("commands %q, want %q", ran, want)
	}
	if got := h.statusBar(); !strings.Contains(got, "#7 dry-run failed (+1 more)") {
		t.Errorf("status bar %q", got)
	}
	fail = true
	h.clock = h.clock.Add(time.Minute)
	h.keys("r")
	if got := h.statusBar(); !strings.Contains(got, "notify command failed: exit status 1: boom") {
		t.Errorf("status bar %q", got)
	}
	h.fetchErr = errors.New("HTTP 502")
	h.clock = h.clock.Add(time.Minute)
	h.keys("r")
	if diffs != 2 {
		t.Errorf("a failed refresh was diffed: %d diffs", diffs)
	}
}

// Y copies the selected PR's branch name, in the list too; without a clipboard it goes
// through the terminal (OSC 52), like the URL.
func TestCopyBranch(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	h.keys("j", "Y")
	if pr, _ := h.m.current(); pr.Number != 90007 || !slices.Equal(h.copied, []string{pr.Branch}) || pr.Branch == "" ||
		!strings.Contains(h.statusBar(), "copied "+pr.Branch) {
		t.Errorf("#%d: copied %q, status bar %q", pr.Number, h.copied, h.statusBar())
	}
	h = newHarness(t, func(_ *harness, opts *Options) { opts.Copy = nil })
	h.start()
	cmd := h.m.onPRKey("copyBranch")
	_, fallback := h.m.Update(cmd())
	if fallback == nil || !strings.Contains(h.statusBar(), "copied topic/KT-990004-example") {
		t.Errorf("no clipboard: %q", h.statusBar())
	}
}
