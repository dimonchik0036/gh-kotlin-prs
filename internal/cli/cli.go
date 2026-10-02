// Package cli wires the commands: the TUI (the bare command on a terminal), `list`
// (the bare command otherwise), `show` and `config`.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/classify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/demo"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/render"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/tui"
)

// Exit codes (SPEC §6).
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

type usageError struct{ error }

// env is everything a run touches outside the process; tests substitute it.
type env struct {
	version        string
	stdin          io.Reader
	stdout, stderr io.Writer
	// configPath is the config file unless --config is given; configFrom says why.
	configPath, configFrom string
	// newClient is called only once flags and config are valid, so usage errors never
	// depend on gh's authentication. account identifies its credentials for the cache.
	newClient func() (client github.Client, account string, err error)
	// newREST is the client of the tool's only writes, PR commands and review requests;
	// it's called only once they passed their checks.
	newREST func() (github.RESTClient, error)
	// demo serves fixtures (GH_KOTLIN_PRS_DEMO): nothing is ever posted.
	demo bool
	// cacheDir holds the cache (internal/cache); "" turns it off.
	cacheDir string
	now      func() time.Time
	// isTerminal reports whether stdout is a terminal, for --hyperlinks auto.
	isTerminal func() bool
	// interactive reports whether stdin and stdout are terminals: the bare command
	// opens the TUI then.
	interactive func() bool
	// runTUI shows the TUI (tui.Run on the process's terminal).
	runTUI func(ctx context.Context, opts tui.Options) error
	// getenv reads the environment (SwiftBar's variables, the plugin's baked-in paths).
	getenv func(string) string
	// swiftbarDir is SwiftBar's plugin folder, "" when it has none.
	swiftbarDir func() string
	// lookGH finds gh, for the plugin script.
	lookGH func() (string, error)
	// start runs a program without waiting for it: a plugin notification.
	start func(argv []string) error
}

// configEnv overrides the default config path.
const configEnv = "GH_KOTLIN_PRS_CONFIG"

// defaultLocation is where the default config path comes from; only there may the file
// be missing.
const defaultLocation = "default location"

// demoEnv names a directory of fixtures to serve instead of GitHub (see internal/demo).
const demoEnv = "GH_KOTLIN_PRS_DEMO"

// Execute runs the CLI and returns the process exit code. version is what --version prints.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	configPath, configFrom := config.DefaultPath(), defaultLocation
	if p := os.Getenv(configEnv); p != "" {
		configPath, configFrom = p, "$"+configEnv
	}
	e := env{
		version:    version,
		stdin:      os.Stdin,
		stdout:     stdout,
		stderr:     stderr,
		configPath: configPath,
		configFrom: configFrom,
		newClient:  github.DefaultClient,
		newREST:    github.DefaultRESTClient,
		cacheDir:   cache.DefaultDir(),
		now:        time.Now,
		isTerminal: func() bool { return term.IsTerminal(int(os.Stdout.Fd())) },
		interactive: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
		},
		runTUI:      func(ctx context.Context, opts tui.Options) error { return tui.Run(ctx, opts, os.Stdin, stdout) },
		getenv:      os.Getenv,
		swiftbarDir: swiftbarPluginDir,
		lookGH:      func() (string, error) { return exec.LookPath("gh") },
		start:       startDetached,
	}
	if dir := os.Getenv(demoEnv); dir != "" {
		fixtures, err := demo.Load(dir)
		if err != nil {
			writef(stderr, "error: %v\n", err)
			return exitError
		}
		e = withDemo(e, fixtures)
	}
	return run(ctx, args, e)
}

// withDemo serves the fixtures through the client and clock seams, without the cache,
// and never posts.
func withDemo(e env, fixtures *demo.Client) env {
	e.newClient = func() (github.Client, string, error) { return fixtures, "", nil }
	e.newREST = func() (github.RESTClient, error) { return nil, errDemoPosts }
	e.demo = true
	e.cacheDir = ""
	e.now = fixtures.Now
	return e
}

// client is the GitHub client behind the cache: responses younger than maxAge come from
// it, and every fetch updates it. --debug prints each query's cost or cache use.
func (e env) client(global globalOptions, maxAge time.Duration) (*cache.Client, error) {
	next, account, err := e.newClient()
	if err != nil {
		return nil, err
	}
	c := &cache.Client{Next: next, Dir: e.cacheDir, Account: account, MaxAge: maxAge, Now: e.now}
	if global.debug {
		c.Debug = e.stderr
	}
	return c, nil
}

func run(ctx context.Context, args []string, e env) int {
	root := newRoot(e)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return exitOK
	}
	writef(e.stderr, "error: %v\n", err)
	var usage usageError
	if errors.As(err, &usage) || strings.HasPrefix(err.Error(), "unknown command") {
		return exitUsage
	}
	return exitError
}

type globalOptions struct {
	debug bool
	// icons and hyperlinks override their config keys when set.
	icons, hyperlinks string
	// config overrides the config file path.
	config string
}

