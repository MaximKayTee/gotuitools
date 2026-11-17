// Package modal is the component contract and a stack of dialogs drawn
// over a program's screen.
//
// While a Stack has a modal open, the top one gets every key, mouse and
// paste message; messages from a modal's Cmds come back to that modal.
// Esc closing a modal is the modal's own: it sets Done.
package modal

import (
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Component is a sized region. Implementations are pointers so SetSize
// sticks; Update returns the same Component. View's Content is the
// region, at most the size given.
type Component interface {
	tea.Model
	// SetSize: content cells. The Cmd is routed as Update's.
	SetSize(w, h int) tea.Cmd
}

// Modal is a dialog. SetSize gives the box it may use, at most the
// screen; it may draw smaller. A border is its own.
type Modal interface {
	Component
	// Done reports closed; the stack drops it after the Update that set it.
	Done() bool
}

// Placer is an optional method: the region's top-left screen cell, set
// after each draw, for mouse coordinates.
type Placer interface{ SetOrigin(x, y int) }

// Disposer is an optional method, called once when a modal closes.
type Disposer interface{ Dispose() }

// Open is a message any Cmd may return: show Modal on top.
type Open struct{ Modal Modal }

// OpenCmd is a Cmd returning Open.
func OpenCmd(m Modal) tea.Cmd { return func() tea.Msg { return Open{m} } }

// Closed is sent after a modal closes: Modal is the closed one.
type Closed struct{ Modal Modal }

// Stack is the open modals, bottom first.
type Stack struct {
	// MarginX, MarginY: cells kept free each side, rows top and bottom.
	// Below MinW x MinH a modal gets the whole screen.
	MarginX, MarginY, MinW, MinH int

	entries []entry
	seq     int
	w, h    int
}

type entry struct {
	id int
	m  Modal
}

// NewStack has margins 2, 1 and minimum 20 x 5.
func NewStack() *Stack { return &Stack{MarginX: 2, MarginY: 1, MinW: 20, MinH: 5} }

// Len is the number of open modals.
func (s *Stack) Len() int { return len(s.entries) }

// Top is the modal taking keys, nil none.
func (s *Stack) Top() Modal {
	if len(s.entries) == 0 {
		return nil
	}
	return s.entries[len(s.entries)-1].m
}

// Modals are the open modals, bottom first.
func (s *Stack) Modals() []Modal {
	out := make([]Modal, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.m
	}
	return out
}

// Box is the size granted to a modal on a w x h screen.
func (s *Stack) Box(w, h int) (bw, bh int) {
	bw, bh = w, h
	if w-2*s.MarginX >= s.MinW {
		bw -= 2 * s.MarginX
	}
	if h-2*s.MarginY >= s.MinH {
		bh -= 2 * s.MarginY
	}
	return bw, bh
}

// Open shows m on top, sized, and returns its Init.
func (s *Stack) Open(m Modal) tea.Cmd {
	if m == nil {
		return nil
	}
	s.seq++
	e := entry{id: s.seq, m: m}
	s.entries = append(s.entries, e)
	var size tea.Cmd
	if s.w > 0 {
		size = s.wrap(e.id, m.SetSize(s.Box(s.w, s.h)))
	}
	return tea.Batch(size, s.wrap(e.id, m.Init()))
}

// Resize sizes every modal for a w x h screen.
func (s *Stack) Resize(w, h int) tea.Cmd {
	s.w, s.h = w, h
	var cmds []tea.Cmd
	for _, e := range s.entries {
		cmds = append(cmds, s.wrap(e.id, e.m.SetSize(s.Box(w, h))))
	}
	return tea.Batch(cmds...)
}

// Update routes msg. handled is true when msg was a modal's: Open, a
// message from a modal's Cmd, or input while one is open. A message
// from a modal's Cmd goes to that modal if still open, then comes back
// untagged for the program. A
// WindowSizeMsg resizes the modals and is not handled, so the program
// sizes itself too.
func (s *Stack) Update(msg tea.Msg) (cmd tea.Cmd, handled bool) {
	switch msg := msg.(type) {
	case Open:
		return s.Open(msg.Modal), true
	case routed:
		if msg.s != s {
			return nil, false
		}
		// to its modal while open, and on to the program: Open, a
		// modal's results (settingsui.ChangedMsg), a closed modal's Cmds
		inner := msg.msg
		return tea.Batch(s.send(msg.id, inner), s.reap(), func() tea.Msg { return inner }), true
	case tea.WindowSizeMsg:
		return s.Resize(msg.Width, msg.Height), false
	case ToModals:
		return s.Broadcast(msg), false
	case tea.KeyPressMsg, tea.KeyReleaseMsg, tea.MouseMsg, tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
		if len(s.entries) == 0 {
			return nil, false
		}
		top := s.entries[len(s.entries)-1].id
		return tea.Batch(s.send(top, msg), s.reap()), true
	}
	return nil, false
}

// Broadcast sends msg to every open modal.
func (s *Stack) Broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, e := range s.entries {
		cmds = append(cmds, s.send(e.id, msg))
	}
	return tea.Batch(append(cmds, s.reap())...)
}

