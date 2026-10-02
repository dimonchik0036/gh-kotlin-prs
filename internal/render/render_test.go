package render

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/width"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// The clock of the model goldens (classify's fixtureNow).
var now = time.Date(2026, 10, 1, 16, 0, 0, 0, time.UTC)

const goldenDir = "../../testdata/golden"

func loadModel(t *testing.T, name string) model.PR {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(goldenDir, "model", name))
	if err != nil {
		t.Fatal(err)
	}
	var pr model.PR
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatal(err)
	}
	return pr
}

// loadModels returns every open-PR golden plus the merged rows.
func loadModels(t *testing.T) []model.PR {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(goldenDir, "model", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no model goldens: %v", err)
	}
	var prs []model.PR
	for _, f := range files {
		pr := loadModel(t, filepath.Base(f))
		if !pr.MergedAt.IsZero() && pr.Section != model.SectionMerged {
			continue // the show view of a merged PR; its row is merged-N.json
		}
		prs = append(prs, pr)
	}
	return prs
}

// plain strips styles the way a non-terminal stdout does.
func plain(b *bytes.Buffer) *colorprofile.Writer {
	return &colorprofile.Writer{Forward: b, Profile: colorprofile.NoTTY}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	golden := filepath.Join(goldenDir, name)
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run `go test ./internal/render -update`)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s (run `go test ./internal/render -update` and review the diff):\n%s", golden, got)
	}
}

var allSections = []model.Section{model.SectionMine, model.SectionReview, model.SectionTeams, model.SectionMerged}

// visible splits the goldens the way `list` does without --all.
func visible(prs []model.PR) ([]model.PR, map[model.Section]model.Hidden) {
	var out []model.PR
	hidden := map[model.Section]model.Hidden{}
	for _, pr := range prs {
		if pr.Hidden {
			h := hidden[pr.Section]
			if pr.Draft {
				h.Drafts++
			} else {
				h.NotWaiting++
			}
			hidden[pr.Section] = h
		} else {
			out = append(out, pr)
		}
	}
	return out, hidden
}

func TestGoldenTable(t *testing.T) {
	prs, hidden := visible(loadModels(t))
	for _, icons := range IconSets {
		t.Run(icons.Name, func(t *testing.T) {
			var b bytes.Buffer
			if err := Table(plain(&b), prs, Options{Icons: icons, Sections: allSections, Hidden: hidden}); err != nil {
				t.Fatal(err)
			}
			checkGolden(t, "table-"+icons.Name+".txt", b.Bytes())
		})
	}
	t.Run("all", func(t *testing.T) {
		var b bytes.Buffer
		if err := Table(plain(&b), loadModels(t), Options{Icons: Unicode, Sections: allSections}); err != nil {
			t.Fatal(err)
		}
		checkGolden(t, "table-all.txt", b.Bytes())
	})
	t.Run("hyperlinks", func(t *testing.T) {
		var b bytes.Buffer
		if err := Table(NewWriter(&b, nil, true), prs, Options{Icons: Unicode, Hyperlinks: true, Sections: allSections, Hidden: hidden}); err != nil {
			t.Fatal(err)
		}
		checkGolden(t, "table-hyperlinks.txt", b.Bytes())
	})
}

func TestGoldenDetail(t *testing.T) {
	for _, n := range []string{"90005", "90001", "90004", "90007"} {
		for _, icons := range IconSets {
			t.Run(n+"-"+icons.Name, func(t *testing.T) {
				var b bytes.Buffer
				if err := Detail(plain(&b), loadModel(t, "pr-"+n+".json"), now, Options{Icons: icons}); err != nil {
					t.Fatal(err)
				}
				name := "show-" + n + ".txt"
				if icons.Name != Unicode.Name {
					name = "show-" + n + "-" + icons.Name + ".txt"
				}
				checkGolden(t, name, b.Bytes())
			})
		}
	}
	t.Run("hyperlinks", func(t *testing.T) {
		var b bytes.Buffer
		if err := Detail(NewWriter(&b, nil, true), loadModel(t, "pr-90001.json"), now, Options{Icons: Unicode, Hyperlinks: true, Org: "JetBrains"}); err != nil {
			t.Fatal(err)
		}
		checkGolden(t, "show-90001-hyperlinks.txt", b.Bytes())
	})
}

