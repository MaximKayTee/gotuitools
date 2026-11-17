package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/theme"
)

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestCommandsPalette(t *testing.T) {
	var reg command.Registry
	var ran string
	run := func(name string) func([]string) tea.Cmd {
		return func([]string) tea.Cmd { ran = name; return nil }
	}
	reg.MustAdd(
		command.Command{Name: "settings.open", Title: "Settings", Key: "ctrl+,", Run: run("settings")},
		command.Command{Name: "app.quit", Title: "Quit", Key: "q", Run: run("quit")},
		command.Command{Name: "secret", Title: "Secret", Hidden: true, Run: run("secret")},
		command.Command{Name: "theme.set", Title: "Set theme", Run: run("theme")},
	)
	var keys command.Keys
	keys.Defaults("normal", reg.All())
	m := Commands(theme.Default(), &reg, &keys, "normal")
	m.SetSize(80, 20)
	if len(m.Shown()) != 3 {
		t.Fatalf("hidden shown: %d", len(m.Shown()))
	}
	typeText(m, "set")
	if got := m.Shown(); len(got) != 2 || got[0].Title != "Settings" || got[0].Note != "ctrl+," {
		t.Fatalf("%+v", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.Done() || ran != "theme" {
		t.Errorf("done %v ran %q", m.Done(), ran)
	}
}

func TestViewAndClick(t *testing.T) {
	var got string
	items := []Item{{Title: "alpha"}, {Title: "beta", Note: "2"}, {Title: "gamma"}}
	m := New(theme.Default(), "Pick", items, func(it Item) tea.Cmd { got = it.Title; return nil })
	m.SetSize(30, 10)
	v := m.View().Content
	lines := strings.Split(v, "\n")
	if len(lines) != 6 {
		t.Fatalf("%d lines:\n%s", len(lines), ansi.Strip(v))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) != 30 {
			t.Errorf("width %d %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	if !strings.Contains(ansi.Strip(lines[3]), "beta") || !strings.HasSuffix(strings.TrimRight(ansi.Strip(lines[3]), "│ "), "2") {
		t.Errorf("%q", ansi.Strip(lines[3]))
	}
	m.SetOrigin(5, 1)
	m.Update(tea.MouseClickMsg{X: 10, Y: 1 + 2 + 2, Button: tea.MouseLeft})
	if got != "gamma" {
		t.Errorf("click chose %q", got)
	}
}

func TestTypedRowAndEmpty(t *testing.T) {
	m := New(theme.Default(), "Open", []Item{{Title: "a.md"}}, nil)
	m.Typed = func(s string) (Item, bool) { return Item{Title: "New: " + s}, true }
	m.SetSize(40, 10)
	typeText(m, "zz")
	if s := m.Shown(); len(s) != 1 || s[0].Title != "New: zz" {
		t.Errorf("%+v", s)
	}
	m.Typed = nil
	m.SetQuery("qq")
	if !strings.Contains(ansi.Strip(m.View().Content), "None") {
		t.Error("no None row")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.Done() {
		t.Error("esc")
	}
}
