package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
)

func TestConfigCommand(t *testing.T) {
	e, out, errOut := testEnv(t, nil, "icons: ascii\nteams: [kotlin-analysis-api]\n")
	if got := run(context.Background(), []string{"config", "--hyperlinks", "never"}, e); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if lines[0] != "# "+e.configPath+" (test, found)" {
		t.Errorf("header %q", lines[0])
	}
	for _, want := range []string{
		"repo: JetBrains/kotlin", "# default",
		"icons: ascii", "teams: [kotlin-analysis-api]", "hyperlinks: never", "refresh: 3m",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	source := map[string]string{}
	for _, l := range lines[1:] {
		key, _, _ := strings.Cut(l, ":")
		_, src, _ := strings.Cut(l, "# ")
		source[key] = src
	}
	for key, want := range map[string]string{"icons": "file", "teams": "file", "hyperlinks": "flag", "repo": "default", "issueURL": "default"} {
		if source[key] != want {
			t.Errorf("%s from %q, want %q", key, source[key], want)
		}
	}
	if len(lines) != 1+len(config.Default().Values(nil)) {
		t.Errorf("%d lines, want a header and every key", len(lines))
	}
}

func TestConfigCommandErrors(t *testing.T) {
	e, _, _ := testEnv(t, nil, "icons: emoji\n")
	if got := run(context.Background(), []string{"config"}, e); got != exitError {
		t.Errorf("an invalid file: exit %d, want %d", got, exitError)
	}
	e, _, _ = testEnv(t, nil, "")
	if got := run(context.Background(), []string{"config", "--icons", "emoji"}, e); got != exitUsage {
		t.Errorf("an invalid flag: exit %d, want %d", got, exitUsage)
	}
}

func TestConfigPath(t *testing.T) {
	e, out, _ := testEnv(t, nil, "")
	if got := run(context.Background(), []string{"config", "path"}, e); got != exitOK || out.String() != e.configPath+"\n" {
		t.Errorf("exit %d, %q", got, out.String())
	}
	other := filepath.Join(t.TempDir(), "other.yml")
	e, out, _ = testEnv(t, nil, "")
	if got := run(context.Background(), []string{"--config", other, "config", "path"}, e); got != exitOK || out.String() != other+"\n" {
		t.Errorf("--config: exit %d, %q", got, out.String())
	}
}

// The environment variable replaces the default path; it needs no client, so Execute is safe here.
func TestConfigEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.yml")
	t.Setenv(configEnv, path)
	var out, errOut strings.Builder
	if got := Execute(context.Background(), []string{"config", "path"}, &out, &errOut, "test"); got != exitOK || out.String() != path+"\n" {
		t.Errorf("exit %d, %q, stderr %q", got, out.String(), errOut.String())
	}
	out.Reset()
	if got := Execute(context.Background(), []string{"config"}, &out, &errOut, "test"); got != exitOK || !strings.HasPrefix(out.String(), "# "+path+" ($"+configEnv+", not found, all defaults)") {
		t.Errorf("exit %d, %q", got, out.String())
	}
}

func TestConfigInit(t *testing.T) {
	e, out, errOut := testEnv(t, nil, "")
	e.configPath = filepath.Join(t.TempDir(), "dir", "config.yml")
	if got := run(context.Background(), []string{"config", "init"}, e); got != exitOK {
		t.Fatalf("exit %d, stderr %q", got, errOut.String())
	}
	data, err := os.ReadFile(e.configPath)
	if err != nil || string(data) != config.Template() || out.String() != "wrote "+e.configPath+"\n" {
		t.Fatalf("file %q, %v, output %q", data, err, out.String())
	}
	if err := os.WriteFile(e.configPath, []byte("icons: ascii\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if got := run(context.Background(), []string{"config", "init"}, e); got != exitError || !strings.Contains(errOut.String(), "--force") {
		t.Errorf("existing file: exit %d, stderr %q", got, errOut.String())
	}
	if data, _ := os.ReadFile(e.configPath); string(data) != "icons: ascii\n" {
		t.Errorf("the existing file was changed: %q", data)
	}
	if got := run(context.Background(), []string{"config", "init", "--force"}, e); got != exitOK {
		t.Errorf("--force: exit %d", got)
	}
	if data, _ := os.ReadFile(e.configPath); string(data) != config.Template() {
		t.Errorf("--force didn't overwrite: %q", data)
	}
}
