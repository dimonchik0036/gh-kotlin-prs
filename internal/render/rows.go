package render

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// Column is a column of the list. Open PRs have every column but Author, which only
// Review has; Recently merged has Next, Number, Issue, Title and Reason.
type Column string

const (
	ColumnNext      Column = "next"
	ColumnNumber    Column = "number"
	ColumnIssue     Column = "issue"
	ColumnTitle     Column = "title"
	ColumnAuthor    Column = "author"
	ColumnDryRun    Column = "dry-run"
	ColumnSafeMerge Column = "safe-merge"
	ColumnReview    Column = "review"
	ColumnThreads   Column = "threads"
	ColumnReason    Column = "reason"
)

// Row is one PR of the list as cells, the same for the table and a TUI list.
type Row struct {
	PR    model.PR
	Cells []Cell
}

// Cell is one cell of a row: spans of styled text, each linked or not. It renders with
// or without hyperlinks; widths treat the escapes as zero-width.
type Cell struct {
	Column Column
	// Max caps the display width, 0 for none: longer text is cut and ends in the ellipsis.
	Max      int
	ellipsis string
	spans    []span
}

// span is the part of a cell with one link target ("" for none).
type span struct {
	url      string
	segments []segment
}

// URL is the cell's link target: the PR, the primary issue, the run's build or comment,
// or the page of the reason. Empty when it links nowhere.
func (c Cell) URL() string {
	for _, s := range c.spans {
		if s.url != "" {
			return s.url
		}
	}
	return ""
}

// Text is the cell's plain text, untruncated.
func (c Cell) Text() string {
	var b strings.Builder
	for _, s := range c.spans {
		for _, seg := range s.segments {
			b.WriteString(seg.text)
		}
	}
	return b.String()
}

// Render styles the cell, cut to Max, its spans hyperlinked when links are on.
func (c Cell) Render(links bool) string {
	spans := c.spans
	if c.Max > 0 && ansi.StringWidth(c.Text()) > c.Max {
		spans = truncateSpans(spans, c.Max, c.ellipsis)
	}
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(styledLink(links, s.url, s.segments...))
	}
	return b.String()
}

// truncateSpans cuts spans wider than limit so that, with tail appended to the cut
// segment in its style, they are limit wide (ansi.Truncate across segments).
func truncateSpans(spans []span, limit int, tail string) []span {
	room := max(limit-ansi.StringWidth(tail), 0)
	var out []span
	for _, s := range spans {
		var segments []segment
		for _, seg := range s.segments {
			if w := ansi.StringWidth(seg.text); w <= room {
				segments, room = append(segments, seg), room-w
				continue
			}
			seg.text = ansi.Truncate(seg.text, room, "") + tail
			return append(out, span{url: s.url, segments: append(segments, seg)})
		}
		out = append(out, span{url: s.url, segments: segments})
	}
	return out
}

func textCell(column Column, url string, segments ...segment) Cell {
	return Cell{Column: column, spans: []span{{url: url, segments: segments}}}
}

// NewRow builds the cells of a PR: who has the move, the number, the issue, the title,
// the author in Review, then for open PRs the dry-run and safe-merge (on a release branch,
// the quality gates), the reviews and threads, and the primary reason.
func NewRow(pr model.PR, icons Icons) Row {
	title := trimIssuePrefix(pr.Title, pr.Issues)
	if pr.Draft {
		title = "[draft] " + title
	}
	titleCell := textCell(ColumnTitle, "", segment{text: title})
	titleCell.Max, titleCell.ellipsis = titleWidth, icons.Ellipsis

	primary := pr.PrimaryReason()
	reasonStyle := lipgloss.NewStyle()
	if pr.Next == model.NextMe {
		reasonStyle = styleMe
	}
	reason := textCell(ColumnReason, primary.URL, segment{text: primary.Text, style: reasonStyle})
	reason.Max, reason.ellipsis = reasonWidth, icons.Ellipsis

	next := textCell(ColumnNext, "", nextSegment(pr.Next, icons))
	number := textCell(ColumnNumber, pr.URL, segment{text: fmt.Sprintf("#%d", pr.Number)})
	if pr.Section == model.SectionMerged {
		return Row{PR: pr, Cells: []Cell{next, number, issueCell(pr), titleCell, reason}}
	}
	threads := Cell{Column: ColumnThreads}
	if pr.UnresolvedThreads > 0 {
		threads = textCell(ColumnThreads, "", segment{text: pluralize(pr.UnresolvedThreads, "thread")})
	}
	cells := []Cell{next, number, issueCell(pr), titleCell}
	if pr.Section == model.SectionReview {
		author := textCell(ColumnAuthor, github.ProfileURL(pr.Author), segment{text: pr.Author})
		author.Max, author.ellipsis = authorWidth, icons.Ellipsis
		cells = append(cells, author)
	}
	// A release branch has no dry-run or safe-merge: its quality gates take their place.
	dryRun, safeMerge := runCell(ColumnDryRun, "DR", pr.DryRun, icons), runCell(ColumnSafeMerge, "SM", pr.SafeMerge, icons)
	if pr.Release {
		dryRun, safeMerge = runCell(ColumnDryRun, "QG", pr.QualityGate, icons), Cell{Column: ColumnSafeMerge}
	}
	return Row{PR: pr, Cells: append(cells,
		dryRun,
		safeMerge,
		reviewCell(pr, icons),
		threads,
		reason,
	)}
}

// issueCell is the primary issue and how many more there are: "KT-123 +1".
func issueCell(pr model.PR) Cell {
	c := Cell{Column: ColumnIssue}
	if len(pr.Issues) == 0 {
		return c
	}
	c.spans = []span{{url: pr.Issues[0].URL, segments: []segment{{text: pr.Issues[0].ID}}}}
	if len(pr.Issues) > 1 {
		c.spans = append(c.spans, span{segments: []segment{{text: fmt.Sprintf(" +%d", len(pr.Issues)-1)}}})
	}
	return c
}

// reviewCell is "approvals/reviewers" plus the code-owners verdict.
func reviewCell(pr model.PR, icons Icons) Cell {
	people := 0
	for _, r := range pr.Reviewers {
		if r.Login != "" {
			people++
		}
	}
	owners := segment{text: icons.OwnersUnknown}
	switch pr.CodeOwners.State {
	case model.CodeOwnersOK:
		owners = segment{text: icons.OwnersOK, style: stylePassed}
	case model.CodeOwnersMissing:
		owners = segment{text: icons.OwnersMissing, style: styleFailed}
	}
	return textCell(ColumnReview, "", segment{text: fmt.Sprintf("%d/%d ", pr.Approvals, people)}, owners)
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
	return nextSegment(next, icons).render()
}

func nextSegment(next model.NextAction, icons Icons) segment {
	switch next {
	case model.NextMe:
		return segment{text: icons.NextMe, style: styleMe}
	case model.NextCI:
		return segment{text: icons.NextCI, style: styleProgress}
	case model.NextDone:
		return segment{text: icons.NextDone, style: stylePassed}
	}
	return segment{text: icons.NextOther}
}

// runCell is "DR ✓", linked to the run's build (or its comment). Only the label looks
// like a link; the status symbol keeps its own style.
func runCell(column Column, label string, run model.Run, icons Icons) Cell {
	symbol, style := runSymbol(run, icons)
	return Cell{Column: column, spans: []span{{url: run.Link(), segments: []segment{
		{text: label}, {text: " "}, {text: symbol, style: style, symbol: true},
	}}}}
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
