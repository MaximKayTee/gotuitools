package sliders

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/theme"
)

func key(m *Model, code rune, text string) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: text})
	return cmd
}

// three are sliders a, b, c at +12, -3, 0 of -12 to 12 with a switch,
// in a box at the screen's corner; changes counts the Changes, got is
// the last one's.
func three(t *testing.T) (m *Model, changes *int, got *[]float64, on *bool) {
	t.Helper()
	changes, got, on = new(int), new([]float64), new(bool)
	m = New(theme.Default(), []Slider{{"a", 12}, {"b", -3.2}, {"c", 40}}, Options{
		Title: "Equaliser", Min: -12, Max: 12, Rows: 2, Switch: true, On: true,
		Change: func(v []float64, o bool) tea.Cmd {
			*changes++
			*got, *on = v, o
			return nil
		},
	})
	m.SetSize(40, 20)
	m.values[2] = 0
	return m, changes, got, on
}

func TestKeys(t *testing.T) {
	m, changes, got, on := three(t)
	if v := New(theme.Default(), []Slider{{"a", 12}, {"b", -3.2}, {"c", 40}}, Options{Min: -12, Max: 12}).Values(); !slices.Equal(v, []float64{12, -3, 12}) {
		t.Fatalf("values at the start %v", v)
	}
	key(m, tea.KeyUp, "") // at Max already
	if *changes != 0 {
		t.Fatalf("a Change with nothing changed")
	}
	key(m, tea.KeyLeft, "") // the first already
	key(m, tea.KeyRight, "")
	key(m, tea.KeyDown, "")
	key(m, 'j', "j")
	key(m, 'l', "l")
	key(m, 'l', "l") // the last already
	key(m, 'k', "k")
	if m.At() != 2 || *changes != 3 || !slices.Equal(*got, []float64{12, -5, 1}) || !*on {
		t.Fatalf("at %d, %d changes, %v, on %v", m.At(), *changes, *got, *on)
	}
	key(m, tea.KeySpace, " ")
	if m.On() || *on || *changes != 4 {
		t.Fatalf("switch: on %v, %d changes", m.On(), *changes)
	}
	key(m, 'r', "r")
	key(m, 'r', "r") // at Zero already
	if !slices.Equal(*got, []float64{0, 0, 0}) || *changes != 5 {
		t.Fatalf("reset: %v, %d changes", *got, *changes)
	}
	if m.Done() {
		t.Fatal("closed")
	}
	key(m, tea.KeyEscape, "")
	if !m.Done() {
		t.Fatal("not closed")
	}
}

func TestMouse(t *testing.T) {
	m, changes, got, _ := three(t)
	click := func(x, y int) { m.Update(tea.MouseClickMsg{X: x + 1, Y: y + 1, Button: tea.MouseLeft}) }
	// inside the border slider a is cells 8 to 10, b 13 to 15, c 18 to
	// 20; the bars are rows 1 to 5, the zero line row 3
	for _, c := range []struct {
		y    int
		want float64
	}{{1, 12}, {2, 6}, {3, 0}, {4, -6}, {5, -12}} {
		click(14, c.y)
		m.Update(tea.MouseReleaseMsg{})
		if m.At() != 1 || (*got)[1] != c.want {
			t.Errorf("click on row %d: at %d, %v, want %v", c.y, m.At(), (*got)[1], c.want)
		}
	}
	click(14, 2)
	n := *changes
	m.Update(tea.MouseMotionMsg{X: 30, Y: 4 + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 30, Y: 4 + 1, Button: tea.MouseLeft})
	if (*got)[1] != -6 || *changes != n+1 {
		t.Errorf("drag: %v, %d changes", (*got)[1], *changes-n)
	}
	m.Update(tea.MouseMotionMsg{X: 15, Y: 19, Button: tea.MouseLeft})
	if (*got)[1] != -12 {
		t.Errorf("drag under the bars: %v", (*got)[1])
	}
	m.Update(tea.MouseReleaseMsg{})
	m.Update(tea.MouseMotionMsg{X: 15, Y: 2, Button: tea.MouseLeft})
	if (*got)[1] != -12 {
		t.Errorf("moved after the release: %v", (*got)[1])
	}
	m.Update(tea.MouseWheelMsg{X: 19 + 1, Y: 3, Button: tea.MouseWheelUp})
	m.Update(tea.MouseWheelMsg{X: 19 + 1, Y: 3, Button: tea.MouseWheelUp})
	m.Update(tea.MouseWheelMsg{X: 8 + 1, Y: 3, Button: tea.MouseWheelDown})
	if !slices.Equal(*got, []float64{11, -12, 2}) {
		t.Errorf("wheel: %v", *got)
	}
	click(4, 3)  // the scale
	click(14, 6) // the labels: the slider, no value
	if !slices.Equal(*got, []float64{11, -12, 2}) {
		t.Errorf("clicks off the bars: %v", *got)
	}
	click(22, 0)
	if m.On() {
		t.Error("click on the switch: on")
	}
}

func TestView(t *testing.T) {
	m, _, _, _ := three(t)
	lines := strings.Split(m.View().Content, "\n")
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 27 {
			t.Errorf("line %d: %d cells", i, w)
		}
		l = ansi.Strip(l)
		lines[i] = strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(l, "│"), "│"), " ")
	}
	want := []string{
		"╭─ Equaliser ─────────────╮",
		"                      On",
		"   +12  ███   │    │",
		"        ███   │    │",
		"     0  ───  ───  ───",
		"         │   ▄▄▄   │",
		"   -12   │    │    │",
		"         a    b    c",
		"        +12  -3    0",
		"╰─ space On/Off  r Reset ─╯",
	}
	if len(lines) != len(want) {
		t.Fatalf("%d lines, want %d", len(lines), len(want))
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d:\n got %q\nwant %q", i, lines[i], w)
		}
	}
	// under the line a cell is the rest of it with the colours swapped
	raw := strings.Split(m.View().Content, "\n")[5]
	if !strings.Contains(raw, "\x1b[7") && !strings.Contains(raw, ";7m") && !strings.Contains(raw, ";7;") {
		t.Errorf("no reverse in %q", raw)
	}
	// a low box: fewer rows of bar; too low: nothing
	m.SetSize(40, 8)
	if n := len(strings.Split(m.View().Content, "\n")); n != 8 {
		t.Errorf("box of 8 rows: %d lines", n)
	}
	m.SetSize(40, 6)
	if v := m.View().Content; v != "" {
		t.Errorf("box of 6 rows: %q", v)
	}
	m.SetSize(10, 20)
	if v := m.View().Content; v != "" {
		t.Errorf("box of 10 cells: %q", v)
	}
}
