package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Source says where a config value comes from.
type Source string

const (
	SourceDefault Source = "default"
	SourceFile    Source = "file"
	SourceFlag    Source = "flag"
)

// Value is one config key, formatted as YAML.
type Value struct {
	Key, Value string
	Source     Source
}

type field struct {
	key, doc string
	get      func(Config) string
}

// fields lists every key in file order, with the comment `config init` writes for it.
var fields = []field{
	{"repo", "GitHub repository, owner/name.", func(c Config) string { return scalar(c.Repo) }},
	{"bots", "Accounts ignored by the \"someone commented\" rules.", func(c Config) string { return list(c.Bots) }},
	{"gateBot", "Posts the quality-gate comments.", func(c Config) string { return scalar(c.GateBot) }},
	{"ownersBot", "Posts the code-owners table and answers commands.", func(c Config) string { return scalar(c.OwnersBot) }},
	{"teams", "Only show team requests to these team slugs; empty means all.", func(c Config) string { return list(c.Teams) }},
	{"refresh", "Refresh interval of the TUI.", func(c Config) string { return duration(c.Refresh) }},
	{"startupMaxAge", "The TUI starts from cached data at most this old (0: never), then refreshes.", func(c Config) string { return duration(c.StartupMaxAge) }},
	{"requestedTimeout", "A command with no reaction or reply after this long is \"no response\".", func(c Config) string { return duration(c.RequestedTimeout) }},
	{"icons", "Symbols: unicode or ascii.", func(c Config) string { return scalar(c.Icons) }},
	{"issueProjects", "Issue IDs recognized in commit trailers, branch names and titles.", func(c Config) string { return list(c.IssueProjects) }},
	{"issueURL", "Issue link; {id} is the issue ID.", func(c Config) string { return scalar(c.IssueURL) }},
	{"hyperlinks", "Terminal links: auto (on a terminal), always or never.", func(c Config) string { return scalar(c.Hyperlinks) }},
}

// LoadSources is Load plus the keys the file sets. A missing file sets none.
func LoadSources(path string) (Config, map[string]Source, error) {
	cfg, err := Load(path)
	if err != nil {
		return cfg, nil, err
	}
	sources := map[string]Source{}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, sources, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return cfg, nil, fmt.Errorf("config: %w", err)
	}
	if len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
		m := doc.Content[0].Content
		for i := 0; i+1 < len(m); i += 2 {
			sources[m[i].Value] = SourceFile
		}
	}
	return cfg, sources, nil
}

// Values lists every key of c with its source: sources overrides the default.
func (c Config) Values(sources map[string]Source) []Value {
	values := make([]Value, len(fields))
	for i, f := range fields {
		src := sources[f.key]
		if src == "" {
			src = SourceDefault
		}
		values[i] = Value{Key: f.key, Value: f.get(c), Source: src}
	}
	return values
}

// Template is the file `config init` writes: every key with its default, commented out,
// so later default changes still apply until a key is uncommented.
func Template() string {
	var b strings.Builder
	b.WriteString("# gh-kotlin-prs config. Every key is optional; uncomment one to change it.\n")
	d := Default()
	for _, f := range fields {
		b.WriteString("\n# " + f.doc + "\n# " + f.key + ": " + f.get(d) + "\n")
	}
	return b.String()
}

func scalar(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		return s
	}
	return strings.TrimSuffix(string(out), "\n")
}

func list(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = scalar(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// duration writes 3m rather than 3m0s.
func duration(d Duration) string {
	s := time.Duration(d).String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
