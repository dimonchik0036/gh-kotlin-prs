package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/tui"
)

var now = time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)

// fakeClient answers the search query with canned numbers and the details query from testdata/raw.
type fakeClient struct {
	t       *testing.T
	queries []string
	// teamRequest adds a request to this team to every fetched PR not authored by the viewer.
	teamRequest string
}

const searchResponse = `{
  "viewer": {"login": "dimonchik0036"},
  "rateLimit": {"cost": 1, "remaining": 4999},
  "mine": {"nodes": [{"number": 90005}, {"number": 90006}, {"number": 90007}]},
  "personal": {"nodes": [{"number": 90001}]},
  "reviewed": {"nodes": [{"number": 90009}, {"number": 90003}, {"number": 90002}]},
  "requested": {"nodes": [{"number": 90001}, {"number": 90008}]},
  "merged": {"nodes": [{"number": 90004, "title": "KT-990005: Example change", "url": "https://github.com/JetBrains/kotlin/pull/90004",
    "isDraft": false, "state": "MERGED", "createdAt": "2026-09-29T23:09:41Z", "updatedAt": "2026-09-30T18:09:28Z", "mergedAt": "2026-09-30T18:09:25Z",
    "headRefName": "topic/KT-990005-example", "author": {"__typename": "User", "login": "dimonchik0036"}}]}
}`

var alias = regexp.MustCompile(`pr(\d+): pullRequest\(number: (\d+)\)`)

