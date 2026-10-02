package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/swiftbar"
)

// runSwiftbar prints the menu-bar plugin's menu. It never fails: SwiftBar shows what
// went wrong in the menu, with the last cached data when there is some.
func runSwiftbar(ctx context.Context, e env, global globalOptions, opts listOptions) error {
	if opts.mine && opts.review {
		return usageError{errors.New("--mine and --review are mutually exclusive")}
	}
	if err := checkMaxAge(opts.maxAge); err != nil {
		return err
	}
	// A bad flag fails; a bad config shows in the menu, over what the cache has for the
	// defaults.
	cfg, ropts, cfgErr := prepare(e, global, "json")
	if usage := (usageError{}); errors.As(cfgErr, &usage) {
		return cfgErr
	}
	if cfgErr != nil {
		cfg = config.Default()
		icons, err := (globalOptions{}).iconsFor(cfg)
		if err != nil {
			return err
		}
		ropts.Icons = icons
	}
	now := e.now()
	sections := opts.sections()
	menu := swiftbar.Menu{Sections: sections, Icons: ropts.Icons, Now: now, MaxAge: opts.maxAge,
		Plugin: e.getenv("SWIFTBAR_PLUGIN_PATH"), GH: e.getenv("GH_KOTLIN_PRS_GH")}
	next, account, err := e.newClient()
	if err != nil {
		menu.Err = err
		if cfgErr != nil {
			menu.Err = cfgErr
		}
		_, err := io.WriteString(e.stdout, swiftbar.Render(menu))
		return err
	}
	client := &cache.Client{Next: next, Dir: e.cacheDir, Account: account, MaxAge: opts.maxAge, Now: e.now}
	if global.debug {
		client.Debug = e.stderr
	}
	var data *listing.Data
	if err = cfgErr; err == nil {
		data, err = listing.Fetch(ctx, client, cfg, now, sections)
	}
	switch {
	case err == nil:
		menu.FetchedAt = client.Oldest
		if client.Cached == 0 {
			menu.Rate = data.RateLimit
			notifyChanges(ctx, e, cfg, data.Classify(cfg, now), menu.Plugin, menu.GH)
		}
	default:
		menu.Err = err
		if cached, ok := listing.Cached(ctx, client, cfg, now, sections, 0); ok {
			data, menu.FetchedAt = cached, cached.FetchedAt
		} else {
			data = nil
		}
	}
	if data != nil {
		menu.Viewer = data.Viewer
		menu.PRs, menu.Hidden = listing.Filter(data.Classify(cfg, now), opts.all, opts.waitingOnMe)
	}
	_, err = io.WriteString(e.stdout, swiftbar.Render(menu))
	return err
}

func newSwiftbarCommand(e env, global *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swiftbar",
		Short: "The menu-bar plugin for SwiftBar: install it, or print its script",
		Long: "A SwiftBar plugin shows your PRs in the menu bar: the number waiting on you, a menu like `list`, " +
			"and per PR its reasons, runs, reviewers and the commands you could post, which open the " +
			"interactive view to ask. It runs `list --format swiftbar`.",
		Args: noArgs,
	}
	var dir string
	var timing pluginTiming
	var force bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Write the plugin into SwiftBar's plugin folder",
		Args:  noArgs,
		RunE: func(*cobra.Command, []string) error {
			return installSwiftbar(e, *global, dir, timing, force)
		},
	}
	install.Flags().StringVar(&dir, "dir", "", "the plugin folder (default: SwiftBar's)")
	timing.flags(install)
	install.Flags().BoolVar(&force, "force", false, "replace an installed plugin")
	var scriptTiming pluginTiming
	script := &cobra.Command{
		Use:   "script",
		Short: "Print the plugin script, to install it by hand",
		Args:  noArgs,
		RunE: func(*cobra.Command, []string) error {
			s, _, err := pluginScript(e, *global, scriptTiming)
			if err != nil {
				return err
			}
			_, err = io.WriteString(e.stdout, s)
			return err
		},
	}
	scriptTiming.flags(script)
	cmd.AddCommand(install, script)
	return cmd
}

// Default plugin timing: a run every 30s is cheap while the cache answers, and the cache
// is shared, so a run picks up within 30s what the TUI fetched or a post changed.
const (
	defaultInterval = 30 * time.Second
	defaultMaxAge   = 3 * time.Minute
)

// pluginTiming is how often SwiftBar runs the plugin (its file name) and how old cached
// data a run takes (its --max-age).
type pluginTiming struct {
	interval, maxAge time.Duration
}

