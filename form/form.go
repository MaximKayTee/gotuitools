// Package form is a box of fields to fill in: a modal.Modal. A field
// is a line of text, one of a list of choices, or a row of toggles.
//
//	type, paste              edit the field (input.Line's keys)
//	tab down, shift+tab up   next, previous field
//	left right space         the choice before, after (a choice field)
//	left right               the toggle before, after (a toggles field)
//	space                    flip the toggle (a toggles field)
//	enter                    submit
//	esc ctrl+c               close
//	click                    that field; a choice field: the choice after;
//	                         a toggles field: flip the toggle clicked
package form

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/input"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// Field is one row: Label, then the value.
type Field struct {
	// Label is drawn before the value; a form whose labels are all ""
	// has no label column.
	Label string
	// Value is the text at the start; of a choice field the choice, the
	// first when it is none of them.
	Value string
	// Choices makes the field one of these.
	Choices []string
	// Toggles makes the field a row of these, each on or off. Value and
	// the field's value are the ones on, joined by ",".
	Toggles []string
	// Limit is the most runes a text field takes; 0 no limit.
	Limit int
}

// Model is a form. Use New.
type Model struct {
	th     theme.Theme
	title  string
	fields []Field
	lines  []input.Line // text fields
	choice []int        // choice fields
	on     [][]bool     // toggles fields
	pos    []int        // toggles fields: the toggle the keys go to
	at     int
	submit func(values []string) tea.Cmd
	// Check runs before submit; its error is shown and the form stays
	// open.
	Check func(values []string) error
	// MaxWidth caps the box width; 0 is 60.
	MaxWidth int

	err  error
	w, h int
	x, y int
	done bool
}

// New is a form of fields; submit runs on enter with a value for each
// field, text fields trimmed.
func New(th theme.Theme, title string, fields []Field, submit func(values []string) tea.Cmd) *Model {
	m := &Model{th: th, title: title, fields: fields, submit: submit,
		lines: make([]input.Line, len(fields)), choice: make([]int, len(fields)),
		on: make([][]bool, len(fields)), pos: make([]int, len(fields))}
	for i, f := range fields {
		if len(f.Toggles) > 0 {
			m.on[i] = make([]bool, len(f.Toggles))
			for v := range strings.SplitSeq(f.Value, ",") {
				if k := slices.Index(f.Toggles, strings.TrimSpace(v)); k >= 0 {
					m.on[i][k] = true
				}
			}
			continue
		}
		if len(f.Choices) > 0 {
			m.choice[i] = max(slices.Index(f.Choices, f.Value), 0)
			continue
		}
		m.lines[i] = input.Of(f.Value)
		m.lines[i].Limit = f.Limit
	}
	return m
}

// Values is a value for each field: the text, trimmed, the choice, or
// the toggles on joined by ",".
func (m *Model) Values() []string {
	out := make([]string, len(m.fields))
	for i, f := range m.fields {
		if len(f.Toggles) > 0 {
			var on []string
			for k, t := range f.Toggles {
				if m.on[i][k] {
					on = append(on, t)
				}
			}
			out[i] = strings.Join(on, ",")
		} else if len(f.Choices) > 0 {
			out[i] = f.Choices[m.choice[i]]
		} else {
			out[i] = strings.TrimSpace(m.lines[i].String())
		}
	}
	return out
}

// At is the field the keys go to.
func (m *Model) At() int { return m.at }

func (m *Model) Init() tea.Cmd { return nil }

// Done reports closed.
func (m *Model) Done() bool { return m.done }

// SetOrigin is where the box was drawn.
func (m *Model) SetOrigin(x, y int) { m.x, m.y = x, y }

// SetSize is the box the form may use.
func (m *Model) SetSize(w, h int) tea.Cmd {
	m.w, m.h = w, h
	return nil
}

func (m *Model) width() int {
	mw := m.MaxWidth
	if mw <= 0 {
		mw = 60
	}
	return min(m.w, mw)
}

// move goes d fields on, around the ends.
func (m *Model) move(d int) {
	if n := len(m.fields); n > 0 {
		m.at = ((m.at+d)%n + n) % n
	}
}

// turn takes the choice d after the field's, around the ends.
func (m *Model) turn(i, d int) {
	n := len(m.fields[i].Choices)
	m.choice[i] = ((m.choice[i]+d)%n + n) % n
}

// flip turns toggle k of field i on or off.
func (m *Model) flip(i, k int) {
	m.on[i][k] = !m.on[i][k]
	m.err = nil
}

// toggleCell is how toggle t is drawn: a cell of its width.
func toggleCell(t string) string { return " " + t + " " }

// toggleAt is the toggle of field i at x cells into the value; -1 none.
func (m *Model) toggleAt(i, x int) int {
	for k, t := range m.fields[i].Toggles {
		w := text.Width(toggleCell(t))
		if x >= 0 && x < w {
			return k
		}
		x -= w
	}
	return -1
}

