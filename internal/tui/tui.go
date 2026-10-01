// Package tui is the interactive view of the bare command on a terminal: the rows of
// `list` with a selection, the `show` detail on enter, a filter, and a background
// refresh with a status bar. It renders through internal/render, like the CLI, and
// starts from the cache when that is young enough.
package tui

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/classify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/render"
)

// Options are everything the TUI needs from outside; tests substitute them.
type Options struct {
	Config config.Config
	// Render holds the icons, hyperlinks, organization and the sections to show.
	Render render.Options
	// All and WaitingOnMe start the TUI like --all and --waiting-on-me.
	All, WaitingOnMe bool
	// Initial is a snapshot from the cache to show until the first refresh, or nil.
	Initial *listing.Data
	// Fetch gets the sections from GitHub.
	Fetch func(ctx context.Context) (*listing.Data, error)
	// FetchPR gets one PR in full, for the detail of a merged one.
	FetchPR func(ctx context.Context, number int) (*github.PRResponse, error)
	Now     func() time.Time
	// Open opens a URL in the browser.
	Open func(url string) error
	// Copy puts text on the system clipboard. When it fails, or is nil, the text goes
	// through the terminal (OSC 52) instead.
	Copy func(text string) error
}

// Run shows the TUI until the user quits or ctx is done.
func Run(ctx context.Context, opts Options, in io.Reader, out io.Writer) error {
	p := tea.NewProgram(New(ctx, opts), tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))
	_, err := p.Run()
	return err
}

type screen int

const (
	screenList screen = iota
	screenDetail
	screenHelp
)

// reclassifyEvery is how often rows are classified again, so ages keep moving.
const reclassifyEvery = 30 * time.Second

// noteFor is how long a note ("copied …") stays in the status bar.
const noteFor = 4 * time.Second

// refreshCooldown: the refresh key does nothing this soon after a successful refresh.
const refreshCooldown = 5 * time.Second

// Model is the TUI's state. Its methods have pointer receivers; Update returns it.
type Model struct {
	opts Options
	ctx  context.Context
	// actions maps each bound key to its action (config.Actions).
	actions map[string]string
	// schedule is tea.Tick; tests stop the clock ticks and the spinner with it.
	schedule func(time.Duration, func(time.Time) tea.Msg) tea.Cmd

	width, height int
	now           time.Time

	data         *listing.Data
	fromCache    bool
	prs          []model.PR // data classified at classifiedAt
	classifiedAt time.Time

	refreshing  bool
	refreshedAt time.Time // when the last refresh succeeded
	nextRefresh time.Time
	err         error // of the last refresh; nil after a success
	rate        github.RateLimit
	rateLimited bool

	all, waitingOnMe bool
	filter           textinput.Model
	filtering        bool

	blocks   []render.Block // what the list shows: filtered, sorted, fitted to the width
	selected int            // the selected PR's number, 0 for none
	offset   int            // the first list line on screen

	screen   screen
	detail   int // the number of the PR in the detail view
	viewport viewport.Model
	// full holds the merged PRs fetched in full for their detail; loading, the ones on the way.
	full    map[int]*github.PRResponse
	loading map[int]bool

	spinner spinner.Model
	note    string
	noteEnd time.Time
}

// New is the model of the TUI; Init starts the first refresh.
func New(ctx context.Context, opts Options) *Model {
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "filter"
	styles := filter.Styles()
	styles.Cursor.Blink = false
	filter.SetStyles(styles)
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	if opts.Render.Icons.Name == render.ASCII.Name {
		sp.Spinner = spinner.Line
	}
	actions := map[string]string{}
	for action, keys := range opts.Config.Keys {
		for _, k := range keys {
			actions[k] = action
		}
	}
	m := &Model{
		opts:        opts,
		actions:     actions,
		ctx:         ctx,
		schedule:    tea.Tick,
		now:         opts.Now(),
		all:         opts.All,
		waitingOnMe: opts.WaitingOnMe,
		filter:      filter,
		viewport:    viewport.New(),
		full:        map[int]*github.PRResponse{},
		loading:     map[int]bool{},
		spinner:     sp,
	}
	if opts.Initial != nil {
		m.data, m.fromCache = opts.Initial, true
		m.reclassify()
	}
	return m
}

