package main

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/help"
	"github.com/MaximKayTee/gotuitools/palette"
	"github.com/MaximKayTee/gotuitools/settings"
	"github.com/MaximKayTee/gotuitools/settingsui"
	"github.com/MaximKayTee/gotuitools/theme"
)

// do adapts an action to Command.Run.
func do(f func()) func([]string) tea.Cmd {
	return func([]string) tea.Cmd { f(); return nil }
}

func (m *model) globalCommands() []command.Command {
	cs := []command.Command{
		{Name: "tab.next", Title: "Next tab", Group: "Tabs", Key: "tab right l",
			Run: do(func() { m.active = (m.active + 1) % len(m.tabs) })},
		{Name: "tab.prev", Title: "Previous tab", Group: "Tabs", Key: "shift+tab left h",
			Run: do(func() { m.active = (m.active + len(m.tabs) - 1) % len(m.tabs) })},
	}
	for i, t := range m.tabs {
		key := ""
		if i < 10 {
			key = fmt.Sprint((i + 1) % 10)
		}
		cs = append(cs, command.Command{Name: "tab." + t.name(), Title: t.title(), Group: "Tabs", Key: key,
			Run: do(func() { m.active = i })})
	}
	return append(cs,
		command.Command{Name: "scroll.up", Title: "Scroll up", Group: "Scroll", Key: "up k", Hidden: true,
			Run: do(func() { m.scrollKey(-1) })},
		command.Command{Name: "scroll.down", Title: "Scroll down", Group: "Scroll", Key: "down j", Hidden: true,
			Run: do(func() { m.scrollKey(1) })},
		command.Command{Name: "scroll.pageup", Title: "Page up", Group: "Scroll", Key: "pgup", Hidden: true,
			Run: do(func() { m.scrollKey(-m.bodyHeight()) })},
		command.Command{Name: "scroll.pagedown", Title: "Page down", Group: "Scroll", Key: "pgdown", Hidden: true,
			Run: do(func() { m.scrollKey(m.bodyHeight()) })},
		command.Command{Name: "palette", Title: "Commands", Group: "General", Key: "ctrl+p", Hidden: true,
			Run: func([]string) tea.Cmd { return m.openPalette() }},
		command.Command{Name: "keys", Title: "Keys", Group: "General", Key: "? f1",
			Run: func([]string) tea.Cmd {
				return m.stack.Open(help.New(m.th, "Keys", m.keyGroups(), m.tabs[m.active].title()))
			}},
		command.Command{Name: "settings", Title: "Settings", Group: "General", Key: "ctrl+,",
			Run: func([]string) tea.Cmd { return m.stack.Open(settingsui.New(m.th, m.store, "Settings")) }},
		command.Command{Name: "quit", Title: "Quit", Group: "General", Key: "q ctrl+c",
			Run: func([]string) tea.Cmd { return tea.Quit }},
	)
}

// tabCommands are t's commands, grouped under its title and available
// while it is the active tab.
func (m *model) tabCommands(t tab) []command.Command {
	cs := t.commands()
	for i := range cs {
		own := cs[i].Available
		cs[i].Group = t.title()
		cs[i].Available = func() bool { return m.tabs[m.active] == t && (own == nil || own()) }
	}
	return cs
}

// scrollKey scrolls the page when it is taller than the window, else the
// active tab's own cursor.
func (m *model) scrollKey(n int) {
	if m.scrollBy(n) {
		return
	}
	if s, ok := m.tabs[m.active].(scroller); ok {
		s.scrollBy(n)
	}
}

// openPalette shows the available commands of the active tab's mode.
func (m *model) openPalette() tea.Cmd {
	mode := m.mode()
	var items []palette.Item
	for _, c := range m.reg.All() {
		if c.Hidden || !c.Can() {
			continue
		}
		items = append(items, palette.Item{Title: c.Label(), Note: m.keys.For(mode, c.Name),
			Group: c.Group, Search: []string{c.Name}, Value: c.Name})
	}
	return m.stack.Open(palette.New(m.th, "Commands", items, func(it palette.Item) tea.Cmd {
		return m.reg.Run(it.Value.(string))
	}))
}

// keyGroups are the keys of every mode, each group once.
func (m *model) keyGroups() []help.Group {
	var out []help.Group
	seen := map[string]bool{}
	for _, t := range m.tabs {
		for _, g := range help.FromCommands(m.reg, m.keys, t.name()) {
			if !seen[g.Name] {
				seen[g.Name] = true
				out = append(out, g)
			}
		}
	}
	return out
}

// modeCommands are the global commands and t's.
func (m *model) modeCommands(t tab) []command.Command {
	return append(m.globals[:len(m.globals):len(m.globals)], m.tabCmds[t.name()]...)
}

// keyModes are each tab's commands, the global ones with them.
func (m *model) keyModes() []settings.KeyMode {
	out := make([]settings.KeyMode, len(m.tabs))
	for i, t := range m.tabs {
		out[i] = settings.KeyMode{Mode: t.name(), Commands: m.modeCommands(t)}
	}
	return out
}

// apply loads the theme and the keys from the settings.
func (m *model) apply() {
	c := m.store.Get()
	th, err := theme.FindFor(c.Theme, m.dark, theme.Dirs(program)...)
	if err != nil {
		m.status = err.Error()
		th = theme.Default()
	}
	m.th = th
	for _, t := range m.tabs {
		if err := m.keys.Load(t.name(), m.modeCommands(t), c.Keys); err != nil {
			m.status = err.Error()
			m.keys.Load(t.name(), m.modeCommands(t), nil)
		}
	}
}

// set saves one setting and applies the settings.
func (m *model) set(key, value string) {
	if _, err := m.store.Set(key, value); err != nil {
		m.status = err.Error()
		return
	}
	m.apply()
}
