package cli

import (
	"context"
	"io"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/update"
)

// checkUpdate is the newer release than the running binary, when updateCheck is on and
// this is a release build. The answer is cached for update.MaxAge, so most runs ask
// nobody; without a cache dir (demo mode too) there's no check, else every plugin run
// would make one. debug, when set, gets the query's cost or cache use. The installed
// version is read anew each time: an upgrade may have replaced the binary since this
// process started.
func checkUpdate(ctx context.Context, e env, cfg config.Config, next github.Client, account string, debug io.Writer) (update.Info, bool) {
	if !cfg.UpdateCheck || e.cacheDir == "" {
		return update.Info{}, false
	}
	client := &cache.Client{Next: next, Dir: e.cacheDir, Account: account, MaxAge: update.MaxAge, Now: e.now, Debug: debug}
	exe, _ := e.executable()
	install, installed := update.ReadInstall(exe)
	u, ok := update.Check(ctx, client, e.version, install)
	u.Installed = installed
	return u, ok
}

// announcement is the release's event when it's still to announce: once per release,
// whether the plugin or the TUI sees it first, and never for a pinned install.
func announcement(e env, u update.Info) (notify.Event, bool) {
	ev, ok := u.Event()
	if !ok || !update.Announce(e.cacheDir, u.Latest) {
		return notify.Event{}, false
	}
	return ev, true
}
