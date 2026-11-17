package modal

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/list"
	"github.com/MaximKayTee/gotuitools/theme"
)

type probe struct {
	name     string
	w, h     int
	got      []tea.Msg
	done     bool
	x, y     int
	disposed bool
}

type ping struct{}

func (p *probe) Init() tea.Cmd { return func() tea.Msg { return ping{} } }
func (p *probe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	p.got = append(p.got, msg)
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" {
		p.done = true
	}
	return p, nil
}
func (p *probe) View() tea.View           { return tea.NewView("[" + p.name + "]") }
func (p *probe) SetSize(w, h int) tea.Cmd { p.w, p.h = w, h; return nil }
func (p *probe) Done() bool               { return p.done }
func (p *probe) SetOrigin(x, y int)       { p.x, p.y = x, y }
func (p *probe) Dispose()                 { p.disposed = true }

// run feeds cmd's messages back through the stack until none are left.
func run(t *testing.T, s *Stack, cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if next, handled := s.Update(msg); handled {
			queue = append(queue, next)
		} else if msg != nil {
			out = append(out, msg)
		}
	}
	return out
}

func TestStackRoutesAndCloses(t *testing.T) {
	s := NewStack()
	s.Resize(80, 24)
	a, b := &probe{name: "a"}, &probe{name: "b"}
	run(t, s, OpenCmd(a))
	run(t, s, s.Open(b))
	if a.w != 76 || a.h != 22 || s.Len() != 2 || s.Top() != b {
		t.Fatalf("size %dx%d len %d", a.w, a.h, s.Len())
	}
	if len(a.got) != 1 || len(b.got) != 1 {
		t.Fatalf("Init messages: a %v b %v", a.got, b.got)
	}
	run(t, s, func() tea.Msg { return tea.KeyPressMsg{Code: 'x', Text: "x"} })
	if len(b.got) != 2 || len(a.got) != 1 {
		t.Error("key not to top only")
	}
	out := run(t, s, func() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyEscape} })
	if s.Len() != 1 || !b.disposed || len(out) != 1 || out[0].(Closed).Modal != b {
		t.Errorf("close: len %d out %v", s.Len(), out)
	}
	view := s.View(strings.Repeat(strings.Repeat(".", 20)+"\n", 5)[:20*5+4], 20)
	if lines := strings.Split(view, "\n"); ansi.Strip(lines[2]) != "........[a]........." || a.x != 8 || a.y != 2 {
		t.Errorf("view %q at %d,%d", ansi.Strip(lines[2]), a.x, a.y)
	}
	if _, handled := s.Update(tea.WindowSizeMsg{Width: 10, Height: 4}); handled || a.w != 10 {
		t.Error("resize")
	}
}

func TestOverlayWideEdges(t *testing.T) {
	base := []string{"日本語日本語"}
	OverlayAt(base, 1, 0, 12, []string{"ab"})
	if got := ansi.Strip(base[0]); ansi.StringWidth(got) != 12 || !strings.Contains(got, "ab") {
		t.Errorf("%q", got)
	}
}

func TestFrame(t *testing.T) {
	lines := Frame(theme.Default(), Box{Title: "Commands", Keys: "esc Close", Pos: list.Pos{Top: 0, Shown: 2, Total: 4}}, []string{"one", "two"}, 20)
	if len(lines) != 4 {
		t.Fatal(len(lines))
	}
	for i, l := range lines {
		if ansi.StringWidth(l) != 20 {
			t.Errorf("line %d width %d: %q", i, ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	if !strings.Contains(ansi.Strip(lines[0]), "Commands") || !strings.Contains(ansi.Strip(lines[3]), "esc Close") || !strings.HasSuffix(ansi.Strip(lines[1]), list.Thumb) {
		t.Errorf("%q", ansi.Strip(strings.Join(lines, "\n")))
	}
}

type result struct{ v string }

// opener's Init is a result for the program; enter opens inner and closes.
type opener struct {
	probe
	inner Modal
	s     *Stack
}

func (o *opener) Init() tea.Cmd { return func() tea.Msg { return result{"init"} } }
func (o *opener) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "enter" {
		o.done = true
		return o, o.s.Open(o.inner)
	}
	return o, nil
}

func TestClosedModalCmdsAndResultsReachProgram(t *testing.T) {
	s := NewStack()
	s.Resize(80, 24)
	inner := &probe{name: "inner"}
	o := &opener{inner: inner, s: s}
	out := run(t, s, s.Open(o))
	if len(out) != 1 || out[0] != (result{"init"}) {
		t.Fatalf("program did not get the modal's result: %v", out)
	}
	run(t, s, func() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyEnter} })
	if s.Len() != 1 || s.Top() != inner {
		t.Fatalf("len %d", s.Len())
	}
	if len(inner.got) != 1 {
		t.Errorf("inner's Init, opened from a closing modal, lost: %v", inner.got)
	}
}

type marked struct{}

func (marked) ToModals() {}

func TestTwoStacksAndToModals(t *testing.T) {
	a, b := NewStack(), NewStack()
	pa, pb := &probe{name: "a"}, &probe{name: "b"}
	a.Open(pa)
	b.Open(pb)
	msg := tea.Msg(routed{a, 1, ping{}})
	if _, handled := b.Update(msg); handled {
		t.Error("other stack's message taken")
	}
	if _, handled := a.Update(msg); !handled || len(pa.got) != 1 {
		t.Error("own message not delivered")
	}
	if _, handled := b.Update(marked{}); handled || len(pb.got) != 1 {
		t.Errorf("ToModals: handled %v got %v", handled, pb.got)
	}
}
