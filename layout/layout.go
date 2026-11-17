// Package layout is screen rectangles, mouse zones, pointer shapes and
// the resize settle.
package layout

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/text"
)

// Rect is a cell rectangle.
type Rect struct{ X, Y, W, H int }

// Empty reports no cells.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Has reports whether cell x, y is inside.
func (r Rect) Has(x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

// Inset is r less n cells on every side.
func (r Rect) Inset(n int) Rect {
	return Rect{r.X + n, r.Y + n, max(r.W-2*n, 0), max(r.H-2*n, 0)}
}

// Left cuts w columns off the left: the cut, the rest less gap columns.
func (r Rect) Left(w, gap int) (left, rest Rect) {
	w = min(max(w, 0), r.W)
	return Rect{r.X, r.Y, w, r.H}, Rect{r.X + w + gap, r.Y, max(r.W-w-gap, 0), r.H}
}

// Top cuts h rows off the top: the cut, the rest less gap rows.
func (r Rect) Top(h, gap int) (top, rest Rect) {
	h = min(max(h, 0), r.H)
	return Rect{r.X, r.Y, r.W, h}, Rect{r.X, r.Y + h + gap, r.W, max(r.H-h-gap, 0)}
}

// Bottom cuts h rows off the bottom: the rest less gap rows, the cut.
func (r Rect) Bottom(h, gap int) (rest, bottom Rect) {
	h = min(max(h, 0), r.H)
	return Rect{r.X, r.Y, r.W, max(r.H-h-gap, 0)}, Rect{r.X, r.Y + r.H - h, r.W, h}
}

// Clamp is v within lo..hi; lo wins when hi < lo.
func Clamp(v, lo, hi int) int { return max(min(v, hi), lo) }

// Share is pct percent of total, clamped to lo..hi and to total.
func Share(total, pct, lo, hi int) int {
	return min(Clamp((total*pct+50)/100, lo, hi), total)
}

// TooSmall is the screen shown below minW x minH: "Window too small"
// centred in w x h. ok is false when the window is big enough.
func TooSmall(w, h, minW, minH int) (view string, ok bool) {
	if w >= minW && h >= minH {
		return "", false
	}
	lines := make([]string, max(h, 1))
	for i := range lines {
		lines[i] = text.Spaces(w)
	}
	lines[len(lines)/2] = text.Center("Window too small", w)
	out := lines[0]
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out, true
}

// Pointer shapes for SetPointer: CSS cursor names, as kitty, ghostty,
// foot and WezTerm take them.
const (
	PointerDefault  = "default"
	PointerPointer  = "pointer"
	PointerText     = "text"
	PointerColumn   = "ew-resize"
	PointerRow      = "ns-resize"
	PointerGrab     = "grab"
	PointerGrabbing = "grabbing"
)

// SetPointer is the OSC 22 sequence setting the mouse pointer shape;
// "" the default. Write it out of band (tea.Raw).
func SetPointer(shape string) string {
	if shape == "" {
		shape = PointerDefault
	}
	return "\x1b]22;" + shape + "\x1b\\"
}

// Settle runs a step once a burst of events stops: a video re-placed
// after the last resize. Bump on each event; the Msg of the last bump
// alone reports Fired.
type Settle struct {
	// Wait is the quiet time before firing; 0 is 200ms.
	Wait time.Duration
	gen  int
}

// SettleMsg is a Settle's tick.
type SettleMsg struct {
	s   *Settle
	gen int
}

// Bump restarts the wait.
func (s *Settle) Bump() tea.Cmd {
	s.gen++
	gen, wait := s.gen, s.Wait
	if wait == 0 {
		wait = 200 * time.Millisecond
	}
	return tea.Tick(wait, func(time.Time) tea.Msg { return SettleMsg{s, gen} })
}

// Fired reports msg is this Settle's tick from its last Bump.
func (s *Settle) Fired(msg tea.Msg) bool {
	m, ok := msg.(SettleMsg)
	return ok && m.s == s && m.gen == s.gen
}