// CloseAll drops every modal.
func (s *Stack) CloseAll() tea.Cmd {
	for _, e := range s.entries {
		if d, ok := e.m.(Disposer); ok {
			d.Dispose()
		}
	}
	s.entries = nil
	return nil
}

func (s *Stack) send(id int, msg tea.Msg) tea.Cmd {
	for i, e := range s.entries {
		if e.id != id {
			continue
		}
		nm, cmd := e.m.Update(msg)
		if nd, ok := nm.(Modal); ok && nd != nil {
			s.entries[i].m = nd
		}
		return s.wrap(id, cmd)
	}
	return nil
}

func (s *Stack) reap() tea.Cmd {
	var cmds []tea.Cmd
	out := s.entries[:0]
	for _, e := range s.entries {
		if e.m.Done() {
			if d, ok := e.m.(Disposer); ok {
				d.Dispose()
			}
			m := e.m
			cmds = append(cmds, func() tea.Msg { return Closed{m} })
			continue
		}
		out = append(out, e)
	}
	clear(s.entries[len(out):])
	s.entries = out
	return tea.Batch(cmds...)
}

// View draws the modals centred over base, a w-wide screen, bottom
// first, and tells each Placer where it went.
func (s *Stack) View(base string, w int) string {
	if len(s.entries) == 0 {
		return base
	}
	lines := strings.Split(base, "\n")
	for _, e := range s.entries {
		x, y := Overlay(lines, e.m.View().Content, w)
		if p, ok := e.m.(Placer); ok {
			p.SetOrigin(x, y)
		}
	}
	return strings.Join(lines, "\n")
}

// routed is a message from modal id's Cmd in stack s.
type routed struct {
	s   *Stack
	id  int
	msg tea.Msg
}

func (s *Stack) wrap(id int, cmd tea.Cmd) tea.Cmd {
	return Tag(cmd, func(m tea.Msg) tea.Msg { return routed{s, id, m} })
}

// ToModals marks a message for every open modal of every stack, as
// well as the program: one that comes back outside the modal's Cmds,
// such as a tea.ExecProcess callback (sys.EditedMsg).
type ToModals interface{ ToModals() }

var (
	teaPkg  = reflect.TypeFor[tea.BatchMsg]().PkgPath()
	cmdType = reflect.TypeFor[tea.Cmd]()
)

// Tag wraps the messages cmd produces with wrap. Bubble Tea's own
// messages pass unwrapped; its batches and sequences have their Cmds
// tagged in turn.
func Tag(cmd tea.Cmd, wrap func(tea.Msg) tea.Msg) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return tagMsg(cmd(), wrap) }
}

func tagMsg(msg tea.Msg, wrap func(tea.Msg) tea.Msg) tea.Msg {
	if msg == nil {
		return nil
	}
	if _, ok := msg.(routed); ok {
		return msg // already tagged by the modal that made it
	}
	t := reflect.TypeOf(msg)
	if t.PkgPath() != teaPkg {
		return wrap(msg)
	}
	if t.Kind() != reflect.Slice || t.Elem() != cmdType {
		return msg
	}
	v := reflect.ValueOf(msg)
	out := reflect.MakeSlice(t, v.Len(), v.Len())
	for i := range v.Len() {
		c, _ := v.Index(i).Interface().(tea.Cmd)
		out.Index(i).Set(reflect.ValueOf(Tag(c, wrap)))
	}
	return out.Interface()
}

const sgrReset = "\x1b[m"

// Overlay draws view centred over base, w cells by len(base) lines, in
// place, and returns its top-left cell.
func Overlay(base []string, view string, w int) (x, y int) {
	src := strings.Split(view, "\n")
	mw := 0
	for i, l := range src {
		src[i] = strings.TrimRight(l, "\r")
		mw = max(mw, ansi.StringWidth(src[i]))
	}
	mw = min(mw, w)
	if view == "" || mw == 0 || len(base) == 0 {
		return 0, 0
	}
	if len(src) > len(base) {
		src = src[:len(base)]
	}
	x, y = (w-mw)/2, (len(base)-len(src))/2
	OverlayAt(base, x, y, w, src)
	return x, y
}

// OverlayAt draws lines over base with their top-left at x, y, in
// place. base is w cells wide; lines past its edges are cut.
func OverlayAt(base []string, x, y, w int, lines []string) {
	mw := 0
	for _, l := range lines {
		mw = max(mw, ansi.StringWidth(l))
	}
	mw = min(mw, w-x)
	if mw <= 0 {
		return
	}
	for i, l := range lines {
		if y+i < 0 || y+i >= len(base) {
			continue
		}
		l = ansi.Truncate(l, mw, "")
		if pad := mw - ansi.StringWidth(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		row := base[y+i]
		if rw := ansi.StringWidth(row); rw < w {
			row += strings.Repeat(" ", w-rw)
		}
		left := ansi.Truncate(row, x, "")
		if pad := x - ansi.StringWidth(left); pad > 0 {
			// a wide character across the left edge
			left += sgrReset + strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(row, x+mw, "")
		if rest := w - x - mw; ansi.StringWidth(right) > rest {
			// a wide character across the right edge
			right = " " + ansi.TruncateLeft(row, x+mw+1, "")
		}
		base[y+i] = left + sgrReset + l + sgrReset + right
	}
}
