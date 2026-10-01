package anonymize

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Fake identifiers start far above the real ones, so they can't be mistaken for them.
const (
	firstFakePR    = 90001
	firstFakeIssue = 990001
	firstFakeSpace = 100000
)

// Map keeps the real → fake numbers of the committed fixtures. It lives in a local,
// gitignored file: committing it would undo the remapping. Issues are keyed by their
// upper-case ID (KT-123); each project gets its own fake numbers.
type Map struct {
	PRs    map[string]int `json:"prs"`
	Issues map[string]int `json:"issues"`
}

func NewMap() *Map { return &Map{PRs: map[string]int{}, Issues: map[string]int{}} }

// LoadMap reads the map; a missing file is reported as fs.ErrNotExist.
func LoadMap(file string) (*Map, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	m := NewMap()
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	// Maps written before other projects were recognized keyed KT issues by number only.
	for k, v := range m.Issues {
		if _, err := strconv.Atoi(k); err == nil {
			delete(m.Issues, k)
			m.Issues["KT-"+k] = v
		}
	}
	return m, nil
}

func (m *Map) Save(file string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o644)
}

// IsNotExist reports whether LoadMap failed because there is no map yet.
func IsNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// fakePR returns the fake number of a real PR, assigning the next free one, above
// every number in taken, on first sight.
func (m *Map) fakePR(real string, taken func(int) bool) int {
	if n, ok := m.PRs[real]; ok {
		return n
	}
	n := firstFakePR
	used := slices.Collect(maps.Values(m.PRs))
	for slices.Contains(used, n) || taken(n) {
		n++
	}
	m.PRs[real] = n
	return n
}

// fakeIssue returns the fake number of a real issue of a project, assigning the next one
// of that project on first sight.
func (m *Map) fakeIssue(project, number string) int {
	project = strings.ToUpper(project)
	if n, ok := m.Issues[project+"-"+number]; ok {
		return n
	}
	n := firstFakeIssue
	for id := range m.Issues {
		if strings.HasPrefix(id, project+"-") {
			n++
		}
	}
	m.Issues[project+"-"+number] = n
	return n
}

var (
	prRef    = regexp.MustCompile(`(/pull/|GITHUB-)(\d+)\b`)
	spaceRef = regexp.MustCompile(`(jetbrains\.team/p/[a-z]+/reviews/|IJ-MR-)(\d+)\b`)
	// Comment anchors in GitHub URLs; the API resolves them to their PR.
	anchorRef  = regexp.MustCompile(`(#discussion_r|#issuecomment-|#pullrequestreview-)(\d+)\b`)
	shortLink  = regexp.MustCompile(`\[([0-9a-f]{7,39})]\(([^)\s]*?)([0-9a-f]{40})\)`)
	fullSHA    = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
	fakeSHAKey = "gh-kotlin-prs fixture sha "
)

// fakeSHA is a stable stand-in of the same shape for a commit SHA.
func fakeSHA(real string) string {
	sum := sha256.Sum256([]byte(fakeSHAKey + real))
	return hex.EncodeToString(sum[:])[:40]
}

// fakeSpace is a stable stand-in for a Space merge-request number.
func fakeSpace(real string) string {
	sum := sha256.Sum256([]byte("gh-kotlin-prs fixture review " + real))
	n, _ := strconv.ParseUint(hex.EncodeToString(sum[:4]), 16, 64)
	return strconv.FormatUint(firstFakeSpace+n%900000, 10)
}

// fakeAnchor is a stable stand-in for a comment id.
func fakeAnchor(real string) string {
	sum := sha256.Sum256([]byte("gh-kotlin-prs fixture comment " + real))
	n, _ := strconv.ParseUint(hex.EncodeToString(sum[:4]), 16, 64)
	return strconv.FormatUint(1_000_000_000+n%1_000_000_000, 10)
}

// fakePath keeps the extension of a file path and hides the rest.
func fakePath(real string) string {
	sum := sha256.Sum256([]byte("gh-kotlin-prs fixture path " + real))
	return "src/f" + hex.EncodeToString(sum[:4]) + path.Ext(real)
}

