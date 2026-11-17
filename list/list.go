// Package list keeps a cursor and a scroll offset over rows, and draws
// the scroll thumb on a border.
package list

// Thumb is the heavy border glyph drawn over the thumb's rows.
const Thumb = "┃"

// Pos is a scrolled list: the first shown row, the rows shown, the rows
// in all. Skip: border rows above the list's (a fixed input line); Len:
// the list's border rows, 0 all after Skip.
type Pos struct {
	Top, Shown, Total int
	Skip, Len         int
}

// Overflows reports more rows than shown.
func (p Pos) Overflows() bool { return p.Total > p.Shown && p.Shown > 0 }

// Span is the thumb on a track of n cells: first cell, length; 0, 0
// when p does not overflow or n < 1. The length is at least 1; the
// thumb touches the track's end only at the last top.
func (p Pos) Span(n int) (start, length int) {
	if !p.Overflows() || n < 1 {
		return 0, 0
	}
	length = max(1, min(n, n*p.Shown/p.Total))
	maxTop := p.Total - p.Shown
	top := min(max(p.Top, 0), maxTop)
	room := n - length
	start = room * top / maxTop
	if top > 0 && start == 0 && room > 0 {
		start = 1
	}
	if top < maxTop && start == room && room > 0 {
		start = room - 1
	}
	return start, length
}

// Rows marks the thumb's rows of a border n rows high: the track is
// Len rows (0: the rest) from Skip.
func (p Pos) Rows(n int) []bool {
	out := make([]bool, max(n, 0))
	track := n - p.Skip
	if p.Len > 0 {
		track = min(track, p.Len)
	}
	s, l := p.Span(track)
	for i := p.Skip + s; i < p.Skip+s+l && i < n; i++ {
		if i >= 0 {
			out[i] = true
		}
	}
	return out
}

// Cursor is a cursor over N one-row items with the first shown row.
// Height is the rows shown.
type Cursor struct {
	At, Top   int
	N, Height int
	// Wrap: up on the first item goes to the last and back.
	Wrap bool
}

// Clamp keeps At on an item and in view.
func (c *Cursor) Clamp() {
	c.At = min(max(c.At, 0), max(c.N-1, 0))
	h := max(c.Height, 1)
	c.Top = min(c.Top, c.At)
	if c.At >= c.Top+h {
		c.Top = c.At - h + 1
	}
	c.Top = max(min(c.Top, c.N-h), 0)
}

// Move moves the cursor by d items; ±1 wraps when Wrap is set.
func (c *Cursor) Move(d int) {
	if c.N == 0 {
		return
	}
	at := c.At + d
	if c.Wrap && (d == 1 || d == -1) {
		at = (at + c.N) % c.N
	}
	c.At = at
	c.Clamp()
}

// Page is the step of page up and down: the rows shown less one.
func (c *Cursor) Page() int { return max(c.Height-1, 1) }

// Key moves for up, down, ctrl+p, ctrl+n, k, j (with vim), pgup, pgdown,
// home, end, g, G (with vim), and reports whether it did.
func (c *Cursor) Key(key string, vim bool) bool {
	switch key {
	case "up", "ctrl+p":
		c.Move(-1)
	case "down", "ctrl+n":
		c.Move(1)
	case "pgup":
		c.Move(-c.Page())
	case "pgdown":
		c.Move(c.Page())
	case "home":
		c.At = 0
		c.Clamp()
	case "end":
		c.At = c.N - 1
		c.Clamp()
	default:
		if !vim {
			return false
		}
		switch key {
		case "k":
			c.Move(-1)
		case "j":
			c.Move(1)
		case "g":
			c.At = 0
			c.Clamp()
		case "G":
			c.At = c.N - 1
			c.Clamp()
		default:
			return false
		}
	}
	return true
}

// Scroll moves the view by d rows, the cursor kept in it.
func (c *Cursor) Scroll(d int) {
	h := max(c.Height, 1)
	c.Top = max(min(c.Top+d, c.N-h), 0)
	c.At = min(max(c.At, c.Top), c.Top+h-1)
	c.Clamp()
}

// Visible is the range of items shown: [from, to).
func (c *Cursor) Visible() (from, to int) {
	return c.Top, min(c.Top+max(c.Height, 0), c.N)
}

// Hit is the item on shown row y, -1 none.
func (c *Cursor) Hit(y int) int {
	if i := c.Top + y; y >= 0 && y < c.Height && i < c.N {
		return i
	}
	return -1
}

// Pos is the cursor's scroll position.
func (c *Cursor) Pos() Pos { return Pos{Top: c.Top, Shown: c.Height, Total: c.N} }

// Scroller is a cursor over items of varying height with the first
// shown item; Render records which item each drawn line is.
type Scroller struct {
	At, Top int
	spans   []int
}

// Render lays n items out in h lines from Top, moving Top so the
// cursor item shows whole.
func (s *Scroller) Render(n, h int, item func(i int, cur bool) []string) []string {
	s.At = min(max(s.At, 0), max(n-1, 0))
	s.Top = min(max(s.Top, 0), s.At)
	heights := map[int]int{}
	height := func(i int) int {
		if v, ok := heights[i]; ok {
			return v
		}
		heights[i] = len(item(i, i == s.At))
		return heights[i]
	}
	for s.Top < s.At {
		used := 0
		for i := s.Top; i <= s.At; i++ {
			used += height(i)
		}
		if used <= h {
			break
		}
		s.Top++
	}
	lines := make([]string, 0, h)
	s.spans = s.spans[:0]
	for i := s.Top; i < n && len(lines) < h; i++ {
		for _, l := range item(i, i == s.At) {
			if len(lines) == h {
				break
			}
			lines = append(lines, l)
			s.spans = append(s.spans, i)
		}
	}
	return lines
}

// Hit is the item at drawn line y, -1 none.
func (s *Scroller) Hit(y int) int {
	if y < 0 || y >= len(s.spans) {
		return -1
	}
	return s.spans[y]
}