// configFile is the config path in effect and where it comes from.
func (g globalOptions) configFile(e env) (path, from string) {
	if g.config != "" {
		return g.config, "--config"
	}
	return e.configPath, e.configFrom
}

// checkConfigFile fails for a config file that --config or $GH_KOTLIN_PRS_CONFIG names
// and that doesn't exist: a typo would silently give the defaults. The default location
// may have none.
func checkConfigFile(path, from string) error {
	if from == defaultLocation {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("config: %s (from %s) doesn't exist", path, from)
	}
	return nil
}

// hyperlinksFor resolves --hyperlinks over the config: auto means only on a terminal,
// and JSON never has links.
func (g globalOptions) hyperlinksFor(e env, cfg config.Config, format string) (bool, error) {
	mode := cfg.Hyperlinks
	if g.hyperlinks != "" {
		if !config.ValidHyperlinks(g.hyperlinks) {
			return false, usageError{fmt.Errorf("unknown --hyperlinks %q, want auto, always or never", g.hyperlinks)}
		}
		mode = g.hyperlinks
	}
	switch {
	case format == "json" || mode == "never":
		return false, nil
	case mode == "always":
		return true, nil
	}
	return e.isTerminal(), nil
}

// iconsFor resolves --icons over the config.
func (g globalOptions) iconsFor(cfg config.Config) (render.Icons, error) {
	name := cfg.Icons
	if g.icons != "" {
		name = g.icons
	}
	icons, err := render.ParseIcons(name)
	if err != nil && g.icons != "" {
		return icons, usageError{err}
	}
	return icons, err
}

var legend = "Symbols (--icons unicode, the default):\n" + render.Unicode.Legend() +
	"\nWith --icons ascii:\n" + render.ASCII.Legend()

func newRoot(e env) *cobra.Command {
	var global globalOptions
	var opts listOptions
	var start tuiStart
	root := &cobra.Command{
		Use:   "kotlin-prs",
		Short: "Your open PRs (in JetBrains/kotlin by default): quality gate, reviews, and whose move it is",
		Long: "Shows the open PRs you're involved in, their dry-run / safe-merge status, where the review " +
			"stands and who has the next move. Without a subcommand it opens the interactive view on a " +
			"terminal (press ? there for its keys) and runs `list` otherwise.\n\n" + legend,
		Args:          noArgs,
		Version:       e.version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if start.number != 0 || start.post != "" {
				if err := start.check(e); err != nil {
					return err
				}
				return runTUI(cmd.Context(), e, global, opts, start)
			}
			if e.interactive() && opts.format == "table" {
				return runTUI(cmd.Context(), e, global, opts, start)
			}
			return runList(cmd.Context(), e, global, opts)
		},
	}
	root.SetVersionTemplate("gh-kotlin-prs {{.Version}}\n")
	root.SetOut(e.stdout)
	root.SetErr(e.stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.PersistentFlags().BoolVar(&global.debug, "debug", false, "print the GraphQL query cost and cache use to stderr")
	root.PersistentFlags().StringVar(&global.config, "config", "", "config file (default: $"+configEnv+", else ~/.config/gh-kotlin-prs/config.yml)")
	root.PersistentFlags().StringVar(&global.icons, "icons", "", "symbols: unicode or ascii (default: the `icons` config key, else unicode)")
	root.PersistentFlags().StringVar(&global.hyperlinks, "hyperlinks", "", "terminal links: auto (on a terminal), always or never (default: the `hyperlinks` config key, else auto)")
	addListFlags(root, &opts)
	root.Flags().IntVar(&start.number, "pr", 0, "open the interactive view on this PR's details")
	root.Flags().StringVar(&start.post, "post", "", "with --pr: also ask to post this command on it ("+actions.Names()+
		"); only y posts. "+actions.RequestReview+" opens the review picker on it")

	var listOpts listOptions
	list := &cobra.Command{
		Use:   "list",
		Short: "List your PRs and review requests",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), e, global, listOpts)
		},
	}
	addListFlags(list, &listOpts)

	var showFormat string
	var showMaxAge time.Duration
	show := &cobra.Command{
		Use:   "show <number>",
		Short: "Show one PR in detail: reviewers, code owners, runs, threads and reasons",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError{errors.New("show takes exactly one PR number")}
			}
			if _, err := parseNumber(args[0]); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			n, _ := parseNumber(args[0])
			return runShow(cmd.Context(), e, global, n, showFormat, showMaxAge)
		},
	}
	show.Flags().StringVar(&showFormat, "format", "table", "output format: table or json")
	addMaxAgeFlag(show, &showMaxAge)

	root.AddCommand(list, show, newRunCommand(e, &global), newSwiftbarCommand(e, &global), newConfigCommand(e, &global))
	return root
}

