package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// picker picks code owners to request a review from on a PR: one node per subsystem of
// the code-owners table, open when it needs me (or has a pick), the rest collapsed. A
// person is picked in every row they're in.
type picker struct {
	pr     model.PR
	rows   []pickerRow
	picked []string // in picking order
	cursor int      // into items()
	offset int      // the first item on screen
	// confirming: enter asked to send the picks; y sends, anything else goes back.
	confirming bool
}

type pickerRow struct {
	actions.Subsystem
	open bool
}

// pickerItem is a line of the tree: a row's header (candidate < 0) or a candidate.
type pickerItem struct{ row, candidate int }

func newPicker(pr model.PR) *picker {
	p := &picker{pr: pr, picked: actions.PreSelected(pr)}
	for _, s := range actions.Subsystems(pr) {
		p.rows = append(p.rows, pickerRow{Subsystem: s})
	}
	for i := range p.rows {
		p.rows[i].open = p.rows[i].NeedsMe || len(p.pickedIn(i)) > 0
	}
	return p
}

func (p *picker) items() []pickerItem {
	var out []pickerItem
	for i, r := range p.rows {
		out = append(out, pickerItem{row: i, candidate: -1})
		if r.open {
			for k := range r.Candidates {
				out = append(out, pickerItem{row: i, candidate: k})
			}
		}
	}
	return out
}

func (p *picker) isPicked(login string) bool {
	return slices.ContainsFunc(p.picked, func(l string) bool { return strings.EqualFold(l, login) })
}

// pickedIn is the row's candidates that are picked.
func (p *picker) pickedIn(row int) []string {
	var out []string
	for _, c := range p.rows[row].Candidates {
		if p.isPicked(c.Login) {
			out = append(out, c.Login)
		}
	}
	return out
}

// toggle picks or unpicks the candidate under the cursor, in every row; on a header it
// opens or closes the row.
func (p *picker) toggle() {
	it := p.items()[p.cursor]
	if it.candidate < 0 {
		p.rows[it.row].open = !p.rows[it.row].open
		return
	}
	login := p.rows[it.row].Candidates[it.candidate].Login
	if p.isPicked(login) {
		p.picked = slices.DeleteFunc(p.picked, func(l string) bool { return strings.EqualFold(l, login) })
	} else {
		p.picked = append(p.picked, login)
	}
}

// setOpen opens or closes the row under the cursor, which moves to its header on a close.
func (p *picker) setOpen(open bool) {
	it := p.items()[p.cursor]
	p.rows[it.row].open = open
	if !open {
		p.cursor = slices.Index(p.items(), pickerItem{row: it.row, candidate: -1})
	}
}

func (p *picker) move(by int) {
	p.cursor = max(0, min(len(p.items())-1, p.cursor+by))
}

// covered counts the rows approved, requested, or with a pick.
func (p *picker) covered() int {
	n := 0
	for i, r := range p.rows {
		if r.Covered || len(p.pickedIn(i)) > 0 {
			n++
		}
	}
	return n
}

// openPicker opens the picker on the current PR, or says why there's nothing to pick.
func (m *Model) openPicker() {
	pr, ok := m.current()
	if !ok || m.data == nil {
		m.setNote("no PR selected")
		return
	}
	if err := actions.CheckOwnOpen(pr, m.data.Viewer); err != nil {
		m.setNote(err.Error())
		return
	}
	p := newPicker(pr)
	if len(p.rows) == 0 {
		m.setNote(fmt.Sprintf("#%d has no code owners to ask", pr.Number))
		return
	}
	m.picker = p
}

// reviewRefusal says why a review of pr can't be requested from logins now, or nil: the
// request's own checks, and the TUI's: not in demo mode, nor twice before a refresh.
func (m *Model) reviewRefusal(pr model.PR, logins []string) error {
	if err := actions.CheckReviewRequest(pr, m.data.Viewer, logins); err != nil {
		return err
	}
	if m.opts.RequestReview == nil && !m.opts.Demo {
		return fmt.Errorf("%w: nothing to request reviews with", actions.ErrNotPosted)
	}
	if slices.ContainsFunc(m.posts[pr.Number], func(p post) bool { return p.reviewers != nil }) {
		return fmt.Errorf("%w: a review of #%d was just requested; the next refresh shows it", actions.ErrNotPosted, pr.Number)
	}
	return nil
}

type reviewRequestedMsg struct {
	number int
	logins []string
	at     time.Time
	err    error
}

// onPickerKey moves, picks, opens and closes rows; enter asks to send what's picked, the
// back action closes. While it asks, y sends and any other key goes back to the picks.
func (m *Model) onPickerKey(key string) tea.Cmd {
	p := m.picker
	if p.confirming {
		p.confirming = false
		if key == "y" {
			return m.sendReviewRequest()
		}
		return nil
	}
	switch action := m.bindings[key]; {
	case action == "back":
		m.picker = nil
		m.setNote("no review requested")
	case key == "enter":
		if err := m.reviewRefusal(p.pr, p.picked); err != nil {
			m.setNote(err.Error())
			return nil
		}
		p.confirming = true
	case key == "space":
		p.toggle()
	case key == "left":
		p.setOpen(false)
	case key == "right":
		p.setOpen(true)
	case action == "up":
		p.move(-1)
	case action == "down":
		p.move(1)
	case action == "pageUp":
		p.move(-m.bodyHeight())
	case action == "pageDown":
		p.move(m.bodyHeight())
	}
	return nil
}

