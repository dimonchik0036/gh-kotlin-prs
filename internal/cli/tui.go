package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/atotto/clipboard"
	"github.com/cli/go-gh/v2/pkg/browser"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/tui"
)

// runTUI opens the TUI on the list's sections, starting from the cache when it holds a
// snapshot at most startupMaxAge old. --max-age and --debug don't apply: every refresh
// fetches, and stderr is the screen.
func runTUI(ctx context.Context, e env, global globalOptions, opts listOptions) error {
	if err := checkMaxAge(opts.maxAge); err != nil {
		return err
	}
	if opts.mine && opts.review {
		return usageError{errors.New("--mine and --review are mutually exclusive")}
	}
	cfg, ropts, err := prepare(e, global, "table")
	if err != nil {
		return err
	}
	next, account, err := e.newClient()
	if err != nil {
		return err
	}
	client := &cache.Client{Next: next, Dir: e.cacheDir, Account: account, Now: e.now}
	sections := opts.sections()
	ropts.Sections = sections

	var initial *listing.Data
	if e.cacheDir != "" && cfg.StartupMaxAge > 0 {
		initial, _ = listing.Cached(ctx, client, cfg, e.now(), sections, time.Duration(cfg.StartupMaxAge))
	}
	return e.runTUI(ctx, tui.Options{
		Config:      cfg,
		Render:      ropts,
		All:         opts.all,
		WaitingOnMe: opts.waitingOnMe,
		Initial:     initial,
		Fetch: func(ctx context.Context) (*listing.Data, error) {
			return listing.Fetch(ctx, client, cfg, e.now(), sections)
		},
		FetchPR: func(ctx context.Context, number int) (*github.PRResponse, error) {
			return github.FetchPR(ctx, client, cfg.Owner(), cfg.Name(), number)
		},
		Now:      e.now,
		Open:     browser.New("", io.Discard, io.Discard).Browse,
		Copy:     clipboard.WriteAll,
		Notifier: notify.New(cfg.Notify, os.Environ(), nil),
	})
}
