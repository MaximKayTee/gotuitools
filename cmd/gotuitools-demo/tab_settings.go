package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/sys"
	"github.com/MaximKayTee/gotuitools/text"
)

// settingsTab lists the settings store's fields.
type settingsTab struct{ m *model }

func (t *settingsTab) name() string           { return "settings" }
func (t *settingsTab) title() string          { return "Settings" }
func (t *settingsTab) update(tea.Msg) tea.Cmd { return nil }
func (t *settingsTab) status() string         { return text.TruncateStart(t.m.store.Path(), 40) }

func (t *settingsTab) commands() []command.Command {
	return []command.Command{
		{Name: "settings.edit", Title: "Edit file", Key: "e",
			Run: func([]string) tea.Cmd { return sys.Edit(sys.Editor(""), t.m.store.Path(), 0) }},
	}
}

func (t *settingsTab) view(w, _ int) string {
	th := t.m.th
	const keyW, kindW = 26, 10
	head := th.Lip("ui.text.focus")
	lines := []string{head.Render(text.Pad("Key", keyW) + text.Pad("Kind", kindW) + "Value"), ""}
	group := ""
	for _, f := range t.m.store.Fields() {
		if f.Group != group {
			group = f.Group
			lines = append(lines, "", th.Render("ui.text.inactive", group))
		}
		value := f.Value
		if value == "" {
			value = "None"
		}
		value = text.Truncate(value, max(w-keyW-kindW, 1))
		if f.IsDefault || f.Value == f.Default {
			value = th.Render("ui.text.inactive", value)
		}
		lines = append(lines, text.Pad(text.Truncate(f.Key, keyW-1), keyW)+
			th.Render("ui.text.inactive", text.Pad(string(f.Kind), kindW))+value)
	}
	return strings.Join(lines, "\n")
}
