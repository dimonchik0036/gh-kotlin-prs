package swiftbar

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/demo"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/render"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var allSections = []model.Section{model.SectionMine, model.SectionReview, model.SectionTeams, model.SectionMerged}

const plugin = "/Users/alice_user/Library/Application Support/SwiftBar/Plugins/kotlin-prs.2m.sh"

// fixtureMenu is the fixtures' menu, fetched a minute before their clock.
func fixtureMenu(t *testing.T) Menu {
	t.Helper()
	fixtures, err := demo.Load("../../testdata/raw")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	d, err := listing.Fetch(context.Background(), fixtures, cfg, fixtures.Now(), allSections)
	if err != nil {
		t.Fatal(err)
	}
	prs, hidden := listing.Filter(d.Classify(cfg, fixtures.Now()), false, false)
	return Menu{PRs: prs, Hidden: hidden, Sections: allSections, Viewer: d.Viewer, Icons: render.Unicode,
		FetchedAt: fixtures.Now().Add(-time.Minute), Now: fixtures.Now(), Plugin: plugin}
}

func golden(t *testing.T, name string, got string) {
	t.Helper()
	path := filepath.Join("../../testdata/golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run `go test ./internal/swiftbar -update`)", err)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Errorf("output differs from %s (run `go test ./internal/swiftbar -update` and review the diff):\n%s", path, got)
	}
}

func TestGoldenMenu(t *testing.T) {
	golden(t, "swiftbar.txt", Render(fixtureMenu(t)))
	m := fixtureMenu(t)
	m.Plugin = ""
	golden(t, "swiftbar-no-plugin.txt", Render(m))
}

func TestTitle(t *testing.T) {
	m := fixtureMenu(t)
	first := func(m Menu) string { line, _, _ := strings.Cut(Render(m), "\n"); return line }
	// The fixtures: two PRs of mine wait on me, one of them with a failed dry-run.
	if got := first(m); got != "2 | sfimage=arrow.triangle.pull sfconfig=eyJyZW5kZXJpbmdNb2RlIjoiUGFsZXR0ZSIsImNvbG9ycyI6WyIjRkYzQjMwIl19" {
		t.Errorf("title %q", got)
	}
	// SwiftBar decodes sfconfig as its SFConfig: renderingMode and colors required.
	var sf struct {
		RenderingMode string   `json:"renderingMode"`
		Colors        []string `json:"colors"`
	}
	data, err := base64.StdEncoding.DecodeString(failedIcon)
	if err != nil || json.Unmarshal(data, &sf) != nil || sf.RenderingMode != "Palette" || len(sf.Colors) != 1 || sf.Colors[0] != "#FF3B30" {
		t.Errorf("sfconfig %q: %v", data, err)
	}
	calm := Menu{Icons: render.Unicode, Now: m.Now, FetchedAt: m.Now, PRs: []model.PR{
		{Number: 1, Section: model.SectionMine, Next: model.NextCI, DryRun: model.Run{Kind: model.DryRun, State: model.RunRunning}},
		{Number: 2, Section: model.SectionMine, Next: model.NextReviewers, DryRun: model.Run{Kind: model.DryRun, State: model.RunFailed, Outdated: true}},
		{Number: 3, Section: model.SectionReview, Next: model.NextAuthor, DryRun: model.Run{Kind: model.DryRun, State: model.RunFailed}},
	}}
	if got := first(calm); got != "⋯ | sfimage=arrow.triangle.pull" {
		t.Errorf("a run going, nothing failed, nothing of mine to do: %q", got)
	}
	calm.PRs = nil
	if got := first(calm); got != " | sfimage=arrow.triangle.pull" {
		t.Errorf("nothing: %q", got)
	}
	// After a failed refresh, "!" goes last once the data shown has missed a refresh
	// (older than twice the max age), and keeps the red.
	red := " | sfimage=arrow.triangle.pull sfconfig=" + failedIcon
	m.Err, m.MaxAge = errors.New("HTTP 502"), 90*time.Second
	for _, tt := range []struct {
		age  time.Duration
		want string
	}{{time.Minute, "2" + red}, {3 * time.Minute, "2" + red}, {4 * time.Minute, "2 !" + red}} {
		if m.FetchedAt = m.Now.Add(-tt.age); first(m) != tt.want {
			t.Errorf("data %s old: %q, want %q", tt.age, first(m), tt.want)
		}
	}
	m.MaxAge, m.FetchedAt = 0, m.Now.Add(-time.Second)
	if got := first(m); got != "2 !"+red {
		t.Errorf("no max age, a failed refresh: %q", got)
	}
	calm.PRs = []model.PR{{Number: 1, Section: model.SectionMine, Next: model.NextCI, DryRun: model.Run{Kind: model.DryRun, State: model.RunRunning}}}
	calm.Err, calm.FetchedAt = errors.New("HTTP 502"), m.Now.Add(-time.Hour)
	if got := first(calm); got != "⋯ ! | sfimage=arrow.triangle.pull" {
		t.Errorf("stale, a run going: %q", got)
	}
	calm.PRs = nil
	if got := first(calm); got != "! | sfimage=arrow.triangle.pull" {
		t.Errorf("stale, nothing else: %q", got)
	}
	calm.Err, calm.FetchedAt = errors.New("authentication token not found for host github.com"), time.Time{}
	out := Render(calm)
	if got := first(calm); got != "! | sfimage=arrow.triangle.pull" || !strings.Contains(out, "\nCouldn't fetch your PRs: authentication token not found for host github.com | color=red\n") {
		t.Errorf("a failed first fetch:\n%s", out)
	}
}