func TestHyperlinks(t *testing.T) {
	pr := loadModel(t, "pr-90005.json")
	var on, off bytes.Buffer
	if err := Table(NewWriter(&on, nil, true), []model.PR{pr}, Options{Icons: Unicode, Hyperlinks: true, Sections: allSections}); err != nil {
		t.Fatal(err)
	}
	if err := Table(NewWriter(&off, nil, false), []model.PR{pr}, Options{Icons: Unicode, Sections: allSections}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		ansi.SetHyperlink(pr.URL) + "#90005" + ansi.ResetHyperlink(),
		ansi.SetHyperlink(pr.Issues[0].URL) + pr.Issues[0].ID + ansi.ResetHyperlink(),
	} {
		if !strings.Contains(on.String(), want) {
			t.Errorf("table lacks %q:\n%q", want, on.String())
		}
	}
	if strings.Contains(off.String(), "\x1b") {
		t.Errorf("links off still writes escapes: %q", off.String())
	}
	// Without a terminal, styles go but links stay.
	if regexp.MustCompile("\x1b\\[[0-9;:]*m").MatchString(on.String()) {
		t.Errorf("SGR left in a non-terminal output: %q", on.String())
	}
	if ansi.Strip(on.String()) != off.String() {
		t.Errorf("links change the layout:\n%s\n%s", ansi.Strip(on.String()), off.String())
	}
}

// Every column starts at the same display offset on every row of a section.
func TestTableColumnsAlign(t *testing.T) {
	prs := loadModels(t)
	for _, opts := range []Options{{Icons: Unicode}, {Icons: ASCII}, {Icons: Unicode, Hyperlinks: true}} {
		icons := opts.Icons
		for _, section := range allSections {
			var rows []model.PR
			for _, pr := range prs {
				if pr.Section == section {
					rows = append(rows, pr)
				}
			}
			if len(rows) < 2 && section != model.SectionMerged {
				continue
			}
			SortRows(rows)
			cells := make([]Row, len(rows))
			for i, pr := range rows {
				cells[i] = NewRow(pr, icons)
			}
			lines := Lines(cells, opts.Hyperlinks)
			var b strings.Builder
			for _, line := range lines {
				b.WriteString(line + "\n")
			}
			var starts [][]int
			for i, line := range lines {
				var cols []int
				pos := 0
				for _, c := range cells[i].Cells {
					cell := c.Render(opts.Hyperlinks)
					if cell == "" {
						cols = append(cols, -1)
						continue
					}
					k := strings.Index(line[pos:], cell)
					if k < 0 {
						t.Fatalf("%s/%s: cell %q not in %q", icons.Name, section, cell, line)
					}
					cols = append(cols, ansi.StringWidth(line[:pos+k]))
					pos += k + len(cell)
				}
				starts = append(starts, cols)
			}
			for col := range starts[0] {
				want := -1
				for i, cols := range starts {
					if cols[col] < 0 {
						continue
					}
					if want < 0 {
						want = cols[col]
					} else if cols[col] != want {
						t.Errorf("%s/%s: column %d starts at %d on row %d, at %d before:\n%s", icons.Name, section, col, cols[col], i, want, b.String())
					}
				}
			}
		}
	}
}

