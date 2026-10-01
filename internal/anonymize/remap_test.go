package anonymize

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
)

const (
	headSHA   = "1111111111111111111111111111111111111111"
	mergeSHA  = "2222222222222222222222222222222222222222"
	remoteSHA = "3333333333333333333333333333333333333333"
)

// A PR with every identifier that could lead back to it.
const traceable = `{"data": {"viewer": {"login": "the_viewer"}, "repository": {"pullRequest": {
  "number": 77, "url": "https://github.com/JetBrains/kotlin/pull/77",
  "title": "KT-123, kt-456, KTIJ-123: Fix", "headRefName": "the_viewer/KT-123.fix", "headRefOid": "` + headSHA + `",
  "author": {"__typename": "User", "login": "the_viewer"},
  "comments": {"nodes": [{"author": {"__typename": "User", "login": "KotlinBuild"}, "isMinimized": false,
    "body": "[Corresponding Merge-Request](https://jetbrains.team/p/ij/reviews/12345/timeline) in ultimate is found.\n> Branch: ` + "`refs/merge/GITHUB-77/safe-merge`" + `\n> Commit: [2222222](https://github.com/JetBrains/kotlin/commit/` + mergeSHA + `)\n> Branch: ` + "`refs/merge/IJ-MR-12345/safe-merge`" + `\n> Commit: [3333333](https://jetbrains.team/p/ij/repositories/ultimate/revision/` + remoteSHA + `)\nSee KT-123."}]},
  "reviewThreads": {"nodes": [{"isResolved": false, "path": "compiler/fir/Resolve.kt", "firstComment": {"nodes": [{"author": {"__typename": "User", "login": "the_viewer"}, "body": "Why?", "url": "https://github.com/JetBrains/kotlin/pull/77#discussion_r2468013579"}]}}]},
  "history": {"nodes": [{"commit": {"message": "Fix the resolve of KT-123 for the_viewer\n\nDetails.\n\n^KT-123 Fixed\n^KTI-7\n^KTIJ-123 Obsolete\n^KT-456 and more text"}}]}
}}}}`

func remapped(t *testing.T, m *Map) string {
	t.Helper()
	a := New(config.Default())
	a.UseMap(m, func(int) bool { return false })
	doc, err := Decode([]byte(traceable))
	if err != nil {
		t.Fatal(err)
	}
	a.Collect(doc)
	if err := a.Rewrite(doc); err != nil {
		t.Fatal(err)
	}
	data, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRemapIdentifiers(t *testing.T) {
	m := NewMap()
	got := remapped(t, m)
	for _, original := range []string{"77", "123", "456", "7", "12345", "2468013579", headSHA, mergeSHA, remoteSHA, "2222222", "3333333", "compiler/fir", "Details", "more text"} {
		if regexp.MustCompile(`\b` + original + `\b`).MatchString(got) {
			t.Errorf("%s survived:\n%s", original, got)
		}
	}
	if m.PRs["77"] != firstFakePR || m.Issues["KT-123"] != firstFakeIssue || m.Issues["KT-456"] != firstFakeIssue+1 ||
		m.Issues["KTIJ-123"] != firstFakeIssue || m.Issues["KTI-7"] != firstFakeIssue {
		t.Errorf("map = %+v", m)
	}
	fakeMerge, fakeRemote, fakeSpaceID := fakeSHA(mergeSHA), fakeSHA(remoteSHA), fakeSpace("12345")
	for _, want := range []string{
		`"number": 90001`,
		`"url": "https://github.com/JetBrains/kotlin/pull/90001"`,
		`"title": "KT-990001, kt-990002, KTIJ-990001: Example change"`,
		`"message": "Commit message.\n\n^KT-990001 Fixed\n^KTI-990001\n^KTIJ-990001 Obsolete"`,
		`"headRefName": "topic/KT-990001-example"`,
		`"headRefOid": "` + fakeSHA(headSHA) + `"`,
		"https://jetbrains.team/p/ij/reviews/" + fakeSpaceID + "/timeline",
		"refs/merge/GITHUB-90001/safe-merge",
		"refs/merge/IJ-MR-" + fakeSpaceID + "/safe-merge",
		"[" + fakeMerge[:7] + "](https://github.com/JetBrains/kotlin/commit/" + fakeMerge + ")",
		"[" + fakeRemote[:7] + "](https://jetbrains.team/p/ij/repositories/ultimate/revision/" + fakeRemote + ")",
		"See KT-990001.",
		`"path": "` + fakePath("compiler/fir/Resolve.kt") + `"`,
		`"url": "https://github.com/JetBrains/kotlin/pull/90001#discussion_r` + fakeAnchor("2468013579") + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %s:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(fakePath("compiler/fir/Resolve.kt"), ".kt") || len(fakeMerge) != 40 || len(fakeSpaceID) != 6 {
		t.Errorf("fakes lost their shape: %q %q %q", fakePath("compiler/fir/Resolve.kt"), fakeMerge, fakeSpaceID)
	}
}

func TestRemapIsStable(t *testing.T) {
	m := NewMap()
	first := remapped(t, m)
	// A second run with the saved map, as on a refetch, yields the same fixture.
	if again := remapped(t, m); again != first {
		t.Error("remapping with the same map differs")
	}
	m.PRs["77"] = 90005
	if !strings.Contains(remapped(t, m), "/pull/90005") {
		t.Error("the map's number isn't used")
	}
}

func TestFakePRSkipsTakenNumbers(t *testing.T) {
	m := NewMap()
	m.PRs["10"] = firstFakePR
	taken := func(n int) bool { return n == firstFakePR+1 }
	if got := m.fakePR("11", taken); got != firstFakePR+2 {
		t.Errorf("fakePR = %d, want %d", got, firstFakePR+2)
	}
}

func TestMapRoundTrip(t *testing.T) {
	file := t.TempDir() + "/map.json"
	if _, err := LoadMap(file); !IsNotExist(err) {
		t.Fatalf("LoadMap of a missing file = %v", err)
	}
	m := NewMap()
	m.PRs["1"], m.Issues["KT-2"] = 90001, 990001
	if err := m.Save(file); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMap(file)
	if err != nil || got.PRs["1"] != 90001 || got.Issues["KT-2"] != 990001 {
		t.Errorf("LoadMap = %+v, %v", got, err)
	}
}

// Maps written before KTIJ and KTI keyed KT issues by number only.
func TestLoadMapMigratesBareKeys(t *testing.T) {
	file := t.TempDir() + "/map.json"
	if err := os.WriteFile(file, []byte(`{"prs": {}, "issues": {"123": 990001, "KTIJ-5": 990001}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadMap(file)
	if err != nil || m.Issues["KT-123"] != 990001 || m.Issues["KTIJ-5"] != 990001 || len(m.Issues) != 2 {
		t.Errorf("LoadMap = %+v, %v", m, err)
	}
}
