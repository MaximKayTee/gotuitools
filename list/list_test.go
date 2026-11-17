package list

import (
	"strconv"
	"testing"
)

func TestSpan(t *testing.T) {
	for _, c := range []struct {
		p       Pos
		n, s, l int
	}{
		{Pos{Top: 0, Shown: 10, Total: 5}, 10, 0, 0},
		{Pos{Top: 0, Shown: 10, Total: 40}, 10, 0, 2},
		{Pos{Top: 30, Shown: 10, Total: 40}, 10, 8, 2},
		{Pos{Top: 1, Shown: 10, Total: 40}, 10, 1, 2},
		{Pos{Top: 29, Shown: 10, Total: 40}, 10, 7, 2},
	} {
		if s, l := c.p.Span(c.n); s != c.s || l != c.l {
			t.Errorf("%+v Span(%d) = %d,%d want %d,%d", c.p, c.n, s, l, c.s, c.l)
		}
	}
	rows := Pos{Top: 0, Shown: 4, Total: 8, Skip: 1}.Rows(5)
	if !rows[1] || !rows[2] || rows[0] || rows[3] {
		t.Errorf("Rows %v", rows)
	}
}

func TestCursor(t *testing.T) {
	c := Cursor{N: 10, Height: 3}
	c.Move(5)
	if c.At != 5 || c.Top != 3 {
		t.Fatalf("%+v", c)
	}
	c.Key("pgdown", false)
	if c.At != 7 {
		t.Errorf("pgdown %+v", c)
	}
	c.Key("end", false)
	if c.At != 9 || c.Top != 7 {
		t.Errorf("end %+v", c)
	}
	if c.Key("j", false) || !c.Key("g", true) || c.At != 0 {
		t.Errorf("vim %+v", c)
	}
	c.Wrap = true
	c.Move(-1)
	if c.At != 9 {
		t.Errorf("wrap %+v", c)
	}
	c.Scroll(-5)
	if c.Top != 2 || c.At != 4 {
		t.Errorf("scroll %+v", c)
	}
	if c.Hit(1) != 3 || c.Hit(3) != -1 {
		t.Error("Hit")
	}
	if f, to := c.Visible(); f != 2 || to != 5 {
		t.Error("Visible")
	}
	empty := Cursor{Height: 3}
	empty.Move(1)
	empty.Clamp()
	if empty.At != 0 || empty.Top != 0 {
		t.Errorf("empty %+v", empty)
	}
}

func TestScroller(t *testing.T) {
	var s Scroller
	item := func(i int, cur bool) []string {
		out := []string{strconv.Itoa(i)}
		if i%2 == 1 {
			out = append(out, "-")
		}
		return out
	}
	s.At = 5
	lines := s.Render(8, 4, item)
	if len(lines) != 4 || s.Hit(len(lines)-1) != 5 && s.Hit(len(lines)-2) != 5 {
		t.Errorf("cursor not shown: %q top %d", lines, s.Top)
	}
	if s.Hit(-1) != -1 || s.Hit(10) != -1 {
		t.Error("Hit outside")
	}
}
