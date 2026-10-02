package render

import (
	"io"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// Linked text is underlined and, unless it has a color of its own, in the terminal
// theme's blue (an ANSI 16-color, so it follows the user's palette). It is written as
// SGR sequences directly: a lipgloss style underlines rune by rune.

// link wraps text in an OSC 8 hyperlink to url when links are on, and styles it so it
// looks clickable. Without links the text stays plain: no fake links. Widths and
// truncation treat the escapes as zero-width.
func link(on bool, url, text string) string {
	return styledLink(on, url, segment{text: text})
}

// segment is part of a link with a style of its own (the bold header, a colored run
// symbol). A symbol segment is clickable but not styled as a link: it keeps exactly its
// own style, so the underline doesn't run into the icon.
type segment struct {
	text   string
	style  lipgloss.Style
	symbol bool
}

func (s segment) render() string { return s.style.Render(s.text) }

// styledLink links one or more segments to url; the hyperlink spans all of them. Text
// segments keep their bold and faint and gain the underline, and take the link color
// only if they have no color of their own. Symbol segments and blanks stay as they are.
// Without links, the segments render in their own styles.
func styledLink(on bool, url string, segments ...segment) string {
	var b strings.Builder
	if !on || url == "" {
		for _, s := range segments {
			b.WriteString(s.render())
		}
		return b.String()
	}
	b.WriteString(ansi.SetHyperlink(url))
	for _, s := range segments {
		switch {
		case s.text == "":
		case s.symbol || strings.TrimSpace(s.text) == "":
			b.WriteString(styled(sgrOf(s.style, false), s.text))
		default:
			b.WriteString(styled(sgrOf(s.style, true), s.text))
		}
	}
	b.WriteString(ansi.ResetHyperlink())
	return b.String()
}

// styled writes text in an SGR style, or as is when the style is empty.
func styled(style ansi.Style, text string) string {
	if len(style) == 0 {
		return text
	}
	return style.String() + text + ansi.ResetStyle
}

// sgrOf is the SGR of a lipgloss style (bold, faint, a 16-color foreground), plus the
// link's underline and default blue for linked text.
func sgrOf(style lipgloss.Style, linkText bool) ansi.Style {
	var s ansi.Style
	if linkText {
		s = s.Underline(true)
	}
	if style.GetBold() {
		s = s.Bold()
	}
	if style.GetFaint() {
		s = s.Faint()
	}
	if fg, ok := style.GetForeground().(ansi.BasicColor); ok {
		s = s.ForegroundColor(fg)
	} else if linkText {
		s = s.ForegroundColor(ansi.Blue)
	}
	return s
}

// NewWriter downsamples styles to what w supports. Without a terminal it strips them,
// and hyperlinks with them, unless hyperlinks are on: then only the colors and
// attributes go.
func NewWriter(w io.Writer, environ []string, hyperlinks bool) io.Writer {
	cw := colorprofile.NewWriter(w, environ)
	if hyperlinks && cw.Profile == colorprofile.NoTTY {
		return sgrStripper{w}
	}
	return cw
}

var sgr = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// StripStyles drops the SGR sequences of s (colors, bold, faint) and keeps everything
// else: its hyperlinks stay.
func StripStyles(s string) string { return sgr.ReplaceAllString(s, "") }

// sgrStripper drops SGR sequences (colors, bold, faint) and keeps everything else.
type sgrStripper struct{ w io.Writer }

func (s sgrStripper) Write(p []byte) (int, error) {
	if _, err := s.w.Write(sgr.ReplaceAll(p, nil)); err != nil {
		return 0, err
	}
	return len(p), nil
}
