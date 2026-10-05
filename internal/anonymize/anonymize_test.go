package anonymize

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
)

// Two responses with made-up "real" people: real_author (an author), real_reviewer
// (a reviewer) and real_owner (only listed in the code-owners table).
const first = `{"data": {
  "viewer": {"login": "the_viewer"},
  "repository": {"pullRequest": {
    "number": 7, "title": "KT-123: Fix the frobnicator for real_author", "headRefName": "real_author/KT-123.fix",
    "author": {"__typename": "User", "login": "real_author"},
    "reviewRequests": {"nodes": [{"requestedReviewer": {"__typename": "User", "login": "real_reviewer"}}, {"requestedReviewer": {"__typename": "Team", "slug": "kotlin-analysis-api"}}]},
    "comments": {"nodes": [
      {"author": {"__typename": "Bot", "login": "kotlin-safemerge"}, "isMinimized": false,
       "body": "<table><tr><td><a href=\"https://github.com/real_owner\"><b><code>real_owner</code></b></a> ⏳</td><td>❌<br><a href=\"https://github.com/Real_Reviewer\"><b><code>Real_Reviewer</code></b></a></td></tr></table>\n<!-- CODE_OWNERS_REVIEW_COMMENT -->"},
      {"author": {"__typename": "Bot", "login": "kotlin-safemerge"}, "isMinimized": false,
       "body": "<table><tr><td><a href=\"https://github.com/real_owner\"><b><code>Secret Name (Secret) &amp; co (real_owner)</code></b></a> ⏳, <a href=\"https://github.com/the_viewer\"><b><code>Viewer Secret (the_viewer)</code></b></a></td></tr></table>"},
      {"author": {"__typename": "User", "login": "KotlinBuild"}, "isMinimized": false, "body": "Quality gate is triggered at https://example.org/build/1 — ping @real_reviewer"},
      {"author": {"__typename": "User", "login": "real_author"}, "isMinimized": false, "body": "/safe-squash-merge --title=\"Secret plans\" --retry\nmore text"},
      {"author": {"__typename": "User", "login": "the_viewer"}, "isMinimized": false, "body": "I talked to real_reviewer about it",
       "reactions": {"nodes": [{"user": {"login": "kotlin-safemerge[bot]"}}]}}
    ]},
    "reviewThreads": {"nodes": [{"path": "compiler/A.kt", "firstComment": {"nodes": [{"author": {"__typename": "User", "login": "real_reviewer"}, "body": "Why, real_author?"}]}}]},
    "commits": {"nodes": [{"commit": {"statusCheckRollup": {"contexts": {"nodes": [{"__typename": "CheckRun", "name": "Code Owners Approval", "title": "Waiting for code owner approvals",
      "summary": "<table><tr><td><a href=\"https://github.com/real_owner\"><b><code>real_owner</code></b></a></td></tr></table>\n<!-- CODE_OWNERS_REVIEW_COMMENT -->"}]}}}}]}
  }}
}}`

const second = `{"data": {
  "viewer": {"login": "the_viewer"},
  "repository": {"pullRequest": {
    "number": 8, "title": "Unrelated", "headRefName": "feature",
    "author": {"__typename": "User", "login": "real_reviewer"},
    "comments": {"nodes": []}
  }}
}}`

