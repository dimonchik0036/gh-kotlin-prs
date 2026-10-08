package config

import (
	"fmt"
	"os"
	"regexp"
	"slices"
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

// Value is one config key formatted as YAML: a scalar or a flow list in Value, or, for
// a map (keys, notify), its sub-keys in Sub, each with its own source.
type Value struct {
	Key, Value string
	Source     Source
	Sub        []Value
}

type field struct {
	key, doc string
	get      func(Config) string
	// sub lists a map's sub-keys and values, instead of get.
	sub func(Config) []Value
}

// fields lists every key in file order, with the comment `config init` writes for it.
var fields = []field{
	{key: "repo", doc: "GitHub repository, owner/name.", get: func(c Config) string { return scalar(c.Repo) }},
	{key: "bots", doc: "Accounts ignored by the \"someone commented\" rules.", get: func(c Config) string { return list(c.Bots) }},
	{key: "gateBot", doc: "Posts the quality-gate comments.", get: func(c Config) string { return scalar(c.GateBot) }},
	{key: "ownersBot", doc: "Posts the code-owners table and answers commands.", get: func(c Config) string { return scalar(c.OwnersBot) }},
	{key: "releaseBranches", doc: "Base branches with the release workflow: no dry-run or safe-merge, quality gates on pushes, a manual merge.", get: func(c Config) string { return scalar(c.ReleaseBranches) }},
	{key: "releaseRunPrefixes", doc: "Branch prefixes whose pushes run a release branch's quality gates; {base} is the release branch.", get: func(c Config) string { return list(c.ReleaseRunPrefixes) }},
	{key: "releaseTeam", doc: "The code-owner team of the release engineers: its members merge a release branch's PRs.", get: func(c Config) string { return scalar(c.ReleaseTeam) }},
	{key: "teams", doc: "Only show team requests to these team slugs; empty means all.", get: func(c Config) string { return list(c.Teams) }},
	{key: "refresh", doc: "Refresh interval of the TUI.", get: func(c Config) string { return c.Refresh.String() }},
	{key: "startupMaxAge", doc: "The TUI starts from cached data at most this old (0: never), then refreshes.", get: func(c Config) string { return c.StartupMaxAge.String() }},
	{key: "requestedTimeout", doc: "A command with no reaction or reply after this long is \"no response\".", get: func(c Config) string { return c.RequestedTimeout.String() }},
	{key: "icons", doc: "Symbols: unicode or ascii.", get: func(c Config) string { return scalar(c.Icons) }},
	{key: "issueProjects", doc: "Issue IDs recognized in commit trailers, branch names and titles.", get: func(c Config) string { return list(c.IssueProjects) }},
	{key: "issueURL", doc: "Issue link; {id} is the issue ID.", get: func(c Config) string { return scalar(c.IssueURL) }},
	{key: "hyperlinks", doc: "Terminal links: auto (on a terminal), always or never.", get: func(c Config) string { return scalar(c.Hyperlinks) }},
	{key: "keys", doc: "TUI keys by action; a key or a list replaces that action's keys.", sub: func(c Config) []Value {
		values := make([]Value, len(Actions))
		for i, a := range Actions {
			values[i] = Value{Key: a.Name, Value: list(c.Keys[a.Name])}
		}
		return values
	}},
	{key: "notify", doc: "Notifications; unset keys keep their defaults. events, command and timeout: the TUI and the " +
		"menu-bar plugin. terminal (auto, osc9, osc777, osc99 or none) and bell: the TUI. swiftbar: the plugin. " +
		"command: run per event, {title}, {body} and {url} replaced, the event as JSON on stdin.", sub: func(c Config) []Value {
		n := c.Notify
		return []Value{
			{Key: "events", Value: list(n.Events)},
			{Key: "terminal", Value: scalar(n.Terminal)},
			{Key: "bell", Value: fmt.Sprint(n.Bell)},
			{Key: "swiftbar", Value: fmt.Sprint(n.Swiftbar)},
			{Key: "command", Value: list(n.Command)},
			{Key: "timeout", Value: n.Timeout.String()},
		}
	}},
}

// LoadSources is Load plus the keys the file sets, a map's sub-keys as "notify.command".
// A missing file sets none.
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
			key, value := m[i].Value, m[i+1]
			sources[key] = SourceFile
			if value.Kind != yaml.MappingNode {
				continue
			}
			for k := 0; k+1 < len(value.Content); k += 2 {
				sources[key+"."+value.Content[k].Value] = SourceFile
			}
		}
	}
	return cfg, sources, nil
}

