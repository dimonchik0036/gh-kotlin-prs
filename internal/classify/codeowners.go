package classify

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// Marks of the code-owners table.
const (
	markNoReview         = "❌"
	markReRequest        = "🔄"
	markChangesRequested = "🔴"
	markApproved         = "✅"
	markFinal            = "🔒"
	markUnavailable      = "⏳"
	unassigned           = "UNASSIGNED"
)

// A role after a member's login: "(QA)" or "(PM)".
var memberRole = regexp.MustCompile(`\((QA|PM)\)`)

// ParseCodeOwners parses the kotlin-safemerge code-owners comment: the first table,
// one row per rule with the paths, the owners (teams, nested teams and members) and the approval.
func ParseCodeOwners(body string) ([]model.CodeOwnerRule, bool) {
	if !IsCodeOwnersComment(body) {
		return nil, false
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, false
	}
	table := find(doc, atom.Table)
	if table == nil {
		return nil, true
	}
	var rules []model.CodeOwnerRule
	for tr := range table.Descendants() {
		if tr.DataAtom != atom.Tr {
			continue
		}
		var cells []*html.Node
		for td := range tr.ChildNodes() {
			if td.DataAtom == atom.Td {
				cells = append(cells, td)
			}
		}
		if len(cells) != 3 {
			continue // the header row
		}
		rule := model.CodeOwnerRule{Paths: parsePaths(cells[0])}
		rule.Teams, rule.Owners = parseOwners(cells[1])
		rule.Mark, rule.Assignees = parseApproval(cells[2])
		rules = append(rules, rule)
	}
	return rules, true
}

func parsePaths(td *html.Node) []string {
	var paths []string
	for n := range td.Descendants() {
		if n.DataAtom == atom.Code {
			paths = append(paths, cleanText(n))
		}
	}
	return paths
}

// parseOwners walks the owners cell: individual owners (user links) first, then teams.
// A team is a <details> with its link in the <summary> and its members and nested teams
// in <li>, or just its link when it has neither.
func parseOwners(td *html.Node) (teams []string, owners []model.Owner) {
	seen := map[string]bool{}
	var walk func(n *html.Node, team string, depth int)
	walk = func(n *html.Node, team string, depth int) {
		for c := range n.ChildNodes() {
			switch {
			case c.DataAtom == atom.Details:
				slug := ""
				if summary := find(c, atom.Summary); summary != nil {
					slug = cleanText(summary)
				}
				if depth == 0 && slug != "" {
					teams = append(teams, slug)
				}
				walk(c, slug, depth+1)
			case c.DataAtom == atom.Summary:
				// The team name, handled by its <details>.
			case c.DataAtom == atom.A && isTeamLink(c):
				if depth == 0 {
					teams = append(teams, cleanText(c)) // a team without members
				}
			case c.DataAtom == atom.A && find(c, atom.Code) != nil:
				login := cleanText(c)
				if seen[strings.ToLower(login)] {
					continue
				}
				seen[strings.ToLower(login)] = true
				suffix := trailingText(c)
				owner := model.Owner{Login: login, Team: team, Unavailable: strings.Contains(suffix, markUnavailable)}
				if m := memberRole.FindStringSubmatch(suffix); m != nil {
					owner.Role = m[1]
				}
				owners = append(owners, owner)
			default:
				walk(c, team, depth)
			}
		}
	}
	walk(td, "", 0)
	return teams, owners
}

// parseApproval reads "<mark><br><user link> 🔒, <user link>" or "<mark><br>UNASSIGNED".
func parseApproval(td *html.Node) (model.ApprovalMark, []model.Assignee) {
	var lead strings.Builder
	for c := range td.ChildNodes() {
		if c.DataAtom == atom.Br {
			break
		}
		lead.WriteString(text(c))
	}
	mark := model.MarkUnknown
	switch m := lead.String(); {
	case strings.Contains(m, markApproved):
		mark = model.MarkApproved
	case strings.Contains(m, markChangesRequested):
		mark = model.MarkChangesRequested
	case strings.Contains(m, markReRequest):
		mark = model.MarkReRequest
	case strings.Contains(m, markNoReview):
		mark = model.MarkNoReview
	}
	var assignees []model.Assignee
	for n := range td.Descendants() {
		if n.DataAtom != atom.A || find(n, atom.Code) == nil {
			continue
		}
		login := cleanText(n)
		if login == unassigned {
			continue
		}
		suffix := trailingText(n)
		assignees = append(assignees, model.Assignee{
			Login:       login,
			Final:       strings.Contains(suffix, markFinal),
			Unavailable: strings.Contains(suffix, markUnavailable),
		})
	}
	return mark, assignees
}

// trailingText is the text right after a node up to the next element, e.g. " ⏳ (QA)" or " 🔒, ".
func trailingText(n *html.Node) string {
	var b strings.Builder
	for s := n.NextSibling; s != nil && s.Type == html.TextNode; s = s.NextSibling {
		b.WriteString(s.Data)
	}
	return b.String()
}

// isTeamLink: a link to a team page, of any organization.
func isTeamLink(n *html.Node) bool {
	_, _, ok := github.ParseTeamURL(attr(n, "href"))
	return ok
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func find(n *html.Node, a atom.Atom) *html.Node {
	for d := range n.Descendants() {
		if d.DataAtom == a {
			return d
		}
	}
	return nil
}

func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
		}
	}
	return b.String()
}

// cleanText drops the zero-width spaces the bot puts into paths to allow line breaks.
func cleanText(n *html.Node) string {
	return strings.TrimSpace(strings.ReplaceAll(text(n), "\u200b", ""))
}
