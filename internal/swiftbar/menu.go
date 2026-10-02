// Package swiftbar is the menu-bar plugin: the menu SwiftBar shows (Render), the plugin
// script it runs (Script), and the plugin's notifications. Every click runs the plugin
// script, which opens the interactive view; nothing in the menu posts anything.
package swiftbar

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/render"
)

// Icon is the menu-bar symbol (an SF Symbol), template-rendered unless something failed.
const Icon = "arrow.triangle.pull"

// failedIcon is the symbol's config while a run of mine failed: SwiftBar 2.1.1's SFConfig
// (renderingMode Hierarchical or Palette, colors required), base64. sfcolor colors only
// symbols in an item's text, never sfimage. #FF3B30 is systemRed, which reads on light
// and dark menu bars.
var failedIcon = base64.StdEncoding.EncodeToString([]byte(`{"renderingMode":"Palette","colors":["#FF3B30"]}`))

// titleWidth is where a PR's title is cut in the menu; its submenu has it all.
const titleWidth = 33

// Menu is what the plugin shows.
type Menu struct {
	// PRs are the rows shown (listing.Filter applied); Hidden counts the rest.
	PRs    []model.PR
	Hidden map[model.Section]model.Hidden
	// Sections to show, in display order.
	Sections []model.Section
	Viewer   string
	Icons    render.Icons
	// FetchedAt is when the data was fetched (the oldest cache entry for cached data).
	FetchedAt, Now time.Time
	// Err is a failed fetch; PRs are then the last cached ones, or none.
	Err error
	// Rate is the API budget after a live fetch, zero for cached data.
	Rate github.RateLimit
	// MaxAge is the plugin's --max-age: after a failed refresh, data older than twice that
	// has missed a refresh, and the title marks it.
	MaxAge time.Duration
	// Plugin is the plugin script (SWIFTBAR_PLUGIN_PATH), which serves the clicks that
	// copy and refresh. "" when the output isn't SwiftBar's, which leaves out the clicks.
	Plugin string
	// GH is gh's path for the clicks that open the interactive view ("gh" when empty).
	GH string
	// ScriptFormat is the running plugin script's (GH_KOTLIN_PRS_SCRIPT, 1 without it);
	// 0 when unknown. An older one than ScriptFormat gets an item to update it. A future
	// incompatible format would also gate which items the binary emits for old scripts;
	// there's nothing to gate yet.
	ScriptFormat int
}

// Render is the plugin's output: the menu-bar title, then the dropdown.
func Render(m Menu) string {
	var b builder
	b.title(m)
	b.line(0, "---", "")
	b.header(m)
	b.line(0, "---", "")
	for _, block := range render.Blocks(m.PRs, render.Options{Icons: m.Icons, Sections: m.Sections, Hidden: m.Hidden}) {
		nested := block.Section == model.SectionTeams || block.Section == model.SectionMerged
		if nested {
			b.line(0, text(block.Title), "font=Menlo-Bold size=12 "+noAction)
		} else {
			b.line(0, text(block.Title), "font=Menlo-Bold size=12")
		}
		depth := 0
		if nested {
			depth = 1
		}
		if block.Empty {
			b.line(depth, text("  nothing here"), "font=Menlo size=12 color=gray trim=false")
		}
		lines := rowLines(block.Rows)
		for i, row := range block.Rows {
			b.pr(m, depth, lines[i], row.PR)
		}
		if block.Hidden != "" {
			hidden := strings.TrimSuffix(block.Hidden, " (--all)")
			b.line(depth, text("  "+hidden+" (in the interactive view, a)"), "size=11 color=gray trim=false")
		}
	}
	b.line(0, "---", "")
	b.footer(m)
	return b.String()
}

type builder struct{ strings.Builder }

// line writes one item: depth levels of submenu ("--" each), the text, its parameters.
func (b *builder) line(depth int, item, params string) {
	b.WriteString(strings.Repeat("--", depth) + item)
	if params != "" {
		b.WriteString(" | " + params)
	}
	b.WriteString("\n")
}

