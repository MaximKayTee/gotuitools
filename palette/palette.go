// Package palette is a list to choose from, narrowed by typing: a
// modal.Modal.
//
//	type            narrow (fuzzy.Search over title, group, search words, note)
//	up down ctrl+p ctrl+n pgup pgdown   move
//	enter           choose
//	an Action's key act on the highlighted item, the palette left open
//	esc ctrl+c      close
//	wheel, click    move, choose
package palette

import (
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

// Item is one entry.
type Item struct {
	Title  string
	Note   string // drawn on the right: a key, a count
	Group  string
	Search []string // more words it is found by
	Value  any
}

// Action is a key that does something with the highlighted item and
// leaves the palette open: remove it, rename it. It is named in the
// bottom border: "ctrl+x Remove".
type Action struct {
	Key   string // as tea names it: "ctrl+x"
	Label string
	Run   func(Item) tea.Cmd
}

// Model is a palette. Use New.
type Model struct {
	th     theme.Theme
	title  string
	all    []Item
	shown  []Item
	line   input.Line
	cur    list.Cursor
	choose func(Item) tea.Cmd
	// Typed makes a last row of what was typed, when it returns true.
	Typed func(text string) (Item, bool)
	// MaxWidth caps the box width; 0 is 72.
	MaxWidth int
	// Actions are keys that act on the highlighted item.
	Actions []Action

	w, h   int
	x, y   int
	done   bool
	chosen bool
}

// New is a palette of items; choose runs on enter.
func New(th theme.Theme, title string, items []Item, choose func(Item) tea.Cmd) *Model {
	m := &Model{th: th, title: title, all: items, choose: choose, cur: list.Cursor{Wrap: true}}
	m.filter()
	return m
}

// Commands is a palette of reg's available, shown commands, each noted
// with its key in mode; choosing one runs it.
func Commands(th theme.Theme, reg *command.Registry, keys *command.Keys, mode string) *Model {
	var items []Item
	for _, c := range reg.All() {
		if c.Hidden || !c.Can() {
			continue
		}
		note := ""
		if keys != nil {
			note = keys.For(mode, c.Name)
		}
		items = append(items, Item{Title: c.Label(), Note: note, Group: c.Group, Search: []string{c.Name}, Value: c.Name})
	}
	return New(th, "Commands", items, func(it Item) tea.Cmd { return reg.Run(it.Value.(string)) })
}

// SetItems replaces the items, what is typed kept: after an Action has
// changed them.
func (m *Model) SetItems(items []Item) {
	at := m.cur.At
	m.all = items
	m.filter()
	m.cur.At = at
	m.cur.Clamp()
}

// SetQuery replaces what is typed.
func (m *Model) SetQuery(q string) { m.line.Set(q); m.filter() }

// Query is what is typed.
func (m *Model) Query() string { return m.line.String() }

// Shown is the items matching the query, best first.
func (m *Model) Shown() []Item { return m.shown }

func (m *Model) filter() {
	q := m.line.String()
	m.shown = fuzzy.Filter(m.all, q, func(it Item) []string {
		return append(append([]string{it.Title, it.Group}, it.Search...), it.Note)
	})
	if m.Typed != nil && strings.TrimSpace(q) != "" {
		if it, ok := m.Typed(q); ok {
			m.shown = append(m.shown, it)
		}
	}
	m.cur.N, m.cur.At, m.cur.Top = len(m.shown), 0, 0
	m.cur.Clamp()
}

func (m *Model) Init() tea.Cmd { return nil }

// Done reports closed.
func (m *Model) Done() bool { return m.done }

// SetOrigin is where the box was drawn.
func (m *Model) SetOrigin(x, y int) { m.x, m.y = x, y }

// SetSize is the box the palette may use.
func (m *Model) SetSize(w, h int) tea.Cmd {
	m.w, m.h = w, h
	m.cur.Height = m.rows()
	m.cur.Clamp()
	return nil
}

func (m *Model) width() int {
	mw := m.MaxWidth
	if mw <= 0 {
		mw = 72
	}
	return min(m.w, mw)
}

// rows is the list rows shown: the box less borders and the input row.
func (m *Model) rows() int { return max(min(m.h-3, max(len(m.all), 1)), 1) }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.done = true
			return m, nil
		case "enter":
			return m, m.pick(m.cur.At)
		case "up", "down", "ctrl+p", "ctrl+n", "pgup", "pgdown":
			m.cur.Key(msg.String(), false)
			return m, nil
		}
		for _, a := range m.Actions {
			if a.Key == msg.String() && m.cur.At < len(m.shown) {
				return m, a.Run(m.shown[m.cur.At])
			}
		}
		if m.line.Key(msg) {
			m.filter()
		}
	case tea.PasteMsg:
		m.line.Update(msg)
		m.filter()
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.cur.Scroll(-3)
		case tea.MouseWheelDown:
			m.cur.Scroll(3)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			// border, input row, then the list
			if i := m.cur.Hit(msg.Y - m.y - 2); i >= 0 && msg.X > m.x && msg.X < m.x+m.width()-1 {
				return m, m.pick(i)
			}
		}
	}
	return m, nil
}

func (m *Model) pick(i int) tea.Cmd {
	if i < 0 || i >= len(m.shown) {
		return nil
	}
	m.done, m.chosen = true, true
	if m.choose == nil {
		return nil
	}
	return m.choose(m.shown[i])
}

// View is the box.
func (m *Model) View() tea.View {
	w := m.width()
	if w < 6 || m.h < 4 {
		return tea.NewView("")
	}
	iw := w - 2
	body := make([]string, 0, m.cur.Height+1)
	line, _ := m.line.View(iw-2, m.th.Lip("ui.text"), m.th.Lip("ui.cursor"))
	body = append(body, " "+line+" ")
	from, to := m.cur.Visible()
	for i := from; i < to; i++ {
		it := m.shown[i]
		// the title keeps its room; a long note loses its start
		note := it.Note
		if room := iw - text.Width(it.Title) - 4; text.Width(note) > room {
			note = text.TruncateStart(note, max(room, 0))
		}
		row := text.Row(" "+it.Title, m.th.Render("ui.text.inactive", note)+" ", iw)
		if i == m.cur.At {
			row = m.th.Render("ui.menu.selected", text.Row(" "+it.Title, note+" ", iw))
		} else {
			row = m.th.Render("ui.menu", row)
		}
		body = append(body, row)
	}
	if len(m.shown) == 0 {
		body = append(body, m.th.Render("ui.text.inactive", " None"))
	}
	for len(body) < m.cur.Height+1 {
		body = append(body, "")
	}
	pos := m.cur.Pos()
	pos.Skip = 1
	keys := make([]string, len(m.Actions))
	for i, a := range m.Actions {
		keys[i] = a.Key + " " + a.Label
	}
	lines := modal.Frame(m.th, modal.Box{Title: m.title, Keys: strings.Join(keys, "  "), Focused: true, Pos: pos}, body, w)
	return tea.NewView(strings.Join(lines, "\n"))
}
