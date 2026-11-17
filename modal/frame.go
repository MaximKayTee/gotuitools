package modal

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/list"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// Box is how Frame draws a bordered box.
type Box struct {
	Title string
	// Keys is a hint drawn in the bottom border: "enter Choose  esc Close".
	Keys    string
	Focused bool
	// Pos marks the scroll thumb on the right border; zero none.
	Pos list.Pos
}

// Frame draws body in a rounded border w cells wide, len(body)+2 rows:
// the title in the top border, Keys in the bottom one, the scroll thumb
// on the right. Styles: ui.window, ui.window.focus, ui.popup, ui.text,
// ui.text.inactive.
func Frame(th theme.Theme, b Box, body []string, w int) []string {
	if w < 2 {
		return nil
	}
	border := th.Lip("ui.window")
	if b.Focused {
		border = th.Lip("ui.window.focus")
	}
	edge := lipgloss.RoundedBorder()
	iw := w - 2
	out := make([]string, 0, len(body)+2)
	out = append(out, borderLine(border, edge.TopLeft, edge.Top, edge.TopRight, th.Render("ui.popup", " "+b.Title+" "), b.Title != "", iw))
	marks := b.Pos.Rows(len(body))
	for i, l := range body {
		right := border.Render(edge.Right)
		if i < len(marks) && marks[i] {
			right = border.Render(list.Thumb)
		}
		out = append(out, border.Render(edge.Left)+text.Fit(l, iw)+right)
	}
	out = append(out, borderLine(border, edge.BottomLeft, edge.Bottom, edge.BottomRight, th.Render("ui.text.inactive", " "+b.Keys+" "), b.Keys != "", iw))
	return out
}

func borderLine(st lipgloss.Style, left, fill, right, label string, has bool, iw int) string {
	if !has || iw < 4 {
		return st.Render(left + strings.Repeat(fill, iw) + right)
	}
	label = ansi.Truncate(label, iw-2, "…")
	rest := iw - 1 - ansi.StringWidth(label)
	return st.Render(left+fill) + label + st.Render(strings.Repeat(fill, max(rest, 0))+right)
}
