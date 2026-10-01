package render

import (
	"fmt"
	"strings"
)

// Icons is the symbol set of every output. Unicode symbols are single-width text
// characters only (East Asian Width "neutral", no emoji presentation), so columns
// line up in any terminal; ASCII is the fallback.
type Icons struct {
	Name string

	NextMe, NextCI, NextOther, NextDone string

	RunNone, RunRequested, RunNoResponse, RunAccepted, RunRunning string
	RunPassed, RunFailed, RunRejected, RunCancelled, Outdated     string

	OwnersOK, OwnersMissing, OwnersUnknown string

	Ellipsis, Separator string
}

var Unicode = Icons{
	Name:   "unicode",
	NextMe: "❯", NextCI: "⟳", NextOther: "∙", NextDone: "✓",
	RunNone: "-", RunRequested: "?", RunNoResponse: "!", RunAccepted: "⋯", RunRunning: "⟳",
	RunPassed: "✓", RunFailed: "✗", RunRejected: "⊘", RunCancelled: "⨯", Outdated: "~",
	OwnersOK: "✓", OwnersMissing: "✗", OwnersUnknown: "?",
	Ellipsis: "⋯", Separator: "∙",
}

var ASCII = Icons{
	Name:   "ascii",
	NextMe: "!", NextCI: ">", NextOther: ".", NextDone: "+",
	RunNone: "-", RunRequested: "?", RunNoResponse: "!", RunAccepted: "^", RunRunning: ">",
	RunPassed: "+", RunFailed: "x", RunRejected: "/", RunCancelled: "c", Outdated: "~",
	OwnersOK: "+", OwnersMissing: "x", OwnersUnknown: "?",
	Ellipsis: "...", Separator: "|",
}

// IconSets are the --icons values.
var IconSets = []Icons{Unicode, ASCII}

func ParseIcons(name string) (Icons, error) {
	for _, set := range IconSets {
		if set.Name == name {
			return set, nil
		}
	}
	return Icons{}, fmt.Errorf("unknown icons %q, want unicode or ascii", name)
}

// Legend explains every symbol, for --help and the README.
func (i Icons) Legend() string {
	line := func(title string, pairs ...string) string {
		var parts []string
		for k := 0; k < len(pairs); k += 2 {
			parts = append(parts, pairs[k]+" "+pairs[k+1])
		}
		return fmt.Sprintf("  %-12s %s\n", title, strings.Join(parts, ", "))
	}
	var b strings.Builder
	b.WriteString(line("next move:", i.NextMe, "yours", i.NextCI, "CI", i.NextOther, "reviewers or author", i.NextDone, "done"))
	b.WriteString(line("DR / SM:", i.RunNone, "none", i.RunRequested, "requested", i.RunNoResponse, "no response",
		i.RunAccepted, "accepted", i.RunRunning, "running", i.RunPassed, "passed", i.RunFailed, "failed",
		i.RunRejected, "rejected", i.RunCancelled, "cancelled", i.Outdated, "prefix: older than the last push"))
	b.WriteString(line("approvals:", "A/R", "approved/reviewing", i.OwnersOK, "code owners ok", i.OwnersMissing, "code owners missing", i.OwnersUnknown, "unknown"))
	return b.String()
}