type (
	tickMsg    struct{}
	fetchedMsg struct {
		data *listing.Data
		err  error
	}
	detailMsg struct {
		number int
		resp   *github.PRResponse
		err    error
	}
	noteMsg string
	copyMsg struct {
		text string
		err  error
	}
)

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.tick(), m.refresh())
}

func (m *Model) tick() tea.Cmd {
	return m.schedule(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// refresh starts a fetch unless one is running.
func (m *Model) refresh() tea.Cmd {
	if m.refreshing {
		return nil
	}
	spin := m.spin()
	m.refreshing = true
	fetch, ctx := m.opts.Fetch, m.ctx
	return tea.Batch(spin, func() tea.Msg {
		data, err := fetch(ctx)
		return fetchedMsg{data: data, err: err}
	})
}

// spin starts the spinner; its ticks stop once nothing is loading.
func (m *Model) spin() tea.Cmd {
	if m.busy() {
		return nil // already spinning
	}
	return m.schedule(m.spinner.Spinner.FPS, func(time.Time) tea.Msg { return m.spinner.Tick() })
}

func (m *Model) busy() bool { return m.refreshing || len(m.loading) > 0 }

// Update handles a message, then scrolls the list to keep the selection on screen.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.scroll()
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuild()
		return nil
	case tickMsg:
		return m.onTick()
	case spinner.TickMsg:
		if !m.busy() {
			return nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return cmd
	case fetchedMsg:
		m.onFetched(msg)
		return nil
	case detailMsg:
		delete(m.loading, msg.number)
		if msg.err != nil {
			m.setNote("couldn't load #" + strconv.Itoa(msg.number) + ": " + shortError(msg.err))
		} else {
			m.full[msg.number] = msg.resp
		}
		m.updateDetail()
		return nil
	case noteMsg:
		m.setNote(string(msg))
		return nil
	case copyMsg:
		m.setNote("copied " + msg.text)
		if msg.err != nil {
			return tea.SetClipboard(msg.text)
		}
		return nil
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	if m.filtering {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) onTick() tea.Cmd {
	m.now = m.opts.Now()
	if m.note != "" && !m.now.Before(m.noteEnd) {
		m.note = ""
	}
	if m.data != nil && m.now.Sub(m.classifiedAt) >= reclassifyEvery {
		m.reclassify()
	}
	cmds := []tea.Cmd{m.tick()}
	if !m.refreshing && !m.nextRefresh.IsZero() && !m.now.Before(m.nextRefresh) {
		cmds = append(cmds, m.refresh())
	}
	return tea.Batch(cmds...)
}

func (m *Model) onFetched(msg fetchedMsg) {
	m.now = m.opts.Now()
	m.refreshing = false
	m.nextRefresh = m.now.Add(time.Duration(m.opts.Config.Refresh))
	if msg.err != nil {
		m.err = msg.err
		m.rateLimited = isRateLimit(msg.err)
		return
	}
	m.err, m.rateLimited = nil, false
	msg.data.FetchedAt, m.refreshedAt = m.now, m.now
	m.data, m.fromCache, m.rate = msg.data, false, msg.data.RateLimit
	m.reclassify()
}

// reclassify classifies the data with the current clock and rebuilds the screen.
func (m *Model) reclassify() {
	m.prs = m.data.Classify(m.opts.Config, m.now)
	m.classifiedAt = m.now
	m.rebuild()
}

// rebuild applies --all, --waiting-on-me and the filter, then fits the rows to the width,
// keeping the selected PR selected while it's still shown.
func (m *Model) rebuild() {
	if m.data == nil {
		return
	}
	prs, hidden := listing.Filter(m.prs, m.all, m.waitingOnMe)
	if q := strings.ToLower(strings.TrimSpace(m.filter.Value())); q != "" {
		var kept []model.PR
		for _, pr := range prs {
			if matches(render.NewRow(pr, m.opts.Render.Icons), q) {
				kept = append(kept, pr)
			}
		}
		prs = kept
	}
	opts := m.opts.Render
	opts.Hidden = hidden
	m.blocks = render.Blocks(prs, opts)
	for i := range m.blocks {
		fit(m.blocks[i].Rows, m.width)
	}
	rows := m.rows()
	if len(rows) == 0 {
		m.selected = 0
	} else if indexOf(rows, m.selected) < 0 {
		m.selected = rows[0].PR.Number
	}
	m.updateDetail()
}

// matches reports whether any cell of the row contains q (lower case).
func matches(row render.Row, q string) bool {
	for _, c := range row.Cells {
		if strings.Contains(strings.ToLower(c.Text()), q) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(row.PR.Title), q)
}

// rows is every shown row, in screen order.
func (m *Model) rows() []render.Row {
	var rows []render.Row
	for _, b := range m.blocks {
		rows = append(rows, b.Rows...)
	}
	return rows
}

func indexOf(rows []render.Row, number int) int {
	for i, r := range rows {
		if r.PR.Number == number {
			return i
		}
	}
	return -1
}

// current is the selected PR in the list, or the one in the detail view.
func (m *Model) current() (model.PR, bool) {
	number := m.selected
	if m.screen == screenDetail {
		return m.detailPR()
	}
	for _, r := range m.rows() {
		if r.PR.Number == number {
			return r.PR, true
		}
	}
	return model.PR{}, false
}

// detailPR is the PR of the detail view: its row, or the full fetch of a merged one.
func (m *Model) detailPR() (model.PR, bool) {
	if resp, ok := m.full[m.detail]; ok {
		c := &classify.Classifier{Config: m.opts.Config, Viewer: resp.Viewer.Login, Now: m.now}
		return c.Show(resp.Repository.PullRequest), true
	}
	for _, pr := range m.prs {
		if pr.Number == m.detail {
			return pr, true
		}
	}
	return model.PR{}, false
}

func (m *Model) setNote(s string) {
	m.note, m.noteEnd = s, m.opts.Now().Add(noteFor)
}

func (m *Model) onKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if m.filtering {
		return m.onFilterKey(msg)
	}
	action := m.actions[key]
	switch action {
	case "quit":
		return tea.Quit
	case "help":
		if m.screen == screenHelp {
			m.screen = screenList
		} else {
			m.screen = screenHelp
		}
		return nil
	case "refresh":
		return m.refreshKey()
	}
	switch m.screen {
	case screenHelp:
		if action == "back" {
			m.screen = screenList
		}
		return nil
	case screenDetail:
		return m.onDetailKey(action)
	case screenList:
		return m.onListKey(action)
	}
	return nil
}

// refreshKey refreshes unless a refresh is running or one just succeeded: a retry after
// a failure goes through at once.
func (m *Model) refreshKey() tea.Cmd {
	now := m.opts.Now()
	switch {
	case m.refreshing:
		m.setNote("already refreshing")
		return nil
	case m.err == nil && !m.refreshedAt.IsZero() && now.Sub(m.refreshedAt) < refreshCooldown:
		m.setNote("refreshed " + agoSeconds(now, m.refreshedAt))
		return nil
	}
	return m.refresh()
}

// agoSeconds is "2s ago", for the cooldown's hint.
func agoSeconds(now, t time.Time) string {
	return strconv.Itoa(int(now.Sub(t)/time.Second)) + "s ago"
}

// onFilterKey edits the filter: enter (or moving the selection) keeps it, esc clears it.
func (m *Model) onFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); {
	case key == "esc":
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
	case key == "enter" || key == "up" || key == "down" || key == "tab":
		m.filtering = false
		m.filter.Blur()
	default:
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuild()
		return cmd
	}
	m.rebuild()
	return nil
}

