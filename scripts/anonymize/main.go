// Command anonymize rewrites raw GraphQL fixtures for committing (see internal/anonymize).
// All files of a batch go through one run, so pseudonyms agree across fixtures. The
// real → fake number map is read from and written back to -map, a local file kept out of git.
//
//	go run ./scripts/anonymize -map testdata/.fixture-map.json -out testdata/raw /tmp/raw/pr-*.json
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/anonymize"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
)

func main() {
	out := flag.String("out", "", "directory to write the anonymized files to")
	mapFile := flag.String("map", "", "real → fake number map, created if missing")
	flag.Parse()
	if *out == "" || *mapFile == "" || flag.NArg() == 0 {
		fail(2, "usage: anonymize -map <map.json> -out <dir> <response.json>...")
	}
	if err := run(*out, *mapFile, flag.Args()); err != nil {
		fail(1, "anonymize:", err)
	}
}

func fail(code int, args ...any) {
	_, _ = fmt.Fprintln(os.Stderr, args...)
	os.Exit(code)
}

func run(out, mapFile string, files []string) error {
	m, err := anonymize.LoadMap(mapFile)
	if anonymize.IsNotExist(err) {
		m, err = anonymize.NewMap(), nil
	}
	if err != nil {
		return err
	}
	a := anonymize.New(config.Default())
	// Without the map's entries, a fake number could already name a committed fixture: one
	// in out, or a derived one next to it (testdata/demo), which the map never has.
	a.UseMap(m, func(n int) bool {
		for _, dir := range []string{out, filepath.Join(filepath.Dir(out), "demo")} {
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("pr-%d.json", n))); err == nil {
				return true
			}
		}
		return false
	})

	files = slices.Sorted(slices.Values(files))
	docs := make([]any, len(files))
	for i, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if docs[i], err = anonymize.Decode(data); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		a.Collect(docs[i])
	}
	for i, f := range files {
		if err := a.Rewrite(docs[i]); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		data, err := anonymize.Encode(docs[i])
		if err != nil {
			return err
		}
		name := anonymize.FileName(docs[i])
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", filepath.Join(filepath.Base(out), name))
	}
	return m.Save(mapFile)
}