// emoji lists the code points below U+3000 with the Unicode Emoji property (emoji-data.txt):
// terminals with an emoji font may draw them double-width.
var emoji = [][2]rune{
	{0x00A9, 0x00A9}, {0x00AE, 0x00AE}, {0x203C, 0x203C}, {0x2049, 0x2049}, {0x2122, 0x2122}, {0x2139, 0x2139},
	{0x2194, 0x2199}, {0x21A9, 0x21AA}, {0x231A, 0x231B}, {0x2328, 0x2328}, {0x23CF, 0x23CF}, {0x23E9, 0x23F3},
	{0x23F8, 0x23FA}, {0x24C2, 0x24C2}, {0x25AA, 0x25AB}, {0x25B6, 0x25B6}, {0x25C0, 0x25C0}, {0x25FB, 0x25FE},
	{0x2600, 0x2604}, {0x260E, 0x260E}, {0x2611, 0x2611}, {0x2614, 0x2615}, {0x2618, 0x2618}, {0x261D, 0x261D},
	{0x2620, 0x2620}, {0x2622, 0x2623}, {0x2626, 0x2626}, {0x262A, 0x262A}, {0x262E, 0x262F}, {0x2638, 0x263A},
	{0x2640, 0x2640}, {0x2642, 0x2642}, {0x2648, 0x2653}, {0x265F, 0x2660}, {0x2663, 0x2663}, {0x2665, 0x2666},
	{0x2668, 0x2668}, {0x267B, 0x267B}, {0x267E, 0x267F}, {0x2692, 0x2697}, {0x2699, 0x2699}, {0x269B, 0x269C},
	{0x26A0, 0x26A1}, {0x26A7, 0x26A7}, {0x26AA, 0x26AB}, {0x26B0, 0x26B1}, {0x26BD, 0x26BE}, {0x26C4, 0x26C5},
	{0x26C8, 0x26C8}, {0x26CE, 0x26CF}, {0x26D1, 0x26D1}, {0x26D3, 0x26D4}, {0x26E9, 0x26EA}, {0x26F0, 0x26F5},
	{0x26F7, 0x26FA}, {0x26FD, 0x26FD}, {0x2702, 0x2702}, {0x2705, 0x2705}, {0x2708, 0x270D}, {0x270F, 0x270F},
	{0x2712, 0x2712}, {0x2714, 0x2714}, {0x2716, 0x2716}, {0x271D, 0x271D}, {0x2721, 0x2721}, {0x2728, 0x2728},
	{0x2733, 0x2734}, {0x2744, 0x2744}, {0x2747, 0x2747}, {0x274C, 0x274C}, {0x274E, 0x274E}, {0x2753, 0x2755},
	{0x2757, 0x2757}, {0x2763, 0x2764}, {0x2795, 0x2797}, {0x27A1, 0x27A1}, {0x27B0, 0x27B0}, {0x27BF, 0x27BF},
	{0x2934, 0x2935}, {0x2B05, 0x2B07}, {0x2B1B, 0x2B1C}, {0x2B50, 0x2B50}, {0x2B55, 0x2B55},
}

// singleWidth: ASCII, or a neutral-width text symbol without emoji presentation.
func singleWidth(r rune) bool {
	if r < 0x80 {
		return true
	}
	if r >= 0x3000 || width.LookupRune(r).Kind() != width.Neutral || ansi.StringWidth(string(r)) != 1 {
		return false
	}
	for _, e := range emoji {
		if r >= e[0] && r <= e[1] {
			return false
		}
	}
	return true
}

func TestIconsAreSingleWidth(t *testing.T) {
	for _, icons := range IconSets {
		v := reflect.ValueOf(icons)
		for i := range v.NumField() {
			s := v.Field(i).String()
			for _, r := range s {
				if !singleWidth(r) || (icons.Name == "ascii" && r >= 0x80) {
					t.Errorf("%s.%s = %q: %U is not a single-width text symbol", icons.Name, v.Type().Field(i).Name, s, r)
				}
			}
		}
	}
}

// The fixtures hold only ASCII text, so every other character of the output is ours.
func TestOutputIsSingleWidth(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(goldenDir, "*.txt"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		ascii := strings.Contains(filepath.Base(f), "ascii")
		for i, line := range strings.Split(string(data), "\n") {
			for _, r := range line {
				if !singleWidth(r) || (ascii && r >= 0x80) {
					t.Errorf("%s:%d: %U (%c) may render double-width", filepath.Base(f), i+1, r, r)
				}
			}
		}
	}
}