func (m *Model) onListKey(action string) tea.Cmd {
	rows := m.rows()
	i := indexOf(rows, m.selected)
	switch action {
	case "up":
		m.selectRow(rows, i-1)
	case "down":
		m.selectRow(rows, i+1)
	case "pageUp":
		m.selectRow(rows, i-m.bodyHeight())
	case "pageDown":
		m.selectRow(rows, i+m.bodyHeight())
	case "first":
		m.selectRow(rows, 0)
	case "last":
		m.selectRow(rows, len(rows)-1)
	case "nextSection":
		m.focusSection(1)
	case "previousSection":
		m.focusSection(-1)
	case "all":
		m.all = !m.all
		m.rebuild()
	case "filter":
		m.filtering = true
		m.rebuild()
		return m.filter.Focus()
	case "back":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.rebuild()
		}
	case "details":
		if m.selected != 0 {
			return m.openDetail(m.selected)
		}
	default:
		return m.onPRKey(action)
	}
	return nil
}

func (m *Model) selectRow(rows []render.Row, i int) {
	if len(rows) == 0 {
		return
	}
	m.selected = rows[max(0, min(i, len(rows)-1))].PR.Number
}

// focusSection selects the first row of the next (dir 1) or previous (-1) section with rows.
func (m *Model) focusSection(dir int) {
	current := 0
	for i, b := range m.blocks {
		if indexOf(b.Rows, m.selected) >= 0 {
			current = i
		}
	}
	for step := 1; step <= len(m.blocks); step++ {
		b := m.blocks[((current+dir*step)%len(m.blocks)+len(m.blocks))%len(m.blocks)]
		if len(b.Rows) > 0 {
			m.selected = b.Rows[0].PR.Number
			return
		}
	}
}

