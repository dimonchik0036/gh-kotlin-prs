package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/tui"
)

// A config file that --config or $GH_KOTLIN_PRS_CONFIG names must exist; the default
// location may have none.
func TestMissingConfigFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "typo.yml")
	sources := []struct {
		name, from string
		// args are the source's flags; set points the env at the file.
		args []string
		set  func(*env)
	}{
		{"--config", "--config", []string{"--config", missing}, func(*env) {}},
		{"$" + configEnv, "$" + configEnv, nil, func(e *env) {
			e.configPath, e.configFrom = missing, "$"+configEnv
			e.getenv = func(k string) string { return map[string]string{configEnv: missing}[k] }
		}},
	}
	want := "config: " + missing + " (from %s) doesn't exist"
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			msg := strings.Replace(want, "%s", src.from, 1)
			for _, args := range [][]string{
				{"list"}, {"show", "90006"}, {"run", "90006", "dry-run-retry", "--yes"}, {}, {"swiftbar", "script"},
				{"swiftbar", "install", "--dir", t.TempDir()},
			} {
				// A nil client fails the test if any of them gets as far as GitHub.
				e, out, errOut := testEnv(t, nil, "")
				src.set(&e)
				e.interactive = func() bool { return true }
				e.runTUI = func(context.Context, tui.Options) error {
					t.Error("the interactive view started")
					return nil
				}
				if got := run(context.Background(), append(args, src.args...), e); got != exitError ||
					errOut.String() != "error: "+msg+"\n" || out.Len() != 0 {
					t.Errorf("%q: exit %d, stdout %q, stderr %q", args, got, out.String(), errOut.String())
				}
			}

			// The plugin shows it in the menu, over the cached data, and exits 0.
			e, out, errOut := swiftbarEnv(t, &fakeClient{t: t}, "")
			if got := run(context.Background(), []string{"list", "--format", "swiftbar"}, e); got != exitOK {
				t.Fatalf("exit %d, stderr %q", got, errOut.String())
			}
			out.Reset()
			plugin := e.getenv
			src.set(&e)
			if src.args == nil {
				e.getenv = func(k string) string {
					if k == configEnv {
						return missing
					}
					return plugin(k)
				}
			} else {
				e.getenv = plugin
			}
			e.start = func(argv []string) error {
				t.Errorf("notified %q", argv)
				return nil
			}
			if got := run(context.Background(), append([]string{"list", "--format", "swiftbar"}, src.args...), e); got != exitOK ||
				!strings.Contains(out.String(), "| color=red tooltip=\""+msg+"\"\n") || !strings.Contains(out.String(), "Mine (3)") {
				t.Errorf("the plugin: exit %d, stderr %q:\n%s", got, errOut.String(), out.String())
			}

			// config and config path say where it is; config init creates it.
			e, out, errOut = testEnv(t, nil, "")
			src.set(&e)
			if got := run(context.Background(), append([]string{"config"}, src.args...), e); got != exitOK ||
				!strings.HasPrefix(out.String(), "# "+missing+" ("+src.from+", not found, all defaults)\n") {
				t.Errorf("config: exit %d, stderr %q:\n%s", got, errOut.String(), out.String())
			}
			out.Reset()
			if got := run(context.Background(), append([]string{"config", "path"}, src.args...), e); got != exitOK || out.String() != missing+"\n" {
				t.Errorf("config path: exit %d, %q", got, out.String())
			}
			out.Reset()
			if got := run(context.Background(), append([]string{"config", "init"}, src.args...), e); got != exitOK || out.String() != "wrote "+missing+"\n" {
				t.Errorf("config init: exit %d, stderr %q", got, errOut.String())
			}
			if err := os.Remove(missing); err != nil {
				t.Error(err)
			}
		})
	}

	// The default location may have no file: the defaults then.
	e, out, errOut := testEnv(t, &fakeClient{t: t}, "")
	if _, err := os.Stat(e.configPath); !os.IsNotExist(err) {
		t.Fatalf("a config file at %s: %v", e.configPath, err)
	}
	if got := run(context.Background(), []string{"list", "--format", "json"}, e); got != exitOK || !strings.HasPrefix(out.String(), "{") {
		t.Errorf("default location: exit %d, stderr %q", got, errOut.String())
	}
}
