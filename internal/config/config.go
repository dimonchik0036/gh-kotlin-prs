// Package config loads ~/.config/gh-kotlin-prs/config.yml. Every key is
// optional; the defaults describe JetBrains/kotlin.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	Repo      string   `yaml:"repo"`
	Bots      []string `yaml:"bots"`
	GateBot   string   `yaml:"gateBot"`
	OwnersBot string   `yaml:"ownersBot"`
	// CherryPickBot opens the cherry-pick PRs of /cherry-pick; empty: none are looked for.
	CherryPickBot string `yaml:"cherryPickBot"`
	// ReleaseBranches matches the base branches the bot treats as release branches: no
	// coordinator there, the quality gates run on pushes to ReleaseRunPrefixes.
	ReleaseBranches string `yaml:"releaseBranches"`
	// ReleaseRunPrefixes are the branch prefixes whose pushes run a release branch's quality
	// gates; {base} is the release branch.
	ReleaseRunPrefixes []string `yaml:"releaseRunPrefixes"`
	// ReleaseTeam is the code-owner team of the release engineers, who merge a release
	// branch's PRs; empty: nobody is.
	ReleaseTeam string `yaml:"releaseTeam"`
	// Teams restricts the "Team requests" section to these team slugs. Empty means all teams.
	Teams []string `yaml:"teams"`
	// Refresh is how often the TUI refreshes in the background.
	Refresh Duration `yaml:"refresh"`
	// StartupMaxAge: the TUI starts from cached data at most this old, else it waits for GitHub.
	StartupMaxAge    Duration `yaml:"startupMaxAge"`
	RequestedTimeout Duration `yaml:"requestedTimeout"`
	// Icons is the symbol set: "unicode" or "ascii".
	Icons string `yaml:"icons"`
	// IssueProjects are the issue tracker projects whose IDs (KT-123) are recognized.
	IssueProjects []string `yaml:"issueProjects"`
	// IssueURL links an issue; {id} is replaced with the issue ID.
	IssueURL string `yaml:"issueURL"`
	// Hyperlinks is "auto" (when stdout is a terminal), "always" or "never".
	Hyperlinks string `yaml:"hyperlinks"`
	// Keys are the TUI's key bindings by action (Actions).
	Keys Keys `yaml:"keys"`
	// Notify configures the TUI's notifications; unset keys keep their defaults.
	Notify Notify `yaml:"notify"`
}

// Duration is a time.Duration written as "3m" in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", node.Line, err)
	}
	*d = Duration(parsed)
	return nil
}

func Default() Config {
	return Config{
		Repo:               "JetBrains/kotlin",
		Bots:               []string{"KotlinBuild", "kotlin-safemerge", "kodee-bot"},
		GateBot:            "KotlinBuild",
		OwnersBot:          "kotlin-safemerge",
		CherryPickBot:      "KotlinBuild",
		ReleaseBranches:    `(?i)^\d+\.\d+\.\d+(?:-(?:RC|Beta)\d*)?$`,
		ReleaseRunPrefixes: []string{"rrr/{base}/", "rrrn/{base}/"},
		ReleaseTeam:        "kotlin-release",
		Refresh:            Duration(3 * time.Minute),
		StartupMaxAge:      Duration(30 * time.Minute),
		RequestedTimeout:   Duration(10 * time.Minute),
		Icons:              "unicode",
		IssueProjects:      []string{"KT", "KTIJ", "KTI"},
		IssueURL:           "https://youtrack.jetbrains.com/issue/{id}",
		Hyperlinks:         "auto",
		Keys:               DefaultKeys(),
		Notify:             DefaultNotify(),
	}
}

// DefaultPath is $XDG_CONFIG_HOME/gh-kotlin-prs/config.yml, falling back to ~/.config.
func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "gh-kotlin-prs", "config.yml")
}

// Load reads the file at path over the defaults. A missing file is not an error.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	return Parse(data)
}