func (b *builder) title(m Menu) {
	mine, failed, running := 0, false, false
	for _, pr := range m.PRs {
		if pr.Next == model.NextMe {
			mine++
		}
		if pr.Section != model.SectionMine {
			continue
		}
		for _, r := range []model.Run{pr.DryRun, pr.SafeMerge} {
			failed = failed || (!r.Outdated && (r.State == model.RunFailed || r.State == model.RunRejected))
			running = running || (r.State.InProgress() && !r.NoResponse)
		}
	}
	var parts []string
	if mine > 0 {
		parts = append(parts, fmt.Sprint(mine))
	}
	if running {
		parts = append(parts, m.Icons.Ellipsis)
	}
	// "!": nothing could be fetched, or the data shown has missed a refresh (one failed
	// refresh alone doesn't mark it, so a blip doesn't flicker).
	if m.Err != nil && (m.FetchedAt.IsZero() || m.Now.Sub(m.FetchedAt) > 2*m.MaxAge) {
		parts = append(parts, "!")
	}
	params := "sfimage=" + Icon
	if failed {
		params += " sfconfig=" + failedIcon
	}
	b.line(0, strings.Join(parts, " "), params)
}

func (b *builder) header(m Menu) {
	mine := 0
	for _, pr := range m.PRs {
		if pr.Next == model.NextMe {
			mine++
		}
	}
	sep := " " + m.Icons.Separator + " "
	switch {
	case m.Err != nil && m.FetchedAt.IsZero():
		b.line(0, text("Couldn't fetch your PRs: "+firstLine(m.Err)), errorParams(m.Err))
		return
	case m.Err != nil:
		b.line(0, text("Refresh failed: "+firstLine(m.Err)), errorParams(m.Err))
		b.line(0, text(fmt.Sprintf("Your move: %d%sdata from %s", mine, sep, model.Ago(m.Now, m.FetchedAt))), "")
	default:
		b.line(0, text(fmt.Sprintf("Your move: %d%supdated %s", mine, sep, model.Ago(m.Now, m.FetchedAt))), "")
	}
	if r := m.Rate; r.Limit > 0 && r.Remaining*10 < r.Limit {
		b.line(0, text(fmt.Sprintf("API budget %d/%d, resets %s", r.Remaining, r.Limit, r.ResetAt.In(m.Now.Location()).Format("15:04"))), "color=orange")
	}
}

// pr writes a PR's row, its submenu, and the ⌥ alternate that opens it in the browser.
// A click on the row, like the submenu's "Details", opens the interactive view on the PR.
func (b *builder) pr(m Menu, depth int, row string, pr model.PR) {
	// The whole title opens the submenu, since the row cuts it.
	style := "font=Menlo size=12 trim=false emojize=false symbolize=false"
	action := noAction
	if m.Plugin != "" {
		action = m.open(pr.Number, "")
	}
	b.line(depth, text(row), style+" "+action)
	sub := depth + 1
	b.line(sub, text(pr.Title), "color=gray trim=false emojize=false symbolize=false")
	if m.Plugin != "" {
		b.line(sub, "Details in the interactive view", m.open(pr.Number, ""))
	}
	if pr.Section != model.SectionMerged {
		b.reasons(sub, pr, m.Icons)
		b.line(sub, "---", "")
		b.runs(sub, pr, m.Icons, m.Now)
		b.reviewers(sub, pr, m.Icons)
	}
	b.line(sub, "---", "")
	b.line(sub, "Open on GitHub", "href="+param(pr.URL))
	if m.Plugin != "" {
		b.line(sub, "Copy link", "bash="+param(m.Plugin)+" param1=copy param2="+param(pr.URL)+" terminal=false")
		cmds := actions.Available(pr, m.Viewer)
		reRequest, assign := reviewItems(pr, m.Viewer)
		if len(cmds) > 0 || len(reRequest) > 0 || assign {
			b.line(sub, "---", "")
		}
		for _, c := range cmds {
			b.line(sub, text(c.Text+m.Icons.Ellipsis), m.open(pr.Number, c.Name)+" tooltip="+param(text(c.Doc)))
		}
		if len(reRequest) > 0 {
			b.line(sub, text("Re-request review from "+strings.Join(reRequest, ", ")+m.Icons.Ellipsis),
				m.exec(m.runWords(pr.Number, actions.RequestReview))+" tooltip="+param("asks y/N in the terminal, then requests it"))
		}
		if assign {
			b.line(sub, text("Assign reviewers"+m.Icons.Ellipsis), m.open(pr.Number, actions.RequestReview)+
				" tooltip="+param("pick the code owners in the interactive view"))
		}
	}
	b.line(depth, text(row), style+" alternate=true href="+param(pr.URL))
}

