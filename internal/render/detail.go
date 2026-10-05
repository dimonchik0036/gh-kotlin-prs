package render

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// markLabels spell out the bot's code-owner marks ❌ 🔄 🔴 ✅.
var markLabels = map[model.ApprovalMark]string{
	model.MarkNoReview:         "no review",
	model.MarkReRequest:        "commented, review not re-requested",
	model.MarkChangesRequested: "changes requested",
	model.MarkApproved:         "approved",
	model.MarkUnknown:          "unknown",
}

// issueLabel: the resolution of a trailer, else where the ID was found.
func issueLabel(issue model.Issue) string {
	switch {
	case issue.Resolution != model.IssueRelated:
		return string(issue.Resolution)
	case issue.Source == model.IssueTrailer:
		return "related"
	}
	return string(issue.Source)
}

// JSON writes the document with two-space indentation.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// ShowOutput is the JSON document of `show --format json`.
type ShowOutput struct {
	Version     int       `json:"version"`
	Viewer      string    `json:"viewer"`
	GeneratedAt time.Time `json:"generatedAt"`
	PR          model.PR  `json:"pr"`
}

// Detail prints everything known about one PR (DetailView), at any width.
func Detail(w io.Writer, pr model.PR, now time.Time, opts Options) error {
	_, err := io.WriteString(w, DetailView(pr, now, opts, 0))
	return err
}