// A failed refresh shows the last data, how old it is, and why; a low budget shows too.
func TestHeader(t *testing.T) {
	m := fixtureMenu(t)
	m.Err, m.FetchedAt = errors.New("HTTP 502\nmore"), m.Now.Add(-14*time.Minute)
	m.Rate = github.RateLimit{Limit: 5000, Remaining: 312, ResetAt: time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)}
	out := Render(m)
	for _, want := range []string{
		"\nRefresh failed: HTTP 502 | color=red\nYour move: 2 ∙ data from 14m ago\n",
		"\nAPI budget 312/5000, resets " + m.Rate.ResetAt.In(m.Now.Location()).Format("15:04") + " | color=orange\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	m.Rate.Remaining = 4000
	if strings.Contains(Render(m), "API budget") {
		t.Error("a budget warning with 80% left")
	}
}

// Anything SwiftBar would parse in a title is neutralized.
func TestEscaping(t *testing.T) {
	m := Menu{Icons: render.Unicode, Sections: allSections, Viewer: "me", Plugin: plugin, PRs: []model.PR{{
		Number: 7, Title: "-- a | b\nc :smile:", URL: "https://example.org/pull/7", Author: "me", Section: model.SectionMine, Next: model.NextMe,
		Reasons: []model.Reason{{Text: "---"}},
		DryRun:  model.Run{Kind: model.DryRun, State: model.RunNone}, SafeMerge: model.Run{Kind: model.SafeMerge, State: model.RunNone},
	}}}
	out := Render(m)
	for _, line := range strings.Split(out, "\n") {
		item, _, _ := strings.Cut(line, " | ")
		item = strings.TrimLeft(item, "-")
		if strings.Contains(item, "|") || strings.Contains(item, "\nc") {
			t.Errorf("an unescaped item: %q", line)
		}
	}
	for _, want := range []string{"-- a ¦ b c :smile:", "--\u200b  --- | trim=false", "emojize=false symbolize=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, `bash="`+plugin+`" param1=copy`) || !strings.Contains(out, "bash=exec param1=gh param2=kotlin-prs param3=--pr param4=7 terminal=true") {
		t.Errorf("the clicks:\n%s", out)
	}
}

// A row has list's reviews cell; the title is cut shorter for it, so the row is as wide as
// without it, and the whole title opens the row's submenu. The row has no tooltip.
func TestRowReviews(t *testing.T) {
	title := strings.Repeat("Long title ", 6)
	pr := model.PR{Number: 7, Title: title, URL: "https://example.org/pull/7", Author: "me", Section: model.SectionMine, Next: model.NextReviewers,
		Approvals: 1, Reviewers: []model.Reviewer{{Login: "alice_user"}, {Login: "bob_user"}},
		CodeOwners: model.CodeOwnersStatus{State: model.CodeOwnersMissing},
		DryRun:     model.Run{Kind: model.DryRun, State: model.RunNone}, SafeMerge: model.Run{Kind: model.SafeMerge, State: model.RunNone}}
	out := Render(Menu{Icons: render.Unicode, Sections: allSections, Viewer: "me", PRs: []model.PR{pr}})
	var row string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "#7") {
			row = line
			break
		}
	}
	item, params, _ := strings.Cut(row, " | ")
	if !strings.HasSuffix(item, "DR -  SM -  1/2 ✗") || !strings.Contains(item, "  Long title Long title Long title⋯  DR") {
		t.Errorf("row %q", item)
	}
	// Without the cell: the marker, the number, the empty issue, a 40-column title, DR and SM.
	if w := len([]rune(item)); w != len([]rune("∙  #7    "))+40+len([]rune("  DR -  SM -")) {
		t.Errorf("the row is %d wide, as wide as before the reviews cell: %q", w, item)
	}
	if strings.Contains(params, "tooltip=") {
		t.Errorf("a tooltip on the row: %q", params)
	}
	if want := "\n--" + title + " | color=gray trim=false emojize=false symbolize=false\n"; !strings.Contains(out, row+want) {
		t.Errorf("the submenu doesn't start with the whole title:\n%s", out)
	}
}

