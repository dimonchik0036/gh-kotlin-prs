package classify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
)

// fixtureNow is the clock for fixture-based tests, shortly after the fixtures were fetched.
var fixtureNow = time.Date(2026, 10, 1, 16, 0, 0, 0, time.UTC)

const rawDir = "../../testdata/raw"

func loadFixture(t *testing.T, number string) *github.PRResponse {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rawDir, "pr-"+number+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data github.PRResponse `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Repository.PullRequest == nil {
		t.Fatalf("pr-%s.json has no pullRequest", number)
	}
	return &envelope.Data
}

func fixtureNumbers(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(rawDir, "pr-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	var out []string
	for _, f := range files {
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "pr-"), ".json"))
	}
	return out
}

// commentBody returns the body of the first comment by login that contains substr.
func commentBody(t *testing.T, number, login, substr string) string {
	t.Helper()
	return fixtureComment(t, number, login, substr).Body
}

func fixtureComment(t *testing.T, number, login, substr string) github.Comment {
	t.Helper()
	for _, c := range loadFixture(t, number).Repository.PullRequest.Comments.Nodes {
		if c.Author.LoginOrEmpty() == login && strings.Contains(c.Body, substr) {
			return c
		}
	}
	t.Fatalf("pr-%s: no comment by %s containing %q", number, login, substr)
	return github.Comment{}
}

func fixtureClassifier(viewer string) *Classifier {
	return &Classifier{Config: config.Default(), Viewer: viewer, Now: fixtureNow}
}
