package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/render"
)

var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleFocused  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Yellow)
	styleFaint    = lipgloss.NewStyle().Faint(true)
	styleSelected = lipgloss.NewStyle().Reverse(true)
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Red)
	styleWarning  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
)

// The narrowest the title and reason columns get when the terminal is too narrow for
// their usual widths; past that, lines are cut at the edge.
const (
	minTitleWidth  = 16
	minReasonWidth = 20
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.content())
	v.AltScreen = true
	v.WindowTitle = "gh kotlin-prs"
	return v
}

// content is the whole screen: the body, the filter line while there's a filter, and
// the status bar.
func (m *Model) content() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	var body []string
	switch {
	case m.screen == screenHelp:
		body = m.helpLines()
	case m.screen == screenDetail:
		body = strings.Split(m.viewport.View(), "\n")
	case m.data == nil:
		body = m.loadingLines()
	default:
		body = m.listLines()
	}
	h := m.bodyHeight()
	if len(body) > h {
		body = body[:h]
	}
	for len(body) < h {
		body = append(body, "")
	}
	lines := body
	if m.showFilter() {
		lines = append(lines, m.filter.View())
	}
	lines = append(lines, m.statusBar())
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) showFilter() bool {
	return m.screen == screenList && (m.filtering || m.filter.Value() != "")
}

// bodyHeight is the screen minus the status bar and the filter line.
func (m *Model) bodyHeight() int {
	h := m.height - 1
	if m.showFilter() {
		h--
	}
	return max(h, 1)
}

// listLines are the lines of the list on screen, from the offset that scroll keeps.
func (m *Model) listLines() []string {
	lines, _, _ := m.listLayout()
	return lines[max(0, min(m.offset, len(lines)-m.bodyHeight())):]
}

// scroll moves the list as little as possible to keep the selected row on screen, and
// the title of its section when it's the section's first row.
func (m *Model) scroll() {
	if m.data == nil || m.screen != screenList || m.height <= 0 {
		return
	}
	lines, selected, top := m.listLayout()
	h := m.bodyHeight()
	if selected >= 0 {
		if top < m.offset {
			m.offset = top
		}
		if selected >= m.offset+h {
			m.offset = selected - h + 1
		}
	}
	m.offset = max(0, min(m.offset, len(lines)-h))
}

// listLayout is the sections as `list` prints them, the selected row highlighted. selected
// is the line of that row and top the first line to keep in view with it; -1 without one.
func (m *Model) listLayout() (lines []string, selected, top int) {
	links := m.opts.Render.Hyperlinks
	selected, top = -1, -1
	for i, b := range m.blocks {
		if i > 0 {
			lines = append(lines, "")
		}
		title := styleTitle
		focused := indexOf(b.Rows, m.selected) >= 0
		if focused {
			title = styleFocused
		}
		titleLine := len(lines)
		lines = append(lines, title.Render(b.Title))
		if b.Empty {
			lines = append(lines, styleFaint.Render("  nothing here"))
		}
		for k, line := range render.Lines(b.Rows, links) {
			if b.Rows[k].PR.Number == m.selected {
				selected, top = len(lines), titleLine
				if k > 0 {
					top = len(lines)
				}
				plain := ansi.Strip(line)
				line = styleSelected.Render(plain + strings.Repeat(" ", max(0, m.width-ansi.StringWidth(plain))))
			}
			lines = append(lines, line)
		}
		if b.Hidden != "" {
			lines = append(lines, styleFaint.Render("  "+strings.Replace(b.Hidden, "(--all)", "("+m.key("all")+" to show)", 1)))
		}
	}
	if m.filter.Value() != "" && selected < 0 {
		lines = append(lines, "", styleFaint.Render("  no PR matches the filter ("+m.key("back")+" clears it)"))
	}
	return lines, selected, top
}

func (m *Model) loadingLines() []string {
	var msg string
	if m.err != nil {
		msg = styleError.Render(m.opts.Render.Icons.RunFailed+" couldn't load your PRs: "+shortError(m.err)) + "\n\n" +
			styleFaint.Render(m.key("refresh")+" to retry, "+m.key("quit")+" to quit")
	} else {
		msg = m.spinner.View() + " Loading PRs from GitHub" + m.opts.Render.Icons.Ellipsis
	}
	return strings.Split(lipgloss.Place(m.width, m.bodyHeight(), lipgloss.Center, lipgloss.Center, msg), "\n")
}

// key is the first key bound to action, for hints.
func (m *Model) key(action string) string {
	if keys := m.opts.Config.Keys[action]; len(keys) > 0 {
		return keys[0]
	}
	return action
}

func (m *Model) helpLines() []string {
	lines := []string{styleTitle.Render("Keys") + styleFaint.Render(" (config `keys`; the filter takes enter to keep it, esc to clear it)"), ""}
	names := make([]string, len(config.Actions))
	width := 0
	for i, a := range config.Actions {
		names[i] = strings.Join(m.opts.Config.Keys[a.Name], " / ")
		width = max(width, ansi.StringWidth(names[i]))
	}
	for i, a := range config.Actions {
		lines = append(lines, "  "+names[i]+strings.Repeat(" ", width-ansi.StringWidth(names[i]))+"  "+a.Doc)
	}
	lines = append(lines, "", styleTitle.Render("Symbols"), "")
	// The legend's lines are wider than most terminals: wrap them after their 15-column label.
	wrap := lipgloss.NewStyle().Width(max(m.width-15, 20))
	for _, l := range strings.Split(strings.TrimSuffix(m.opts.Render.Icons.Legend(), "\n"), "\n") {
		for i, part := range strings.Split(wrap.Render(l[15:]), "\n") {
			prefix := l[:15]
			if i > 0 {
				prefix = strings.Repeat(" ", 15)
			}
			lines = append(lines, prefix+strings.TrimRight(part, " "))
		}
	}
	return append(lines, "", styleFaint.Render("  refreshes every "+m.opts.Config.Refresh.String()+" (config `refresh`); "+m.key("help")+" or "+m.key("back")+" to go back"))
}

