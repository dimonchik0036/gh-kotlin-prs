// Package update tells whether a newer release of the tool is out: the latest release of
// its repository, against the running version and how gh installed it. The menu-bar
// plugin and the TUI show it, and announce each release once (Announce).
package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/semver"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
)

// Repo is the tool's repository, whose latest release is the one to have.
const Repo = "dimonchik0036/gh-kotlin-prs"

// MaxAge is how long the answer about the latest release is cached: two checks a day are
// plenty, and the plugin runs every 30s.
const MaxAge = 12 * time.Hour

// Timeout bounds a check, so a slow answer never holds the menu up.
const Timeout = 5 * time.Second

// Upgrade is the command that upgrades a gh extension install.
const Upgrade = "gh extension upgrade kotlin-prs"

// Install is how the running binary was installed, which says how to upgrade it.
type Install int

const (
	// InstallOther: not by gh (go install, a copy): it upgrades the way it came.
	InstallOther Install = iota
	// InstallGH: `gh extension install`; `gh extension upgrade` gets the latest release.
	InstallGH
	// InstallPinned: `gh extension install --pin`, which `gh extension upgrade` refuses.
	InstallPinned
)

// Info is a newer release than the running one.
type Info struct {
	// Current is the running version, Latest the newer release's tag, URL its page.
	Current, Latest, URL string
	Install              Install
}

const latestQuery = `query LatestRelease($owner: String!, $name: String!) {
  rateLimit { limit cost remaining resetAt }
  repository(owner: $owner, name: $name) { latestRelease { tagName url } }
}`

// Latest is the tag and the page of the repository's latest release: the newest that is
// neither a draft nor a pre-release, the one `gh extension upgrade` installs. "" when it
// has none.
func Latest(ctx context.Context, client github.Client) (tag, url string, err error) {
	owner, name, _ := strings.Cut(Repo, "/")
	var resp struct {
		Repository struct {
			LatestRelease *struct {
				TagName string `json:"tagName"`
				URL     string `json:"url"`
			} `json:"latestRelease"`
		} `json:"repository"`
	}
	if err := client.DoWithContext(ctx, latestQuery, map[string]any{"owner": owner, "name": name}, &resp); err != nil {
		return "", "", err
	}
	if r := resp.Repository.LatestRelease; r != nil {
		return r.TagName, r.URL, nil
	}
	return "", "", nil
}

// Newer reports whether latest is a newer version than current. Both must be semantic
// versions (v1.2.3, v1.2.3-rc.1): a local build ("dev (5efe46a)") has nothing newer.
func Newer(current, latest string) bool {
	return semver.IsValid(current) && semver.IsValid(latest) && semver.Compare(latest, current) > 0
}

// Check is the newer release than current, if there is one; a failed query is none.
func Check(ctx context.Context, client github.Client, current string, install Install) (Info, bool) {
	if !semver.IsValid(current) {
		return Info{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tag, url, err := Latest(ctx, client)
	if err != nil || !Newer(current, tag) {
		return Info{}, false
	}
	return Info{Current: current, Latest: tag, URL: url, Install: install}, true
}

// manifest is the part of gh's manifest.yml, next to a binary extension it installed,
// that tells a pinned install.
type manifest struct {
	Owner    string `yaml:"owner"`
	Name     string `yaml:"name"`
	IsPinned bool   `yaml:"ispinned"`
}

// ReadInstall tells how gh installed the binary at executable, from the manifest.yml gh
// keeps next to it. The file is gh's own and undocumented: without it, or when it doesn't
// read as this extension's, the binary wasn't installed by gh.
func ReadInstall(executable string) Install {
	if executable == "" {
		return InstallOther
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(executable), "manifest.yml"))
	if err != nil {
		return InstallOther
	}
	var m manifest
	if err := yaml.Unmarshal(data, &m); err != nil || m.Owner+"/"+m.Name != Repo {
		return InstallOther
	}
	if m.IsPinned {
		return InstallPinned
	}
	return InstallGH
}

// Event is the notification of the release. A pinned install gets none: it chose its
// version.
func (i Info) Event() (notify.Event, bool) {
	if i.Install == InstallPinned {
		return notify.Event{}, false
	}
	body := "You have " + i.Current
	if i.Install == InstallGH {
		body += ": " + Upgrade
	}
	return notify.Event{Kind: notify.Update, Title: "gh kotlin-prs " + i.Latest + " is out", Body: body, URL: i.URL}, true
}

// announcedPrefix names the marker of the release last announced, an empty file in the
// cache dir ("update-announced-v0.11.0"), shared by the plugin and the TUI so that
// whichever sees a release first tells of it, once. Not a .json: the cache drops those
// not written for 3 days, which would announce the release again.
const announcedPrefix = "update-announced-"

// Announce reports whether the release latest is still to announce, and records it as
// announced: true once per release, for the plugin and the TUI together. Creating the
// release's marker decides it (O_EXCL), so of two processes asking at once only one
// announces; the markers of other releases go. Never without a cache dir, where nothing
// could remember it, or for a tag that isn't a version, which couldn't name the file.
func Announce(dir, latest string) bool {
	if dir == "" || !semver.IsValid(latest) {
		return false
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	marker := filepath.Join(dir, announcedPrefix+latest)
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()
	others, _ := filepath.Glob(filepath.Join(dir, announcedPrefix+"*"))
	for _, other := range others {
		if other != marker {
			_ = os.Remove(other)
		}
	}
	return true
}