func (t *pluginTiming) flags(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&t.interval, "interval", defaultInterval, "how often SwiftBar runs the plugin (its file name)")
	cmd.Flags().DurationVar(&t.maxAge, "max-age", defaultMaxAge, "how old cached data a run answers with; live fetches are this far apart")
}

// check refuses a max-age below the interval: every run would fetch.
func (t *pluginTiming) check() error {
	if t.maxAge <= 0 || t.maxAge.Truncate(time.Second) != t.maxAge {
		return usageError{fmt.Errorf("--max-age %s isn't a positive whole number of seconds", t.maxAge)}
	}
	if t.maxAge < t.interval {
		return usageError{fmt.Errorf("--max-age %s is shorter than --interval %s: every run would fetch", t.maxAge, t.interval)}
	}
	return nil
}

// pluginScript is the plugin script and its file name for the timing.
func pluginScript(e env, global globalOptions, timing pluginTiming) (script, name string, err error) {
	name, err = swiftbar.FileName(timing.interval)
	if err != nil {
		return "", "", usageError{err}
	}
	if err := timing.check(); err != nil {
		return "", "", err
	}
	gh, err := e.lookGH()
	if err != nil {
		return "", "", fmt.Errorf("gh isn't on the PATH, and the plugin needs it: %w", err)
	}
	if gh, err = filepath.Abs(gh); err != nil {
		return "", "", err
	}
	cfgPath, from := global.config, "--config"
	if cfgPath == "" {
		cfgPath, from = e.getenv(configEnv), "$"+configEnv
	}
	if cfgPath != "" {
		if cfgPath, err = filepath.Abs(cfgPath); err != nil {
			return "", "", err
		}
		if err := checkConfigFile(cfgPath, from); err != nil {
			return "", "", err
		}
	}
	return swiftbar.Script(swiftbar.ScriptOptions{GH: gh, Config: cfgPath, MaxAge: timing.maxAge}), name, nil
}

// installSwiftbar writes the plugin into the folder, replacing an installed one only
// with force.
func installSwiftbar(e env, global globalOptions, dir string, timing pluginTiming, force bool) error {
	script, name, err := pluginScript(e, global, timing)
	if err != nil {
		return err
	}
	if dir == "" {
		if dir = e.swiftbarDir(); dir == "" {
			return errors.New("SwiftBar has no plugin folder yet: open SwiftBar and choose one, or pass --dir")
		}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("the plugin folder %s doesn't exist", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var installed []string
	for _, entry := range entries {
		if swiftbar.IsPlugin(entry.Name()) {
			installed = append(installed, entry.Name())
		}
	}
	if len(installed) > 0 && !force {
		return fmt.Errorf("%s already has the plugin (%s); --force replaces it", dir, strings.Join(installed, ", "))
	}
	for _, old := range installed {
		if old != name {
			if err := os.Remove(filepath.Join(dir, old)); err != nil {
				return err
			}
		}
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return err
	}
	writef(e.stdout, "wrote %s\n", path)
	return nil
}

// notifyChanges notifies of what changed since the plugin's last live run, and makes
// this run the next one's baseline. The first run has nothing to compare with. Events go
// through SwiftBar (or AppleScript), whose click opens the interactive view with gh,
// unless notify.swiftbar is off, and the notify.command hook either way. The baseline
// moves on even with nothing to notify through, so turning SwiftBar's back on replays
// nothing.
func notifyChanges(ctx context.Context, e env, cfg config.Config, prs []model.PR, plugin, gh string) {
	if e.cacheDir == "" {
		return
	}
	prev, ok := swiftbar.LoadBaseline(e.cacheDir)
	if err := swiftbar.SaveBaseline(e.cacheDir, prs); err != nil {
		writef(e.stderr, "notifications: %v\n", err)
	}
	if !ok {
		return
	}
	n := notify.New(cfg.Notify, nil, nil)
	for _, ev := range n.Enabled(notify.Diff(prev, prs)) {
		if cfg.Notify.Swiftbar {
			if err := e.start(swiftbar.NotificationArgv(plugin, gh, ev)); err != nil {
				writef(e.stderr, "notification: %v\n", err)
			}
		}
		if err := n.Run(ctx, ev); err != nil {
			writef(e.stderr, "notify command: %v\n", err)
		}
	}
}

// swiftbarPluginDir is SwiftBar's plugin folder from its settings, "" when it has none.
func swiftbarPluginDir() string {
	out, err := exec.Command("defaults", "read", "com.ameba.SwiftBar", "PluginDirectory").Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, rest)
		}
	}
	return dir
}

// startDetached starts a program and lets it run on its own: env.start, which shows the
// plugin's notifications (open swiftbar://notify, or osascript).
func startDetached(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
