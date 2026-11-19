package main

import (
	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/help"
)

// keysTab is KEYS.md as help.Markdown writes it from the registry.
type keysTab struct{ m *model }

func (t *keysTab) name() string                { return "keys" }
func (t *keysTab) title() string               { return "Keys" }
func (t *keysTab) status() string              { return "help.Markdown" }
func (t *keysTab) update(tea.Msg) tea.Cmd      { return nil }
func (t *keysTab) commands() []command.Command { return nil }

func (t *keysTab) view(int, int) string {
	return t.m.th.Render("ui.text", help.Markdown("Keys", "", t.m.keyGroups()))
}
