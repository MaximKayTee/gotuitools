// Package sliders is a box of vertical sliders side by side, each a
// value between the same two bounds, drawn as a bar from a zero line:
// a modal.Modal. An equaliser's bands. A change is reported as it is
// made.
//
//	left right, h l      the slider before, after
//	up down, k j         a step up, down
//	r                    every slider to Zero
//	space                the switch (Options.Switch)
//	enter esc ctrl+c     close
//	click, drag          that slider, to the value of the row
//	wheel                a step on the slider under the pointer
package sliders

import (
	"math"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// Slider is one bar: Label under it, then its value.
type Slider struct {
	Label string
	Value float64
}

// Options configure a Model.
type Options struct {
	Title string
	// Min and Max bound every value. Zero, from Min to Max, is where
	// the bars start and what r sets.
	Min, Max, Zero float64
	// Step is the change of a key or the wheel, and what a value is a
	// multiple of from Zero; 0 is 1.
	Step float64
	// Format is a value as text, under its slider and on the scale;
	// nil: the number with its sign, "0" for 0.
	Format func(v float64) string
	// Switch adds a switch, On its state at the start: drawn On or Off
	// over the sliders, the bars muted while off.
	Switch, On bool
	// Rows is the cells of bar over the zero line, and under it; 0 is
	// 4. Fewer in a low box.
	Rows int
	// Change runs after each change, with every value and the switch.
	Change func(values []float64, on bool) tea.Cmd
}

// Model is the box. Use New.
type Model struct {
	th     theme.Theme
	o      Options
	labels []string
	values []float64
	on     bool
	at     int
	// drag is the slider the pointer holds, -1 none.
	drag int

	w, h int
	x, y int
	done bool
}

// New is a box of sliders; values outside Min to Max are brought in.
func New(th theme.Theme, sliders []Slider, o Options) *Model {
	if o.Step <= 0 {
		o.Step = 1
	}
	if o.Max < o.Min {
		o.Min, o.Max = o.Max, o.Min
	}
	o.Zero = min(max(o.Zero, o.Min), o.Max)
	if o.Rows <= 0 {
		o.Rows = 4
	}
	m := &Model{th: th, o: o, on: o.On || !o.Switch, drag: -1}
	for _, s := range sliders {
		m.labels = append(m.labels, s.Label)
		m.values = append(m.values, m.snap(s.Value))
	}
	return m
}

// Values is every slider's value.
func (m *Model) Values() []float64 { return append([]float64(nil), m.values...) }

// On is the switch; true without one.
func (m *Model) On() bool { return m.on }

// At is the slider the keys go to.
func (m *Model) At() int { return m.at }

func (m *Model) Init() tea.Cmd { return nil }

// Done reports closed.
func (m *Model) Done() bool { return m.done }

// SetOrigin is where the box was drawn.
func (m *Model) SetOrigin(x, y int) { m.x, m.y = x, y }

// SetSize is the box the sliders may use.
func (m *Model) SetSize(w, h int) tea.Cmd {
	m.w, m.h = w, h
	return nil
}

// snap is v as a multiple of Step from Zero, within Min to Max.
func (m *Model) snap(v float64) float64 {
	if math.IsNaN(v) {
		v = m.o.Zero
	}
	v = m.o.Zero + math.Round((v-m.o.Zero)/m.o.Step)*m.o.Step
	return min(max(v, m.o.Min), m.o.Max)
}

func (m *Model) format(v float64) string {
	if m.o.Format != nil {
		return m.o.Format(v)
	}
	if v == 0 {
		return "0"
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if v > 0 {
		s = "+" + s
	}
	return s
}

// changed reports the values.
func (m *Model) changed() tea.Cmd {
	if m.o.Change == nil {
		return nil
	}
	return m.o.Change(m.Values(), m.on)
}

// set gives slider i the value v; the Change when it is another.
func (m *Model) set(i int, v float64) tea.Cmd {
	if i < 0 || i >= len(m.values) {
		return nil
	}
	if v = m.snap(v); v == m.values[i] {
		return nil
	}
	m.values[i] = v
	return m.changed()
}

// reset gives every slider Zero.
func (m *Model) reset() tea.Cmd {
	same := true
	for i, v := range m.values {
		if v != m.o.Zero {
			m.values[i], same = m.o.Zero, false
		}
	}
	if same {
		return nil
	}
	return m.changed()
}

func (m *Model) flip() tea.Cmd {
	if !m.o.Switch {
		return nil
	}
	m.on = !m.on
	return m.changed()
}

// geometry is where the parts are drawn inside the border.
type geometry struct {
	// left is the cells before the scale; scale the width of its
	// numbers; col of a slider; gap between two; x0 where the first
	// starts.
	left, scale, col, gap, x0 int
	// up and down are the rows of bar over the zero line and under it;
	// top the body row of the first of them.
	up, down, top int
	// w is the box's width, rows its body's.
	w, rows int
}

// axis is the body row of the zero line.
func (g geometry) axis() int { return g.top + g.up }

// layout fits the parts into the box; false when it is too small.
func (m *Model) layout() (geometry, bool) {
	n := len(m.values)
	var g geometry
	for _, v := range []float64{m.o.Min, m.o.Zero, m.o.Max} {
		g.scale = max(g.scale, text.Width(m.format(v)))
		g.col = max(g.col, text.Width(m.format(v)))
	}
	for _, l := range m.labels {
		g.col = max(g.col, text.Width(l))
	}
	g.col = max(g.col, 1)
	g.x0 = 1 + g.scale + 2
	g.gap = 2
	need := func() int { return 2 + g.x0 + n*g.col + max(n-1, 0)*g.gap + 1 }
	if need() > m.w {
		g.gap = 1
	}
	// room for the title and the keys in the border: the sliders in its
	// middle
	g.w = min(max(need(), text.Width(m.o.Title)+6, text.Width(m.keys())+6), m.w)
	g.left = max(g.w-need(), 0) / 2
	g.x0 += g.left
	if m.o.Max > m.o.Zero {
		g.up = m.o.Rows
	}
	if m.o.Min < m.o.Zero {
		g.down = m.o.Rows
	}
	if m.o.Switch {
		g.top = 1
	}
	// the zero line, the labels, the values; the border
	rest := m.h - 2 - g.top - 3
	for g.up+g.down > rest && g.up+g.down > 0 {
		if g.up > 0 {
			g.up--
		}
		if g.down > 0 && g.up+g.down > rest {
			g.down--
		}
	}
	g.rows = g.top + g.up + g.down + 3
	if n == 0 || g.w < 2+g.x0+g.col || m.h < g.rows+2 || (m.o.Max > m.o.Zero && g.up == 0) || (m.o.Min < m.o.Zero && g.down == 0) {
		return g, false
	}
	return g, true
}

// sliderAt is the slider drawn at x cells inside the border; -1 none.
func (g geometry) sliderAt(x, n int) int {
	x -= g.x0 - g.gap/2
	if x < 0 {
		return -1
	}
	if i := x / (g.col + g.gap); i < n {
		return i
	}
	return -1
}

// valueAt is the value of body row y: Max at the top of the bars, Min
// at their foot; false off them.
func (m *Model) valueAt(g geometry, y int) (float64, bool) {
	switch r := y - g.axis(); {
	case r == 0:
		return m.o.Zero, true
	case r < 0 && -r <= g.up:
		return m.o.Zero + float64(-r)/float64(g.up)*(m.o.Max-m.o.Zero), true
	case r > 0 && r <= g.down:
		return m.o.Zero - float64(r)/float64(g.down)*(m.o.Zero-m.o.Min), true
	}
	return 0, false
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	n := len(m.values)
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c", "enter":
			m.done = true
		case "left", "h":
			m.at = max(m.at-1, 0)
		case "right", "l":
			m.at = max(min(m.at+1, n-1), 0)
		case "up", "k":
			return m, m.step(m.at, 1)
		case "down", "j":
			return m, m.step(m.at, -1)
		case "r":
			return m, m.reset()
		case "space":
			return m, m.flip()
		}
	case tea.MouseClickMsg:
		g, ok := m.layout()
		if !ok || msg.Button != tea.MouseLeft {
			break
		}
		x, y := msg.X-m.x-1, msg.Y-m.y-1
		if m.o.Switch && y == 0 && x >= g.w-2-m.switchWidth() {
			return m, m.flip()
		}
		i := g.sliderAt(x, n)
		if i < 0 || y < g.top || y >= g.rows {
			break
		}
		m.at = i
		if v, ok := m.valueAt(g, y); ok {
			m.drag = i
			return m, m.set(i, v)
		}
	case tea.MouseMotionMsg:
		g, ok := m.layout()
		if !ok || m.drag < 0 || msg.Button != tea.MouseLeft {
			break
		}
		// off the bars' ends: the end
		y := min(max(msg.Y-m.y-1, g.top), g.top+g.up+g.down)
		if v, ok := m.valueAt(g, y); ok {
			return m, m.set(m.drag, v)
		}
	case tea.MouseReleaseMsg:
		m.drag = -1
	case tea.MouseWheelMsg:
		g, ok := m.layout()
		if !ok {
			break
		}
		i := g.sliderAt(msg.X-m.x-1, n)
		switch msg.Button {
		case tea.MouseWheelUp:
			return m, m.step(i, 1)
		case tea.MouseWheelDown:
			return m, m.step(i, -1)
		}
	}
	return m, nil
}

