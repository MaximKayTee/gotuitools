package layout

import tea "charm.land/bubbletea/v2"

// Zone is a screen rectangle taking mouse input. Any handler may be nil.
type Zone struct {
	Rect
	Click func(x, y int) tea.Cmd
	// Drag follows the pointer after a click in the zone, button held;
	// Release ends it.
	Drag    func(x, y int) tea.Cmd
	Release func(x, y int) tea.Cmd
	Wheel   func(d int) tea.Cmd // -1 up, 1 down
	Middle  func(x, y int) tea.Cmd
	Right   func(x, y int) tea.Cmd
	// Hover follows the pointer over the zone, no drag in progress;
	// Leave runs once it is off the zone.
	Hover func(x, y int) tea.Cmd
	Leave func() tea.Cmd
	// Pointer is the shape over the zone: PointerColumn on a divider.
	Pointer string
}

// Zones are the mouse zones of the last draw: later ones are on top.
// Reset at the start of each draw, Add while drawing, Update in Update.
type Zones struct {
	list    []Zone
	drag    *Zone
	hover   *Zone
	pointer string
	// Back and Forward run on the mouse's back and forward buttons.
	Back, Forward func() tea.Cmd
}

// Reset forgets the zones; a drag in progress continues.
func (z *Zones) Reset() {
	if z.drag != nil {
		d := *z.drag
		z.drag = &d
	}
	z.list = z.list[:0]
}

// Add puts a zone on top.
func (z *Zones) Add(zone Zone) { z.list = append(z.list, zone) }

// At is the topmost zone at x, y that ok accepts; nil none.
func (z *Zones) At(x, y int, ok func(*Zone) bool) *Zone {
	for i := len(z.list) - 1; i >= 0; i-- {
		if z.list[i].Has(x, y) && (ok == nil || ok(&z.list[i])) {
			return &z.list[i]
		}
	}
	return nil
}

// Dragging reports a drag in progress.
func (z *Zones) Dragging() bool { return z.drag != nil }

// Update handles mouse messages: handled is false for anything else
// and for mouse input no zone took. A motion is handled when a Hover
// or a Leave ran.
func (z *Zones) Update(msg tea.Msg) (cmd tea.Cmd, handled bool) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		switch msg.Button {
		case tea.MouseBackward:
			if z.Back != nil {
				return z.Back(), true
			}
		case tea.MouseForward:
			if z.Forward != nil {
				return z.Forward(), true
			}
		case tea.MouseMiddle:
			if hit := z.At(msg.X, msg.Y, func(z *Zone) bool { return z.Middle != nil }); hit != nil {
				return hit.Middle(msg.X, msg.Y), true
			}
		case tea.MouseRight:
			if hit := z.At(msg.X, msg.Y, func(z *Zone) bool { return z.Right != nil }); hit != nil {
				return hit.Right(msg.X, msg.Y), true
			}
		case tea.MouseLeft:
			if hit := z.At(msg.X, msg.Y, func(z *Zone) bool { return z.Click != nil || z.Drag != nil }); hit != nil {
				var shape tea.Cmd
				if hit.Drag != nil {
					d := *hit
					z.drag = &d
					if hit.Pointer != "" && hit.Pointer != z.pointer {
						z.pointer = hit.Pointer
						shape = tea.Raw(SetPointer(hit.Pointer))
					}
				}
				if hit.Click != nil {
					return tea.Batch(shape, hit.Click(msg.X, msg.Y)), true
				}
				return shape, true
			}
		}
	case tea.MouseMotionMsg:
		if z.drag != nil {
			return z.drag.Drag(msg.X, msg.Y), true
		}
		var cmds []tea.Cmd
		shape := ""
		if hit := z.At(msg.X, msg.Y, func(z *Zone) bool { return z.Pointer != "" }); hit != nil {
			shape = hit.Pointer
		}
		if shape != z.pointer {
			z.pointer = shape
			cmds = append(cmds, tea.Raw(SetPointer(shape)))
		}
		hit := z.At(msg.X, msg.Y, func(z *Zone) bool { return z.Hover != nil })
		handled := false
		if was := z.hover; was != nil && (hit == nil || hit.Rect != was.Rect) {
			z.hover, handled = nil, true
			if was.Leave != nil {
				cmds = append(cmds, was.Leave())
			}
		}
		if hit != nil {
			h := *hit
			z.hover, handled = &h, true
			cmds = append(cmds, hit.Hover(msg.X, msg.Y))
		}
		return tea.Batch(cmds...), handled
	case tea.MouseReleaseMsg:
		if d := z.drag; d != nil {
			z.drag = nil
			var shape tea.Cmd
			if z.pointer != "" {
				z.pointer = ""
				shape = tea.Raw(SetPointer(""))
			}
			if d.Release != nil {
				return tea.Batch(shape, d.Release(msg.X, msg.Y)), true
			}
			return shape, true
		}
	case tea.MouseWheelMsg:
		d := 0
		switch msg.Button {
		case tea.MouseWheelUp:
			d = -1
		case tea.MouseWheelDown:
			d = 1
		}
		if hit := z.At(msg.X, msg.Y, func(z *Zone) bool { return z.Wheel != nil }); hit != nil && d != 0 {
			return hit.Wheel(d), true
		}
	}
	return nil, false
}

// Divider is a zone on a draggable rule between panes: column x for a
// vertical rule (row y for a horizontal one when Row is set); set gets
// the pointer's column (row) while dragged, done runs on release.
func Divider(r Rect, row bool, set func(pos int), done func()) Zone {
	shape := PointerColumn
	if row {
		shape = PointerRow
	}
	pos := func(x, y int) int {
		if row {
			return y
		}
		return x
	}
	return Zone{
		Rect:    r,
		Pointer: shape,
		Drag:    func(x, y int) tea.Cmd { set(pos(x, y)); return nil },
		Release: func(int, int) tea.Cmd {
			if done != nil {
				done()
			}
			return nil
		},
	}
}
