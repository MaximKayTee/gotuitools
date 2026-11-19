package main

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/input"
	"github.com/MaximKayTee/gotuitools/layout"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/sys"
	"github.com/MaximKayTee/gotuitools/text"
)

type inputTab struct {
	m       *model
	lines   [3]input.Line
	at      int
	editing bool
}

func newInputTab(m *model) *inputTab { return &inputTab{m: m} }

func (t *inputTab) name() string     { return "input" }
func (t *inputTab) title() string    { return "Input" }
func (t *inputTab) capturing() bool  { return t.editing }
func (t *inputTab) scrollBy(n int)   { t.at = layout.Clamp(t.at+n, 0, len(t.lines)-1) }
func (t *inputTab) titles() []string { return []string{"Plain", "Limit", "Mask"} }

func (t *inputTab) status() string {
	l := &t.lines[t.at]
	return text.Facts(text.Plural(int64(utf8.RuneCountInString(l.String())), "rune"),
		"cursor "+text.Thousands(int64(l.Cursor())))
}

func (t *inputTab) commands() []command.Command {
	return []command.Command{
		{Name: "input.edit", Title: "Edit", Key: "enter i", Run: do(func() { t.editing = true })},
		{Name: "input.copy", Title: "Copy", Key: "y",
			Run: func([]string) tea.Cmd { return sys.Copy(t.lines[t.at].String()) }},
		{Name: "input.clear", Title: "Clear", Key: "x", Run: do(func() { t.lines[t.at].Set("") })},
	}
}

// configure applies the limit and mask settings.
func (t *inputTab) configure() {
	c := t.m.store.Get()
	t.lines[1].Limit = c.Input.Limit
	t.lines[2].Mask, _ = utf8.DecodeRuneInString(c.Input.Mask)
}

func (t *inputTab) update(msg tea.Msg) tea.Cmd {
	t.configure()
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if !t.editing {
			return nil
		}
		switch msg.String() {
		case "esc", "enter":
			t.editing = false
		case "up", "shift+tab":
			t.scrollBy(-1)
		case "down", "tab":
			t.scrollBy(1)
		default:
			t.lines[t.at].Key(msg)
		}
	case tea.PasteMsg:
		if t.editing {
			t.lines[t.at].Update(msg)
		}
	case tea.MouseClickMsg:
		if i := msg.Y / 3; msg.X < t.boxWidth(t.m.width) && i < len(t.lines) {
			t.at, t.editing = i, true
		}
	}
	return nil
}

func (t *inputTab) boxWidth(w int) int { return min(w, 48) }

func (t *inputTab) view(w, _ int) string {
	t.configure()
	th := t.m.th
	bw := t.boxWidth(w)
	c := t.m.store.Get()
	notes := []string{"", "input.limit " + text.Thousands(int64(c.Input.Limit)), "input.mask " + c.Input.Mask}
	var out []string
	for i, title := range t.titles() {
		focused := i == t.at
		cursor := th.Lip("ui.text")
		if focused && t.editing {
			cursor = th.Lip("ui.cursor")
		}
		v, _ := t.lines[i].View(bw-2, th.Lip("ui.text"), cursor)
		box := modal.Box{Title: title, Focused: focused}
		if focused && t.editing {
			box.Keys = "esc Done"
		}
		frame := modal.Frame(th, box, []string{v}, bw)
		frame[1] += "  " + th.Render("ui.text.inactive", notes[i])
		out = append(out, frame...)
	}
	return strings.Join(out, "\n")
}
