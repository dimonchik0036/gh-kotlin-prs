package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/tui"
)

// fakeREST records the posts instead of making them.
type fakeREST struct {
	posts []string // "POST path body"
	err   error
}

func (f *fakeREST) DoWithContext(_ context.Context, method, path string, body io.Reader, resp any) error {
	data, _ := io.ReadAll(body)
	f.posts = append(f.posts, method+" "+path+" "+string(data))
	if f.err != nil {
		return f.err
	}
	return json.Unmarshal([]byte(`{"html_url": "https://github.com/JetBrains/kotlin/pull/90006#issuecomment-42"}`), resp)
}

func runEnv(t *testing.T, terminal bool, answer string) (env, *fakeREST, *strings.Builder, *strings.Builder) {
	t.Helper()
	e, out, errOut := testEnv(t, &fakeClient{t: t}, "")
	rest := &fakeREST{}
	e.newREST = func() (github.RESTClient, error) { return rest, nil }
	e.interactive = func() bool { return terminal }
	e.stdin = strings.NewReader(answer)
	return e, rest, out, errOut
}

func TestRunPosts(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		terminal bool
		answer   string
	}{
		{"--yes", []string{"run", "90006", "dry-run-retry", "--yes"}, false, ""},
		{"y", []string{"run", "#90006", "dry-run-retry"}, true, "y\n"},
		{"Yes", []string{"run", "90006", "dry-run-retry"}, true, "Yes\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, rest, out, errOut := runEnv(t, tt.terminal, tt.answer)
			if got := run(context.Background(), tt.args, e); got != exitOK {
				t.Fatalf("exit %d, stderr %q", got, errOut.String())
			}
			want := `POST repos/JetBrains/kotlin/issues/90006/comments {"body":"/dry-run --retry"}`
			if len(rest.posts) != 1 || rest.posts[0] != want {
				t.Errorf("posts %q, want %q", rest.posts, want)
			}
			for _, w := range []string{"#90006 KT-990004: Example change\n/dry-run --retry\n", "posted https://github.com/JetBrains/kotlin/pull/90006#issuecomment-42\n"} {
				if !strings.Contains(out.String(), w) {
					t.Errorf("output lacks %q:\n%s", w, out.String())
				}
			}
		})
	}
}

func TestRunRefuses(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		terminal bool
		answer   string
		exit     int
		stderr   string
	}{
		{"declined", []string{"run", "90006", "dry-run"}, true, "n\n", exitError, "not posted: cancelled"},
		{"enter means no", []string{"run", "90006", "dry-run"}, true, "\n", exitError, "not posted: cancelled"},
		{"no answer", []string{"run", "90006", "dry-run"}, true, "", exitError, "not posted: cancelled"},
		{"no terminal", []string{"run", "90006", "dry-run"}, false, "y\n", exitUsage, "not posted: no terminal to ask on; pass --yes to post /dry-run"},
		{"not mine", []string{"run", "90001", "dry-run", "--yes"}, false, "", exitError, "not posted: #90001 isn't yours (by alice_user)"},
		{"not approved", []string{"run", "90006", "safe-merge", "--yes"}, false, "", exitError, "not posted: #90006 isn't approved: code owners missing"},
		{"nothing to cancel", []string{"run", "90006", "cancel-coordinator", "--yes"}, false, "", exitError, "no dry-run or safe-merge is requested or running on #90006"},
		{"unknown command", []string{"run", "90006", "safe-squash-merge"}, true, "y\n", exitUsage, `unknown command "safe-squash-merge", want one of dry-run, dry-run-retry,`},
		{"not a number", []string{"run", "abc", "dry-run"}, true, "y\n", exitUsage, `not a PR number: "abc"`},
		{"missing command", []string{"run", "90006"}, true, "y\n", exitUsage, "run takes a PR number and a command"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, rest, _, errOut := runEnv(t, tt.terminal, tt.answer)
			if got := run(context.Background(), tt.args, e); got != tt.exit || !strings.Contains(errOut.String(), tt.stderr) {
				t.Errorf("exit %d, stderr %q; want %d, %q", got, errOut.String(), tt.exit, tt.stderr)
			}
			if len(rest.posts) != 0 {
				t.Errorf("posted %q", rest.posts)
			}
		})
	}
}

func TestRunPostFails(t *testing.T) {
	e, rest, out, errOut := runEnv(t, false, "")
	rest.err = errors.New("HTTP 403: Resource not accessible by integration")
	if got := run(context.Background(), []string{"run", "90006", "fixup", "--yes"}, e); got != exitError ||
		!strings.Contains(errOut.String(), "post /fixup to #90006: HTTP 403") || strings.Contains(out.String(), "posted") {
		t.Errorf("exit %d, stdout %q, stderr %q", got, out.String(), errOut.String())
	}
}

// Demo mode shows the fixtures and never posts.
func TestRunInDemoMode(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv(demoEnv, "../../testdata/raw")
	t.Setenv(configEnv, filepath.Join(t.TempDir(), "none.yml"))
	var out, errOut strings.Builder
	if got := Execute(context.Background(), []string{"run", "90006", "dry-run", "--yes"}, &out, &errOut, "test"); got != exitError ||
		!strings.Contains(errOut.String(), "not posted: demo mode never posts") {
		t.Errorf("exit %d, stderr %q", got, errOut.String())
	}
}

// The TUI posts through the same REST client as `run`; in demo mode it gets no way to post.
func TestTUIPosts(t *testing.T) {
	e, rest, _, errOut := runEnv(t, true, "")
	var opts tui.Options
	e.runTUI = func(_ context.Context, o tui.Options) error { opts = o; return nil }
	if got := run(context.Background(), nil, e); got != exitOK || opts.Post == nil {
		t.Fatalf("exit %d, stderr %q, post %v", got, errOut.String(), opts.Post != nil)
	}
	url, err := opts.Post(context.Background(), 90006, "/fixup")
	if err != nil || url == "" || len(rest.posts) != 1 || rest.posts[0] != `POST repos/JetBrains/kotlin/issues/90006/comments {"body":"/fixup"}` {
		t.Errorf("post: %q, %v, %q", url, err, rest.posts)
	}
	e.demo = true
	if got := run(context.Background(), nil, e); got != exitOK || opts.Post != nil {
		t.Errorf("demo mode: exit %d, post %v", got, opts.Post != nil)
	}
}
