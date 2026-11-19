package form

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/theme"
)

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func key(m *Model, code rune, mod tea.KeyMod) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Mod: mod})
	return cmd
}

func fields() []Field {
	return []Field{
		{Label: "Name", Value: "jazz"},
		{Label: "Country"},
		{Label: "Sort", Choices: []string{"Clicks", "Votes", "Name"}, Value: "Votes"},
	}
}

func TestFillAndSubmit(t *testing.T) {
	var got []string
	m := New(theme.Default(), "Search", fields(), func(v []string) tea.Cmd { got = v; return nil })
	m.SetSize(80, 20)
	typeText(m, " fm ")
	key(m, tea.KeyTab, 0)
	typeText(m, "FR")
	key(m, tea.KeyDown, 0)
	if m.At() != 2 {
		t.Fatalf("at %d", m.At())
	}
	key(m, tea.KeyRight, 0)
	key(m, tea.KeyRight, 0) // around the end
	key(m, tea.KeyLeft, 0)
	typeText(m, "x") // a choice takes no text
	if v := m.Values(); !slices.Equal(v, []string{"jazz fm", "FR", "Name"}) {
		t.Fatalf("%q", v)
	}
	key(m, tea.KeyTab, 0)
	if m.At() != 0 {
		t.Fatalf("tab on the last field: at %d", m.At())
	}
	key(m, tea.KeyTab, tea.ModShift)
	if m.At() != 2 {
		t.Fatalf("shift+tab on the first field: at %d", m.At())
	}
	key(m, tea.KeyEnter, 0)
	if !m.Done() || !slices.Equal(got, []string{"jazz fm", "FR", "Name"}) {
		t.Errorf("done %v got %q", m.Done(), got)
	}
}

func TestEscAndCheck(t *testing.T) {
	ran := false
	m := New(theme.Default(), "Add", []Field{{}}, func([]string) tea.Cmd { ran = true; return nil })
	m.Check = func(v []string) error {
		if !strings.HasPrefix(v[0], "http") {
			return errors.New("not an address")
		}
		return nil
	}
	m.SetSize(40, 10)
	typeText(m, "radio")
	key(m, tea.KeyEnter, 0)
	if m.Done() || ran {
		t.Fatal("submitted past Check")
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "not an address") {
		t.Errorf("no error shown:\n%s", v)
	}
	typeText(m, "!")
	if v := ansi.Strip(m.View().Content); strings.Contains(v, "not an address") {
		t.Errorf("error stays after an edit:\n%s", v)
	}
	m.Update(tea.PasteMsg{Content: "x"})
	key(m, tea.KeyEscape, 0)
	if !m.Done() || ran {
		t.Errorf("esc: done %v ran %v", m.Done(), ran)
	}
}

func TestViewAndClick(t *testing.T) {
	m := New(theme.Default(), "Search", fields(), nil)
	for _, w := range []int{80, 30, 12} {
		m.SetSize(w, 20)
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != 5 {
			t.Fatalf("w %d: %d lines", w, len(lines))
		}
		for i, l := range lines {
			if n := ansi.StringWidth(l); n != min(w, 60) {
				t.Errorf("w %d line %d: %d cells", w, i, n)
			}
		}
	}
	m.SetSize(80, 20)
	v := ansi.Strip(m.View().Content)
	for _, s := range []string{"Search", "Name", "jazz", "Country", "Sort", "Votes"} {
		if !strings.Contains(v, s) {
			t.Errorf("lacks %q:\n%s", s, v)
		}
	}
	m.SetOrigin(10, 5)
	click := func(x, y int) { m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})) }
	click(20, 8) // the third row
	if m.At() != 2 || m.Values()[2] != "Votes" {
		t.Fatalf("click: at %d, %q", m.At(), m.Values())
	}
	click(20, 8)
	if m.Values()[2] != "Name" {
		t.Errorf("second click: %q", m.Values())
	}
	click(20, 6)
	if m.At() != 0 {
		t.Errorf("click on the first row: at %d", m.At())
	}
	click(10, 7) // the border
	click(20, 9) // under the fields
	if m.At() != 0 {
		t.Errorf("click off the fields: at %d", m.At())
	}

	// no labels: no label column
	one := New(theme.Default(), "Add", []Field{{Value: "abc"}}, nil)
	one.SetSize(20, 10)
	if l := strings.Split(ansi.Strip(one.View().Content), "\n")[1]; !strings.HasPrefix(l, "│ abc") {
		t.Errorf("%q", l)
	}
}