func TestTableSections(t *testing.T) {
	prs := loadModels(t)
	t.Run("empty optional sections are hidden", func(t *testing.T) {
		var b bytes.Buffer
		if err := Table(plain(&b), prs, Options{Icons: Unicode, Sections: []model.Section{model.SectionMine, model.SectionReview, model.SectionTeams}}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "Team requests") {
			t.Error("the empty Team requests section is shown")
		}
		if strings.Contains(b.String(), "Recently merged") {
			t.Error("Recently merged is shown though not requested")
		}
	})
	t.Run("Mine and Review are shown even when empty", func(t *testing.T) {
		var b bytes.Buffer
		if err := Table(plain(&b), nil, Options{Icons: Unicode, Sections: allSections}); err != nil {
			t.Fatal(err)
		}
		want := "Mine (0)\n  nothing here\n\nReview (0)\n  nothing here\n"
		if b.String() != want {
			t.Errorf("got %q, want %q", b.String(), want)
		}
	})
	t.Run("hidden rows are counted", func(t *testing.T) {
		var b bytes.Buffer
		hidden := map[model.Section]model.Hidden{model.SectionReview: {NotWaiting: 7, Drafts: 1}, model.SectionTeams: {Drafts: 1}}
		if err := Table(plain(&b), nil, Options{Icons: Unicode, Sections: allSections, Hidden: hidden}); err != nil {
			t.Fatal(err)
		}
		want := "Mine (0)\n  nothing here\n\nReview (0)\n  +7 reviews not waiting on you, 1 draft (--all)\n\nTeam requests (0)\n  +1 draft (--all)\n"
		if b.String() != want {
			t.Errorf("got %q, want %q", b.String(), want)
		}
	})
	t.Run("my move first", func(t *testing.T) {
		var b bytes.Buffer
		if err := Table(plain(&b), prs, Options{Icons: Unicode, Sections: []model.Section{model.SectionReview}}); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(b.String()), "\n")[1:]
		seenOther := false
		for _, l := range lines {
			mine := strings.HasPrefix(l, Unicode.NextMe)
			if mine && seenOther {
				t.Errorf("a row for me after others:\n%s", b.String())
			}
			seenOther = seenOther || !mine
		}
	})
}

func TestRunSymbol(t *testing.T) {
	tests := []struct {
		run  model.Run
		want string
	}{
		{model.Run{State: model.RunNone}, "-"},
		{model.Run{State: model.RunRequested}, "?"},
		{model.Run{State: model.RunRequested, NoResponse: true}, "!"},
		{model.Run{State: model.RunAccepted}, "⋯"},
		{model.Run{State: model.RunRunning}, "⟳"},
		{model.Run{State: model.RunPassed}, "✓"},
		{model.Run{State: model.RunPassed, Outdated: true}, "~✓"},
		{model.Run{State: model.RunFailed}, "✗"},
		{model.Run{State: model.RunRejected}, "⊘"},
		{model.Run{State: model.RunCancelled}, "⨯"},
	}
	for _, tt := range tests {
		var b bytes.Buffer
		w := plain(&b)
		_, _ = w.WriteString(RunSymbol(tt.run, Unicode))
		if b.String() != tt.want {
			t.Errorf("%+v: %q, want %q", tt.run, b.String(), tt.want)
		}
	}
}

// The README carries the same legend as --help.
func TestREADMELegend(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, icons := range IconSets {
		if !strings.Contains(string(readme), icons.Legend()) {
			t.Errorf("README.md lacks the %s legend:\n%s", icons.Name, icons.Legend())
		}
	}
}

func TestParseIcons(t *testing.T) {
	for _, name := range []string{"unicode", "ascii"} {
		icons, err := ParseIcons(name)
		if err != nil {
			t.Errorf("ParseIcons(%q): %v", name, err)
		} else if icons.Name != name {
			t.Errorf("ParseIcons(%q) = %q", name, icons.Name)
		}
	}
	if _, err := ParseIcons("emoji"); err == nil {
		t.Error("ParseIcons(emoji) succeeded")
	}
}

func TestJSONHasVersion(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, model.Output{Version: model.Version, Viewer: "me", GeneratedAt: now, PRs: loadModels(t)[:1]}); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int               `json:"version"`
		PRs     []json.RawMessage `json:"prs"`
	}
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != model.Version || len(doc.PRs) != 1 {
		t.Errorf("version %d, %d PRs", doc.Version, len(doc.PRs))
	}
}