// open opens the interactive view on PR number, with command's question ("" for none),
// in the terminal of SwiftBar's settings. SwiftBar types "<its variables>; <bash>
// <params>" into a new tab of the user's shell: with "exec gh kotlin-prs --pr N" the
// tab closes when the interactive view quits.
func (m Menu) open(number int, command string) string {
	return m.exec(openWords(m.GH, number, command))
}

// exec runs words in the terminal of SwiftBar's settings, as open does.
func (m Menu) exec(words []string) string {
	return bashParams(words) + " terminal=true"
}

// bashParams are an item's bash and params for words.
func bashParams(words []string) string {
	s := ""
	for i, w := range words {
		if i == 0 {
			s = "bash=" + param(w)
		} else {
			s += fmt.Sprintf(" param%d=%s", i, param(w))
		}
	}
	return s
}

// runWords run a `run` command on PR number: "<gh> kotlin-prs run N <command>", without
// exec, so the tab stays open with the outcome.
func (m Menu) runWords(number int, command string) []string {
	words := openWords(m.GH, 0, "")[1:]
	return append(words, "run", fmt.Sprint(number), command)
}

// reviewItems say which review items a PR of the viewer gets: "Re-request review from"
// the people to re-request, and "Assign reviewers…" while a code-owner rule is unassigned.
func reviewItems(pr model.PR, viewer string) (reRequest []string, assign bool) {
	if actions.CheckOwnOpen(pr, viewer) != nil {
		return nil, false
	}
	for _, s := range actions.Subsystems(pr) {
		assign = assign || s.Status == "unassigned"
	}
	return actions.PreSelected(pr), assign
}

// openWords are the words of the line that opens the interactive view on PR number (0
// for none), with command's question: "exec <gh> kotlin-prs --pr N --post <command>".
func openWords(gh string, number int, command string) []string {
	if gh == "" {
		gh = "gh"
	}
	words := []string{"exec", gh, "kotlin-prs"}
	if number != 0 {
		words = append(words, "--pr", fmt.Sprint(number))
	}
	if command != "" {
		words = append(words, "--post", command)
	}
	return words
}

// nextWords say whose move it is.
var nextWords = map[model.NextAction]string{
	model.NextMe:        "Your move",
	model.NextCI:        "Waiting on CI",
	model.NextReviewers: "Waiting on reviewers",
	model.NextAuthor:    "Waiting on the author",
	model.NextDone:      "Done",
}

// reasonWidth is where a reason is cut in the menu; the tooltip has it all.
const reasonWidth = 80

func (b *builder) reasons(depth int, pr model.PR, icons render.Icons) {
	b.line(depth, nextWords[pr.Next], "color=gray")
	for _, r := range pr.Reasons {
		params := "trim=false emojize=false symbolize=false"
		if r.URL != "" {
			params += " href=" + param(r.URL)
		}
		if ansi.StringWidth(r.Text) > reasonWidth {
			params += " tooltip=" + param(text(r.Text))
		}
		b.line(depth, text("  "+ansi.Truncate(r.Text, reasonWidth, icons.Ellipsis)), params)
	}
}

func (b *builder) runs(depth int, pr model.PR, icons render.Icons, now time.Time) {
	if len(pr.Runs) == 0 {
		b.line(depth, "Runs: none", "color=gray")
		return
	}
	b.line(depth, "Runs", "color=gray")
	for _, r := range pr.Runs {
		s := fmt.Sprintf("  %s %s %s", r.Kind, ansi.Strip(render.RunSymbol(r, icons)), r.State)
		if r.Outdated {
			s += " (outdated)"
		}
		if !r.Started.IsZero() {
			s += " " + model.Ago(now, r.Started)
		}
		params := "trim=false"
		if link := r.Link(); link != "" {
			params += " href=" + param(link)
		}
		b.line(depth, text(s), params)
	}
}

