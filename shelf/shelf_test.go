package shelf

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/layout"
)

func keys(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return out
}

func look(opened *[2]int) Look {
	return Look{TileW: 4, TileH: 2,
		Tile: func(g, i int, marked bool) []string {
			mark := " "
			if marked {
				mark = "*"
			}
			return []string{fmt.Sprintf("%d.%d%s", g, i, mark), "----"}
		},
		Open: func(g, i int) tea.Cmd { *opened = [2]int{g, i}; return nil },
	}
}

func TestMoves(t *testing.T) {
	var s Shelf
	if _, _, ok := s.Marked(); ok || s.Across(1) || s.Down(1) {
		t.Fatal("an empty shelf has a mark")
	}
	s.Set([]Group{{Name: "Albums", Keys: keys("a", 5)}, {Name: "EPs"}, {Name: "Singles", Keys: keys("s", 2)}})
	if g, i, ok := s.Marked(); !ok || g != 0 || i != 0 || s.Len() != 3 {
		t.Fatalf("marked %d %d %v", g, i, ok)
	}
	if !s.Across(3) || s.Across(9) != true || s.Across(1) {
		t.Fatal("across: not to the end and no further")
	}
	s.Down(1)
	if g, _, ok := s.Marked(); ok || g != 1 {
		t.Fatalf("a group without tiles: %d %v", g, ok)
	}
	if !s.Down(5) || s.Down(1) {
		t.Fatal("down: not to the last and no further")
	}
	s.Across(1)
	// set again: the marks on the same keys, the marked group the same
	s.Set([]Group{{Name: "Live", Keys: keys("l", 1)}, {Name: "Singles", Keys: []string{"new", "s0", "s1"}}, {Name: "Albums", Keys: []string{"a0", "a4"}}})
	if g, i, _ := s.Marked(); g != 1 || i != 2 {
		t.Errorf("after Set: marked %d %d, want Singles s1", g, i)
	}
	s.Down(1)
	if g, i, _ := s.Marked(); g != 2 || i != 1 {
		t.Errorf("Albums: marked %d %d, want a4", g, i)
	}
	s.Mark(0, 7)
	if g, i, _ := s.Marked(); g != 0 || i != 0 {
		t.Errorf("Mark: %d %d", g, i)
	}
}

func TestLines(t *testing.T) {
	var s Shelf
	var opened [2]int
	l := look(&opened)
	if got := s.Lines(l, 0, 0, 40, 10, nil); len(got) != 1 || got[0] != "  None" {
		t.Fatalf("empty: %q", got)
	}
	s.Set([]Group{{Name: "Albums", Keys: keys("a", 5)}, {Name: "EPs", Note: "Loading"}, {Name: "Singles", Keys: keys("s", 2)}})
	var z layout.Zones
	// 3 tiles of 4 cells and gaps of 2 fit 17 cells; two groups and the
	// third's name fit 9 rows
	got := s.Lines(l, 5, 3, 17, 9, &z)
	want := []string{
		" Albums  1/5",
		" 0.0*  0.1   0.2 ",
		" ----  ----  ----",
		"",
		" EPs",
		" Loading",
		"",
		"",
		" Singles",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if l.Per(17) != 3 || l.Per(16) != 2 || l.Visible(9) != 2 || l.Visible(3) != 1 {
		t.Errorf("per %d %d, visible %d %d", l.Per(17), l.Per(16), l.Visible(9), l.Visible(3))
	}
	click := func(x, y int) { z.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) }
	// the second tile is cells 12 to 15, rows 4 and 5
	click(12, 5)
	if g, i, _ := s.Marked(); g != 0 || i != 1 || opened != [2]int{} {
		t.Fatalf("click: marked %d %d, opened %v", g, i, opened)
	}
	click(15, 4)
	if opened != [2]int{0, 1} {
		t.Fatalf("click on the marked tile: opened %v", opened)
	}
	// the mark past what is shown scrolls its group, and the groups
	s.Across(3)
	z.Reset()
	if got := s.Lines(l, 5, 3, 17, 9, &z); got[0] != " Albums  5/5" || got[1] != " 0.2   0.3   0.4*" {
		t.Errorf("scrolled across:\n%s", strings.Join(got, "\n"))
	}
	z.Update(tea.MouseWheelMsg{X: 6, Y: 4, Button: tea.MouseWheelDown})
	z.Update(tea.MouseWheelMsg{X: 6, Y: 4, Button: tea.MouseWheelDown})
	z.Reset()
	got = s.Lines(l, 5, 3, 17, 9, &z)
	if g, i, _ := s.Marked(); g != 2 || i != 0 || got[0] != " EPs" || got[4] != " Singles" || got[5] != " 2.0*  2.1 " {
		t.Errorf("wheel: marked %d %d\n%s", g, i, strings.Join(got, "\n"))
	}
	if n := len(s.Lines(l, 0, 0, 17, 2, nil)); n != 2 {
		t.Errorf("2 rows: %d lines", n)
	}
}
