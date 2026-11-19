package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/list"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/text"
)

type listTab struct {
	m *model
	c list.Cursor
}

func (t *listTab) name() string  { return "list" }
func (t *listTab) title() string { return "List" }

func (t *listTab) status() string {
	if t.c.N == 0 {
		return "list.rows 0"
	}
	return "Row " + text.Thousands(int64(t.c.At+1)) + " of " + text.Thousands(int64(t.c.N))
}

func (t *listTab) commands() []command.Command {
	return []command.Command{
		{Name: "list.top", Title: "First row", Key: "home g", Run: do(func() { t.c.Key("home", false) })},
		{Name: "list.bottom", Title: "Last row", Key: "end G", Run: do(func() { t.c.Key("end", false) })},
	}
}

func (t *listTab) scrollBy(n int) {
	if n == 1 || n == -1 {
		t.c.Move(n)
	} else {
		t.c.At += n
		t.c.Clamp()
	}
}

// size fits the cursor to the settings and an h-row frame.
func (t *listTab) size(h int) {
	c := t.m.store.Get().List
	t.c.N, t.c.Wrap, t.c.Height = c.Rows, c.Wrap, max(h-2, 1)
	t.c.Clamp()
}

func (t *listTab) update(msg tea.Msg) tea.Cmd {
	t.size(t.m.bodyHeight())
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.X < listWidth(t.m.width) {
			if i := t.c.Hit(msg.Y - 1); i >= 0 {
				t.c.At = i
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			t.c.Scroll(-3)
		case tea.MouseWheelDown:
			t.c.Scroll(3)
		}
	}
	return nil
}

func listWidth(w int) int { return min(w, 40) }

func (t *listTab) view(w, h int) string {
	t.size(h)
	th := t.m.th
	from, to := t.c.Visible()
	body := make([]string, 0, t.c.Height)
	numW := len(text.Thousands(int64(t.c.N)))
	for i := from; i < to; i++ {
		row := " Row " + text.Pad(text.Thousands(int64(i+1)), numW)
		scope := "ui.menu"
		if i == t.c.At {
			scope = "ui.menu.selected"
		}
		body = append(body, th.Lip(scope).Width(listWidth(w)-2).Render(row))
	}
	for len(body) < t.c.Height {
		body = append(body, "")
	}
	title := text.Plural(int64(t.c.N), "row")
	return strings.Join(modal.Frame(th, modal.Box{Title: title, Focused: true, Pos: t.c.Pos()}, body, listWidth(w)), "\n")
}
