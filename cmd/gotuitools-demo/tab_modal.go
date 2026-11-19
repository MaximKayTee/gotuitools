package main

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/files"
	"github.com/MaximKayTee/gotuitools/help"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/palette"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

type modalTab struct{ m *model }

func (t *modalTab) name() string           { return "modal" }
func (t *modalTab) title() string          { return "Modal" }
func (t *modalTab) update(tea.Msg) tea.Cmd { return nil }
func (t *modalTab) status() string {
	return "Open: " + text.Thousands(int64(t.m.stack.Len()))
}

func (t *modalTab) commands() []command.Command {
	return []command.Command{
		{Name: "modal.theme", Title: "Choose theme", Key: "p",
			Run: func([]string) tea.Cmd { return t.m.stack.Open(t.chooser()) }},
		{Name: "modal.file", Title: "Choose file", Key: "o",
			Run: func([]string) tea.Cmd { return t.m.stack.Open(t.files(files.Options{Title: "File"})) }},
		{Name: "modal.folder", Title: "Choose folder", Key: "O",
			Run: func([]string) tea.Cmd {
				return t.m.stack.Open(t.files(files.Options{Title: "Folder", Folders: true, New: true}))
			}},
		{Name: "modal.reset", Title: "Reset settings", Key: "r",
			Run: func([]string) tea.Cmd {
				return t.m.stack.Open(&confirm{th: t.m.th, title: "Reset settings",
					body: []string{"Every setting back to its default,", "keys included."},
					yes:  t.reset})
			}},
	}
}

// chooser is a palette of theme names; choosing one sets the theme.
func (t *modalTab) chooser() *palette.Model {
	current := t.m.store.Get().Theme
	var items []palette.Item
	for _, n := range themeNames() {
		it := palette.Item{Title: n, Value: n}
		if n == current {
			it.Note = "current"
		}
		if slices.Contains(theme.BuiltIn(), n) {
			it.Group = "Built in"
		} else {
			it.Group = "Files"
		}
		items = append(items, it)
	}
	return palette.New(t.m.th, "Theme", items, func(it palette.Item) tea.Cmd {
		t.m.set("theme", it.Value.(string))
		return nil
	})
}

// files is a chooser on disk; the path chosen goes to the status line.
func (t *modalTab) files(o files.Options) *files.Model {
	return files.New(t.m.th, o, func(path string) tea.Cmd {
		t.m.status = path
		return nil
	})
}

func (t *modalTab) reset() tea.Cmd {
	for _, f := range t.m.store.Fields() {
		if _, err := t.m.store.Reset(f.Key); err != nil {
			t.m.status = err.Error()
		}
	}
	t.m.apply()
	return nil
}

func (t *modalTab) view(w, _ int) string {
	th := t.m.th
	rows := []struct{ cmd, what string }{
		{"palette", "palette.Commands over the registry"},
		{"modal.theme", "palette.New over theme names"},
		{"keys", "help.New: the keys screen"},
		{"settings", "settingsui.New over the settings store"},
		{"modal.file", "files.New: a file on disk"},
		{"modal.folder", "files.New with Folders and New"},
		{"modal.reset", "a Modal of this program, drawn with modal.Frame"},
	}
	keyW := 10
	lines := []string{th.Render("ui.text.focus", "Modals"), ""}
	for _, r := range rows {
		c, _ := t.m.reg.Get(r.cmd)
		key := help.KeyName(t.m.keys.For(t.name(), r.cmd))
		lines = append(lines, th.Render("ui.statusline.key", text.Pad(key, keyW))+
			text.Pad(c.Label(), 18)+th.Render("ui.text.inactive", text.Truncate(r.what, max(w-keyW-18, 1))))
	}
	return strings.Join(lines, "\n")
}

// confirm is a yes-or-no dialog.
type confirm struct {
	th    theme.Theme
	title string
	body  []string
	yes   func() tea.Cmd
	w     int
	done  bool
}

func (c *confirm) Init() tea.Cmd { return nil }
func (c *confirm) Done() bool    { return c.done }

func (c *confirm) SetSize(w, _ int) tea.Cmd {
	c.w = min(w, 44)
	return nil
}

func (c *confirm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "enter", "y":
			c.done = true
			return c, c.yes()
		case "esc", "n", "q":
			c.done = true
		}
	}
	return c, nil
}

func (c *confirm) View() tea.View {
	body := []string{""}
	for _, l := range c.body {
		body = append(body, " "+l)
	}
	body = append(body, "")
	return tea.NewView(strings.Join(modal.Frame(c.th, modal.Box{Title: c.title, Keys: "enter Confirm  esc Cancel", Focused: true}, body, c.w), "\n"))
}
