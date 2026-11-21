package help

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/theme"
)

func TestKeyName(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+p": "Ctrl+P", "g g": "g g", "f1": "F1", "shift+tab": "Shift+Tab",
		"pgdown": "PgDn", "ctrl+,": "Ctrl+Comma", ",": "Comma", "alt+enter": "Alt+Enter", "?": "?", "G": "G", "ctrl++": "Ctrl++", "+": "+", "ctrl+b +": "Ctrl+B +",
	} {
		if got := KeyName(in); got != want {
			t.Errorf("KeyName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBar(t *testing.T) {
	hints := []Hint{{"enter", "Open"}, {"?", "Keys"}, {"q", "Quit"}}
	for _, w := range []int{10, 30, 80} {
		b := Bar(theme.Default(), "Ready", hints, w)
		if ansi.StringWidth(b) != w {
			t.Errorf("width %d: %d %q", w, ansi.StringWidth(b), ansi.Strip(b))
		}
	}
	if s := ansi.Strip(Bar(theme.Default(), "Ready", hints, 30)); !strings.Contains(s, "enter Open") || strings.Contains(s, "Quit") {
		t.Errorf("dropping from the end: %q", s)
	}
}

func TestFromCommandsMarkdownCheck(t *testing.T) {
	var reg command.Registry
	nop := func([]string) tea.Cmd { return nil }
	reg.MustAdd(
		command.Command{Name: "move.down", Title: "Down", Group: "Move", Key: "down j", Run: nop},
		command.Command{Name: "top", Title: "Top", Group: "Move", Key: "g,g", Run: nop},
		command.Command{Name: "quit", Title: "Quit", Group: "App", Key: "ctrl+c q", Run: nop},
		command.Command{Name: "nokey", Title: "No key", Group: "App", Run: nop},
	)
	var keys command.Keys
	keys.Defaults("normal", reg.All())
	groups := FromCommands(&reg, &keys, "normal")
	if len(groups) != 2 || groups[0].Name != "Move" || groups[0].Rows[0].Keys != "j, Down" || groups[1].Rows[0].Keys != "q, Ctrl+C" || len(groups[1].Rows) != 1 {
		t.Fatalf("%+v", groups)
	}
	md := Markdown("Keys", "Intro.", groups)
	if probs := Check(md, groups); len(probs) != 0 {
		t.Errorf("own markdown: %v", probs)
	}
	extra := md + "\n## Input\n\n| Input | What |\n|---|---|\n| x | y |\n"
	if probs := Check(extra, groups); len(probs) != 0 {
		t.Errorf("non-key table counted: %v", probs)
	}
	if probs := Check(strings.Replace(md, "| g g | Top |\n", "", 1), groups); len(probs) != 2 {
		t.Errorf("missing row: %v", probs)
	}
	if h := Hints(&reg, &keys, "normal", "quit", "nokey", "top"); len(h) != 2 || h[0].Key != "q" || h[1].Key != "g g" {
		t.Errorf("hints %+v", h)
	}
}

func TestScreen(t *testing.T) {
	var groups []Group
	for _, name := range []string{"One", "Two", "Three"} {
		g := Group{Name: name}
		for range 8 {
			g.Rows = append(g.Rows, Row{Keys: "x", Action: "Does a thing " + name})
		}
		groups = append(groups, g)
	}
	s := New(theme.Default(), "Keys", groups, "Three")
	s.SetSize(100, 12)
	v := ansi.Strip(s.View().Content)
	if !strings.Contains(v, "Three") || !strings.Contains(v, "esc Close") {
		t.Errorf("open group not shown:\n%s", v)
	}
	lines := strings.Split(s.View().Content, "\n")
	for _, l := range lines {
		if ansi.StringWidth(l) != ansi.StringWidth(lines[0]) {
			t.Errorf("ragged: %q", ansi.Strip(l))
		}
	}
	s.SetSize(50, 12)
	s.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if s.top != s.maxTop() || s.maxTop() == 0 {
		t.Errorf("end: top %d max %d", s.top, s.maxTop())
	}
	s.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !s.Done() {
		t.Error("esc")
	}
}

// The command every program adds: the keys screen on ? and F1.
func TestCommand(t *testing.T) {
	opened := false
	c := Command(func() tea.Cmd { opened = true; return nil })
	c.Run(nil)
	if c.Name != "keys" || c.Key != "? f1" || !opened {
		t.Fatalf("%+v opened %v", c, opened)
	}
}
