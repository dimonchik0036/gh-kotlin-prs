package demo

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
)

func TestLoad(t *testing.T) {
	c, err := Load("../../testdata/raw")
	if err != nil {
		t.Fatal(err)
	}
	if c.viewer != "dimonchik0036" || len(c.prs) == 0 {
		t.Errorf("viewer %q, %d PRs", c.viewer, len(c.prs))
	}
	if c.Now().Minute() != 0 || c.Now().Second() != 0 || c.Now().Before(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("clock %v, want a full hour after the fixtures", c.Now())
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("an empty directory loads")
	}
	if _, err := Load("../../testdata/raw" + string(filepath.ListSeparator) + "../../testdata/raw"); err == nil || !strings.Contains(err.Error(), "in another fixture too") {
		t.Errorf("the same PR twice: %v", err)
	}
}

// The demo client answers internal/github's real queries.
func TestQueries(t *testing.T) {
	c, err := Load("../../testdata/raw")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	search, err := github.SearchSections(ctx, c, "JetBrains/kotlin", "KotlinBuild", c.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if search.Viewer != "dimonchik0036" || len(search.Mine) == 0 || len(search.Personal) == 0 || len(search.Reviewed) == 0 {
		t.Errorf("search = %+v", search)
	}
	for _, n := range search.Mine {
		if c.raw[n].State != "OPEN" || c.raw[n].Author.Login != search.Viewer {
			t.Errorf("#%d isn't an open PR of the viewer", n)
		}
	}
	for _, pr := range search.Merged {
		if pr.MergedAt == nil || pr.MergedAt.Before(c.Now().Add(-24*time.Hour)) {
			t.Errorf("#%d merged outside the window", pr.Number)
		}
	}
	prs, _, err := github.FetchPRs(ctx, c, "JetBrains", "kotlin", search.Mine, nil)
	if err != nil || len(prs) != len(search.Mine) {
		t.Errorf("FetchPRs: %d PRs, %v", len(prs), err)
	}
	one, err := github.FetchPR(ctx, c, "JetBrains", "kotlin", search.Mine[0])
	if err != nil || one.Repository.PullRequest.Number != search.Mine[0] || one.Viewer.Login != search.Viewer {
		t.Errorf("FetchPR = %+v, %v", one, err)
	}
	if _, err := github.FetchPR(ctx, c, "JetBrains", "kotlin", 1); err == nil {
		t.Error("FetchPR of a missing fixture succeeded")
	}
}