// Values lists every key of c with its source: sources overrides the default.
func (c Config) Values(sources map[string]Source) []Value {
	source := func(key string) Source {
		if s := sources[key]; s != "" {
			return s
		}
		return SourceDefault
	}
	values := make([]Value, len(fields))
	for i, f := range fields {
		if f.sub == nil {
			values[i] = Value{Key: f.key, Value: f.get(c), Source: source(f.key)}
			continue
		}
		sub := f.sub(c)
		for k := range sub {
			sub[k].Source = source(f.key + "." + sub[k].Key)
		}
		values[i] = Value{Key: f.key, Sub: sub}
	}
	return values
}

// alignUpTo: source comments line up after the values of a block, except after the
// lines longer than this, which get a single space.
const alignUpTo = 60

// Describe writes values as block YAML that reads back as the same config: a line per
// key, a map's sub-keys indented under it, each line ending in its source. The comments
// line up per block: the top-level keys, and each map.
func Describe(values []Value) string {
	var top []line
	blocks := map[int][]line{} // a map's sub-key lines, after the top-level line they follow
	for _, v := range values {
		if v.Sub == nil {
			top = append(top, line{text: v.Key + ": " + v.Value, comment: string(v.Source)})
			continue
		}
		top = append(top, line{text: v.Key + ":"})
		for _, s := range v.Sub {
			blocks[len(top)-1] = append(blocks[len(top)-1], line{text: "  " + s.Key + ": " + s.Value, comment: string(s.Source)})
		}
	}
	top = aligned(top)
	var b strings.Builder
	for i, l := range top {
		b.WriteString(l.text + "\n")
		for _, s := range aligned(blocks[i]) {
			b.WriteString(s.text + "\n")
		}
	}
	return b.String()
}

type line struct{ text, comment string }

// aligned appends each commented line's comment, lined up after the block's values.
func aligned(lines []line) []line {
	width := 0
	for _, l := range lines {
		if l.comment != "" && len(l.text) <= alignUpTo {
			width = max(width, len(l.text))
		}
	}
	out := make([]line, len(lines))
	for i, l := range lines {
		out[i] = l
		switch {
		case l.comment == "":
		case len(l.text) <= alignUpTo:
			out[i].text = l.text + strings.Repeat(" ", width-len(l.text)) + "  # " + l.comment
		default:
			out[i].text = l.text + " # " + l.comment
		}
	}
	return out
}

// Template is the file `config init` writes: every key with its default, commented out,
// so later default changes still apply until a key is uncommented.
func Template() string {
	var b strings.Builder
	b.WriteString(templateHeader + "\n")
	for _, v := range Default().Values(nil) {
		f := fields[slices.IndexFunc(fields, func(f field) bool { return f.key == v.Key })]
		b.WriteString("\n")
		for _, l := range docLines(f.doc) {
			b.WriteString("# " + l + "\n")
		}
		if v.Sub == nil {
			b.WriteString("# " + v.Key + ": " + v.Value + "\n")
			continue
		}
		b.WriteString("# " + v.Key + ":\n")
		for _, s := range v.Sub {
			b.WriteString("#   " + s.Key + ": " + s.Value + "\n")
		}
	}
	return b.String()
}

const templateHeader = "# gh-kotlin-prs config. Every key is optional; uncomment one to change it."

// docLines wraps a key's doc to 100 columns.
func docLines(doc string) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(doc) {
		if cur != "" && len(cur)+1+len(w) > 98 {
			lines, cur = append(lines, cur), ""
		}
		if cur != "" {
			cur += " "
		}
		cur += w
	}
	return append(lines, cur)
}

// scalar is s as a block value: as yaml writes it, unless that doesn't read back as s
// before a comment; then single-quoted.
func scalar(s string) string {
	out, err := yaml.Marshal(s)
	plain := strings.TrimSuffix(string(out), "\n")
	var m map[string]any
	if err == nil && !strings.Contains(plain, "\n") && yaml.Unmarshal([]byte("k: "+plain+"  # c"), &m) == nil && m["k"] == s {
		return plain
	}
	return quote(s)
}

// plainItem: what a flow list item can be without quotes.
var plainItem = regexp.MustCompile(`^[A-Za-z0-9_./~+-][A-Za-z0-9_./~+=:@-]*$`)

// list is items as a flow list, each plain when that reads back as itself, else single-quoted.
func list(items []string) string {
	out := make([]string, len(items))
	for i, s := range items {
		var back []any
		if plainItem.MatchString(s) && yaml.Unmarshal([]byte("["+s+"]"), &back) == nil && len(back) == 1 && back[0] == s {
			out[i] = s
		} else {
			out[i] = quote(s)
		}
	}
	return "[" + strings.Join(out, ", ") + "]"
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// String is 3m rather than 3m0s.
func (d *Duration) String() string {
	s := time.Duration(*d).String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