// step moves slider i by d steps.
func (m *Model) step(i, d int) tea.Cmd {
	if i < 0 || i >= len(m.values) {
		return nil
	}
	return m.set(i, m.values[i]+float64(d)*m.o.Step)
}

// keys is the hint in the border.
func (m *Model) keys() string {
	if m.o.Switch {
		return "space On/Off  r Reset"
	}
	return "r Reset"
}

func (m *Model) switchText() string {
	if m.on {
		return " On "
	}
	return " Off "
}

func (m *Model) switchWidth() int { return text.Width(m.switchText()) }

// blocks fill a cell from its foot by eighths.
var blocks = [...]string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// eighths is how much of the bars' rows over or under the zero line
// value v fills, in eighths of a cell: positive over it.
func (m *Model) eighths(g geometry, v float64) int {
	switch {
	case v > m.o.Zero:
		return int(math.Round((v - m.o.Zero) / (m.o.Max - m.o.Zero) * float64(g.up*8)))
	case v < m.o.Zero:
		return -int(math.Round((m.o.Zero - v) / (m.o.Zero - m.o.Min) * float64(g.down*8)))
	}
	return 0
}

// View is the box: the switch, the bars over and under the zero line
// with the scale beside them, the labels, the values.
func (m *Model) View() tea.View {
	g, ok := m.layout()
	if !ok {
		return tea.NewView("")
	}
	muted := m.th.Lip("ui.text.inactive")
	plain, focus := m.th.Lip("ui.text"), m.th.Lip("ui.text.focus")
	if !m.on {
		plain, focus = muted, muted
	}
	// the bar is an odd number of cells, the line of an empty one in
	// their middle
	bw := g.col - (1 - g.col%2)
	pad := (g.col - bw) / 2
	cell := func(s string) string { return text.Spaces(pad) + s + text.Spaces(g.col-bw-pad) }
	empty := cell(text.Spaces(bw/2) + "│" + text.Spaces(bw/2))
	between := text.Spaces(g.gap)
	scale := func(y int) string {
		s := ""
		switch {
		case y == g.axis():
			s = m.format(m.o.Zero)
		case y == g.top && g.up > 0:
			s = m.format(m.o.Max)
		case y == g.axis()+g.down && g.down > 0:
			s = m.format(m.o.Min)
		}
		return text.Spaces(g.left+1) + muted.Render(text.FitLeft(s, g.scale)) + "  "
	}
	iw := g.w - 2
	body := make([]string, 0, g.rows)
	if m.o.Switch {
		st := muted
		if m.on {
			st = m.th.Lip("ui.selection")
		}
		body = append(body, text.Spaces(iw-m.switchWidth())+st.Render(m.switchText()))
	}
	for y := g.top; y <= g.axis()+g.down; y++ {
		var b strings.Builder
		b.WriteString(scale(y))
		for i, v := range m.values {
			if i > 0 {
				b.WriteString(between)
			}
			st := plain
			if i == m.at {
				st = focus
			}
			e := m.eighths(g, v)
			switch r := g.axis() - y; {
			case r == 0:
				b.WriteString(st.Render(cell(strings.Repeat("─", bw))))
			case r > 0:
				// row r over the line holds eighths 8(r-1) to 8r
				if k := min(e-8*(r-1), 8); k > 0 {
					b.WriteString(st.Render(cell(strings.Repeat(blocks[k], bw))))
				} else {
					b.WriteString(muted.Render(empty))
				}
			default:
				// under the line a cell fills from its top: the rest
				// of it from its foot, the colours swapped
				switch k := min(-e-8*(-r-1), 8); {
				case k >= 8:
					b.WriteString(st.Render(cell(strings.Repeat(blocks[8], bw))))
				case k > 0:
					b.WriteString(text.Spaces(pad) + st.Reverse(true).Render(strings.Repeat(blocks[8-k], bw)) + text.Spaces(g.col-bw-pad))
				default:
					b.WriteString(muted.Render(empty))
				}
			}
		}
		body = append(body, b.String())
	}
	var labels, values strings.Builder
	labels.WriteString(text.Spaces(g.x0))
	values.WriteString(text.Spaces(g.x0))
	for i, v := range m.values {
		if i > 0 {
			labels.WriteString(between)
			values.WriteString(between)
		}
		st := muted
		if i == m.at {
			st = m.th.Lip("ui.menu.selected")
		}
		labels.WriteString(st.Render(text.Center(text.Truncate(m.labels[i], g.col), g.col)))
		values.WriteString(m.th.Render("ui.text", text.Center(m.format(v), g.col)))
	}
	body = append(body, labels.String(), values.String())
	lines := modal.Frame(m.th, modal.Box{Title: m.o.Title, Keys: m.keys(), Focused: true}, body, g.w)
	return tea.NewView(strings.Join(lines, "\n"))
}
