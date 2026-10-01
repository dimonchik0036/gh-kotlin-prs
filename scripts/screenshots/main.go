// Command screenshots shrinks an SVG written by freeze: freeze embeds the whole JetBrains
// Mono font (~270 KB); this keeps only the glyphs the image uses, so the screenshot is a
// few KB and still renders the same everywhere (no external font, no fallback glyphs).
//
//	go run . in.svg out.svg
package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"html"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"

	"github.com/tdewolff/font"
)

var (
	embedded = regexp.MustCompile(`base64,([A-Za-z0-9+/=]+)\)`)
	text     = regexp.MustCompile(`>([^<]*)<`)
)

func main() {
	if len(os.Args) != 3 {
		fail("usage: screenshots <in.svg> <out.svg>")
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fail("screenshots:", err)
	}
}

func fail(args ...any) {
	_, _ = fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

func run(in, out string) error {
	svg, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	loc := embedded.FindSubmatchIndex(svg)
	if loc == nil {
		return fmt.Errorf("%s: no embedded font", in)
	}
	data, err := base64.StdEncoding.DecodeString(string(svg[loc[2]:loc[3]]))
	if err != nil {
		return err
	}
	sfnt, err := font.ParseSFNT(data, 0)
	if err != nil {
		return err
	}
	glyphs := []uint16{0} // .notdef
	for _, m := range text.FindAllSubmatch(svg[loc[1]:], -1) {
		for _, r := range html.UnescapeString(string(m[1])) {
			if r == '\n' {
				continue
			}
			g := sfnt.GlyphIndex(r)
			if g == 0 {
				return fmt.Errorf("the font has no glyph for %U %q", r, r)
			}
			glyphs = append(glyphs, g)
		}
	}
	slices.Sort(glyphs)
	subset, err := sfnt.Subset(slices.Compact(glyphs), font.SubsetOptions{Tables: font.KeepMinTables})
	if err != nil {
		return err
	}
	trimmed, err := keepModified(subset.Write(), sfnt.Tables["head"])
	if err != nil {
		return err
	}
	small := slices.Concat(svg[:loc[2]], []byte(base64.StdEncoding.EncodeToString(trimmed)), svg[loc[3]:])
	fitted, err := fit(small, sfnt)
	if err != nil {
		return err
	}
	return os.WriteFile(out, fitted, 0o644)
}

var (
	canvas   = regexp.MustCompile(`<svg width="([0-9.]+)" height="([0-9.]+)"`)
	backdrop = regexp.MustCompile(`<rect width="([0-9.]+)"`)
	fontSize = regexp.MustCompile(`font-size="([0-9.]+)px"`)
	line     = regexp.MustCompile(`(?s)<text x="([0-9.]+)px"[^>]*>(.*?)</text>`)
	tag      = regexp.MustCompile(`<[^>]*>`)
)

// fit makes the canvas at least as wide as the longest line plus the left padding on the
// right too, measured with the font's own advances, and adds a viewBox so viewers that
// draw the image smaller scale it instead of cropping it.
func fit(svg []byte, sfnt *font.SFNT) ([]byte, error) {
	c := canvas.FindSubmatch(svg)
	fs := fontSize.FindSubmatch(svg)
	if c == nil || fs == nil {
		return nil, fmt.Errorf("unexpected SVG layout")
	}
	width, _ := strconv.ParseFloat(string(c[1]), 64)
	size, _ := strconv.ParseFloat(string(fs[1]), 64)
	scale := size / float64(sfnt.UnitsPerEm())
	needed := width
	for _, m := range line.FindAllSubmatch(svg, -1) {
		x, _ := strconv.ParseFloat(string(m[1]), 64)
		end := x
		for _, r := range html.UnescapeString(string(tag.ReplaceAll(m[2], nil))) {
			end += float64(sfnt.GlyphAdvance(sfnt.GlyphIndex(r))) * scale
		}
		needed = max(needed, end+x)
	}
	w, h := string(c[1]), string(c[2])
	if needed > width {
		w = strconv.FormatFloat(math.Ceil(needed), 'f', 2, 64)
	}
	svg = canvas.ReplaceAll(svg, []byte(`<svg width="`+w+`" height="`+h+`" viewBox="0 0 `+w+` `+h+`"`))
	if needed > width {
		loc := backdrop.FindSubmatchIndex(svg)
		if loc == nil {
			return nil, fmt.Errorf("no background rect")
		}
		svg = slices.Concat(svg[:loc[2]], []byte(w), svg[loc[3]:])
	}
	return svg, nil
}

// keepModified puts the original font's modified date back into the head table (the
// writer stamps the current time) and fixes the checksums, so the output is reproducible.
func keepModified(ttf, origHead []byte) ([]byte, error) {
	if len(origHead) < 36 || len(ttf) < 12 {
		return nil, fmt.Errorf("unexpected font layout")
	}
	numTables := int(binary.BigEndian.Uint16(ttf[4:]))
	for i := range numTables {
		rec := 12 + 16*i
		if string(ttf[rec:rec+4]) != "head" {
			continue
		}
		offset := binary.BigEndian.Uint32(ttf[rec+8:])
		length := binary.BigEndian.Uint32(ttf[rec+12:])
		head := ttf[offset : offset+length]
		copy(head[28:36], origHead[28:36])
		binary.BigEndian.PutUint32(head[8:], 0) // checkSumAdjustment
		binary.BigEndian.PutUint32(ttf[rec+4:], checksum(ttf[offset:offset+(length+3)&^3]))
		binary.BigEndian.PutUint32(head[8:], 0xB1B0AFBA-checksum(ttf))
		return ttf, nil
	}
	return nil, fmt.Errorf("no head table")
}

func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var word [4]byte
		copy(word[:], b[i:min(i+4, len(b))])
		sum += binary.BigEndian.Uint32(word[:])
	}
	return sum
}