// With links on, a run URL becomes a short linked label and the header drops the raw PR
// URL, the number being its link; with links off both stay in full.
func TestDetailURLs(t *testing.T) {
	pr := loadModel(t, "pr-90001.json")
	build := pr.Runs[0].BuildURL
	var on, off bytes.Buffer
	if err := Detail(NewWriter(&on, nil, true), pr, now, Options{Icons: Unicode, Hyperlinks: true}); err != nil {
		t.Fatal(err)
	}
	if err := Detail(NewWriter(&off, nil, false), pr, now, Options{Icons: Unicode}); err != nil {
		t.Fatal(err)
	}
	wantOn := ansi.SetHyperlink(build) + "build " + build[strings.LastIndex(build, "/")+1:] + ansi.ResetHyperlink()
	if !strings.Contains(on.String(), wantOn) {
		t.Errorf("links on lack %q:\n%q", wantOn, on.String())
	}
	if text := ansi.Strip(on.String()); strings.Contains(text, build) || strings.Contains(text, pr.URL) {
		t.Errorf("links on still print a raw URL:\n%s", text)
	}
	for _, want := range []string{"\n" + pr.URL + "\n", build} {
		if !strings.Contains(off.String(), want) {
			t.Errorf("links off lack %q:\n%s", want, off.String())
		}
	}
}

func TestRunURL(t *testing.T) {
	for _, c := range []struct{ url, label string }{
		{"https://ci.example.com/build/1076982024", "build 1076982024"},
		{"https://ci.example.com/build/1076982024/", "build 1076982024"},
		{"https://ci.example.com/viewLog.html?buildId=12", "/viewLog.html?buildId=12"},
		{"https://ci.example.com/runs/abc", "/runs/abc"},
		{"https://ci.example.com", "https://ci.example.com"},
		{"https://ci.example.com/", "https://ci.example.com/"},
		{"not a url", "not a url"},
	} {
		if got := ansi.Strip(runURL(true, c.url)); got != c.label {
			t.Errorf("runURL(%q) = %q, want %q", c.url, got, c.label)
		}
		if got := runURL(false, c.url); got != c.url {
			t.Errorf("runURL without links (%q) = %q", c.url, got)
		}
	}
	if got := runURL(true, ""); got != "" {
		t.Errorf("runURL of no URL = %q", got)
	}
}

// show spells out the bot's code-owner marks and the final / unavailable flags in words.
func TestDetailCodeOwnerWords(t *testing.T) {
	pr := model.PR{Number: 7, Next: model.NextMe,
		Reviewers: []model.Reviewer{
			{Login: "alice_user", State: model.ReviewerCommented, CodeOwner: true, ReRequest: true},
			{Login: "bob_user", State: model.ReviewerApproved, CodeOwner: true, Final: true},
			{Login: "carol_user", State: model.ReviewerPending, Requested: true, Unavailable: true},
		},
		CodeOwners: model.CodeOwnersStatus{State: model.CodeOwnersMissing, Rules: []model.CodeOwnerRule{
			{Paths: []string{"/a/"}, Teams: []string{"team-a"}, Mark: model.MarkReRequest,
				Assignees: []model.Assignee{{Login: "alice_user"}, {Login: "dave_user"}}},
			{Paths: []string{"/b/"}, Mark: model.MarkApproved, Owners: []model.Owner{{Login: "bob_user"}, {Login: "erin_user"}},
				Assignees: []model.Assignee{{Login: "bob_user", Final: true}, {Login: "erin_user", Final: true, Unavailable: true}}},
			{Paths: []string{"/c/"}, Teams: []string{"team-c"}, Mark: model.MarkNoReview},
			{Paths: []string{"/d/"}, Teams: []string{"team-d"}, Mark: model.MarkNoReview, Assignees: []model.Assignee{{Login: "carol_user", Unavailable: true}}},
		}},
	}
	for _, icons := range IconSets {
		var b bytes.Buffer
		if err := Detail(plain(&b), pr, now, Options{Icons: icons}); err != nil {
			t.Fatal(err)
		}
		sep := " " + icons.Separator + " "
		for _, want := range []string{
			"  " + icons.OwnersMissing + " /a/\n      commented, review not re-requested: alice_user, dave_user" + sep + "owners: team-a\n",
			"  " + icons.OwnersOK + " /b/\n      approved: bob_user (final), erin_user (final, unavailable)" + sep + "owners: bob_user, erin_user\n",
			"      no reviewer assigned" + sep + "owners: team-c\n",
			"      no review: carol_user (unavailable)" + sep + "owners: team-d\n",
			"code owner, needs a re-request\n",
			"code owner, final\n",
			"requested, unavailable\n",
		} {
			if !strings.Contains(b.String(), want) {
				t.Errorf("%s: show lacks %q:\n%s", icons.Name, want, b.String())
			}
		}
	}
}