// Every item with a submenu has an action, so SwiftBar 2.1.1 keeps it enabled when it
// patches the item: a PR's row opens its details like the submenu's "Details", a
// section's does nothing. An alternate row has its main row's text and opens the PR.
// A Review row has the author after the title, and its submenu who wrote it and when they
// last pushed; Mine has neither.
func TestReviewAuthor(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	none := func(kind model.RunKind) model.Run { return model.Run{Kind: kind, State: model.RunNone} }
	out := Render(Menu{Icons: render.Unicode, Sections: allSections, Viewer: "me", Now: now, PRs: []model.PR{
		{Number: 7, Title: "Their change", Author: "a_rather_long_login", Section: model.SectionReview, Next: model.NextMe,
			LastPush: now.Add(-2 * time.Hour), DryRun: none(model.DryRun), SafeMerge: none(model.SafeMerge)},
		{Number: 8, Title: "My change", Author: "me", Section: model.SectionMine, Next: model.NextMe,
			LastPush: now.Add(-time.Hour), DryRun: none(model.DryRun), SafeMerge: none(model.SafeMerge)},
	}})
	for _, want := range []string{
		"  #7    Their change  a_rather_lo⋯  DR -  SM -",
		"\n--Their change | color=gray trim=false emojize=false symbolize=false\n--by a_rather_long_login · pushed 2h ago | color=gray trim=false\n",
		"\n--My change | color=gray trim=false emojize=false symbolize=false\n--Your move",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "by me") {
		t.Errorf("a Mine submenu says who wrote it:\n%s", out)
	}
}

func TestParentsHaveAnAction(t *testing.T) {
	m := fixtureMenu(t)
	pr := model.PR{Number: 7, Title: "Resolve class/file annotations", URL: "https://example.org/pull/7", Author: m.Viewer, Section: model.SectionTeams,
		Next: model.NextReviewers, DryRun: model.Run{Kind: model.DryRun, State: model.RunNone}, SafeMerge: model.Run{Kind: model.SafeMerge, State: model.RunNone}}
	m.PRs = append(m.PRs, pr)
	for _, plugin := range []string{m.Plugin, ""} {
		m.Plugin = plugin
		lines := strings.Split(strings.TrimSuffix(Render(m), "\n"), "\n")
		// parse is SwiftBar's MenuItemNode.parseLine: "--" per level, "---" a separator.
		parse := func(line string) (level int, separator bool, item, params string) {
			for strings.HasPrefix(line, "--") && line != "---" {
				line, level = line[2:], level+1
			}
			item, params, _ = strings.Cut(line, " | ")
			return level, line == "---", item, params
		}
		has := func(params, param string) bool { return strings.Contains(" "+params+" ", " "+param+" ") }
		rows, sections := 0, 0
		lastAt := map[int]string{} // the last main item at each level
		for i, line := range lines {
			level, separator, item, params := parse(line)
			if separator {
				continue
			}
			if i+1 < len(lines) {
				if next, _, _, _ := parse(lines[i+1]); next > level {
					switch {
					case strings.HasPrefix(params, "font=Menlo-Bold"):
						sections++
						if !has(params, noAction) {
							t.Errorf("a section without its no-op: %q", line)
						}
					case plugin == "":
						rows++
						if !has(params, noAction) {
							t.Errorf("a PR row outside SwiftBar without its no-op: %q", line)
						}
					default:
						rows++
						// The submenu's second item is "Details in the interactive view".
						_, _, details, detailsParams := parse(lines[i+2])
						if details != "Details in the interactive view" || !strings.HasSuffix(params, " "+detailsParams) ||
							!has(params, "param3=--pr") || !has(params, "terminal=true") {
							t.Errorf("a PR row that doesn't open its details: %q, then %q", line, lines[i+2])
						}
					}
				}
			}
			if has(params, "alternate=true") {
				if item != lastAt[level] || !strings.Contains(params, "href=https://") {
					t.Errorf("alternate %q, main %q", line, lastAt[level])
				}
			} else {
				lastAt[level] = item
			}
		}
		if rows < 5 || sections != 2 {
			t.Errorf("plugin %q: only %d rows and %d sections checked", plugin, rows, sections)
		}
	}
}

