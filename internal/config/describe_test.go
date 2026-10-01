package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
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
	var uncommented []string
	for _, line := range strings.Split(tmpl, "\n") {
		if rest, ok := strings.CutPrefix(line, "# "); ok && strings.Contains(rest, ": ") && !strings.Contains(rest, ". ") {
			line = rest
		}
		uncommented = append(uncommented, line)
	}
	cfg, err := Parse([]byte(strings.Join(uncommented, "\n")))
	if err != nil {
		t.Fatalf("uncommented template: %v\n%s", err, strings.Join(uncommented, "\n"))
	}
	if !slices.Equal(cfg.Values(nil), Default().Values(nil)) {
		t.Errorf("uncommented template = %+v, want the defaults", cfg)
	}
}

func TestLoadSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("icons: ascii\nteams: [kotlin-analysis-api]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, sources, err := LoadSources(path)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Value{}
	for _, v := range cfg.Values(sources) {
		got[v.Key] = v
	}
	for key, want := range map[string]Value{
		"icons":            {"icons", "ascii", SourceFile},
		"teams":            {"teams", "[kotlin-analysis-api]", SourceFile},
		"repo":             {"repo", "JetBrains/kotlin", SourceDefault},
		"refresh":          {"refresh", "3m", SourceDefault},
		"issueProjects":    {"issueProjects", "[KT, KTIJ, KTI]", SourceDefault},
		"issueURL":         {"issueURL", "https://youtrack.jetbrains.com/issue/{id}", SourceDefault},
		"requestedTimeout": {"requestedTimeout", "10m", SourceDefault},
	} {
		if got[key] != want {
			t.Errorf("%s = %+v, want %+v", key, got[key], want)
		}
	}
	if _, sources, err := LoadSources(filepath.Join(t.TempDir(), "missing.yml")); err != nil || len(sources) != 0 {
		t.Errorf("missing file: %v, %v", sources, err)
	}
}

func TestDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{3 * time.Minute: "3m", time.Hour: "1h", 90 * time.Second: "1m30s", 2*time.Hour + 30*time.Minute: "2h30m"} {
		if got := (*Duration)(&d).String(); got != want {
			t.Errorf("Duration(%v).String() = %q, want %q", d, got, want)
		}
	}
}
