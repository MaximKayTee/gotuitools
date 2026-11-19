package main

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/fuzzy"
	"github.com/MaximKayTee/gotuitools/input"
	"github.com/MaximKayTee/gotuitools/list"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// fuzzyTab searches every scope of the built-in themes.
type fuzzyTab struct {
	m       *model
	items   []string
	query   input.Line
	shown   []int
	c       list.Cursor
	editing bool
}

func newFuzzyTab(m *model) *fuzzyTab {
	t := &fuzzyTab{m: m}
	for _, name := range theme.BuiltIn() {
		if th, err := theme.Find(name); err == nil {
			t.items = append(t.items, th.Scopes()...)
		}
	}
	slices.Sort(t.items)
	t.items = slices.Compact(t.items)
	t.search()
	return t
}

func (t *fuzzyTab) name() string    { return "fuzzy" }
func (t *fuzzyTab) title() string   { return "Fuzzy" }
func (t *fuzzyTab) capturing() bool { return t.editing }
func (t *fuzzyTab) scrollBy(n int)  { t.c.Move(n) }

func (t *fuzzyTab) status() string {
	return text.Thousands(int64(len(t.shown))) + " of " + text.Plural(int64(len(t.items)), "scope")
}

func (t *fuzzyTab) commands() []command.Command {
	return []command.Command{
		{Name: "fuzzy.search", Title: "Search", Key: "/ enter", Run: do(func() { t.editing = true })},
	}
}

func (t *fuzzyTab) search() {
	t.shown = fuzzy.Search(len(t.items), t.query.String(), func(i int) []string { return []string{t.items[i]} })
	t.c.N, t.c.At, t.c.Top = len(t.shown), 0, 0
}

func (t *fuzzyTab) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if !t.editing {
			return nil
		}
		switch msg.String() {
		case "esc", "enter":
			t.editing = false
		case "up", "ctrl+p":
			t.c.Move(-1)
		case "down", "ctrl+n":
			t.c.Move(1)
		default:
			before := t.query.String()
			t.query.Key(msg)
			if t.query.String() != before {
				t.search()
			}
		}
	case tea.PasteMsg:
		if t.editing && t.query.Update(msg) {
			t.search()
		}
	case tea.MouseClickMsg:
		if msg.Y < 3 {
			t.editing = true
		} else if i := t.c.Hit(msg.Y - 4); i >= 0 {
			t.c.At = i
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

func (t *fuzzyTab) view(w, h int) string {
	th := t.m.th
	bw := min(w, 64)
	cursor := th.Lip("ui.text")
	if t.editing {
		cursor = th.Lip("ui.cursor")
	}
	q, _ := t.query.View(bw-2, th.Lip("ui.text"), cursor)
	box := modal.Box{Title: "Query", Focused: t.editing}
	if t.editing {
		box.Keys = "esc Done"
	}
	out := modal.Frame(th, box, []string{q}, bw)

	t.c.Height = max(h-5, 1)
	t.c.Clamp()
	from, to := t.c.Visible()
	query := t.query.String()
	var body []string
	for i := from; i < to; i++ {
		s := t.items[t.shown[i]]
		tier := fuzzy.Tier(query, s)
		score, _ := fuzzy.Score(s, query)
		note := "tier " + strconv.Itoa(tier)
		if tier < 0 {
			note = "score " + strconv.Itoa(score)
		}
		scope := "ui.menu"
		if i == t.c.At {
			scope = "ui.menu.selected"
		}
		body = append(body, th.Lip(scope).Render(text.Row(" "+s, th.Render("ui.text.inactive", note)+" ", bw-2)))
	}
	for len(body) < t.c.Height {
		body = append(body, "")
	}
	out = append(out, modal.Frame(th, modal.Box{Title: "Scopes", Pos: t.c.Pos()}, body, bw)...)
	return strings.Join(out, "\n")
}
