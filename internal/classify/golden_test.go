package classify

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const goldenDir = "../../testdata/golden/model"

// TestGoldenModel: raw GraphQL response → model JSON, for every fixture in testdata/raw.
// Merged fixtures also get the row of the Recently merged section. The render package
// reads these goldens back as its input.
func TestGoldenModel(t *testing.T) {
	for _, n := range fixtureNumbers(t) {
		t.Run(n, func(t *testing.T) {
			resp := loadFixture(t, n)
			c := fixtureClassifier(resp.Viewer.Login)
			raw := resp.Repository.PullRequest
			checkGolden(t, "pr-"+n+".json", c.Show(raw))
			if raw.State == "MERGED" {
				checkGolden(t, "merged-"+n+".json", c.Merged(raw))
			}
		})
	}
}

func checkGolden(t *testing.T, name string, v any) {
	t.Helper()
	var got bytes.Buffer
	enc := json.NewEncoder(&got)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join(goldenDir, name)
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run `go test ./internal/classify -update`)", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("model differs from %s (run `go test ./internal/classify -update` and review the diff):\n%s", golden, got.String())
	}
}
