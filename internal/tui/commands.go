package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// post is a command posted from the TUI: until a refresh that started after it, its
// run shows as requested, and it isn't posted twice.
type post struct {
	cmd actions.Command
	url string
	at  time.Time
}

// menu lists the commands that can be posted on a PR now.
type menu struct {
	pr     model.PR
	items  []actions.Command
	cursor int
}

// confirmation waits for y to post cmd on pr.
type confirmation struct {
	cmd actions.Command
	pr  model.PR
}

type postedMsg struct {
	number int
	cmd    actions.Command
	url    string
	at     time.Time
	err    error
}

// commandOf is the command an action posts.
func commandOf(action string) (actions.Command, bool) {
	for _, c := range actions.Commands {
		if c.Action == action {
			return c, true
		}
	}
	return actions.Command{}, false
}

// refusal says why c can't be posted on pr now, or nil: the command's own checks, and
// the TUI's: it doesn't post in demo mode, nor the same command twice before a refresh
// shows the first.
func (m *Model) refusal(c actions.Command, pr model.PR) error {
	if err := actions.Check(c, pr, m.data.Viewer); err != nil {
		return err
	}
	if m.opts.Post == nil {
		return fmt.Errorf("%w: demo mode never posts", actions.ErrNotPosted)
	}
	for _, p := range m.posts[pr.Number] {
		if p.cmd == c {
			return fmt.Errorf("%w: %s was just posted on #%d; the next refresh shows what the bot made of it", actions.ErrNotPosted, c.Text, pr.Number)
		}
	}
	return nil
}

// ask asks to confirm c on the current PR, or says why it can't be posted.
func (m *Model) ask(c actions.Command) {
	pr, ok := m.current()
	if !ok || m.data == nil {
		m.setNote("no PR selected")
		return
	}
	if err := m.refusal(c, pr); err != nil {
		m.setNote(err.Error())
		return
	}
	m.confirm = &confirmation{cmd: c, pr: pr}
}

// openMenu lists the commands for the current PR, or says why there are none.
func (m *Model) openMenu() {
	pr, ok := m.current()
	if !ok || m.data == nil {
		m.setNote("no PR selected")
		return
	}
	var items []actions.Command
	var why error
	for _, c := range actions.Commands {
		if err := m.refusal(c, pr); err != nil {
			why = err
		} else {
			items = append(items, c)
		}
	}
	if len(items) == 0 {
		m.setNote(fmt.Sprintf("nothing to post on #%d now (%v)", pr.Number, why))
		return
	}
	m.menu = &menu{pr: pr, items: items}
}

// onMenuKey picks a command by its key, or moves and picks with enter; anything else
// that isn't a movement closes the menu.
func (m *Model) onMenuKey(key string) tea.Cmd {
	mn := m.menu
	action := m.bindings[key]
	if c, ok := commandOf(action); ok {
		m.menu = nil
		m.ask(c)
		return nil
	}
	switch {
	case action == "up":
		mn.cursor = max(0, mn.cursor-1)
	case action == "down":
		mn.cursor = min(len(mn.items)-1, mn.cursor+1)
	case key == "enter":
		m.menu = nil
		m.ask(mn.items[mn.cursor])
	default:
		m.menu = nil
	}
	return nil
}

// onConfirmKey posts on y; any other key, enter and esc included, cancels.
func (m *Model) onConfirmKey(key string) tea.Cmd {
	c := m.confirm
	m.confirm = nil
	if key != "y" {
		m.setNote("not posted")
		return nil
	}
	post, ctx := m.opts.Post, m.ctx
	return func() tea.Msg {
		url, err := post(ctx, c.pr.Number, c.cmd.Text)
		return postedMsg{number: c.pr.Number, cmd: c.cmd, url: url, at: m.opts.Now(), err: err}
	}
}

// onPosted marks the post's run requested until the next refresh, or shows the failure.
func (m *Model) onPosted(msg postedMsg) {
	if msg.err != nil {
		m.setNote(m.opts.Render.Icons.RunFailed + " couldn't post " + msg.cmd.Text + " to #" + fmt.Sprint(msg.number) + ": " + shortError(msg.err))
		return
	}
	m.posts[msg.number] = append(m.posts[msg.number], post{cmd: msg.cmd, url: msg.url, at: msg.at})
	m.setNote(fmt.Sprintf("posted %s to #%d", msg.cmd.Text, msg.number))
	if m.data != nil {
		m.reclassify()
	}
}

// postedSince is the PRs posted on at or after t.
func (m *Model) postedSince(t time.Time) map[int]bool {
	own := map[int]bool{}
	for n, posts := range m.posts {
		for _, p := range posts {
			if !p.at.Before(t) {
				own[n] = true
			}
		}
	}
	return own
}

// forgetPosts drops the posts made before t: a refresh that started after them sees
// their comments.
func (m *Model) forgetPosts(t time.Time) {
	for n, posts := range m.posts {
		var kept []post
		for _, p := range posts {
			if !p.at.Before(t) {
				kept = append(kept, p)
			}
		}
		if len(kept) == 0 {
			delete(m.posts, n)
		} else {
			m.posts[n] = kept
		}
	}
}