// Team links come from the configured organization.
func TestDetailTeamLinks(t *testing.T) {
	pr := model.PR{Number: 7, URL: "https://github.com/ExampleOrg/project/pull/7", Next: model.NextReviewers,
		Reviewers: []model.Reviewer{{Team: "core", State: model.ReviewerPending, Requested: true}, {Login: "alice_user", State: model.ReviewerPending}}}
	var b bytes.Buffer
	if err := Detail(NewWriter(&b, nil, true), pr, now, Options{Icons: Unicode, Hyperlinks: true, Org: "ExampleOrg"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		ansi.SetHyperlink("https://github.com/orgs/ExampleOrg/teams/core") + "core" + ansi.ResetHyperlink(),
		ansi.SetHyperlink("https://github.com/alice_user") + "alice_user" + ansi.ResetHyperlink(),
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("detail lacks %q:\n%q", want, b.String())
		}
	}
	b.Reset()
	if err := Detail(NewWriter(&b, nil, true), pr, now, Options{Icons: Unicode, Hyperlinks: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "/teams/") {
		t.Errorf("a team link without an organization:\n%q", b.String())
	}
}

// Linked text is underlined and blue (a theme color) on a terminal; the header link
// keeps its bold. Without links nothing is underlined.
func TestHyperlinkStyle(t *testing.T) {
	pr := loadModel(t, "pr-90005.json")
	render := func(links bool, view func(w io.Writer, opts Options) error) string {
		var b bytes.Buffer
		w := &colorprofile.Writer{Forward: &b, Profile: colorprofile.ANSI}
		if err := view(w, Options{Icons: Unicode, Hyperlinks: links, Sections: allSections}); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	table := func(w io.Writer, opts Options) error { return Table(w, []model.PR{pr}, opts) }
	detail := func(w io.Writer, opts Options) error { return Detail(w, pr, now, opts) }

	linked := regexp.MustCompile("\x1b]8;;([^\a]*)\a\x1b\\[([0-9;]*)m([^\x1b]*)\x1b\\[m\x1b]8;;\a")
	params := func(out, text string) []string {
		for _, m := range linked.FindAllStringSubmatch(out, -1) {
			if m[3] == text {
				return strings.Split(m[2], ";")
			}
		}
		t.Fatalf("no styled link around %q in %q", text, out)
		return nil
	}
	for _, text := range []string{"#90005", pr.Issues[0].ID} {
		if p := params(render(true, table), text); !slices.Contains(p, "4") || !slices.Contains(p, "34") {
			t.Errorf("table %s: SGR %v, want underline (4) and blue (34)", text, p)
		}
	}
	if p := params(render(true, detail), "#90005"); !slices.Contains(p, "1") || !slices.Contains(p, "4") || !slices.Contains(p, "34") {
		t.Errorf("show header: SGR %v, want bold, underline and blue", p)
	}
	for name, out := range map[string]string{"table": render(false, table), "show": render(false, detail)} {
		if strings.Contains(out, "\x1b]8;") || regexp.MustCompile("\x1b\\[[0-9;]*\\b4\\b[0-9;]*m").MatchString(out) {
			t.Errorf("%s without links has link styling: %q", name, out)
		}
	}
}

// NO_COLOR (the Ascii profile) drops the link color and keeps the underline.
func TestHyperlinkStyleWithoutColor(t *testing.T) {
	var b bytes.Buffer
	w := &colorprofile.Writer{Forward: &b, Profile: colorprofile.ASCII}
	if _, err := io.WriteString(w, link(true, "https://example.org", "#7")); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "\x1b]8;;https://example.org\a\x1b[4m#7\x1b[m\x1b]8;;\a"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Run cells link their build and keep the symbol's color; a reason of mine keeps its
// bold yellow and gains the underline.
func TestRunCellAndReasonLinks(t *testing.T) {
	pr := loadModel(t, "pr-90006.json") // a failed dry-run, my move
	var b bytes.Buffer
	w := &colorprofile.Writer{Forward: &b, Profile: colorprofile.ANSI}
	if err := Table(w, []model.PR{pr}, Options{Icons: Unicode, Hyperlinks: true, Sections: allSections}); err != nil {
		t.Fatal(err)
	}
	build := pr.DryRun.BuildURL
	for _, want := range []string{
		ansi.SetHyperlink(build) + "\x1b[4;34mDR\x1b[m \x1b[31m" + Unicode.RunFailed + "\x1b[m" + ansi.ResetHyperlink(),
		ansi.SetHyperlink(pr.PrimaryReason().URL) + "\x1b[4;1;33m" + pr.Primary() + "\x1b[m" + ansi.ResetHyperlink(),
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("table lacks %q:\n%q", want, b.String())
		}
	}
	if pr.PrimaryReason().URL != build {
		t.Errorf("the failed dry-run reason links %q, want the build", pr.PrimaryReason().URL)
	}
}

// A status symbol inside a link keeps exactly its own style: an outdated one is faint,
// with no underline and no link color, and neither is the blank before it.
func TestLinkedSymbolHasNoUnderline(t *testing.T) {
	run := model.Run{State: model.RunFailed, Outdated: true, BuildURL: "https://example.org/build/1"}
	var b bytes.Buffer
	w := &colorprofile.Writer{Forward: &b, Profile: colorprofile.ANSI}
	if _, err := io.WriteString(w, runCell(ColumnDryRun, "DR", run, Unicode).Render(true)); err != nil {
		t.Fatal(err)
	}
	want := ansi.SetHyperlink(run.BuildURL) + "\x1b[4;34mDR\x1b[m \x1b[2m~" + Unicode.RunFailed + "\x1b[m" + ansi.ResetHyperlink()
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
	if got := ansi.StringWidth(b.String()); got != ansi.StringWidth("DR ~"+Unicode.RunFailed) {
		t.Errorf("width %d", got)
	}
}

// The list's blocks carry what the table prints: titles, sorted rows, notes.
func TestBlocks(t *testing.T) {
	prs, hidden := visible(loadModels(t))
	blocks := Blocks(prs, Options{Icons: Unicode, Sections: allSections, Hidden: hidden})
	var titles []string
	for _, b := range blocks {
		titles = append(titles, b.Title)
		for i, row := range b.Rows {
			if row.PR.Section != b.Section {
				t.Errorf("%s: row #%d from %s", b.Title, row.PR.Number, row.PR.Section)
			}
			if i > 0 && row.PR.Next == model.NextMe && b.Rows[i-1].PR.Next != model.NextMe {
				t.Errorf("%s: #%d of mine after others", b.Title, row.PR.Number)
			}
		}
	}
	if got, want := titles, []string{"Mine (3)", "Review (0)", "Recently merged (24h) (2)"}; !slices.Equal(got, want) {
		t.Errorf("titles %q, want %q", got, want)
	}
	review := blocks[1]
	if review.Empty || review.Hidden != "+3 reviews not waiting on you, 1 draft (--all)" {
		t.Errorf("Review: empty %v, hidden %q", review.Empty, review.Hidden)
	}
	empty := Blocks(nil, Options{Icons: Unicode, Sections: allSections})
	if len(empty) != 2 || !empty[0].Empty || !empty[1].Empty {
		t.Errorf("without PRs: %+v, want Mine and Review, empty", empty)
	}
}

// A row's cells name their column and link target, the same in every renderer.
func TestRowCells(t *testing.T) {
	pr := loadModel(t, "pr-90006.json")
	row := NewRow(pr, Unicode)
	var columns []Column
	links := map[Column]string{}
	for _, c := range row.Cells {
		columns = append(columns, c.Column)
		links[c.Column] = c.URL()
	}
	want := []Column{ColumnNext, ColumnNumber, ColumnIssue, ColumnTitle, ColumnDryRun, ColumnSafeMerge, ColumnReview, ColumnThreads, ColumnReason}
	if !slices.Equal(columns, want) {
		t.Errorf("columns %v, want %v", columns, want)
	}
	for column, url := range map[Column]string{
		ColumnNumber: pr.URL, ColumnIssue: pr.Issues[0].URL, ColumnDryRun: pr.DryRun.Link(),
		ColumnSafeMerge: pr.SafeMerge.Link(), ColumnReason: pr.PrimaryReason().URL, ColumnTitle: "",
	} {
		if links[column] != url {
			t.Errorf("%s links %q, want %q", column, links[column], url)
		}
	}
	if got := row.Cells[2].Text(); got != pr.Issues[0].ID+" +1" {
		t.Errorf("issue cell %q", got)
	}
	merged := NewRow(model.PR{Number: 1, Section: model.SectionMerged, Next: model.NextDone}, Unicode)
	columns = nil
	for _, c := range merged.Cells {
		columns = append(columns, c.Column)
	}
	if want := []Column{ColumnNext, ColumnNumber, ColumnIssue, ColumnTitle, ColumnReason}; !slices.Equal(columns, want) {
		t.Errorf("merged columns %v, want %v", columns, want)
	}
}

// Review rows have the author after the title, linked to their profile and cut to 12
// columns; the title keeps its width.
func TestRowAuthor(t *testing.T) {
	pr := model.PR{Number: 1, Title: strings.Repeat("x", 60), Author: "a_rather_long_login", Section: model.SectionReview, Next: model.NextMe}
	row := NewRow(pr, Unicode)
	var columns []Column
	for _, c := range row.Cells {
		columns = append(columns, c.Column)
	}
	want := []Column{ColumnNext, ColumnNumber, ColumnIssue, ColumnTitle, ColumnAuthor, ColumnDryRun, ColumnSafeMerge, ColumnReview, ColumnThreads, ColumnReason}
	if !slices.Equal(columns, want) {
		t.Fatalf("columns %v, want %v", columns, want)
	}
	if author := row.Cells[4]; author.Render(false) != "a_rather_lo⋯" || author.URL() != "https://github.com/a_rather_long_login" {
		t.Errorf("author %q, %q", author.Render(false), author.URL())
	}
	if got := ansi.StringWidth(row.Cells[3].Render(false)); got != titleWidth {
		t.Errorf("title %d wide, want %d", got, titleWidth)
	}
}

// Max cuts a cell like ansi.Truncate, across its segments, keeping links and styles.
func TestCellMax(t *testing.T) {
	c := textCell(ColumnReason, "https://example.org", segment{text: "dry-run "}, segment{text: "failed 2h ago", style: styleFailed})
	c.ellipsis = Unicode.Ellipsis
	for _, n := range []int{0, 30, 21, 12, 8, 5, 1} {
		c.Max = n
		got := c.Render(true)
		want := ansi.Truncate(c.Text(), n, Unicode.Ellipsis)
		if n == 0 {
			want = c.Text()
		}
		if ansi.Strip(got) != want {
			t.Errorf("Max %d: %q, want %q", n, ansi.Strip(got), want)
		}
		if strings.Count(got, "\x1b]8;;https") != 1 || !strings.HasSuffix(got, ansi.ResetHyperlink()) {
			t.Errorf("Max %d: the link is broken: %q", n, got)
		}
	}
	c.Max = 12
	if got, want := c.Render(false), "dry-run "+styleFailed.Render("fai"+Unicode.Ellipsis); got != want {
		t.Errorf("the cut segment keeps its style: %q, want %q", got, want)
	}
}

// The detail view at a width cuts long lines and closes their links; at 0 it's `show`.
func TestDetailViewWidth(t *testing.T) {
	pr := loadModel(t, "pr-90001.json")
	opts := Options{Icons: Unicode, Hyperlinks: true, Org: "JetBrains"}
	var b bytes.Buffer
	if err := Detail(&b, pr, now, opts); err != nil {
		t.Fatal(err)
	}
	if got := DetailView(pr, now, opts, 0); got != b.String() {
		t.Errorf("DetailView at width 0 differs from Detail")
	}
	for _, limit := range []int{40, 72} {
		view := DetailView(pr, now, opts, limit)
		cut := false
		for _, line := range strings.Split(view, "\n") {
			if w := ansi.StringWidth(line); w > limit {
				t.Errorf("limit %d: a line of %d: %q", limit, w, ansi.Strip(line))
			}
			cut = cut || strings.HasSuffix(ansi.Strip(line), Unicode.Ellipsis)
			if strings.Count(line, "\x1b]8;;http") != strings.Count(line, "\x1b]8;;\a") {
				t.Errorf("limit %d: an unclosed link in %q", limit, line)
			}
		}
		if !cut {
			t.Errorf("limit %d: nothing was cut:\n%s", limit, ansi.Strip(view))
		}
		if strings.Count(view, "\n") != strings.Count(b.String(), "\n") {
			t.Errorf("limit %d changes the line count", limit)
		}
	}
}