// Parse decodes YAML over the defaults.
func Parse(data []byte) (Config, error) {
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	if _, _, ok := strings.Cut(cfg.Repo, "/"); !ok {
		return cfg, fmt.Errorf("config: repo must be owner/name, got %q", cfg.Repo)
	}
	if _, err := regexp.Compile(cfg.ReleaseBranches); err != nil {
		return cfg, fmt.Errorf("config: releaseBranches: %w", err)
	}
	for _, p := range cfg.ReleaseRunPrefixes {
		if !strings.Contains(p, "{base}") {
			return cfg, fmt.Errorf("config: releaseRunPrefixes must contain {base}, got %q", p)
		}
	}
	if cfg.Refresh <= 0 {
		return cfg, fmt.Errorf("config: refresh must be positive, got %s", time.Duration(cfg.Refresh))
	}
	if cfg.StartupMaxAge < 0 {
		return cfg, fmt.Errorf("config: startupMaxAge must not be negative, got %s", time.Duration(cfg.StartupMaxAge))
	}
	if cfg.Icons != "unicode" && cfg.Icons != "ascii" {
		return cfg, fmt.Errorf("config: icons must be unicode or ascii, got %q", cfg.Icons)
	}
	for i, p := range cfg.IssueProjects {
		if !projectName.MatchString(p) {
			return cfg, fmt.Errorf("config: issueProjects: %q is not a project key like KT", p)
		}
		cfg.IssueProjects[i] = strings.ToUpper(p)
	}
	if !ValidHyperlinks(cfg.Hyperlinks) {
		return cfg, fmt.Errorf("config: hyperlinks must be auto, always or never, got %q", cfg.Hyperlinks)
	}
	if !strings.Contains(cfg.IssueURL, "{id}") {
		return cfg, fmt.Errorf("config: issueURL must contain {id}, got %q", cfg.IssueURL)
	}
	if err := cfg.Keys.validate(); err != nil {
		return cfg, err
	}
	if err := cfg.Notify.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

var projectName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// matchNothing is the pattern of an empty issueProjects: a class no character is in.
var matchNothing = regexp.MustCompile(`[^\s\S]`)

// ValidHyperlinks reports whether v is a hyperlinks mode.
func ValidHyperlinks(v string) bool { return v == "auto" || v == "always" || v == "never" }

// IssuePattern matches issue IDs of the configured projects, case-insensitively:
// group 1 is the project, group 2 the number.
func (c Config) IssuePattern() *regexp.Regexp {
	if len(c.IssueProjects) == 0 {
		return matchNothing
	}
	return regexp.MustCompile(`(?i)\b(` + c.projects() + `)-(\d+)\b`)
}

// TrailerPattern matches issue trailers in commit messages, one per line: `^KT-123`,
// optionally followed by a resolution (`^KT-123 Fixed`). Group 1 is the ID, group 2
// the resolution.
func (c Config) TrailerPattern() *regexp.Regexp {
	if len(c.IssueProjects) == 0 {
		return matchNothing
	}
	return regexp.MustCompile(`(?im)^\^((?:` + c.projects() + `)-\d+)(?:[ \t]+([A-Za-z]+))?[ \t]*\r?$`)
}

// projects is the alternation of the project keys, longest first so KTIJ-1 isn't read as KTI.
func (c Config) projects() string {
	projects := slices.Clone(c.IssueProjects)
	slices.SortFunc(projects, func(a, b string) int { return len(b) - len(a) })
	for i, p := range projects {
		projects[i] = regexp.QuoteMeta(p)
	}
	return strings.Join(projects, "|")
}

// IssueLink is the URL of an issue.
func (c Config) IssueLink(id string) string {
	return strings.ReplaceAll(c.IssueURL, "{id}", id)
}

// IsReleaseBranch reports whether branch is a release branch (ReleaseBranches); none with
// an empty or invalid pattern.
func (c Config) IsReleaseBranch(branch string) bool {
	if c.ReleaseBranches == "" {
		return false
	}
	re, err := regexp.Compile(c.ReleaseBranches)
	return err == nil && re.MatchString(branch)
}

// ReleaseRunBranch reports whether pushes to branch run the quality gates of the release
// branch base (ReleaseRunPrefixes).
func (c Config) ReleaseRunBranch(branch, base string) bool {
	return slices.ContainsFunc(c.ReleaseRunPrefixes, func(p string) bool {
		return strings.HasPrefix(branch, strings.ReplaceAll(p, "{base}", base))
	})
}

// ReleaseRunPrefixesOf are ReleaseRunPrefixes for the release branch base.
func (c Config) ReleaseRunPrefixesOf(base string) []string {
	out := make([]string, len(c.ReleaseRunPrefixes))
	for i, p := range c.ReleaseRunPrefixes {
		out[i] = strings.ReplaceAll(p, "{base}", base)
	}
	return out
}

func (c Config) Owner() string {
	owner, _, _ := strings.Cut(c.Repo, "/")
	return owner
}

func (c Config) Name() string {
	_, name, _ := strings.Cut(c.Repo, "/")
	return name
}

// IsBot reports whether login belongs to a bot: one of Bots, or any `*[bot]` app account.
// GitHub spells app logins with and without the `[bot]` suffix depending on the field.
func (c Config) IsBot(login string) bool {
	if strings.HasSuffix(login, "[bot]") {
		return true
	}
	for _, bot := range c.Bots {
		if strings.EqualFold(bot, login) {
			return true
		}
	}
	return false
}

// SameLogin compares logins case-insensitively, ignoring the `[bot]` suffix.
func SameLogin(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "[bot]"), strings.TrimSuffix(b, "[bot]"))
}
