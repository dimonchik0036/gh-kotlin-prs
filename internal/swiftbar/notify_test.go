package swiftbar

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
)

var event = notify.Event{Kind: notify.RunFailed, Number: 7, Title: `#7 dry-run "failed"`, Body: "Example & change", URL: "https://example.org/pull/7?x=1&y=2#issuecomment-3"}

func TestNotificationArgv(t *testing.T) {
	argv := NotificationArgv(plugin, "/opt/homebrew/bin/gh", event)
	if len(argv) != 3 || argv[0] != "open" || argv[1] != "-g" {
		t.Fatalf("argv %q", argv)
	}
	u, err := url.Parse(argv[2])
	if err != nil || u.Scheme != "swiftbar" || u.Host != "notify" {
		t.Fatalf("URL %q: %v", argv[2], err)
	}
	q := swiftbarQuery(t, argv[2])
	if q["plugin"] != "kotlin-prs" || shown(q["title"]) != event.Title || shown(q["body"]) != event.Body || q["href"] != "" {
		t.Errorf("query %v", q)
	}
	// A click opens the interactive view on the PR, as the menu's items do.
	want := map[string]string{"bash": "exec", "param1": "/opt/homebrew/bin/gh", "param2": "kotlin-prs", "param3": "--pr", "param4": "7", "terminal": "true"}
	for _, order := range [][]string{nil, {"title", "body"}, {"body", "title"}} {
		click := clickParams(q, order)
		for k, v := range want {
			if click[k] != v {
				t.Errorf("order %v: the click's %s is %q, want %q (%v)", order, k, click[k], v, click)
			}
		}
	}
	// What SwiftBar would read wrong: apostrophes, plus signs, blanks, a leading quote.
	tricky := notify.Event{Number: 7, Title: "#7 KT-1 +1", Body: "'Don't' \"fix\"\tit\n a+b", URL: "https://example.org/a+b"}
	q = swiftbarQuery(t, NotificationArgv(plugin, "", tricky)[2])
	if shown(q["title"]) != "#7 KT-1 \uff0b1" || shown(q["body"]) != "\u200b'Don't' \"fix\" it a\uff0bb" {
		t.Errorf("tricky text: %v", q)
	}
	for _, order := range [][]string{nil, {"title", "body"}, {"body", "title"}} {
		if click := clickParams(q, order); click["bash"] != "exec" || click["param1"] != "gh" || click["param4"] != "7" || click["terminal"] != "true" {
			t.Errorf("order %v: the click's params %v", order, click)
		}
	}
	// An event about no PR opens its page.
	q = swiftbarQuery(t, NotificationArgv(plugin, "", notify.Event{Title: "t", URL: event.URL})[2])
	if q["href"] != event.URL || q["bash"] != "" {
		t.Errorf("no PR: %v", q)
	}
	osa := NotificationArgv("", "gh", event)
	if want := []string{"osascript", "-e", `display notification "Example & change" with title "#7 dry-run \"failed\""`}; len(osa) != 3 || osa[2] != want[2] {
		t.Errorf("osascript %q", osa)
	}
}

// swiftbarQuery is SwiftBar's URL.queryParameters: URLComponents' query items, which
// leave "+" as it is.
func swiftbarQuery(t *testing.T, raw string) map[string]string {
	t.Helper()
	_, query, _ := strings.Cut(raw, "?")
	q := map[string]string{}
	for _, item := range strings.Split(query, "&") {
		k, v, _ := strings.Cut(item, "=")
		v, err := url.PathUnescape(v)
		if err != nil {
			t.Fatalf("%q: %v", item, err)
		}
		q[k] = v
	}
	return q
}

// shown is a title or body as SwiftBar's notification shows it.
func shown(v string) string { return strings.ReplaceAll(v, "+", " ") }

// clickParams are the parameters SwiftBar 2.1.1 runs on a click on its notification: it
// joins the query's parameters, in its dictionary's order (first the keys of order, then
// the rest sorted), as "key=value", quoting a value with a space in "'", and reads them
// back with MenuLineParameters.getParams.
func clickParams(q map[string]string, order []string) map[string]string {
	keys := slices.Clone(order)
	var rest []string
	for k := range q {
		if !slices.Contains(order, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	var parts []string
	for _, k := range append(keys, rest...) {
		v := q[k]
		if strings.Contains(v, " ") {
			v = "'" + v + "'"
		}
		parts = append(parts, k+"="+v)
	}
	return getParams([]rune(strings.Join(parts, " ")))
}

// getParams is SwiftBar 2.1.1's MenuLineParameters.getParams.
func getParams(chars []rune) map[string]string {
	params := map[string]string{}
	blank := func(c rune) bool { return c == ' ' || c == '\t' }
	pos := 0
	for pos < len(chars) {
		for pos < len(chars) && blank(chars[pos]) {
			pos++
		}
		if pos >= len(chars) {
			break
		}
		keyStart := pos
		for pos < len(chars) && chars[pos] != '=' {
			pos++
		}
		if pos >= len(chars) {
			break
		}
		key := strings.ToLower(strings.TrimSpace(string(chars[keyStart:pos])))
		pos++
		for pos < len(chars) && blank(chars[pos]) {
			pos++
		}
		var value strings.Builder
		if pos < len(chars) && (chars[pos] == '"' || chars[pos] == '\'') {
			quote, escaped := chars[pos], false
			for pos++; pos < len(chars); pos++ {
				c := chars[pos]
				if escaped {
					value.WriteRune('\\')
					value.WriteRune(c)
					escaped = false
				} else if c == '\\' {
					escaped = true
				} else if c == quote {
					pos++
					break
				} else {
					value.WriteRune(c)
				}
			}
		} else {
			start := pos
			for pos < len(chars) && !blank(chars[pos]) {
				pos++
			}
			value.WriteString(string(chars[start:pos]))
		}
		params[key] = value.String()
	}
	return params
}

func TestBaseline(t *testing.T) {
	dir := t.TempDir()
	if _, ok := LoadBaseline(dir); ok {
		t.Error("a baseline in an empty dir")
	}
	prs := []model.PR{{Number: 7, Section: model.SectionMine, Next: model.NextMe}}
	if err := SaveBaseline(dir, prs); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadBaseline(dir)
	if !ok || len(got) != 1 || got[0].Number != 7 {
		t.Errorf("baseline %+v, %v", got, ok)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("left %v", entries)
	}
	for _, data := range []string{"{", `{"format": 2, "prs": []}`} {
		if err := os.WriteFile(filepath.Join(dir, baselineFile), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := LoadBaseline(dir); ok {
			t.Errorf("%q read as a baseline", data)
		}
	}
	if err := SaveBaseline("", prs); err == nil {
		t.Error("saved without a dir")
	}
}