// labelWidth is the width of the label column; 0 none.
func (m *Model) labelWidth() int {
	lw := 0
	for _, f := range m.fields {
		lw = max(lw, text.Width(f.Label))
	}
	return min(lw, (m.width()-2)/2)
}

// valueX is where the values start inside the border.
func (m *Model) valueX() int {
	if lw := m.labelWidth(); lw > 0 {
		return 1 + lw + 2
	}
	return 1
}

// toggles draws toggles field i in vw cells: those on in the selection
// colours, those off muted, the one the keys go to underlined.
func (m *Model) toggles(i, vw int) string {
	var b strings.Builder
	used := 0
	for k, t := range m.fields[i].Toggles {
		c := toggleCell(t)
		if used+text.Width(c) > vw {
			break
		}
		used += text.Width(c)
		st := m.th.Lip("ui.text.inactive")
		if m.on[i][k] {
			st = m.th.Lip("ui.selection")
		}
		if i == m.at && k == m.pos[i] {
			st = st.Underline(true).Bold(true)
		}
		b.WriteString(st.Render(c))
	}
	return b.String() + strings.Repeat(" ", vw-used)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if len(m.fields) == 0 {
		m.done = true
		return m, nil
	}
	isChoice := len(m.fields[m.at].Choices) > 0
	isToggles := len(m.fields[m.at].Toggles) > 0
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.done = true
			return m, nil
		case "enter":
			return m, m.send()
		case "tab", "down":
			m.move(1)
			return m, nil
		case "shift+tab", "up":
			m.move(-1)
			return m, nil
		}
		if isToggles {
			n := len(m.fields[m.at].Toggles)
			switch msg.String() {
			case "left":
				m.pos[m.at] = (m.pos[m.at] + n - 1) % n
			case "right":
				m.pos[m.at] = (m.pos[m.at] + 1) % n
			case "space":
				m.flip(m.at, m.pos[m.at])
			}
			return m, nil
		}
		if isChoice {
			switch msg.String() {
			case "left":
				m.turn(m.at, -1)
			case "right", "space":
				m.turn(m.at, 1)
			}
			return m, nil
		}
		if m.lines[m.at].Key(msg) {
			m.err = nil
		}
	case tea.PasteMsg:
		if !isChoice && !isToggles && m.lines[m.at].Update(msg) {
			m.err = nil
		}
	case tea.MouseClickMsg:
		// border, then a row a field
		i := msg.Y - m.y - 1
		if msg.Button != tea.MouseLeft || i < 0 || i >= len(m.fields) || msg.X <= m.x || msg.X >= m.x+m.width()-1 {
			break
		}
		if len(m.fields[i].Choices) > 0 && m.at == i {
			m.turn(i, 1)
		}
		if len(m.fields[i].Toggles) > 0 {
			if k := m.toggleAt(i, msg.X-m.x-1-m.valueX()); k >= 0 {
				m.pos[i] = k
				m.flip(i, k)
			}
		}
		m.at = i
	}
	return m, nil
}

func (m *Model) send() tea.Cmd {
	v := m.Values()
	if m.Check != nil {
		if m.err = m.Check(v); m.err != nil {
			return nil
		}
	}
	m.done = true
	if m.submit == nil {
		return nil
	}
	return m.submit(v)
}

// View is the box: a row a field, the error under them.
func (m *Model) View() tea.View {
	w := m.width()
	if w < 8 || m.h < 3 {
		return tea.NewView("")
	}
	iw := w - 2
	lw := m.labelWidth()
	vw := iw - 2
	if lw > 0 {
		vw -= lw + 2
	}
	body := make([]string, 0, len(m.fields)+1)
	for i, f := range m.fields {
		row := " "
		if lw > 0 {
			row += m.th.Render("ui.text.inactive", text.Fit(f.Label, lw)) + "  "
		}
		switch {
		case len(f.Toggles) > 0:
			row += m.toggles(i, vw)
		case len(f.Choices) > 0 && i == m.at:
			row += m.th.Render("ui.menu.selected", text.Truncate(" "+f.Choices[m.choice[i]]+" ", vw))
		case len(f.Choices) > 0:
			row += m.th.Render("ui.text", text.Truncate(" "+f.Choices[m.choice[i]]+" ", vw))
		case i == m.at:
			line, _ := m.lines[i].View(vw, m.th.Lip("ui.text"), m.th.Lip("ui.cursor"))
			row += line
		default:
			row += m.th.Render("ui.text", text.Truncate(m.lines[i].String(), vw))
		}
		body = append(body, row)
	}
	if m.err != nil {
		body = append(body, " "+m.th.Render("error", text.Truncate(text.OneLine(m.err.Error()), iw-2)))
	}
	lines := modal.Frame(m.th, modal.Box{Title: m.title, Focused: true}, body, w)
	return tea.NewView(strings.Join(lines, "\n"))
}
