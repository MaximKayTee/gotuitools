// Package text lays out and formats strings for the terminal: widths in
// cells, ANSI-aware.
package text

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Width is the display width of s in cells, escapes not counted.
func Width(s string) int { return ansi.StringWidth(s) }

// Spaces is n spaces; "" for n <= 0.
func Spaces(n int) string { return strings.Repeat(" ", max(n, 0)) }

// TruncateStart cuts s to w cells from the left, starting with "…"
// when cut: for paths.
func TruncateStart(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return "…" + ansi.TruncateLeft(s, ansi.StringWidth(s)-w+1, "")
}

// Truncate cuts s to w cells, ending in "…" when cut.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// Cut cuts s to w cells, no ellipsis.
func Cut(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "")
}

// Fit pads or truncates s to exactly w cells.
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = Truncate(s, w)
	return s + Spaces(w-ansi.StringWidth(s))
}

// FitLeft is Fit, right-aligned.
func FitLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = Truncate(s, w)
	return Spaces(w-ansi.StringWidth(s)) + s
}

// Pad pads s with spaces to at least w cells.
func Pad(s string, w int) string { return s + Spaces(w-ansi.StringWidth(s)) }

// Center pads s on both sides to w cells; truncates when wider.
func Center(s string, w int) string {
	s = Truncate(s, w)
	gap := w - ansi.StringWidth(s)
	return Spaces(gap/2) + s + Spaces(gap-gap/2)
}

// Block pads or cuts lines to h lines of exactly w cells.
func Block(lines []string, w, h int) []string {
	out := make([]string, max(h, 0))
	for i := range out {
		if i < len(lines) {
			out[i] = Fit(lines[i], w)
		} else {
			out[i] = Spaces(w)
		}
	}
	return out
}

// Row is left and right in w cells: right flush right, left truncated
// to make room, at least one space between.
func Row(left, right string, w int) string {
	rw := ansi.StringWidth(right)
	if rw >= w {
		return FitLeft(right, w)
	}
	if right == "" {
		return Fit(left, w)
	}
	return Fit(left, w-rw-1) + " " + right
}

// Columns joins panes of equal height side by side with sep between.
func Columns(h int, sep string, panes ...[]string) []string {
	out := make([]string, max(h, 0))
	var b strings.Builder
	for y := range out {
		b.Reset()
		for i, p := range panes {
			if i > 0 {
				b.WriteString(sep)
			}
			if y < len(p) {
				b.WriteString(p[y])
			}
		}
		out[y] = b.String()
	}
	return out
}

// Wrap breaks s into lines of at most w cells, trailing spaces trimmed.
// Line breaks in s are kept.
func Wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	lines := strings.Split(ansi.Wrap(s, w, ""), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// WrapN breaks s into at most n lines of w cells. Whitespace runs
// collapse, paragraphs stay apart, blank-line runs become one; the last
// line ends in "…" when more would follow.
func WrapN(s string, w, n int) []string {
	if w <= 0 || n <= 0 {
		return nil
	}
	var lines []string
	blank := false
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		para = strings.Join(strings.Fields(para), " ")
		if para == "" {
			blank = len(lines) > 0
			continue
		}
		if blank {
			lines = append(lines, "")
			blank = false
		}
		for _, l := range strings.Split(ansi.Wrap(para, w, ""), "\n") {
			lines = append(lines, strings.TrimRight(l, " "))
		}
	}
	if len(lines) > n {
		lines = lines[:n]
		lines[n-1] = strings.TrimRight(ansi.Truncate(lines[n-1], max(w-1, 0), ""), " ") + "…"
	}
	return lines
}

// On puts background bg under text, again after each SGR reset in it.
// nil bg returns text.
func On(text string, bg color.Color) string {
	if bg == nil {
		return text
	}
	r, g, b, _ := bg.RGBA()
	set := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	return set + strings.NewReplacer("\x1b[0m", "\x1b[0m"+set, "\x1b[m", "\x1b[m"+set).Replace(text) + "\x1b[m"
}

// Facts joins the non-empty parts with " · ".
func Facts(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// Clean makes one line safe for a terminal: tab is a space, newline is
// ↵, other control characters (C0, DEL, C1) print as \xNN.
func Clean(s string) string {
	if !strings.ContainsFunc(s, IsControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r == '\n':
			b.WriteString("↵")
		case IsControl(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CleanText is Clean on every line, line breaks kept.
func CleanText(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = Clean(l)
	}
	return strings.Join(lines, "\n")
}

// IsControl reports a C0, DEL or C1 control character.
func IsControl(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }

// OneLine joins whitespace runs into single spaces and cleans the result.
func OneLine(s string) string { return Clean(strings.Join(strings.Fields(s), " ")) }

// FirstLine is s up to its first line break.
func FirstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// Strip removes escape sequences.
func Strip(s string) string { return ansi.Strip(s) }
