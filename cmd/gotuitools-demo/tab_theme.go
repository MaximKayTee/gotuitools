package main

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// uiScopes are the scopes gotuitools components draw with.
var uiScopes = []string{
	"ui.background", "ui.text", "ui.text.focus", "ui.text.inactive",
	"ui.selection", "ui.cursor", "ui.statusline", "ui.statusline.key",
	"ui.window", "ui.window.focus", "ui.popup", "ui.menu", "ui.menu.selected",
	"error", "warning", "info", "hint",
}

type themeTab struct{ m *model }

func (t *themeTab) name() string           { return "theme" }
func (t *themeTab) title() string          { return "Theme" }
func (t *themeTab) update(tea.Msg) tea.Cmd { return nil }
func (t *themeTab) status() string         { return t.m.th.Name() }
func (t *themeTab) commands() []command.Command {
	return []command.Command{
		{Name: "theme.next", Title: "Next theme", Key: "t", Run: do(func() { t.step(1) })},
		{Name: "theme.prev", Title: "Previous theme", Key: "T", Run: do(func() { t.step(-1) })},
	}
}

// step sets the theme setting to the next or previous theme name.
func (t *themeTab) step(d int) {
	names := themeNames()
	i := slices.Index(names, t.m.store.Get().Theme)
	t.m.set("theme", names[(i+d+len(names))%len(names)])
}

func (t *themeTab) view(w, _ int) string {
	th := t.m.th
	head := th.Lip("ui.text.focus")
	lines := []string{head.Render("Scopes"), ""}
	for _, s := range uiScopes {
		lines = append(lines, text.Pad(th.Render("ui.text.inactive", s), 20)+
			text.Pad(th.Render(s, " Sample "), 10)+" "+describe(th.Style(s)))
	}
	lines = append(lines, "", head.Render("Palette"), "")
	lines = append(lines, swatches(th, w)...)
	return strings.Join(lines, "\n")
}

// describe is a style as fg, bg and modifiers.
func describe(s theme.Style) string {
	var parts []string
	if s.Fg != "" {
		parts = append(parts, "fg "+s.Fg)
	}
	if s.Bg != "" {
		parts = append(parts, "bg "+s.Bg)
	}
	for _, mod := range []struct {
		on   bool
		name string
	}{{s.Bold, "bold"}, {s.Dim, "dim"}, {s.Italic, "italic"}, {s.Underline, "underline"},
		{s.Strikethrough, "strikethrough"}, {s.Reverse, "reverse"}} {
		if mod.on {
			parts = append(parts, mod.name)
		}
	}
	if len(parts) == 0 {
		return "None"
	}
	return strings.Join(parts, " ")
}

// swatches are the colours of the theme's own scopes, as many to a row as fit.
func swatches(th theme.Theme, w int) []string {
	var colours []string
	for _, s := range th.Scopes() {
		st := th.Style(s)
		for _, c := range []string{st.Fg, st.Bg} {
			if c != "" {
				colours = append(colours, c)
			}
		}
	}
	slices.Sort(colours)
	colours = slices.Compact(colours)
	cells := make([]string, len(colours))
	for i, c := range colours {
		cells[i] = theme.Style{Bg: c}.Render("    ") + " " + text.Pad(c, 8)
	}
	per := max(w/14, 1)
	var rows []string
	for i := 0; i < len(cells); i += per {
		rows = append(rows, strings.Join(cells[i:min(i+per, len(cells))], " "))
	}
	return rows
}
