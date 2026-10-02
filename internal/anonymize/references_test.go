package anonymize

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const root = "../.."

// prReference is a link or a reference to a PR of the repository.
var prReference = regexp.MustCompile(`JetBrains/kotlin#(\d+)|github\.com/JetBrains/kotlin/pull/(\d+)`)

// Outside the fixtures, the repository refers only to fake PRs: the fixtures' 900xx, or the
// small made-up numbers of unit tests. Real ones are four digits and up.
func TestNoRealPRReferences(t *testing.T) {
	for _, file := range trackedTextFiles(t) {
		if strings.HasPrefix(file.path, "testdata/raw/") || strings.HasPrefix(file.path, "testdata/demo/") {
			continue
		}
		for _, m := range prReference.FindAllSubmatchIndex(file.data, -1) {
			digits := m[2:4]
			if digits[0] < 0 {
				digits = m[4:6]
			}
			n, _ := strconv.Atoi(string(file.data[digits[0]:digits[1]]))
			if n >= 1000 && n/100 != 900 {
				t.Errorf("%s:%d: %s refers to a real PR; use a fake number (900xx)", file.path, file.line(m[0]), file.data[m[0]:m[1]])
			}
		}
	}
}

// With the local real → fake map (testdata/.fixture-map.json, never committed), no tracked
// file, fixtures included, names the fixtures' real PRs or issues. Without it, nothing to check.
func TestNoRealFixtureNumbers(t *testing.T) {
	m, err := LoadMap(filepath.Join(root, "testdata", ".fixture-map.json"))
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("no local fixture map")
	}
	if err != nil {
		t.Fatal(err)
	}
	var prs, issues []string
	for n := range m.PRs {
		prs = append(prs, regexp.QuoteMeta(n))
	}
	for id := range m.Issues {
		issues = append(issues, regexp.QuoteMeta(id))
	}
	var alternatives []string
	if len(prs) > 0 {
		slices.Sort(prs)
		alternatives = append(alternatives, `(?:#|/pull/|--pr\s+|\bPR\s+|"number":\s*)(?:`+strings.Join(prs, "|")+`)\b`)
	}
	if len(issues) > 0 {
		slices.Sort(issues)
		alternatives = append(alternatives, `\b(?:`+strings.Join(issues, "|")+`)\b`)
	}
	if len(alternatives) == 0 {
		t.Skip("empty fixture map")
	}
	known := regexp.MustCompile(`(?i)` + strings.Join(alternatives, "|"))
	for _, file := range trackedTextFiles(t) {
		for _, loc := range known.FindAllIndex(file.data, -1) {
			t.Errorf("%s:%d: %s is a real number from the fixture map", file.path, file.line(loc[0]), file.data[loc[0]:loc[1]])
		}
	}
}

type trackedFile struct {
	path string
	data []byte
}

func (f trackedFile) line(offset int) int { return bytes.Count(f.data[:offset], []byte("\n")) + 1 }

// trackedTextFiles are the files git tracks, binaries (a NUL byte) left out.
func trackedTextFiles(t *testing.T) []trackedFile {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	var files []trackedFile
	for _, path := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		data, err := os.ReadFile(filepath.Join(root, path))
		if errors.Is(err, fs.ErrNotExist) {
			continue // deleted, not yet staged
		}
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.ContainsRune(data, 0) {
			files = append(files, trackedFile{path: path, data: data})
		}
	}
	if len(files) == 0 {
		t.Fatal("git tracks no files")
	}
	return files
}
