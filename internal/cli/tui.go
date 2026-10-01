package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/atotto/clipboard"
	"github.com/cli/go-gh/v2/pkg/browser"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/tui"
)

// runTUI opens the TUI on the list's sections, starting from the cache when it holds a
// snapshot at most startupMaxAge old. --max-age and --debug don't apply: every refresh
// fetches, and stderr is the screen.
func runTUI(ctx context.Context, e env, global globalOptions, opts listOptions, start tuiStart) error {
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
	post := func(ctx context.Context, number int, text string) (string, error) {
		rest, err := e.newREST()
		if err != nil {
			return "", err
		}
		return github.PostComment(ctx, rest, cfg.Repo, number, text)
	}
	if e.demo {
		post = nil
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
		Now:          e.now,
		Open:         browser.New("", io.Discard, io.Discard).Browse,
		Post:         post,
		Copy:         clipboard.WriteAll,
		Notifier:     notify.New(cfg.Notify, os.Environ(), nil),
		Start:        start.number,
		StartCommand: start.command,
	})
}

// tuiStart is where the interactive view opens: --pr's details, and --post's question.
type tuiStart struct {
	number  int
	post    string
	command *actions.Command
}

// check validates --pr and --post: they open the interactive view, so they need a terminal.
func (s *tuiStart) check(e env) error {
	if s.number < 0 {
		return usageError{fmt.Errorf("not a PR number: %d", s.number)}
	}
	if s.post != "" {
		if s.number == 0 {
			return usageError{errors.New("--post needs --pr")}
		}
		c, ok := actions.Find(s.post)
		if !ok {
			return usageError{fmt.Errorf("unknown command %q for --post, want one of %s", s.post, actions.Names())}
		}
		s.command = &c
	}
	if !e.interactive() {
		return usageError{errors.New("the interactive view needs a terminal")}
	}
	return nil
}
