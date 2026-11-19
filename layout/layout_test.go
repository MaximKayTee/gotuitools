package layout

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRect(t *testing.T) {
	r := Rect{0, 0, 80, 24}
	left, rest := r.Left(30, 1)
	if left != (Rect{0, 0, 30, 24}) || rest != (Rect{31, 0, 49, 24}) {
		t.Errorf("Left %v %v", left, rest)
	}
	body, status := r.Bottom(1, 0)
	if status != (Rect{0, 23, 80, 1}) || body.H != 23 {
		t.Errorf("Bottom %v %v", body, status)
	}
	top, _ := r.Top(100, 0)
	if top.H != 24 || !r.Has(79, 23) || r.Has(80, 0) || r.Inset(1) != (Rect{1, 1, 78, 22}) {
		t.Error("Top/Has/Inset")
	}
	if Share(100, 30, 40, 60) != 40 || Share(20, 30, 40, 60) != 20 || Clamp(5, 1, 3) != 3 {
		t.Error("Share/Clamp")
	}
}

func TestTooSmall(t *testing.T) {
	if _, small := TooSmall(80, 24, 40, 12); small {
		t.Error("big enough")
	}
	v, small := TooSmall(30, 5, 40, 12)
	if lines := strings.Split(v, "\n"); !small || len(lines) != 5 || !strings.Contains(lines[2], "Window too small") {
		t.Errorf("%q", v)
	}
}

func TestZones(t *testing.T) {
	var z Zones
	var log []string
	z.Add(Zone{Rect: Rect{0, 0, 10, 10}, Click: func(x, y int) tea.Cmd { log = append(log, "under"); return nil }})
	z.Add(Zone{Rect: Rect{0, 0, 5, 5}, Click: func(x, y int) tea.Cmd { log = append(log, "over"); return nil },
		Wheel: func(d int) tea.Cmd { log = append(log, "wheel"); return nil }})
	width := 30
	z.Add(Divider(Rect{20, 0, 1, 10}, false, func(x int) { width = x }, func() { log = append(log, "saved") }))
	z.Update(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	z.Update(tea.MouseClickMsg{X: 7, Y: 7, Button: tea.MouseLeft})
	z.Update(tea.MouseWheelMsg{X: 1, Y: 1, Button: tea.MouseWheelDown})
	if _, h := z.Update(tea.MouseClickMsg{X: 50, Y: 1, Button: tea.MouseLeft}); h {
		t.Error("click on nothing handled")
	}
	if cmd, _ := z.Update(tea.MouseMotionMsg{X: 20, Y: 3}); cmd == nil {
		t.Error("no pointer change over divider")
	}
	z.Update(tea.MouseClickMsg{X: 20, Y: 3, Button: tea.MouseLeft})
	z.Reset() // a redraw during the drag
	z.Update(tea.MouseMotionMsg{X: 26, Y: 3})
	z.Update(tea.MouseReleaseMsg{X: 26, Y: 3})
	if width != 26 || z.Dragging() {
		t.Errorf("drag width %d", width)
	}
	if strings.Join(log, ",") != "over,under,wheel,saved" {
		t.Errorf("%v", log)
	}
	// hover: over, moved within, a redraw, off
	log = nil
	z.Reset()
	bar := Zone{Rect: Rect{0, 12, 10, 1}, Hover: func(x, y int) tea.Cmd { log = append(log, "hover"); return nil },
		Leave: func() tea.Cmd { log = append(log, "leave"); return nil }}
	z.Add(bar)
	if _, h := z.Update(tea.MouseMotionMsg{X: 3, Y: 12}); !h {
		t.Error("hover not handled")
	}
	z.Update(tea.MouseMotionMsg{X: 4, Y: 12})
	z.Reset()
	z.Add(bar)
	z.Update(tea.MouseMotionMsg{X: 5, Y: 12})
	if _, h := z.Update(tea.MouseMotionMsg{X: 5, Y: 13}); !h {
		t.Error("leave not handled")
	}
	if _, h := z.Update(tea.MouseMotionMsg{X: 5, Y: 14}); h {
		t.Error("motion over nothing handled")
	}
	if strings.Join(log, ",") != "hover,hover,hover,leave" {
		t.Errorf("hover %v", log)
	}
	if SetPointer("") != "\x1b]22;default\x1b\\" {
		t.Error("SetPointer")
	}
}

func TestSettle(t *testing.T) {
	var s Settle
	first := s.Bump()
	last := s.Bump()
	_ = first
	var other Settle
	other.Bump()
	if s.Fired(SettleMsg{&s, 1}) || !s.Fired(SettleMsg{&s, 2}) || s.Fired(SettleMsg{&other, 2}) || last == nil {
		t.Error("Fired")
	}
}
