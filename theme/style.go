package theme

import (
	"sync"

	"charm.land/lipgloss/v2"
)

// A Style is how a piece of text is drawn.
type Style struct {
	Fg, Bg        string // "#rrggbb", or "0" to "255" for a terminal palette colour; "" leaves it alone
	Bold          bool
	Dim           bool
	Italic        bool
	Underline     bool
	Strikethrough bool
	Reverse       bool
}

// Over returns s laid on top of under: its colours where it has them,
// and the attributes of both.
func (s Style) Over(under Style) Style {
	if s.Fg == "" {
		s.Fg = under.Fg
	}
	if s.Bg == "" {
		s.Bg = under.Bg
	}
	s.Bold = s.Bold || under.Bold
	s.Dim = s.Dim || under.Dim
	s.Italic = s.Italic || under.Italic
	s.Underline = s.Underline || under.Underline
	s.Strikethrough = s.Strikethrough || under.Strikethrough
	s.Reverse = s.Reverse || under.Reverse
	return s
}

// Render returns text with the escape codes that draw it in this style.
// The zero Style returns text unchanged.
func (s Style) Render(text string) string {
	if s == (Style{}) {
		return text
	}
	return s.Lip().Render(text)
}

// built holds the lipgloss style made for each Style rendered so far.
var built sync.Map

// Lip is the style as a lipgloss.Style.
func (s Style) Lip() lipgloss.Style {
	if l, ok := built.Load(s); ok {
		return l.(lipgloss.Style)
	}
	l := lipgloss.NewStyle().
		TabWidth(lipgloss.NoTabConversion).
		Bold(s.Bold).
		Faint(s.Dim).
		Italic(s.Italic).
		Underline(s.Underline).
		Strikethrough(s.Strikethrough).
		Reverse(s.Reverse)
	if s.Fg != "" {
		l = l.Foreground(lipgloss.Color(s.Fg))
	}
	if s.Bg != "" {
		l = l.Background(lipgloss.Color(s.Bg))
	}
	built.Store(s, l)
	return l
}