// updateDetail renders the detail view's PR at the current width and clock.
func (m *Model) updateDetail() {
	if m.screen != screenDetail || m.width <= 0 {
		return
	}
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.bodyHeight())
	pr, ok := m.detailPR()
	if !ok {
		m.viewport.SetContent(styleFaint.Render(fmt.Sprintf("#%d is no longer in the list (esc: back)", m.detail)))
		return
	}
	content := render.DetailView(pr, m.now, m.opts.Render, m.width)
	if m.loading[m.detail] {
		content = m.spinner.View() + " loading the details" + m.opts.Render.Icons.Ellipsis + "\n\n" + content
	}
	m.viewport.SetContent(content)
}

// statusBar is the state of the data on the left, key hints on the right.
func (m *Model) statusBar() string {
	icons := m.opts.Render.Icons
	sep := " " + icons.Separator + " "
	var left []string
	switch {
	case m.data == nil: // the loading screen says it all
	case m.refreshing:
		left = append(left, m.spinner.View()+" refreshing"+icons.Ellipsis+" ("+m.dataAge()+")")
	case m.err != nil:
		why := shortError(m.err)
		if m.rateLimited {
			why = "API rate limit reached" + m.resets()
		}
		left = append(left, styleError.Render(icons.RunFailed+" refresh failed: "+why+" ("+m.dataAge()+")"), m.key("refresh")+" to retry")
	default:
		left = append(left, m.dataAge(), "next refresh in "+until(m.now, m.nextRefresh))
	}
	if warn := m.budgetWarning(); warn != "" {
		left = append(left, styleWarning.Render(warn))
	}
	if m.note != "" {
		left = append(left, m.note)
	}
	hint := func(actions ...string) string {
		parts := make([]string, len(actions))
		for i, a := range actions {
			parts[i] = m.key(a) + " " + a
		}
		return strings.Join(parts, sep)
	}
	hints := hint("help", "quit")
	switch {
	case m.screen == screenDetail:
		hints = hint("back", "open", "build", "help", "quit")
	case m.screen == screenList && m.data != nil:
		hints = hint("details", "filter", "refresh", "help", "quit")
	}
	l := strings.Join(left, sep)
	gap := m.width - ansi.StringWidth(l) - ansi.StringWidth(hints)
	if gap < 2 {
		return l
	}
	return l + strings.Repeat(" ", gap) + styleFaint.Render(hints)
}

// dataAge is "updated 2m ago", or "cached 12m ago" for the snapshot from the cache.
func (m *Model) dataAge() string {
	what := "updated "
	if m.fromCache {
		what = "cached "
	}
	return what + model.Ago(m.now, m.data.FetchedAt)
}

// budgetWarning is shown when less than a tenth of the hourly budget is left, unless a
// request was refused for the rate limit: the error says so then. Cached responses
// never count.
func (m *Model) budgetWarning() string {
	r := m.rate
	if m.rateLimited || r.Limit <= 0 || r.Remaining*10 >= r.Limit {
		return ""
	}
	return fmt.Sprintf("! API budget %d/%d%s", r.Remaining, r.Limit, m.resets())
}

// resets is ", resets 18:00" from the last live response, or nothing.
func (m *Model) resets() string {
	if m.rate.ResetAt.IsZero() {
		return ""
	}
	return ", resets " + m.rate.ResetAt.In(m.now.Location()).Format("15:04")
}

// until is "2m" or "40s" from now to t, rounded up; "now" once it's passed.
func until(now, t time.Time) string {
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int((d+time.Second-1)/time.Second))
	}
	return fmt.Sprintf("%dm", int((d+time.Minute-1)/time.Minute))
}

// fit narrows the reason, then the title column of a section's rows until the rows fit
// in width, down to their minimum widths.
func fit(rows []render.Row, width int) {
	if len(rows) == 0 || width <= 0 {
		return
	}
	widths := make([]int, len(rows[0].Cells))
	for _, r := range rows {
		for i, c := range r.Cells {
			w := ansi.StringWidth(c.Text())
			if c.Max > 0 {
				w = min(w, c.Max)
			}
			widths[i] = max(widths[i], w)
		}
	}
	total := 2 * (len(widths) - 1)
	for _, w := range widths {
		total += w
	}
	excess := total - width
	for _, flex := range []struct {
		column render.Column
		least  int
	}{{render.ColumnReason, minReasonWidth}, {render.ColumnTitle, minTitleWidth}} {
		i := columnIndex(rows[0], flex.column)
		if excess <= 0 || i < 0 {
			continue
		}
		cut := min(excess, widths[i]-flex.least)
		if cut <= 0 {
			continue
		}
		for r := range rows {
			rows[r].Cells[i].Max = widths[i] - cut
		}
		excess -= cut
	}
}

func columnIndex(row render.Row, column render.Column) int {
	for i, c := range row.Cells {
		if c.Column == column {
			return i
		}
	}
	return -1
}
