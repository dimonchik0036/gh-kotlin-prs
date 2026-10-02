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
	if resp == nil {
		return nil
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
		// gh queries the terminal's background before it runs the extension (OSC 11, then
		// CSI 6n); the replies can be left on stdin ahead of the answer.
		{"y after terminal replies", []string{"run", "90006", "dry-run-retry"}, true, "\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[12;1Ry\n"},
		{"yes after a BEL-ended reply", []string{"run", "90006", "dry-run-retry"}, true, "\x1b]11;rgb:ffff/ffff/ffff\a\x1b[3;7Ryes\n"},
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
		{"n after terminal replies", []string{"run", "90006", "dry-run"}, true, "\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[12;1Rn\n", exitError, "not posted: cancelled"},
		{"only terminal replies", []string{"run", "90006", "dry-run"}, true, "\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[12;1R\n", exitError, "not posted: cancelled"},
		{"y inside a reply", []string{"run", "90006", "dry-run"}, true, "\x1b]11;y\x1b\\\n", exitError, "not posted: cancelled"},
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
	t.Setenv(configEnv, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no config file there
	var out, errOut strings.Builder
	for _, args := range [][]string{{"run", "90006", "dry-run", "--yes"}, {"run", "90006", "request-review", "bob_user", "--yes"}} {
		out.Reset()
		errOut.Reset()
		if got := Execute(context.Background(), args, &out, &errOut, "test"); got != exitError ||
			!strings.Contains(errOut.String(), "not posted: demo mode never posts") {
			t.Errorf("%q: exit %d, stderr %q", args, got, errOut.String())
		}
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

func TestStartFlags(t *testing.T) {
	e, _, _, errOut := runEnv(t, true, "")
	var opts tui.Options
	e.runTUI = func(_ context.Context, o tui.Options) error { opts = o; return nil }
	if got := run(context.Background(), []string{"--pr", "90006", "--post", "dry-run-retry"}, e); got != exitOK ||
		opts.Start != 90006 || opts.StartCommand == nil || opts.StartCommand.Text != "/dry-run --retry" {
		t.Errorf("exit %d, stderr %q, start %d %+v", got, errOut.String(), opts.Start, opts.StartCommand)
	}
	for _, tt := range []struct {
		args     []string
		terminal bool
		stderr   string
	}{
		{[]string{"--post", "fixup"}, true, "--post needs --pr"},
		{[]string{"--pr", "90006", "--post", "safe-squash-merge"}, true, `unknown command "safe-squash-merge" for --post`},
		{[]string{"--pr", "90006"}, false, "the interactive view needs a terminal"},
		{[]string{"--pr", "-3"}, true, "not a PR number: -3"},
	} {
		e, _, _, errOut := runEnv(t, tt.terminal, "")
		e.runTUI = func(context.Context, tui.Options) error { t.Errorf("%q started the TUI", tt.args); return nil }
		if got := run(context.Background(), tt.args, e); got != exitUsage || !strings.Contains(errOut.String(), tt.stderr) {
			t.Errorf("%q: exit %d, stderr %q", tt.args, got, errOut.String())
		}
	}
}

// request-review asks the code owners named, or by default the ones to re-request, in
// one request, and drops the PR's cached details.
func TestRunRequestReview(t *testing.T) {
	// dave_user, an owner of /analysis/, requested changes before the last push.
	changesRequested := func(_ int, pr map[string]any) {
		review := map[string]any{"author": map[string]any{"__typename": "User", "login": "dave_user"}, "state": "CHANGES_REQUESTED",
			"submittedAt": "2026-09-30T20:00:00Z", "url": "https://github.com/JetBrains/kotlin/pull/90006#pullrequestreview-1"}
		for _, key := range []string{"latestOpinionatedReviews", "reviews"} {
			nodes := pr[key].(map[string]any)
			nodes["nodes"] = append(nodes["nodes"].([]any), review)
		}
	}
	for _, tt := range []struct {
		name   string
		args   []string
		edit   func(int, map[string]any)
		answer string
		want   string // the request's body
	}{
		{"the logins given, as the table spells them", []string{"Bob_User", "carol_user", "bob_user"}, nil, "y\n", `{"reviewers":["bob_user","carol_user"]}`},
		{"the ones to re-request by default", nil, changesRequested, "yes\n", `{"reviewers":["dave_user"]}`},
		{"--yes", []string{"carol_user", "--yes"}, nil, "", `{"reviewers":["carol_user"]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, rest, out, errOut := runEnv(t, true, tt.answer)
			client := &fakeClient{t: t, edit: tt.edit}
			e.newClient = func() (github.Client, string, error) { return client, "test-account", nil }
			if got := run(context.Background(), append([]string{"run", "90006", "request-review"}, tt.args...), e); got != exitOK {
				t.Fatalf("exit %d, stderr %q", got, errOut.String())
			}
			if want := "POST repos/JetBrains/kotlin/pulls/90006/requested_reviewers " + tt.want; len(rest.posts) != 1 || rest.posts[0] != want {
				t.Errorf("posts %q, want %q", rest.posts, want)
			}
			if !strings.Contains(out.String(), "#90006 KT-990004: Example change\nrequest a review from ") || !strings.Contains(out.String(), "requested a review of #90006 from ") {
				t.Errorf("output:\n%s", out.String())
			}
			if entries, _ := filepath.Glob(filepath.Join(e.cacheDir, "PullRequest-*")); len(entries) != 0 {
				t.Errorf("the PR's cached details are left: %q", entries)
			}
		})
	}
}

func TestRunRequestReviewRefuses(t *testing.T) {
	const candidates = "the code owners to ask, by rule:\n" +
		"  /analysis/ (requested: erin_user): bob_user, carol_user, dave_user, erin_user\n" +
		"  /compiler/fir/ +1 (✓ trent_user): quinn_user, rupert_user, sybil_user, trent_user, ursula_user, victor_user, " +
		"walter_user, yvonne_user, heidi3_user, zoe_user, xavier_user, alice2_user\n"
	for _, tt := range []struct {
		name   string
		args   []string
		answer string
		exit   int
		stderr string
	}{
		// erin_user is requested already and trent_user approved: nobody to re-request.
		{"nobody to re-request", []string{"run", "90006", "request-review"}, "y\n", exitError,
			"not posted: nobody to re-request on #90006; name " + candidates},
		{"not a code owner", []string{"run", "90006", "request-review", "bob_user", "zed_user"}, "y\n", exitError,
			"not posted: zed_user is no code owner of #90006 to ask; " + candidates},
		{"myself", []string{"run", "90006", "request-review", "dimonchik0036"}, "y\n", exitError, "dimonchik0036 is no code owner of #90006"},
		{"declined", []string{"run", "90006", "request-review", "bob_user"}, "n\n", exitError, "not posted: cancelled"},
		{"not mine", []string{"run", "90001", "request-review", "bob_user", "--yes"}, "", exitError, "not posted: #90001 isn't yours"},
		{"no terminal", []string{"run", "90006", "request-review", "bob_user"}, "y\n", exitUsage, "pass --yes to request the review"},
		{"logins after another command", []string{"run", "90006", "dry-run", "bob_user"}, "y\n", exitUsage,
			"run takes a PR number and a command, and request-review the logins to ask"},
		{"the command first", []string{"run", "request-review", "90006"}, "y\n", exitUsage, `not a PR number: "request-review"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, rest, _, errOut := runEnv(t, tt.name != "no terminal", tt.answer)
			if got := run(context.Background(), tt.args, e); got != tt.exit || !strings.Contains(errOut.String(), tt.stderr) {
				t.Errorf("exit %d, stderr %q; want %d, %q", got, errOut.String(), tt.exit, tt.stderr)
			}
			if len(rest.posts) != 0 {
				t.Errorf("requested %q", rest.posts)
			}
		})
	}
}

func TestRunRequestReviewFails(t *testing.T) {
	e, rest, out, errOut := runEnv(t, false, "")
	rest.err = errors.New("HTTP 422: reviews may only be requested from collaborators")
	if got := run(context.Background(), []string{"run", "90006", "request-review", "bob_user", "--yes"}, e); got != exitError ||
		!strings.Contains(errOut.String(), "request a review of #90006 from bob_user: HTTP 422") || strings.Contains(out.String(), "requested a review") {
		t.Errorf("exit %d, stdout %q, stderr %q", got, out.String(), errOut.String())
	}
}

// A posted command drops the PR's cached details, so the next run (the plugin's) fetches
// what the bot made of it; a refusal or a declined question keeps them.
func TestRunForgetsThePR(t *testing.T) {
	cached := func(e env) bool {
		entries, _ := filepath.Glob(filepath.Join(e.cacheDir, "PullRequest-*"))
		return len(entries) > 0
	}
	for _, tt := range []struct {
		name   string
		args   []string
		answer string
		kept   bool
	}{
		{"posted", []string{"run", "90006", "dry-run-retry"}, "y\n", false},
		{"declined", []string{"run", "90006", "dry-run-retry"}, "n\n", true},
		{"refused", []string{"run", "90006", "safe-merge"}, "y\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _, _ := runEnv(t, true, tt.answer)
			run(context.Background(), tt.args, e)
			if cached(e) != tt.kept {
				t.Errorf("the PR's cache entry is there: %v, want %v", cached(e), tt.kept)
			}
		})
	}
	// The TUI's post does the same.
	e, rest, _, errOut := runEnv(t, true, "")
	var opts tui.Options
	e.runTUI = func(_ context.Context, o tui.Options) error { opts = o; return nil }
	if got := run(context.Background(), []string{"show", "90006"}, e); got != exitOK || !cached(e) {
		t.Fatalf("show: exit %d, stderr %q, cached %v", got, errOut.String(), cached(e))
	}
	run(context.Background(), nil, e)
	rest.err = errors.New("HTTP 502")
	if _, err := opts.Post(context.Background(), 90006, "/fixup"); err == nil || !cached(e) {
		t.Errorf("a failed post: %v, cached %v", err, cached(e))
	}
	rest.err = nil
	if _, err := opts.Post(context.Background(), 90006, "/fixup"); err != nil || cached(e) {
		t.Errorf("a post: %v, cached %v", err, cached(e))
	}
}