func (b *builder) reviewers(depth int, pr model.PR, icons render.Icons) {
	b.line(depth, text(fmt.Sprintf("Reviewers (%d approved)", pr.Approvals)), "color=gray")
	for _, r := range pr.Reviewers {
		who := r.Login
		if r.Team != "" {
			who = "team " + r.Team
		}
		s := "  " + who + " " + string(r.State)
		if r.CodeOwner {
			s += " (code owner)"
		}
		b.line(depth, text(s), "trim=false")
	}
	b.line(depth, text("Code owners: "+string(pr.CodeOwners.State)), "color=gray")
	for _, rule := range pr.CodeOwners.Missing() {
		var who []string
		for _, a := range rule.Assignees {
			who = append(who, a.Login)
		}
		s := "  " + icons.OwnersMissing + " " + strings.Join(rule.Paths, ", ")
		if len(who) > 0 {
			s += ": " + strings.Join(who, ", ")
		}
		b.line(depth, text(s), "trim=false")
	}
}

func (b *builder) footer(m Menu) {
	if m.Plugin == "" {
		b.line(0, "Refresh now", "refresh=true")
	} else {
		b.line(0, "Refresh now", "bash="+param(m.Plugin)+" param1=refresh terminal=false refresh=true")
		b.line(0, "Open the interactive view", m.open(0, ""))
	}
	if m.ScriptFormat > 0 && m.ScriptFormat < ScriptFormat {
		b.line(0, "Update the plugin script", m.updateAction())
	}
}

// updateAction runs `swiftbar update` on the running plugin's folder in the background,
// then SwiftBar runs the plugin again.
func (m Menu) updateAction() string {
	words := append(openWords(m.GH, 0, "")[1:], "swiftbar", "update")
	if m.Plugin != "" {
		words = append(words, "--dir", filepath.Dir(m.Plugin))
	}
	return bashParams(words) + " terminal=false refresh=true"
}

// rowLines are the rows as aligned plain text: who has the move, the number, the issue,
// the title, and for open PRs the dry-run, the safe-merge and the reviews (approvals of
// the people reviewing and the code-owners verdict), the cells and symbols of `list`.
func rowLines(rows []render.Row) []string {
	keep := []render.Column{render.ColumnNext, render.ColumnNumber, render.ColumnIssue, render.ColumnTitle,
		render.ColumnDryRun, render.ColumnSafeMerge, render.ColumnReview}
	short := make([]render.Row, len(rows))
	for i, r := range rows {
		short[i].PR = r.PR
		for _, c := range r.Cells {
			if slices.Contains(keep, c.Column) {
				if c.Column == render.ColumnTitle {
					c.Max = titleWidth
				}
				short[i].Cells = append(short[i].Cells, c)
			}
		}
	}
	lines := render.Lines(short, false)
	for i, l := range lines {
		lines[i] = ansi.Strip(l)
	}
	return lines
}

// text neutralizes what SwiftBar would read in an item's text: "|" starts the
// parameters, a leading "-" makes a submenu or separator, a newline a new item.
func text(s string) string {
	s = strings.NewReplacer("|", "¦", "\n", " ", "\r", " ").Replace(s)
	if strings.HasPrefix(strings.TrimLeft(s, " "), "-") {
		s = "\u200b" + s
	}
	return s
}

// noAction is the action of an item with a submenu and nothing to do on a click (a
// section's, or a PR's outside SwiftBar). Every such item needs an action: SwiftBar 2.1.1
// patches a changed item in place and, for an item without one, clears the action AppKit
// gave it for its submenu, so the item turns grey and its submenu stays shut until a full
// rebuild (SwiftBar #512, fixed in 2.1.2). "." is the href SwiftBar skips on a click.
const noAction = "href=."

// param quotes a parameter value when it has blanks; SwiftBar takes quoted values.
func param(v string) string {
	v = strings.NewReplacer("\"", "%22", "\n", " ").Replace(v)
	if strings.ContainsAny(v, " \t'") {
		return "\"" + v + "\""
	}
	return v
}

// errorWidth is where an error is cut in the menu; the tooltip has it all.
const errorWidth = 80

func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return ansi.Truncate(s, errorWidth, "...")
}

// errorParams are an error line's: red, with the whole first line as the tooltip when
// the line cuts it (a config path's "doesn't exist" comes last).
func errorParams(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	if ansi.StringWidth(s) <= errorWidth {
		return "color=red"
	}
	return "color=red tooltip=" + param(text(s))
}