// remapText replaces every traceable identifier in a string: issue numbers, PR numbers
// in URLs and merge refs, Space merge requests and commit SHAs.
func (a *Anonymizer) remapText(s string) string {
	s = a.issues.ReplaceAllStringFunc(s, func(m string) string {
		g := a.issues.FindStringSubmatch(m)
		return g[1] + "-" + strconv.Itoa(a.fakes.fakeIssue(g[1], g[2]))
	})
	s = prRef.ReplaceAllStringFunc(s, func(m string) string {
		g := prRef.FindStringSubmatch(m)
		if n, ok := a.fakes.PRs[g[2]]; ok {
			return g[1] + strconv.Itoa(n)
		}
		return m
	})
	s = spaceRef.ReplaceAllStringFunc(s, func(m string) string {
		g := spaceRef.FindStringSubmatch(m)
		return g[1] + fakeSpace(g[2])
	})
	s = anchorRef.ReplaceAllStringFunc(s, func(m string) string {
		g := anchorRef.FindStringSubmatch(m)
		a.anchors[g[2]] = true
		return g[1] + fakeAnchor(g[2])
	})
	s = shortLink.ReplaceAllStringFunc(s, func(m string) string {
		g := shortLink.FindStringSubmatch(m)
		if !strings.HasPrefix(g[3], g[1]) {
			return m
		}
		fake := fakeSHA(g[3])
		a.shas[g[3]], a.fakeSHAs[fake] = true, true
		return "[" + fake[:len(g[1])] + "](" + g[2] + fake + ")"
	})
	return fullSHA.ReplaceAllStringFunc(s, func(m string) string {
		a.shas[m] = true
		if a.fakeSHAs[m] {
			return m
		}
		f := fakeSHA(m)
		a.fakeSHAs[f] = true
		return f
	})
}

// collectIdentifiers assigns fake numbers to the PR and the KT issues of one object,
// in the order Collect sees them.
func (a *Anonymizer) collectIdentifiers(obj map[string]any) {
	if _, ok := obj["headRefName"]; ok {
		if n, ok := obj["number"].(json.Number); ok {
			a.fakes.fakePR(n.String(), a.taken)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		if s, ok := obj[k].(string); ok {
			for _, g := range a.issues.FindAllStringSubmatch(s, -1) {
				a.fakes.fakeIssue(g[1], g[2])
			}
		}
	}
}

// remap rewrites the identifiers of one object: the PR's own number, file paths of
// review threads, and every string field.
func (a *Anonymizer) remap(obj map[string]any) {
	if _, ok := obj["headRefName"]; ok {
		if n, ok := obj["number"].(json.Number); ok {
			obj["number"] = json.Number(strconv.Itoa(a.fakes.fakePR(n.String(), a.taken)))
		}
	}
	if _, ok := obj["isResolved"]; ok {
		if p, ok := obj["path"].(string); ok {
			obj["path"] = fakePath(p)
		}
	}
	for k, v := range obj {
		if s, ok := v.(string); ok && k != "login" && k != "__typename" {
			obj[k] = a.remapText(s)
		}
	}
}

// checkIdentifiers fails if a real PR number, KT issue number or commit SHA survived.
func (a *Anonymizer) checkIdentifiers(text string) []string {
	var leaked []string
	for number := range a.fakes.PRs {
		if regexp.MustCompile(`(/pull/|GITHUB-|"number": )` + number + `\b`).MatchString(text) {
			leaked = append(leaked, "PR "+number)
		}
	}
	for id := range a.fakes.Issues {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(id) + `\b`).MatchString(text) {
			leaked = append(leaked, id)
		}
	}
	for id := range a.anchors {
		if regexp.MustCompile(`(#discussion_r|#issuecomment-|#pullrequestreview-)` + id + `\b`).MatchString(text) {
			leaked = append(leaked, "comment "+id)
		}
	}
	for sha := range a.shas {
		if !a.fakeSHAs[sha] && strings.Contains(text, sha) {
			leaked = append(leaked, "commit "+sha[:7])
		}
	}
	return leaked
}