func anonymizeBoth(t *testing.T) (string, string) {
	t.Helper()
	a := New(config.Default())
	var docs []any
	for _, src := range []string{first, second} {
		doc, err := Decode([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		a.Collect(doc)
		docs = append(docs, doc)
	}
	var out []string
	for _, doc := range docs {
		if err := a.Rewrite(doc); err != nil {
			t.Fatal(err)
		}
		data, err := Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(data))
	}
	return out[0], out[1]
}

func TestAnonymize(t *testing.T) {
	got, other := anonymizeBoth(t)
	for _, secret := range []string{"real_author", "real_reviewer", "Real_Reviewer", "real_owner", "Secret plans", "frobnicator", "talked", "Why,", "Secret Name", "Viewer Secret"} {
		if strings.Contains(got, secret) || strings.Contains(other, secret) {
			t.Errorf("%q survived:\n%s", secret, got)
		}
	}
	for _, kept := range []string{
		`"login": "the_viewer"`,
		`"login": "kotlin-safemerge"`,
		`"login": "KotlinBuild"`,
		`"login": "kotlin-safemerge[bot]"`,
		`"slug": "kotlin-analysis-api"`,
		`"number": 90001`,
		`"path": "compiler/A.kt"`,
		// Pseudonyms in order of first appearance, walking keys sorted: the author, then
		// the logins of the comments (the table lists real_owner first), then the requests.
		`"author": {
          "__typename": "User",
          "login": "alice_user"`,
		`<a href=\"https://github.com/bob_user\"><b><code>bob_user</code></b></a> ⏳`,
		`❌<br><a href=\"https://github.com/carol_user\"><b><code>carol_user</code></b></a>`,
		`<a href=\"https://github.com/bob_user\"><b><code>Bob User (bob_user)</code></b></a> ⏳, ` +
			`<a href=\"https://github.com/the_viewer\"><b><code>Example Name (the_viewer)</code></b></a>`,
		"Quality gate is triggered at https://example.org/build/1 — ping @carol_user",
		`"body": "/safe-squash-merge --retry"`,
		`"summary": "<table><tr><td><a href=\"https://github.com/bob_user\"><b><code>bob_user</code></b></a></td></tr></table>`,
		`"title": "Waiting for code owner approvals"`,
		`"body": "Comment text."`,
		`"body": "Review comment text."`,
		`"title": "KT-990001: Example change"`,
		`"headRefName": "topic/KT-990001-example"`,
	} {
		if !strings.Contains(got, kept) {
			t.Errorf("output lacks %s:\n%s", kept, got)
		}
	}
	// The same person gets the same pseudonym in every document of the batch.
	for _, want := range []string{`"login": "carol_user"`, `"title": "Example change"`, `"headRefName": "topic/example-90002"`} {
		if !strings.Contains(other, want) {
			t.Errorf("second document lacks %s:\n%s", want, other)
		}
	}
}

func TestAnonymizeIsDeterministic(t *testing.T) {
	a1, b1 := anonymizeBoth(t)
	a2, b2 := anonymizeBoth(t)
	if a1 != a2 || b1 != b2 {
		t.Error("two runs over the same input differ")
	}
}

func TestRewriteRejectsLeaks(t *testing.T) {
	a := New(config.Default())
	doc, err := Decode([]byte(`{"pr": {"author": {"__typename": "User", "login": "real_author"}, "url": "https://example.org/real_author/x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	a.Collect(doc)
	if err := a.Rewrite(doc); err == nil || !strings.Contains(err.Error(), "real_author") {
		t.Errorf("Rewrite = %v, want a leak error naming real_author", err)
	}
}

func TestPseudonymsWrapAround(t *testing.T) {
	a := New(config.Default())
	for i := range len(pseudonyms) + 2 {
		a.register("user_" + string(rune('a'+i%26)) + strings.Repeat("x", i/26))
	}
	if a.names["user_zz"] != "" || a.names["user_ax"] != "alice2_user" || a.names["user_bx"] != "bob2_user" {
		t.Errorf("names = %v", a.names)
	}
	// The made-up names have no digits, so the parser takes them for names.
	if got := pseudonymName("alice2_user"); got != "Alice User" {
		t.Errorf("pseudonymName(alice2_user) = %q", got)
	}
}

// Every committed fixture went through the anonymizer: logins are pseudonyms, the
// viewer or bots, and human text is filler or a bare command.
func TestCommittedFixturesAreAnonymized(t *testing.T) {
	files, err := committedFixtures()
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	pseudonym := regexp.MustCompile(`^(` + strings.Join(pseudonyms, "|") + `)\d*_user$`)
	cfg := config.Default()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		viewer := lookup(doc, "data", "viewer", "login")
		a := New(cfg)
		walk(doc, func(obj map[string]any) {
			if login, ok := obj["login"].(string); ok && login != viewer && !a.isBot(obj) && !pseudonym.MatchString(login) {
				t.Errorf("%s: login %q is not a pseudonym", filepath.Base(f), login)
			}
			if text, ok := botText(a, obj); ok {
				for _, m := range profileLink.FindAllStringSubmatch(obj[text].(string), -1) {
					if m[1] != viewer && !pseudonym.MatchString(m[1]) {
						t.Errorf("%s: bot comment links %q", filepath.Base(f), m[1])
					}
				}
				for _, m := range namedLogin.FindAllStringSubmatch(obj[text].(string), -1) {
					if name, login := m[2], m[3]; name != keptName && (!pseudonym.MatchString(login) || name != pseudonymName(login)) {
						t.Errorf("%s: bot comment names %q", filepath.Base(f), name)
					}
				}
			} else if body, ok := obj["body"].(string); ok && body != commentFiller && body != threadFiller && body != commandOnly(body) {
				t.Errorf("%s: human text %.40q", filepath.Base(f), body)
			}
			if msg, ok := obj["message"].(string); ok && msg != a.commitMessage(msg) {
				t.Errorf("%s: commit message %.40q isn't filler and trailers", filepath.Base(f), msg)
			}
		})
	}
}

// committedFixtures are the fetched fixtures and the derived demo one (testdata/demo).
func committedFixtures() ([]string, error) {
	raw, err := filepath.Glob("../../testdata/raw/*.json")
	if err != nil {
		return nil, err
	}
	derived, err := filepath.Glob("../../testdata/demo/*.json")
	return append(raw, derived...), err
}

// Every committed fixture has fake PR and issue numbers (see remap.go), for every project.
func TestCommittedFixturesHaveFakeNumbers(t *testing.T) {
	files, err := committedFixtures()
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	issues := config.Default().IssuePattern()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(FileName(doc), "pr-"), ".json")); err != nil || n < firstFakePR {
			t.Errorf("%s: PR number %s is not a fake one", filepath.Base(f), FileName(doc))
		}
		for _, g := range issues.FindAllStringSubmatch(string(data), -1) {
			if n, _ := strconv.Atoi(g[2]); n < firstFakeIssue {
				t.Errorf("%s: %s is not a fake issue", filepath.Base(f), g[0])
			}
		}
	}
}
