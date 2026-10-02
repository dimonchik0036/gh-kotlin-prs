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
	authorWidth = 12
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

// Options control the table and the detail view.
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

// Block is one section of the list: what the table prints for it, and what a TUI list
// shows with the same rows.
type Block struct {
	Section model.Section
	// Title is "Mine (3)": the section and how many rows it shows.
	Title string
	// Rows come in display order: my move first.
	Rows []Row
	// Empty is a section with no rows, shown or hidden: the table says "nothing here".
	Empty bool
	// Hidden counts the rows only --all shows: "+4 reviews not waiting on you (--all)".
	Hidden string
}

// Blocks groups the PRs by section, in display order. Mine and Review are always there;
// the other sections only when they have rows or hidden rows.
func Blocks(prs []model.PR, opts Options) []Block {
	bySection := map[model.Section][]model.PR{}
	for _, pr := range prs {
		bySection[pr.Section] = append(bySection[pr.Section], pr)
	}
	var blocks []Block
	for _, section := range sectionOrder {
		prs, hidden := bySection[section], opts.Hidden[section]
		always := section == model.SectionMine || section == model.SectionReview
		if !slices.Contains(opts.Sections, section) || (len(prs) == 0 && hidden.Total() == 0 && !always) {
			continue
		}
		prs = slices.Clone(prs)
		SortRows(prs)
		rows := make([]Row, len(prs))
		for i, pr := range prs {
			rows[i] = NewRow(pr, opts.Icons)
		}
		blocks = append(blocks, Block{
			Section: section,
			Title:   fmt.Sprintf("%s (%d)", sectionTitles[section], len(rows)),
			Rows:    rows,
			Empty:   len(rows) == 0 && hidden.Total() == 0,
			Hidden:  hiddenLine(section, hidden),
		})
	}
	return blocks
}

// Table prints the PRs grouped by section (Blocks).
func Table(w io.Writer, prs []model.PR, opts Options) error {
	var b strings.Builder
	for i, block := range Blocks(prs, opts) {
		if i > 0 {
			b.WriteString("\n")
		}
		writef(&b, "%s\n", styleHeader.Render(block.Title))
		if block.Empty {
			b.WriteString(styleFaint.Render("  nothing here") + "\n")
		}
		for _, line := range Lines(block.Rows, opts.Hyperlinks) {
			b.WriteString(line + "\n")
		}
		if block.Hidden != "" {
			b.WriteString(styleFaint.Render("  "+block.Hidden) + "\n")
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

// Lines renders rows as lines, their cells aligned into columns two spaces apart.
func Lines(rows []Row, links bool) []string {
	cells := make([][]string, len(rows))
	for i, row := range rows {
		cells[i] = make([]string, len(row.Cells))
		for j, c := range row.Cells {
			cells[i][j] = c.Render(links)
		}
	}
	return align(cells)
}

// writeGrid writes cells aligned into columns.
func writeGrid(b *strings.Builder, cells [][]string) {
	for _, line := range align(cells) {
		b.WriteString(line + "\n")
	}
}

// align pads cells into columns two spaces apart; lines have no trailing blanks.
func align(cells [][]string) []string {
	if len(cells) == 0 {
		return nil
	}
	widths := make([]int, len(cells[0]))
	for _, row := range cells {
		for i, c := range row {
			widths[i] = max(widths[i], ansi.StringWidth(c))
		}
	}
	lines := make([]string, len(cells))
	for k, row := range cells {
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
		lines[k] = strings.TrimRight(line.String(), " ")
	}
	return lines
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