// sendReviewRequest closes the picker and requests the review from its picks.
func (m *Model) sendReviewRequest() tea.Cmd {
	p := m.picker
	m.picker = nil
	request, ctx, number, logins := m.opts.RequestReview, m.ctx, p.pr.Number, slices.Clone(p.picked)
	if m.opts.Demo {
		return func() tea.Msg { return reviewRequestedMsg{number: number, logins: logins, at: m.opts.Now()} }
	}
	return func() tea.Msg {
		err := request(ctx, number, logins)
		return reviewRequestedMsg{number: number, logins: logins, at: m.opts.Now(), err: err}
	}
}

// reviewPrompt is the question enter asks in the picker.
func (p *picker) reviewPrompt() string {
	return fmt.Sprintf("Request a review of #%d from %s? [y/N]", p.pr.Number, strings.Join(p.picked, ", "))
}

// onReviewRequested shows the people as requested until the next refresh, or the failure.
func (m *Model) onReviewRequested(msg reviewRequestedMsg) {
	who := strings.Join(msg.logins, ", ")
	if msg.err != nil {
		m.setNote(fmt.Sprintf("%s couldn't request a review of #%d from %s: %s", m.opts.Render.Icons.RunFailed, msg.number, who, shortError(msg.err)))
		return
	}
	m.posts[msg.number] = append(m.posts[msg.number], post{reviewers: msg.logins, at: msg.at})
	m.setNote(m.demoNote() + fmt.Sprintf("requested a review of #%d from %s", msg.number, who))
	if m.data != nil {
		m.reclassify()
	}
}

// pickerLines are the picker: a title with the keys, the tree, the coverage.
func (m *Model) pickerLines() []string {
	p := m.picker
	icons := m.opts.Render.Icons
	sep := " " + icons.Separator + " "
	title := styleTitle.Render(fmt.Sprintf("Request review on #%d", p.pr.Number)) + "  " +
		styleFaint.Render(strings.Join([]string{"↑↓ move", "space pick", "←→ collapse/expand", "enter send", m.key("back") + " cancel"}, sep))
	var tree []string
	pathWidth := 0
	for _, r := range p.rows {
		pathWidth = max(pathWidth, ansi.StringWidth(rowPath(r.Subsystem)))
	}
	nameWidth := 0
	for _, r := range p.rows {
		for _, c := range r.Candidates {
			nameWidth = max(nameWidth, ansi.StringWidth(candidateName(c)))
		}
	}
	items := p.items()
	for i, it := range items {
		r := p.rows[it.row]
		var line string
		if it.candidate < 0 {
			mark := "▸"
			if r.open {
				mark = "▾"
			}
			status := r.Status
			if picked := p.pickedIn(it.row); len(picked) > 0 {
				status = "→ " + strings.Join(picked, ", ")
			}
			line = fmt.Sprintf("%s %-*s  %s", mark, pathWidth, rowPath(r.Subsystem), styleFaint.Render(status))
		} else {
			c := r.Candidates[it.candidate]
			box := "[ ]"
			if p.isPicked(c.Login) {
				box = "[x]"
			}
			name := candidateName(c)
			pad := strings.Repeat(" ", nameWidth-ansi.StringWidth(name))
			if c.Unavailable {
				name = styleFaint.Render(name)
			}
			line = "    " + box + " " + name
			if hint := c.Hint(); hint != "" {
				line += pad + "  " + styleFaint.Render(hint)
			}
		}
		if i == p.cursor {
			plain := ansi.Strip(line)
			line = styleSelected.Render(plain + strings.Repeat(" ", max(0, m.width-ansi.StringWidth(plain))))
		}
		tree = append(tree, line)
	}
	who := "nobody picked"
	if len(p.picked) > 0 {
		who = "will request: " + strings.Join(p.picked, ", ")
	}
	footer := []string{"  " + styleFaint.Render(strings.Repeat("─", min(36, max(0, m.width-2)))),
		fmt.Sprintf("  %d of %d subsystems covered%s%s", p.covered(), len(p.rows), sep, who)}

	room := max(1, m.bodyHeight()-2-len(footer))
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+room {
		p.offset = p.cursor - room + 1
	}
	p.offset = max(0, min(p.offset, len(tree)-room))
	tree = tree[p.offset:min(len(tree), p.offset+room)]
	return append(append([]string{title, ""}, tree...), footer...)
}

// rowPath is the row's first path, with "+N" for its other paths.
func rowPath(s actions.Subsystem) string {
	if s.More > 0 {
		return fmt.Sprintf("%s +%d", s.Path, s.More)
	}
	return s.Path
}

// candidateName is the login with the display name and the bot's marks:
// "judy_user (Judy Doe) (QA) ⏳".
func candidateName(c actions.Candidate) string {
	name := c.Login
	if c.Name != "" {
		name += " (" + c.Name + ")"
	}
	if c.Role != "" {
		name += " (" + c.Role + ")"
	}
	if c.Unavailable {
		name += " ⏳"
	}
	return name
}
