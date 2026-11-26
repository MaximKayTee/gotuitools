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

// With Wrap a group's tiles are in rows, a grid: down and up move a
// row in the column, into the next group from the last row; a folded
// group is its name and one stop; the lines scroll to the marked row;
// only the tiles of rows shown are drawn; a click on a name runs Fold.
func TestGrid(t *testing.T) {
	var s Shelf
	var opened [2]int
	drawn, folded := 0, -1
	l := look(&opened)
	tile := l.Tile
	l.Tile = func(g, i int, marked bool) []string { drawn++; return tile(g, i, marked) }
	l.Wrap = true
	l.Fold = func(g int) tea.Cmd { folded = g; return nil }
	groups := []Group{{Name: "Albums", Keys: keys("a", 5)}, {Name: "EPs", Keys: keys("e", 3), Folded: true}, {Name: "Singles", Keys: keys("s", 2)}, {Name: "Live", Note: "Loading"}}
	s.Set(groups)
	var z layout.Zones
	got := s.Lines(l, 5, 3, 17, 20, &z)
	want := []string{
		" ▾ Albums  5",
		" 0.0*  0.1   0.2 ",
		" ----  ----  ----",
		" 0.3   0.4 ",
		" ----  ----",
		"",
		" ▸ EPs  3",
		" ▾ Singles  2",
		" 2.0   2.1 ",
		" ----  ----",
		"",
		" ▾ Live",
		" Loading",
		"",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	at := func() string {
		g, i, ok := s.Marked()
		if !ok {
			return fmt.Sprintf("%d", g)
		}
		return fmt.Sprintf("%d.%d", g, i)
	}
	// down a row in the column, the last tile where the row is short
	s.Across(2)
	s.Down(1)
	if at() != "0.4" {
		t.Errorf("down from 0.2: %s", at())
	}
	s.Across(-1)
	s.Down(-1)
	s.Across(1)
	s.Down(1)
	if at() != "0.4" {
		t.Errorf("down from 0.1: %s", at())
	}
	// out of the last row: the folded group, one stop; then the next
	if !s.Down(1) || at() != "1" {
		t.Errorf("down from the last row: %s", at())
	}
	if !s.Down(1) || at() != "2.0" {
		t.Errorf("down from the folded group: %s", at())
	}
	s.Across(1)
	if !s.Down(5) || at() != "3" || s.Down(1) {
		t.Errorf("down to the end: %s", at())
	}
	// up into the last row of an open group, in the column
	s.Mark(2, 1)
	groups[1].Folded = false
	s.Set(groups)
	s.Lines(l, 5, 3, 17, 20, nil)
	if at() != "2.1" {
		t.Fatalf("after Set: %s", at())
	}
	s.Down(-1)
	if at() != "1.1" {
		t.Errorf("up into the group opened: %s", at())
	}
	s.Down(-1)
	if at() != "0.4" {
		t.Errorf("up into the last row: %s", at())
	}
	s.Down(-1)
	if at() != "0.1" || s.Down(-1) {
		t.Errorf("up to the first row and no further: %s", at())
	}

	// 5 rows: the lines scroll to the marked row, with its group's name
	// when it is the first; the tiles of rows not shown are not drawn
	groups[1].Folded = true
	s.Set(groups)
	s.Mark(2, 0)
	drawn = 0
	z.Reset()
	got = s.Lines(l, 5, 3, 17, 5, &z)
	want = []string{"", " ▸ EPs  3", " ▾ Singles  2", " 2.0*  2.1 ", " ----  ----"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") || drawn != 2 {
		t.Errorf("%d tiles drawn, got\n%s\nwant\n%s", drawn, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	click := func(x, y int) { z.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) }
	// the names are rows 4 and 5, the second tile of Singles cells 12 to
	// 15 of rows 6 and 7
	click(8, 4)
	if folded != 1 {
		t.Errorf("click on a name: folded %d", folded)
	}
	click(12, 7)
	if at() != "2.1" {
		t.Errorf("click on a tile: %s", at())
	}
	click(12, 6)
	if opened != [2]int{2, 1} {
		t.Errorf("click on the marked tile: opened %v", opened)
	}
	z.Update(tea.MouseWheelMsg{X: 6, Y: 4, Button: tea.MouseWheelUp})
	z.Update(tea.MouseWheelMsg{X: 6, Y: 4, Button: tea.MouseWheelUp})
	// over the folded group, then the first column of the row above it
	if got := s.Lines(l, 5, 3, 17, 5, nil); at() != "0.3" || got[0] != " 0.3*  0.4 " || got[1] != " ----  ----" {
		t.Errorf("wheel up twice: %s\n%s", at(), strings.Join(got, "\n"))
	}
	if got := s.Lines(l, 5, 3, 17, 1, nil); len(got) != 1 {
		t.Errorf("a row: %d lines", len(got))
	}
}

// A folded group in a shelf of rows is its name with its count.
func TestFoldedRow(t *testing.T) {
	var s Shelf
	var opened [2]int
	l := look(&opened)
	l.Fold = func(int) tea.Cmd { return nil }
	s.Set([]Group{{Name: "Albums", Keys: keys("a", 2)}, {Name: "EPs", Keys: keys("e", 3), Folded: true}, {Name: "Singles", Keys: keys("s", 1)}})
	got := s.Lines(l, 0, 0, 17, 12, nil)
	want := []string{" ▾ Albums", " 0.0*  0.1 ", " ----  ----", "", " ▸ EPs  3", " ▾ Singles", " 2.0 ", " ----", ""}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	s.Down(1)
	if g, _, ok := s.Marked(); ok || g != 1 {
		t.Errorf("on the folded group: %d %v", g, ok)
	}
}