func addListFlags(cmd *cobra.Command, opts *listOptions) {
	f := cmd.Flags()
	f.BoolVar(&opts.mine, "mine", false, "only your PRs (and recently merged ones)")
	f.BoolVar(&opts.review, "review", false, "only PRs you review (and team requests)")
	f.BoolVar(&opts.waitingOnMe, "waiting-on-me", false, "only PRs where the next move is yours")
	f.BoolVar(&opts.all, "all", false, "also show reviews not waiting on you and drafts in Review")
	f.BoolVar(&opts.noTeams, "no-teams", false, "hide the Team requests section")
	f.BoolVar(&opts.noMerged, "no-merged", false, "hide the Recently merged section")
	f.StringVar(&opts.format, "format", "table", "output format: table or json")
	addMaxAgeFlag(cmd, &opts.maxAge)
}

func addMaxAgeFlag(cmd *cobra.Command, maxAge *time.Duration) {
	cmd.Flags().DurationVar(maxAge, "max-age", 0, "use cached data younger than this, e.g. 5m (default: always fetch)")
}

func checkMaxAge(maxAge time.Duration) error {
	if maxAge < 0 {
		return usageError{fmt.Errorf("--max-age %s is negative", maxAge)}
	}
	return nil
}

func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError{fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())}
	}
	return nil
}

func parseNumber(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(s, "#"))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("not a PR number: %q", s)
	}
	return n, nil
}

func checkFormat(format string) error {
	switch format {
	case "table", "json":
		return nil
	case "swiftbar":
		return usageError{errors.New("--format swiftbar is only for list")}
	}
	return usageError{fmt.Errorf("unknown --format %q, want table, json or swiftbar (list)", format)}
}

// prepare validates the config and the global flags. It runs before anything touches
// authentication or the network.
func prepare(e env, global globalOptions, format string) (config.Config, render.Options, error) {
	path, from := global.configFile(e)
	if err := checkConfigFile(path, from); err != nil {
		return config.Default(), render.Options{}, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return cfg, render.Options{}, err
	}
	icons, err := global.iconsFor(cfg)
	if err != nil {
		return cfg, render.Options{}, err
	}
	links, err := global.hyperlinksFor(e, cfg, format)
	return cfg, render.Options{Icons: icons, Hyperlinks: links, Org: cfg.Owner()}, err
}

func runList(ctx context.Context, e env, global globalOptions, opts listOptions) error {
	if opts.format == "swiftbar" {
		return runSwiftbar(ctx, e, global, opts)
	}
	if err := checkFormat(opts.format); err != nil {
		return err
	}
	if opts.mine && opts.review {
		return usageError{errors.New("--mine and --review are mutually exclusive")}
	}
	if err := checkMaxAge(opts.maxAge); err != nil {
		return err
	}
	cfg, ropts, err := prepare(e, global, opts.format)
	if err != nil {
		return err
	}
	client, err := e.client(global, opts.maxAge)
	if err != nil {
		return err
	}
	now := e.now()
	l, err := collect(ctx, client, cfg, now, opts)
	if err != nil {
		return err
	}
	if opts.format == "json" {
		out := model.Output{Version: model.Version, Viewer: l.viewer, GeneratedAt: now.UTC(), PRs: sortedForJSON(l.prs, opts.sections())}
		if len(l.hidden) > 0 {
			out.Hidden = l.hidden
		}
		return render.JSON(e.stdout, out)
	}
	ropts.Sections, ropts.Hidden = opts.sections(), l.hidden
	return render.Table(render.NewWriter(e.stdout, os.Environ(), ropts.Hyperlinks), l.prs, ropts)
}

// sortedForJSON orders the PRs the way the table shows them.
func sortedForJSON(prs []model.PR, sections []model.Section) []model.PR {
	out := make([]model.PR, 0, len(prs)) // non-nil: JSON "prs" is [] when empty
	for _, s := range sections {
		var rows []model.PR
		for _, pr := range prs {
			if pr.Section == s {
				rows = append(rows, pr)
			}
		}
		render.SortRows(rows)
		out = append(out, rows...)
	}
	return out
}

func runShow(ctx context.Context, e env, global globalOptions, number int, format string, maxAge time.Duration) error {
	if err := checkFormat(format); err != nil {
		return err
	}
	if err := checkMaxAge(maxAge); err != nil {
		return err
	}
	cfg, ropts, err := prepare(e, global, format)
	if err != nil {
		return err
	}
	client, err := e.client(global, maxAge)
	if err != nil {
		return err
	}
	resp, err := github.FetchPR(ctx, client, cfg.Owner(), cfg.Name(), number)
	if err != nil {
		return err
	}
	now := e.now()
	c := &classify.Classifier{Config: cfg, Viewer: resp.Viewer.Login, Now: now}
	pr := c.Show(resp.Repository.PullRequest)
	if format == "json" {
		return render.JSON(e.stdout, render.ShowOutput{Version: model.Version, Viewer: resp.Viewer.Login, GeneratedAt: now.UTC(), PR: pr})
	}
	return render.Detail(render.NewWriter(e.stdout, os.Environ(), ropts.Hyperlinks), pr, now, ropts)
}

// writef prints diagnostics (errors, --debug costs): a failed write to stderr isn't worth reporting.
func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