// A PR of mine with someone to re-request gets an item that runs `run N request-review`
// in the terminal; one with an unassigned code-owner rule an item that opens the picker.
func TestReviewItems(t *testing.T) {
	m := fixtureMenu(t)
	pr := model.PR{Number: 7, Title: "Example change", URL: "https://example.org/pull/7", Author: m.Viewer, Section: model.SectionMine,
		Next: model.NextMe, LastPush: time.Unix(2000, 0),
		CodeOwners: model.CodeOwnersStatus{State: model.CodeOwnersMissing, Rules: []model.CodeOwnerRule{
			{Paths: []string{"/analysis/"}, Mark: model.MarkReRequest, Assignees: []model.Assignee{{Login: "bob_user"}},
				Owners: []model.Owner{{Login: "bob_user"}, {Login: "carol_user"}}},
			{Paths: []string{"/compiler/fir/"}, Mark: model.MarkNoReview, Owners: []model.Owner{{Login: "dave_user"}}},
		}},
		Reviewers: []model.Reviewer{{Login: "bob_user", State: model.ReviewerCommented, ReRequest: true}},
	}
	m.PRs = []model.PR{pr}
	out := Render(m)
	for _, want := range []string{
		// No exec: the tab stays open with the outcome.
		"\n--Re-request review from bob_user⋯ | bash=gh param1=kotlin-prs param2=run param3=7 param4=request-review terminal=true " +
			`tooltip="asks y/N in the terminal, then requests it"` + "\n",
		"\n--Assign reviewers⋯ | bash=exec param1=gh param2=kotlin-prs param3=--pr param4=7 param5=--post param6=request-review terminal=true " +
			`tooltip="pick the code owners in the interactive view"` + "\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the menu lacks %q:\n%s", want, out)
		}
	}
	// Nobody to re-request, nothing unassigned, or not mine: no items.
	pr.CodeOwners.Rules = pr.CodeOwners.Rules[:1]
	pr.Reviewers[0].Requested = true
	m.PRs = []model.PR{pr}
	if out := Render(m); strings.Contains(out, "Re-request review") || strings.Contains(out, "Assign reviewers") {
		t.Errorf("items without anything to do:\n%s", out)
	}
	pr = m.PRs[0]
	pr.Author, pr.Reviewers[0].Requested = "erin_user", false
	m.PRs = []model.PR{pr}
	if out := Render(m); strings.Contains(out, "Re-request review") {
		t.Errorf("someone else's PR:\n%s", out)
	}
	m.Plugin = ""
	pr.Author = m.Viewer
	m.PRs = []model.PR{pr}
	if out := Render(m); strings.Contains(out, "Re-request review") {
		t.Errorf("outside SwiftBar:\n%s", out)
	}
}

func TestUpdateItem(t *testing.T) {
	m := fixtureMenu(t)
	m.GH, m.ScriptFormat = "/opt/homebrew/bin/gh", 1
	want := "\nUpdate the plugin script | bash=/opt/homebrew/bin/gh param1=kotlin-prs param2=swiftbar param3=update " +
		`param4=--dir param5="/Users/alice_user/Library/Application Support/SwiftBar/Plugins" terminal=false refresh=true` + "\n"
	if out := Render(m); !strings.HasSuffix(out, want) {
		t.Errorf("format 1: the menu doesn't end with the item:\n%s", out)
	}
	m.Plugin = ""
	if out := Render(m); !strings.HasSuffix(out, "\nUpdate the plugin script | bash=/opt/homebrew/bin/gh param1=kotlin-prs param2=swiftbar param3=update terminal=false refresh=true\n") {
		t.Errorf("no plugin path, so no --dir:\n%s", out)
	}
	for _, format := range []int{0, ScriptFormat, ScriptFormat + 1} {
		m.ScriptFormat = format
		if out := Render(m); strings.Contains(out, "Update the plugin script") {
			t.Errorf("format %d: an update item", format)
		}
	}
}

// "Copy branch" follows "Copy link", through the script's copy verb; a PR without a
// branch name has none.
func TestCopyBranchItem(t *testing.T) {
	m := fixtureMenu(t)
	m.PRs = []model.PR{{Number: 7, Title: "Example change", URL: "https://example.org/pull/7", Branch: "bob_user/KT-1.fix", Author: m.Viewer,
		Section: model.SectionMine, Next: model.NextMe}}
	want := "\n--Copy link | bash=\"" + plugin + "\" param1=copy param2=https://example.org/pull/7 terminal=false\n" +
		"--Copy branch | bash=\"" + plugin + "\" param1=copy param2=bob_user/KT-1.fix terminal=false\n"
	if out := Render(m); !strings.Contains(out, want) {
		t.Errorf("the menu lacks %q:\n%s", want, out)
	}
	m.PRs[0].Branch = ""
	if out := Render(m); strings.Contains(out, "Copy branch") {
		t.Errorf("a branch item without a branch:\n%s", out)
	}
}
