package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// Every key of Config is described, in the order of the struct.
func TestFieldsCoverConfig(t *testing.T) {
	var keys []string
	typ := reflect.TypeFor[Config]()
	for i := range typ.NumField() {
		keys = append(keys, typ.Field(i).Tag.Get("yaml"))
	}
	var described []string
	for _, f := range fields {
		described = append(described, f.key)
	}
	if !slices.Equal(keys, described) {
		t.Errorf("described keys %q, want %q", described, keys)
	}
}

// Uncommenting the template gives the defaults back.
func TestTemplate(t *testing.T) {
	tmpl := Template()
	if cfg, err := Parse([]byte(tmpl)); err != nil || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("the commented template = %+v, %v", cfg, err)
	}
	docs := map[string]bool{templateHeader: true}
	for _, f := range fields {
		for _, l := range docLines(f.doc) {
			docs["# "+l] = true
		}
	}
	var uncommented []string
	for _, line := range strings.Split(tmpl, "\n") {
		if l, ok := strings.CutPrefix(line, "# "); ok && !docs[line] {
			line = l
		}
		uncommented = append(uncommented, line)
	}
	cfg, err := Parse([]byte(strings.Join(uncommented, "\n")))
	if err != nil {
		t.Fatalf("uncommented template: %v\n%s", err, strings.Join(uncommented, "\n"))
	}
	if !reflect.DeepEqual(cfg.Values(nil), Default().Values(nil)) {
		t.Errorf("uncommented template = %+v, want the defaults", cfg)
	}
	for _, line := range strings.Split(tmpl, "\n") {
		if len(line) > 100 {
			t.Errorf("a line of %d columns: %s", len(line), line)
		}
	}
	if !strings.Contains(tmpl, "\n# keys:\n#   up: [up, k]\n") || !strings.Contains(tmpl, "\n# notify:\n#   events: [runPassed, ") {
		t.Errorf("maps aren't blocks:\n%s", tmpl)
	}
}

// The description reads back as the same config, a map's sub-keys each with their source.
func TestDescribe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	file := "icons: ascii\nteams: [kotlin-analysis-api]\nkeys: {copy: c}\n" +
		"notify: {command: [sh, -c, 'cat >> /tmp/kp-events.jsonl', '{title}', 'it''s'], events: [merged], swiftbar: false}\n"
	if err := os.WriteFile(path, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, sources, err := LoadSources(path)
	if err != nil {
		t.Fatal(err)
	}
	out := Describe(cfg.Values(sources))
	back, err := Parse([]byte(out))
	if err != nil || !reflect.DeepEqual(back, cfg) {
		t.Fatalf("the description reads back as %+v, %v, want %+v:\n%s", back, err, cfg, out)
	}
	for _, want := range []string{
		"\nicons: ascii ", "\nkeys:\n  up: [up, k] ",
		"\n  copy: [c]                     # file\n",
		"\n  up: [up, k]                   # default\n",
		"\n  command: [sh, -c, 'cat >> /tmp/kp-events.jsonl', '{title}', 'it''s'] # file\n",
		"\n  events: [merged]  # file\n",
		"\n  terminal: auto    # default\n",
		"\n  swiftbar: false   # file\n",
		"\nrepo: JetBrains/kotlin                               # default\n",
		"\nissueURL: https://youtrack.jetbrains.com/issue/{id}  # default\n",
	} {
		if !strings.Contains("\n"+out, want) {
			t.Errorf("the description lacks %q:\n%s", want, out)
		}
	}
	// A value too long to align with the others gets one space before its comment.
	long := Default()
	long.Notify.Command = []string{strings.Repeat("x", 80)}
	if out := Describe(long.Values(nil)); !strings.Contains(out, "\n  command: ["+strings.Repeat("x", 80)+"] # default\n") ||
		!strings.Contains(out, "\n  terminal: auto  # default\n") {
		t.Errorf("long values:\n%s", out)
	}
}

func TestQuoting(t *testing.T) {
	for _, c := range []struct {
		items []string
		want  string
	}{
		{[]string{"up", "k", "shift+tab", "/", "-c", "--app-name=kp", "KT-1"}, "[up, k, shift+tab, /, -c, --app-name=kp, KT-1]"},
		{[]string{"?", "{title}", "a b", "a,b", "true", "1", "", "it's", "#x", "https://example.org/{id}"},
			`['?', '{title}', 'a b', 'a,b', 'true', '1', '', 'it''s', '#x', 'https://example.org/{id}']`},
	} {
		if got := list(c.items); got != c.want {
			t.Errorf("list(%q) = %s, want %s", c.items, got, c.want)
		}
		var back []string
		if err := yaml.Unmarshal([]byte(list(c.items)), &back); err != nil || !slices.Equal(back, c.items) {
			t.Errorf("list(%q) reads back as %q, %v", c.items, back, err)
		}
	}
	for s, want := range map[string]string{
		"auto": "auto", "https://youtrack.jetbrains.com/issue/{id}": "https://youtrack.jetbrains.com/issue/{id}",
		"a # b": "'a # b'", "true": `"true"`, "": `""`, "cat >> /tmp/x": "cat >> /tmp/x",
	} {
		if got := scalar(s); got != want {
			t.Errorf("scalar(%q) = %s, want %s", s, got, want)
		}
	}
}

func TestDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{3 * time.Minute: "3m", time.Hour: "1h", 90 * time.Second: "1m30s", 2*time.Hour + 30*time.Minute: "2h30m"} {
		if got := (*Duration)(&d).String(); got != want {
			t.Errorf("Duration(%v).String() = %q, want %q", d, got, want)
		}
	}
}