func (f *fakeClient) DoWithContext(_ context.Context, query string, vars map[string]any, resp any) error {
	f.queries = append(f.queries, query)
	if strings.Contains(query, "mine: search") {
		if !strings.Contains(vars["merged"].(string), "merged:>=2026-09-30T00:00:00Z") {
			f.t.Errorf("merged query %q doesn't start at the UTC day 24h ago", vars["merged"])
		}
		return json.Unmarshal([]byte(searchResponse), resp)
	}
	if n, ok := vars["number"].(int); ok {
		data, err := os.ReadFile(filepath.Join("../../testdata/raw", fmt.Sprintf("pr-%d.json", n)))
		if err != nil {
			return err
		}
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
		}
		return json.Unmarshal(envelope.Data, resp)
	}
	repo := map[string]json.RawMessage{}
	for _, m := range alias.FindAllStringSubmatch(query, -1) {
		data, err := os.ReadFile(filepath.Join("../../testdata/raw", "pr-"+m[1]+".json"))
		if err != nil {
			return err
		}
		var envelope struct {
			Data struct {
				Repository struct {
					PullRequest map[string]any `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
		}
		pr := envelope.Data.Repository.PullRequest
		if author := pr["author"].(map[string]any)["login"]; f.teamRequest != "" && author != "dimonchik0036" {
			pr["reviewRequests"] = map[string]any{"nodes": []any{map[string]any{"requestedReviewer": map[string]any{"__typename": "Team", "slug": f.teamRequest}}}}
		}
		repo["pr"+m[1]], _ = json.Marshal(pr)
	}
	body, _ := json.Marshal(map[string]any{"rateLimit": map[string]any{"cost": len(repo)}, "repository": repo})
	return json.Unmarshal(body, resp)
}

func numbers(prs []model.PR) map[model.Section][]int {
	out := map[model.Section][]int{}
	for _, pr := range prs {
		out[pr.Section] = append(out[pr.Section], pr.Number)
	}
	for _, v := range out {
		slices.Sort(v)
	}
	return out
}

func TestCollect(t *testing.T) {
	tests := []struct {
		name   string
		opts   listOptions
		teams  []string
		team   string
		want   map[model.Section][]int
		hidden map[model.Section]model.Hidden
	}{
		{
			name: "default: only reviews waiting on me, no drafts in Review",
			want: map[model.Section][]int{
				model.SectionMine:   {90005, 90006, 90007},
				model.SectionTeams:  {90008},
				model.SectionMerged: {90004},
			},
			hidden: map[model.Section]model.Hidden{model.SectionReview: {NotWaiting: 3, Drafts: 1}},
		},
		{
			name: "--all",
			opts: listOptions{all: true},
			want: map[model.Section][]int{
				model.SectionMine:   {90005, 90006, 90007},
				model.SectionReview: {90001, 90002, 90003, 90009},
				model.SectionTeams:  {90008},
				model.SectionMerged: {90004},
			},
		},
		{
			name: "--mine --no-merged",
			opts: listOptions{mine: true, noMerged: true},
			want: map[model.Section][]int{model.SectionMine: {90005, 90006, 90007}},
		},
		{
			name: "--review --no-teams --all",
			opts: listOptions{review: true, noTeams: true, all: true},
			want: map[model.Section][]int{model.SectionReview: {90001, 90002, 90003, 90009}},
		},
		{
			name: "--waiting-on-me --all",
			opts: listOptions{waitingOnMe: true, all: true},
			want: map[model.Section][]int{
				model.SectionMine:   {90006, 90007},
				model.SectionReview: {90001},
			},
		},
		{
			name:  "teams config keeps only requests to those teams",
			teams: []string{"kotlin-build-infrastructure"},
			team:  "kotlin-analysis-api",
			want: map[model.Section][]int{
				model.SectionMine:   {90005, 90006, 90007},
				model.SectionMerged: {90004},
			},
			hidden: map[model.Section]model.Hidden{model.SectionReview: {NotWaiting: 3, Drafts: 1}},
		},
		{
			name:   "teams config matching the request",
			teams:  []string{"Kotlin-Analysis-API"},
			team:   "kotlin-analysis-api",
			opts:   listOptions{review: true},
			want:   map[model.Section][]int{model.SectionTeams: {90008}},
			hidden: map[model.Section]model.Hidden{model.SectionReview: {NotWaiting: 3, Drafts: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Teams = tt.teams
			client := &fakeClient{t: t, teamRequest: tt.team}
			l, err := collect(context.Background(), client, cfg, now, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if l.viewer != "dimonchik0036" {
				t.Errorf("viewer = %q", l.viewer)
			}
			if got := numbers(l.prs); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("sections:\n got %v\nwant %v", got, tt.want)
			}
			if tt.hidden == nil {
				tt.hidden = map[model.Section]model.Hidden{}
			}
			if fmt.Sprint(l.hidden) != fmt.Sprint(tt.hidden) {
				t.Errorf("hidden = %v, want %v", l.hidden, tt.hidden)
			}
			if len(client.queries) != 2 {
				t.Errorf("%d requests, want a search and one details batch", len(client.queries))
			}
		})
	}
}

func TestCollectFetchesOnlyShownSections(t *testing.T) {
	client := &fakeClient{t: t}
	if _, err := collect(context.Background(), client, config.Default(), now, listOptions{mine: true}); err != nil {
		t.Fatal(err)
	}
	details := client.queries[1]
	if got := len(alias.FindAllString(details, -1)); got != 3 {
		t.Errorf("fetched %d PRs, want the 3 of Mine:\n%s", got, details)
	}
}

func TestSortedForJSON(t *testing.T) {
	prs := []model.PR{
		{Number: 1, Section: model.SectionMerged, Next: model.NextDone},
		{Number: 2, Section: model.SectionReview, Next: model.NextAuthor},
		{Number: 3, Section: model.SectionMine, Next: model.NextReviewers},
		{Number: 4, Section: model.SectionMine, Next: model.NextMe},
	}
	var got []int
	for _, pr := range sortedForJSON(prs, listOptions{}.sections()) {
		got = append(got, pr.Number)
	}
	if want := []int{4, 3, 2, 1}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
}

// testEnv never reaches gh's authentication or the user's config: the client comes
// from client, or fails the test when nil.
func testEnv(t *testing.T, client github.Client, configYAML string) (env, *strings.Builder, *strings.Builder) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if configYAML != "" {
		if err := os.WriteFile(path, []byte(configYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut strings.Builder
	return env{
		version:    "v0.0.0-test",
		stdout:     &out,
		stderr:     &errOut,
		configPath: path,
		configFrom: "test",
		newClient: func() (github.Client, string, error) {
			if client == nil {
				t.Error("a GitHub client was requested")
				return nil, "", errors.New("no client in this test")
			}
			return client, "test-account", nil
		},
		newREST: func() (github.RESTClient, error) {
			t.Error("a REST client was requested: a post")
			return nil, errors.New("no posting in this test")
		},
		stdin:       strings.NewReader(""),
		getenv:      func(string) string { return "" },
		swiftbarDir: func() string { return "" },
		lookGH:      func() (string, error) { return "/opt/homebrew/bin/gh", nil },
		start: func(argv []string) error {
			t.Errorf("started %q", argv)
			return nil
		},
		cacheDir:    t.TempDir(),
		now:         func() time.Time { return now },
		isTerminal:  func() bool { return false },
		interactive: func() bool { return false },
		runTUI: func(context.Context, tui.Options) error {
			t.Error("the TUI was started")
			return nil
		},
	}, &out, &errOut
}

func TestExitCodes(t *testing.T) {
	tests := []struct {
		args   []string
		config string
		want   int
	}{
		{args: []string{"list", "--bogus"}, want: exitUsage},
		{args: []string{"list", "--format", "xml"}, want: exitUsage},
		{args: []string{"show", "90005", "--format", "swiftbar"}, want: exitUsage},
		{args: []string{"list", "--mine", "--review"}, want: exitUsage},
		{args: []string{"list", "--icons", "emoji"}, want: exitUsage},
		{args: []string{"show", "90005", "--icons", "emoji"}, want: exitUsage},
		{args: []string{"list", "--hyperlinks", "sometimes"}, want: exitUsage},
		{args: []string{"list"}, config: "hyperlinks: sometimes\n", want: exitError},
		{args: []string{"show"}, want: exitUsage},
		{args: []string{"show", "abc"}, want: exitUsage},
		{args: []string{"frobnicate"}, want: exitUsage},
		{args: []string{"list"}, config: "icons: emoji\n", want: exitError},
		{args: []string{"list"}, config: "refresh: soon\n", want: exitError},
		{args: []string{"list"}, config: "keys: {copy: b}\n", want: exitError},
		{args: []string{"list"}, config: "keys: {paste: p}\n", want: exitError},
		{args: []string{"list", "--max-age", "-1m"}, want: exitUsage},
		{args: []string{"show", "90005", "--max-age", "soon"}, want: exitUsage},
		{args: []string{"--help"}, want: exitOK},
	}
	for _, tt := range tests {
		e, _, errOut := testEnv(t, nil, tt.config)
		if got := run(context.Background(), tt.args, e); got != tt.want {
			t.Errorf("%q with config %q: exit %d, want %d (stderr %q)", tt.args, tt.config, got, tt.want, errOut.String())
		}
	}
}

func TestVersion(t *testing.T) {
	e, out, _ := testEnv(t, nil, "")
	if got := run(context.Background(), []string{"--version"}, e); got != exitOK || out.String() != "gh-kotlin-prs v0.0.0-test\n" {
		t.Errorf("exit %d, output %q", got, out.String())
	}
}

func TestHyperlinksMode(t *testing.T) {
	tests := []struct {
		flag, config, format string
		terminal, want       bool
	}{
		{format: "table", terminal: true, want: true},
		{format: "table", terminal: false, want: false},
		{flag: "always", format: "table", want: true},
		{flag: "never", format: "table", terminal: true, want: false},
		{config: "always", format: "table", want: true},
		{flag: "never", config: "always", format: "table", want: false},
		{flag: "always", format: "json", terminal: true, want: false},
	}
	for _, tt := range tests {
		e, _, _ := testEnv(t, nil, "")
		e.isTerminal = func() bool { return tt.terminal }
		cfg := config.Default()
		if tt.config != "" {
			cfg.Hyperlinks = tt.config
		}
		got, err := globalOptions{hyperlinks: tt.flag}.hyperlinksFor(e, cfg, tt.format)
		if err != nil || got != tt.want {
			t.Errorf("%+v: %v, %v", tt, got, err)
		}
	}
}

func TestRunListHyperlinks(t *testing.T) {
	e, out, errOut := testEnv(t, &fakeClient{t: t}, "")
	if got := run(context.Background(), []string{"list", "--hyperlinks", "always"}, e); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "\x1b]8;;https://github.com/JetBrains/kotlin/pull/90005\a#90005\x1b]8;;\a") {
		t.Errorf("no PR link in:\n%q", out.String())
	}
}

func TestClientErrorExitsWithError(t *testing.T) {
	e, _, errOut := testEnv(t, nil, "")
	e.newClient = func() (github.Client, string, error) {
		return nil, "", errors.New("authentication token not found for host github.com")
	}
	if got := run(context.Background(), []string{"list"}, e); got != exitError || !strings.Contains(errOut.String(), "authentication token") {
		t.Errorf("exit %d, stderr %q", got, errOut.String())
	}
}

func TestRunListJSON(t *testing.T) {
	e, out, errOut := testEnv(t, &fakeClient{t: t}, "")
	if got := run(context.Background(), []string{"list", "--format", "json"}, e); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	var doc model.Output
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != model.Version || doc.Viewer != "dimonchik0036" || len(doc.PRs) != 5 || !doc.GeneratedAt.Equal(now) {
		t.Errorf("version %d, viewer %q, %d PRs, generated %v", doc.Version, doc.Viewer, len(doc.PRs), doc.GeneratedAt)
	}
}

// --max-age answers from the responses of an earlier run while they're young enough,
// classified with the current clock.
func TestMaxAge(t *testing.T) {
	client := &fakeClient{t: t}
	e, out, errOut := testEnv(t, client, "")
	clock := now
	e.now = func() time.Time { return clock }
	list := func(args ...string) string {
		t.Helper()
		out.Reset()
		errOut.Reset()
		if got := run(context.Background(), append([]string{"list", "--format", "json", "--debug"}, args...), e); got != exitOK {
			t.Fatalf("exit %d, stderr %q", got, errOut.String())
		}
		return out.String()
	}
	list()
	if len(client.queries) != 2 {
		t.Fatalf("%d requests", len(client.queries))
	}
	clock = now.Add(4 * time.Minute)
	cached := list("--max-age", "5m")
	if len(client.queries) != 2 || strings.Count(errOut.String(), "from the cache, fetched 4m0s ago") != 2 {
		t.Errorf("%d requests, debug:\n%s", len(client.queries), errOut.String())
	}
	var doc model.Output
	if err := json.Unmarshal([]byte(cached), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.GeneratedAt.Equal(clock) || len(doc.PRs) != 5 {
		t.Errorf("generated %v, %d PRs", doc.GeneratedAt, len(doc.PRs))
	}
	list("--max-age", "4m")
	if len(client.queries) != 4 || !strings.Contains(errOut.String(), "(cached 4m0s ago, --max-age 4m0s)") {
		t.Errorf("%d requests, debug:\n%s", len(client.queries), errOut.String())
	}

	if got := run(context.Background(), []string{"show", "90005", "--max-age", "5m"}, e); got != exitOK {
		t.Fatalf("show: exit %d, stderr %q", got, errOut.String())
	}
	before := len(client.queries)
	if got := run(context.Background(), []string{"show", "90005", "--max-age", "5m", "--debug"}, e); got != exitOK || len(client.queries) != before {
		t.Errorf("show: exit %d, %d requests, want none", got, len(client.queries)-before)
	}
}

// GH_KOTLIN_PRS_DEMO serves the fixtures through the real classify and render path,
// and never touches the cache.
func TestDemoMode(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv(demoEnv, "../../testdata/raw")
	t.Setenv(configEnv, filepath.Join(t.TempDir(), "none.yml"))
	var out, errOut strings.Builder
	if got := Execute(context.Background(), []string{"list", "--format", "json"}, &out, &errOut, "test"); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	var doc model.Output
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Viewer != "dimonchik0036" || len(doc.PRs) == 0 || doc.GeneratedAt.Minute() != 0 {
		t.Errorf("viewer %q, %d PRs, generated %v", doc.Viewer, len(doc.PRs), doc.GeneratedAt)
	}
	out.Reset()
	if got := Execute(context.Background(), []string{"show", "90006", "--icons", "ascii"}, &out, &errOut, "test"); got != exitOK {
		t.Fatalf("show: exit %d, stderr %q", got, errOut.String())
	}
	for _, want := range []string{"#90006 KT-990004: Example change", "dry-run failed", "Code owners: missing", "Runs"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("show lacks %q:\n%s", want, out.String())
		}
	}
	if got := Execute(context.Background(), []string{"list", "--max-age", "1h"}, &out, &errOut, "test"); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	if files, _ := os.ReadDir(cacheHome); len(files) != 0 {
		t.Errorf("demo mode wrote to the cache: %v", files)
	}
	t.Setenv(demoEnv, t.TempDir())
	if got := Execute(context.Background(), []string{"list"}, &out, &errOut, "test"); got != exitError {
		t.Errorf("an empty demo directory: exit %d", got)
	}
}

// On a terminal the bare command opens the TUI, starting from the cache while it's at
// most startupMaxAge old; pipes, subcommands and --format json get `list`.
func TestBareCommandOpensTUI(t *testing.T) {
	client := &fakeClient{t: t}
	e, out, errOut := testEnv(t, client, "")
	clock := now
	e.now = func() time.Time { return clock }
	e.interactive = func() bool { return true }
	var started []tui.Options
	e.runTUI = func(_ context.Context, opts tui.Options) error {
		started = append(started, opts)
		return nil
	}
	bare := func(args ...string) tui.Options {
		t.Helper()
		if got := run(context.Background(), args, e); got != exitOK {
			t.Fatalf("exit %d, stderr %q", got, errOut.String())
		}
		if len(started) == 0 {
			t.Fatal("no TUI")
		}
		return started[len(started)-1]
	}
	opts := bare()
	if opts.Initial != nil || len(client.queries) != 0 {
		t.Errorf("an empty cache: initial %v, %d requests before the TUI", opts.Initial, len(client.queries))
	}
	data, err := opts.Fetch(context.Background())
	if err != nil || data.Viewer != "dimonchik0036" || len(client.queries) != 2 {
		t.Fatalf("Fetch: %v, %d requests", err, len(client.queries))
	}

	clock = now.Add(20 * time.Minute)
	opts = bare("--mine", "--all")
	if opts.Initial == nil {
		t.Error("no snapshot of Mine from the details of every section")
	}
	if !opts.All || !slices.Equal(opts.Render.Sections, []model.Section{model.SectionMine, model.SectionMerged}) {
		t.Errorf("--mine --all: all %v, sections %v", opts.All, opts.Render.Sections)
	}
	opts = bare()
	if opts.Initial == nil || !opts.Initial.FetchedAt.Equal(now) {
		t.Fatalf("a 20m old cache: initial %+v", opts.Initial)
	}
	clock = now.Add(31 * time.Minute)
	if opts := bare(); opts.Initial != nil {
		t.Error("a 31m old cache was used")
	}
	e2, _, _ := testEnv(t, client, "startupMaxAge: 0s\n")
	e2.cacheDir, e2.interactive, e2.runTUI = e.cacheDir, e.interactive, e.runTUI
	clock = now
	if opts := bare(); opts.Initial == nil {
		t.Fatal("no snapshot")
	}
	if got := run(context.Background(), nil, e2); got != exitOK || started[len(started)-1].Initial != nil {
		t.Errorf("startupMaxAge 0 used the cache")
	}

	n := len(started)
	for _, args := range [][]string{{"list"}, {"--format", "json"}} {
		out.Reset()
		if got := run(context.Background(), args, e); got != exitOK || len(started) != n || out.Len() == 0 {
			t.Errorf("%q: exit %d, TUI %v, output %d bytes", args, got, len(started) != n, out.Len())
		}
	}
	e.interactive = func() bool { return false }
	if got := run(context.Background(), nil, e); got != exitOK || len(started) != n {
		t.Errorf("without a terminal: exit %d, TUI %v", got, len(started) != n)
	}
}
