// Package render prints the model as a table, a detail view or JSON. Styles are
// always emitted; the caller's writer downsamples or strips them (colorprofile).
package render

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

const (
	titleWidth  = 50
	reasonWidth = 70
)

var (
	styleHeader   = lipgloss.NewStyle().Bold(true)
	styleMe       = lipgloss.NewStyle().Foreground(lipgloss.Yellow).Bold(true)
	styleFaint    = lipgloss.NewStyle().Faint(true)
	stylePassed   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	styleFailed   = lipgloss.NewStyle().Foreground(lipgloss.Red)
	styleProgress = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
)

// Sections in display order.
var sectionOrder = []model.Section{model.SectionMine, model.SectionReview, model.SectionTeams, model.SectionMerged}

var sectionTitles = map[model.Section]string{
	model.SectionMine:   "Mine",
	model.SectionReview: "Review",
	model.SectionTeams:  "Team requests",
	model.SectionMerged: "Recently merged (24h)",
}

// SortRows orders a section: rows where the move is mine first, then by last update.
func SortRows(prs []model.PR) {
	slices.SortStableFunc(prs, func(a, b model.PR) int {
		if am, bm := a.Next == model.NextMe, b.Next == model.NextMe; am != bm {
			if am {
				return -1
			}
			return 1
		}
		return cmp.Or(b.Updated.Compare(a.Updated), cmp.Compare(b.MergedAt.Unix(), a.MergedAt.Unix()), b.Number-a.Number)
	})
}

// Options control the table.
type Options struct {
	Icons Icons
	// Hyperlinks turns PR numbers, issue IDs, builds and logins into terminal links.
	Hyperlinks bool
	// Org owns the repository; team links point to its teams.
	Org string
	// Sections to show, in any order; they print in display order.
	Sections []model.Section
	// Hidden counts the rows left out of each section (shown with --all).
	Hidden map[model.Section]model.Hidden
}