// DetailView is everything known about one PR: reasons, reviewers, code owners, runs,
// threads and checks; `show` prints it, a TUI detail pane shows it. Lines wider than
// width (0 for no limit) are cut with the ellipsis, their links and styles closed.
func DetailView(pr model.PR, now time.Time, opts Options, width int) string {
	icons, links := opts.Icons, opts.Hyperlinks
	var b strings.Builder
	title := " " + pr.Title
	if pr.Draft {
		title += " [draft]"
	}
	writef(&b, "%s%s\n", styledLink(links, pr.URL, segment{text: fmt.Sprintf("#%d", pr.Number), style: styleHeader}), styleHeader.Render(title))
	// With links on, the number is the link; without, the URL is the only way to it.
	if !links || pr.URL == "" {
		writef(&b, "%s\n", pr.URL)
	}
	meta := []string{"by " + pr.Author, pr.Branch}
	if !pr.LastPush.IsZero() {
		meta = append(meta, "pushed "+model.Ago(now, pr.LastPush))
	}
	writef(&b, "%s\n", styleFaint.Render(strings.Join(meta, " "+icons.Separator+" ")))
	if len(pr.Issues) > 0 {
		var issues []string
		for _, issue := range pr.Issues {
			issues = append(issues, link(links, issue.URL, issue.ID)+" ("+issueLabel(issue)+")")
		}
		writef(&b, "%s\n", strings.Join(issues, " "+icons.Separator+" "))
	}

	writef(&b, "\n%s %s %s\n", NextMarker(pr.Next, icons), styleHeader.Render("Next:"), pr.Next)
	for _, r := range pr.Reasons {
		writef(&b, "  - %s\n", link(links, r.URL, r.Text))
	}

	section(&b, "Reviewers")
	if len(pr.Reviewers) == 0 {
		b.WriteString(styleFaint.Render("  none") + "\n")
	}
	var rows [][]string
	for _, r := range pr.Reviewers {
		name := link(links, github.ProfileURL(r.Login), r.Login)
		if r.Name != "" {
			name += " (" + r.Name + ")"
		}
		if r.Team != "" {
			name = "team " + link(links && opts.Org != "", github.TeamURL(opts.Org, r.Team), r.Team)
		}
		state := string(r.State)
		if !r.At.IsZero() {
			state += " " + model.Ago(now, r.At)
		}
		var flags []string
		if r.Requested {
			flags = append(flags, "requested")
		}
		if r.CodeOwner {
			flags = append(flags, "code owner")
		}
		if r.Final {
			flags = append(flags, "final")
		}
		if r.ReRequest {
			flags = append(flags, "needs a re-request")
		}
		if r.Unavailable {
			flags = append(flags, "unavailable")
		}
		rows = append(rows, []string{"  " + name, state, strings.Join(flags, ", ")})
	}
	writeGrid(&b, rows)

	section(&b, fmt.Sprintf("Code owners: %s", pr.CodeOwners.State))
	if pr.CodeOwners.Check != "" {
		writef(&b, "  check: %s\n", pr.CodeOwners.Check)
	}
	for _, rule := range pr.CodeOwners.Rules {
		mark := styleFailed.Render(icons.OwnersMissing)
		if rule.Approved() {
			mark = stylePassed.Render(icons.OwnersOK)
		}
		writef(&b, "  %s %s\n", mark, strings.Join(rule.Paths, ", "))
		var assignees []string
		for _, a := range rule.Assignees {
			var notes []string
			if a.Final {
				notes = append(notes, "final")
			}
			if a.Unavailable {
				notes = append(notes, "unavailable")
			}
			s := a.Login
			if len(notes) > 0 {
				s += " (" + strings.Join(notes, ", ") + ")"
			}
			assignees = append(assignees, s)
		}
		owners := strings.Join(rule.Teams, ", ")
		if owners == "" {
			var logins []string
			for _, o := range rule.Owners {
				logins = append(logins, o.Login)
			}
			owners = strings.Join(logins, ", ")
		}
		line := markLabels[rule.Mark]
		switch {
		case len(assignees) > 0:
			line += ": " + strings.Join(assignees, ", ")
		case rule.Mark == model.MarkNoReview:
			line = "no reviewer assigned"
		}
		if owners != "" {
			line += " " + icons.Separator + " owners: " + owners
		}
		writef(&b, "      %s\n", styleFaint.Render(ansi.Truncate(line, 100, icons.Ellipsis)))
	}

	section(&b, "Runs")
	if len(pr.Runs) == 0 {
		b.WriteString(styleFaint.Render("  none") + "\n")
	}
	rows = nil
	for _, run := range pr.Runs {
		state := RunSymbol(run, icons) + " " + string(run.State)
		if run.Outdated {
			state += " (outdated)"
		}
		if run.NoResponse {
			state += " (no response)"
		}
		reason := run.Reason
		if run.Error != "" {
			reason = strings.TrimPrefix(reason+", "+run.Error, ", ")
		}
		rows = append(rows, []string{"  " + string(run.Kind), state, model.Ago(now, run.Started), runURL(links, run.BuildURL), reason})
	}
	writeGrid(&b, rows)

	section(&b, fmt.Sprintf("Unresolved threads (%d)", pr.UnresolvedThreads))
	rows = nil
	for _, t := range pr.Threads {
		last := "last: " + t.LastAuthor
		if !t.LastAt.IsZero() {
			last += " " + model.Ago(now, t.LastAt)
		}
		if t.Outdated {
			last += " (outdated)"
		}
		rows = append(rows, []string{"  " + link(links, github.ProfileURL(t.Author), t.Author), link(links, t.URL, t.Path), last})
		rows = append(rows, []string{"", styleFaint.Render(ansi.Truncate(t.FirstLine, 100, icons.Ellipsis)), ""})
	}
	writeGrid(&b, rows)

	if len(pr.Checks) > 0 {
		section(&b, "Checks")
		rows = nil
		for _, c := range pr.Checks {
			rows = append(rows, []string{"  " + c.Name, c.State})
		}
		writeGrid(&b, rows)
	}
	if width <= 0 {
		return b.String()
	}
	lines := strings.Split(b.String(), "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, icons.Ellipsis)
	}
	return strings.Join(lines, "\n")
}

// runURL prints a run's URL in full without links, the only way to get it from plain
// output. With links, a short label stands for it: "build <id>" when the last path
// segment is a numeric build id, else the URL without its scheme and host.
func runURL(links bool, raw string) string {
	if !links || raw == "" {
		return raw
	}
	label := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		path := strings.TrimSuffix(u.EscapedPath(), "/")
		if id := path[strings.LastIndex(path, "/")+1:]; id != "" && strings.Trim(id, "0123456789") == "" {
			label = "build " + id
		} else if uri := u.RequestURI(); uri != "/" {
			label = uri
		}
	}
	return link(true, raw, label)
}

func section(b *strings.Builder, title string) {
	writef(b, "\n%s\n", styleHeader.Render(title))
}

// writef is fmt.Fprintf into a strings.Builder, whose writes never fail.
func writef(b *strings.Builder, format string, args ...any) {
	_, _ = fmt.Fprintf(b, format, args...)
}
