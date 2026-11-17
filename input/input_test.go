package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func press(l *Line, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "home":
			msg = tea.KeyPressMsg{Code: tea.KeyHome}
		case "ctrl+w":
			msg = tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}
		case "ctrl+u":
			msg = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
		case "ctrl+k":
			msg = tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
		case "alt+f":
			msg = tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt}
		case "alt+b":
			msg = tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		l.Key(msg)
	}
}

func TestEditing(t *testing.T) {
	var l Line
	press(&l, "a", "b", "c", "left", "X")
	if l.String() != "abXc" || l.Cursor() != 3 {
		t.Fatalf("%q %d", l.String(), l.Cursor())
	}
	press(&l, "backspace", "home", "Y")
	if l.String() != "Yabc" {
		t.Fatalf("%q", l.String())
	}
	l = Of("one two three")
	press(&l, "ctrl+w")
	if l.String() != "one two " {
		t.Fatalf("ctrl+w %q", l.String())
	}
	press(&l, "alt+b", "alt+b", "ctrl+k")
	if l.String() != "" {
		t.Fatalf("ctrl+k %q", l.String())
	}
	l = Of("one two")
	press(&l, "alt+b", "ctrl+u")
	if l.String() != "two" || l.Cursor() != 0 {
		t.Fatalf("ctrl+u %q", l.String())
	}
	press(&l, "alt+f")
	if l.Cursor() != 3 {
		t.Fatalf("alt+f %d", l.Cursor())
	}
	l.Update(tea.PasteMsg{Content: "x\ny"})
	if l.String() != "twox y" {
		t.Fatalf("paste %q", l.String())
	}
	if l.Key(tea.KeyPressMsg{Code: tea.KeyUp}) {
		t.Error("up taken")
	}
	l = Line{Limit: 3}
	l.Insert("abcdef")
	if l.String() != "abc" {
		t.Errorf("limit %q", l.String())
	}
}

func TestViewScrollsToCursor(t *testing.T) {
	l := Of("abcdefghij")
	v, x := l.View(5, lipgloss.NewStyle(), lipgloss.NewStyle())
	if ansi.Strip(v) != "ghij " || x != 4 {
		t.Errorf("%q %d", ansi.Strip(v), x)
	}
	l.at = 0
	v, x = l.View(5, lipgloss.NewStyle(), lipgloss.NewStyle())
	if ansi.Strip(v) != "abcde" || x != 0 {
		t.Errorf("%q %d", ansi.Strip(v), x)
	}
}
