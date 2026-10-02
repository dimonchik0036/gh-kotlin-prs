package swiftbar

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/notify"
)

// baselineFile holds the rows the plugin last notified up to, in the cache dir, and when
// their data was fetched: a run with newer data notifies of what changed since.
const baselineFile = "swiftbar-baseline.json"

// baselineFormat is the version of the file: 2 added FetchedAt. A format-1 file (none of
// v0.5.2 and older) reads with a zero FetchedAt; another one is no baseline.
const baselineFormat = 2

// Baseline is the rows of the data notified up to, and when that data was fetched (the
// oldest of its parts).
type Baseline struct {
	PRs       []model.PR
	FetchedAt time.Time
}

type baselineDoc struct {
	Format    int        `json:"format"`
	FetchedAt time.Time  `json:"fetchedAt,omitzero"`
	PRs       []model.PR `json:"prs"`
}

// LoadBaseline is the baseline; false when there's none to compare with.
func LoadBaseline(dir string) (Baseline, bool) {
	data, err := os.ReadFile(filepath.Join(dir, baselineFile))
	if err != nil {
		return Baseline{}, false
	}
	var b baselineDoc
	if err := json.Unmarshal(data, &b); err != nil || (b.Format != baselineFormat && b.Format != 1) {
		return Baseline{}, false
	}
	return Baseline{PRs: b.PRs, FetchedAt: b.FetchedAt}, true
}

// SaveBaseline replaces the baseline atomically (a temp file renamed over it).
func SaveBaseline(dir string, b Baseline) error {
	if dir == "" {
		return errors.New("no cache dir")
	}
	data, err := json.Marshal(baselineDoc{Format: baselineFormat, FetchedAt: b.FetchedAt.UTC(), PRs: b.PRs})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(dir, baselineFile))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// NotificationArgv is how a plugin run shows an event: through SwiftBar when it runs the
// plugin, else through AppleScript. SwiftBar's notification opens the interactive view on
// the event's PR on a click, with gh ("gh" when empty), as the menu's items do; an event
// about no PR opens its page.
func NotificationArgv(plugin, gh string, e notify.Event) []string {
	if plugin == "" {
		script := fmt.Sprintf("display notification %s with title %s", appleString(e.Body), appleString(e.Title))
		return []string{"osascript", "-e", script}
	}
	var q []string
	add := func(key, value string) {
		q = append(q, key+"="+strings.ReplaceAll(url.QueryEscape(value), "+", "%20"))
	}
	add("plugin", PluginName(plugin))
	// SwiftBar shows the title and the body with each "+" a space, and on a click reads
	// every parameter of the URL as an item's, the way it reads a menu line: a value with
	// a space ends at its next "'", which would cut bash and its params out. So spaces go
	// as "+", the text's own "+" as a fullwidth one, and a quote can't start a value.
	for _, kv := range [][2]string{{"title", e.Title}, {"body", e.Body}} {
		v := strings.ReplaceAll(strings.Join(strings.Fields(kv[1]), " "), "+", "\uff0b")
		if strings.HasPrefix(v, "'") || strings.HasPrefix(v, `"`) {
			v = "\u200b" + v
		}
		q = append(q, kv[0]+"="+url.QueryEscape(v))
	}
	switch {
	case e.Number != 0:
		for i, w := range openWords(gh, e.Number, "") {
			if i == 0 {
				add("bash", w)
			} else {
				add(fmt.Sprintf("param%d", i), w)
			}
		}
		add("terminal", "true")
	case e.URL != "":
		add("href", e.URL)
	}
	return []string{"open", "-g", "swiftbar://notify?" + strings.Join(q, "&")}
}

// PluginName is SwiftBar's name for the plugin at path: its file name up to the first dot.
func PluginName(path string) string {
	name, _, _ := strings.Cut(filepath.Base(path), ".")
	return name
}

// appleString is s as an AppleScript string literal.
func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s) + `"`
}