func (m *Model) openDetail(number int) tea.Cmd {
	m.screen, m.detail = screenDetail, number
	m.viewport.SetYOffset(0)
	m.updateDetail()
	pr, ok := m.detailPR()
	if !ok || pr.Section != model.SectionMerged || m.opts.FetchPR == nil || m.loading[number] {
		return nil
	}
	// A merged row comes from the search; its detail needs the PR in full.
	spin := m.spin()
	m.loading[number] = true
	fetch, ctx := m.opts.FetchPR, m.ctx
	return tea.Batch(spin, func() tea.Msg {
		resp, err := fetch(ctx, number)
		return detailMsg{number: number, resp: resp, err: err}
	})
}

// onDetailKey scrolls the details with the list's movement keys.
func (m *Model) onDetailKey(action string) tea.Cmd {
	switch action {
	case "back":
		m.screen = screenList
	case "up":
		m.viewport.ScrollUp(1)
	case "down":
		m.viewport.ScrollDown(1)
	case "pageUp":
		m.viewport.PageUp()
	case "pageDown":
		m.viewport.PageDown()
	case "first":
		m.viewport.GotoTop()
	case "last":
		m.viewport.GotoBottom()
	default:
		return m.onPRKey(action)
	}
	return nil
}

// onPRKey handles the actions on the current PR: open it, open its build, copy its URL.
func (m *Model) onPRKey(action string) tea.Cmd {
	pr, ok := m.current()
	if !ok {
		return nil
	}
	switch action {
	case "open":
		return m.open(pr.URL)
	case "build":
		if url := buildLink(pr); url != "" {
			return m.open(url)
		}
		m.setNote("#" + strconv.Itoa(pr.Number) + " has no build")
	case "copy":
		copyText, url := m.opts.Copy, pr.URL
		if copyText == nil {
			return func() tea.Msg { return copyMsg{text: url, err: errors.ErrUnsupported} }
		}
		return func() tea.Msg { return copyMsg{text: url, err: copyText(url)} }
	}
	return nil
}

func (m *Model) open(url string) tea.Cmd {
	open := m.opts.Open
	return func() tea.Msg {
		if err := open(url); err != nil {
			return noteMsg("couldn't open " + url + ": " + shortError(err))
		}
		return noteMsg("opened " + url)
	}
}

// buildLink is the page of the PR's newest run: its build, or its comment before a build exists.
func buildLink(pr model.PR) string {
	for _, run := range pr.Runs {
		if link := run.Link(); link != "" {
			return link
		}
	}
	return ""
}

// isRateLimit reports whether GitHub refused a request for the rate limit.
func isRateLimit(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "rate limit") || strings.Contains(s, "ratelimit")
}

// shortError is the first line of an error without the request in front of it
// (`Post "https://…": `), cut to 60 columns.
func shortError(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	if strings.HasPrefix(s, "Post \"") || strings.HasPrefix(s, "Get \"") {
		if _, rest, ok := strings.Cut(s, "\": "); ok {
			s = rest
		}
	}
	return ansi.Truncate(s, 60, "...")
}