// Table prints the PRs grouped by section. Mine and Review always print; the other
// sections only when they have rows or hidden rows.
func Table(w io.Writer, prs []model.PR, opts Options) error {
	bySection := map[model.Section][]model.PR{}
	for _, pr := range prs {
		bySection[pr.Section] = append(bySection[pr.Section], pr)
	}
	var b strings.Builder
	first := true
	for _, section := range sectionOrder {
		rows, hidden := bySection[section], opts.Hidden[section]
		always := section == model.SectionMine || section == model.SectionReview
		if !slices.Contains(opts.Sections, section) || (len(rows) == 0 && hidden.Total() == 0 && !always) {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		writef(&b, "%s\n", styleHeader.Render(fmt.Sprintf("%s (%d)", sectionTitles[section], len(rows))))
		if len(rows) == 0 && hidden.Total() == 0 {
			b.WriteString(styleFaint.Render("  nothing here") + "\n")
		}
		rows = slices.Clone(rows)
		SortRows(rows)
		writeRows(&b, rows, opts)
		if line := hiddenLine(section, hidden); line != "" {
			b.WriteString(styleFaint.Render("  "+line) + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// hiddenLine is "+4 reviews not waiting on you, 1 draft (--all)".
func hiddenLine(section model.Section, h model.Hidden) string {
	var parts []string
	switch {
	case h.NotWaiting == 0:
	case section == model.SectionReview:
		parts = append(parts, pluralize(h.NotWaiting, "review")+" not waiting on you")
	default:
		parts = append(parts, fmt.Sprintf("%d hidden", h.NotWaiting))
	}
	if h.Drafts > 0 {
		parts = append(parts, pluralize(h.Drafts, "draft"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "+" + strings.Join(parts, ", ") + " (--all)"
}

func writeRows(b *strings.Builder, rows []model.PR, opts Options) {
	cells := make([][]string, len(rows))
	for i, pr := range rows {
		cells[i] = rowCells(pr, opts)
	}
	writeGrid(b, cells)
}

// writeGrid aligns cells into columns two spaces apart.
func writeGrid(b *strings.Builder, cells [][]string) {
	if len(cells) == 0 {
		return
	}
	widths := make([]int, len(cells[0]))
	for _, row := range cells {
		for i, c := range row {
			widths[i] = max(widths[i], ansi.StringWidth(c))
		}
	}
	for _, row := range cells {
		var line strings.Builder
		for i, c := range row {
			if i > 0 {
				line.WriteString("  ")
			}
			line.WriteString(c)
			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-ansi.StringWidth(c)))
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " ") + "\n")
	}
}

func rowCells(pr model.PR, opts Options) []string {
	icons := opts.Icons
	title := trimIssuePrefix(pr.Title, pr.Issues)
	if pr.Draft {
		title = "[draft] " + title
	}
	title = ansi.Truncate(title, titleWidth, icons.Ellipsis)

	primary := pr.PrimaryReason()
	reasonStyle := lipgloss.NewStyle()
	if pr.Next == model.NextMe {
		reasonStyle = styleMe
	}
	reason := styledLink(opts.Hyperlinks, primary.URL, segment{text: ansi.Truncate(primary.Text, reasonWidth, icons.Ellipsis), style: reasonStyle})
	if pr.Section == model.SectionMerged {
		return []string{NextMarker(pr.Next, icons), numberCell(pr, opts), issueCell(pr, opts), title, reason}
	}
	threads := ""
	if pr.UnresolvedThreads > 0 {
		threads = pluralize(pr.UnresolvedThreads, "thread")
	}
	return []string{
		NextMarker(pr.Next, icons),
		numberCell(pr, opts),
		issueCell(pr, opts),
		title,
		runCell("DR", pr.DryRun, opts),
		runCell("SM", pr.SafeMerge, opts),
		ReviewSummary(pr, icons),
		threads,
		reason,
	}
}

func numberCell(pr model.PR, opts Options) string {
	return link(opts.Hyperlinks, pr.URL, fmt.Sprintf("#%d", pr.Number))
}

// issueCell is the primary issue and how many more there are: "KT-123 +1".
func issueCell(pr model.PR, opts Options) string {
	if len(pr.Issues) == 0 {
		return ""
	}
	primary := link(opts.Hyperlinks, pr.Issues[0].URL, pr.Issues[0].ID)
	if len(pr.Issues) == 1 {
		return primary
	}
	return fmt.Sprintf("%s +%d", primary, len(pr.Issues)-1)
}

// trimIssuePrefix drops the issue IDs Kotlin titles usually start with
// ("KT-1, KT-2: Fix …"): they have their own column.
func trimIssuePrefix(title string, issues []model.Issue) string {
	rest := title
	for trimmed := true; trimmed; {
		trimmed = false
		for _, issue := range issues {
			if r, ok := strings.CutPrefix(rest, issue.ID); ok && (r == "" || strings.ContainsRune(":, ", rune(r[0]))) {
				rest, trimmed = strings.TrimLeft(r, ", "), true
			}
		}
	}
	if rest == title {
		return title
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, ":"))
}

// NextMarker is the first column: who has the move.
func NextMarker(next model.NextAction, icons Icons) string {
	switch next {
	case model.NextMe:
		return styleMe.Render(icons.NextMe)
	case model.NextCI:
		return styleProgress.Render(icons.NextCI)
	case model.NextDone:
		return stylePassed.Render(icons.NextDone)
	}
	return icons.NextOther
}

// runCell is "DR ✓", linked to the run's build (or its comment) when links are on. Only
// the label looks like a link; the status symbol keeps its own style.
func runCell(label string, run model.Run, opts Options) string {
	symbol, style := runSymbol(run, opts.Icons)
	return styledLink(opts.Hyperlinks, run.Link(),
		segment{text: label}, segment{text: " "}, segment{text: symbol, style: style, symbol: true})
}

// RunSymbol is the state of a run; outdated runs get the Outdated prefix and are faint.
func RunSymbol(run model.Run, icons Icons) string {
	symbol, style := runSymbol(run, icons)
	return style.Render(symbol)
}

func runSymbol(run model.Run, icons Icons) (string, lipgloss.Style) {
	var s string
	style := lipgloss.NewStyle()
	switch run.State {
	case model.RunRequested:
		s, style = icons.RunRequested, styleProgress
		if run.NoResponse {
			s, style = icons.RunNoResponse, styleFailed
		}
	case model.RunAccepted:
		s, style = icons.RunAccepted, styleProgress
	case model.RunRunning:
		s, style = icons.RunRunning, styleProgress
	case model.RunPassed:
		s, style = icons.RunPassed, stylePassed
	case model.RunFailed:
		s, style = icons.RunFailed, styleFailed
	case model.RunRejected:
		s, style = icons.RunRejected, styleFailed
	case model.RunCancelled:
		s = icons.RunCancelled
	default:
		return icons.RunNone, lipgloss.NewStyle()
	}
	if run.Outdated {
		return icons.Outdated + s, styleFaint
	}
	return s, style
}

// ReviewSummary is "approvals/reviewers" plus the code-owners verdict.
func ReviewSummary(pr model.PR, icons Icons) string {
	people := 0
	for _, r := range pr.Reviewers {
		if r.Login != "" {
			people++
		}
	}
	owners := icons.OwnersUnknown
	switch pr.CodeOwners.State {
	case model.CodeOwnersOK:
		owners = stylePassed.Render(icons.OwnersOK)
	case model.CodeOwnersMissing:
		owners = styleFailed.Render(icons.OwnersMissing)
	}
	return fmt.Sprintf("%d/%d %s", pr.Approvals, people, owners)
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
