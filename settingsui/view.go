package settingsui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/settings"
	"github.com/MaximKayTee/gotuitools/text"
)

func (m *Model) width() int { return min(m.w, 80) }

// View is the box.
func (m *Model) View() tea.View {
	w := m.width()
	if w < 20 || m.h < 7 {
		return tea.NewView("")
	}
	iw := w - 2
	th := m.th
	body := make([]string, 0, m.h-2)

	// search row
	var search string
	if m.focus == focusSearch && m.edit == editNone && m.search.String() == "" {
		search = th.Render("ui.text.focus", " / ") + th.Render("ui.cursor", " ") + th.Render("ui.text.inactive", "Search")
	} else if m.focus == focusSearch && m.edit == editNone {
		line, _ := m.search.View(iw-4, th.Lip("ui.text"), th.Lip("ui.cursor"))
		search = th.Render("ui.text.focus", " / ") + line
	} else if q := m.search.String(); q != "" {
		search = th.Render("ui.text.inactive", " / ") + th.Render("ui.text", q)
	} else {
		search = th.Render("ui.text.inactive", " / Search")
	}
	body = append(body, search)

	if m.edit == editPick {
		body = append(body, m.pickRows(iw)...)
	} else {
		body = append(body, m.listRows(iw)...)
	}
	body = append(body, "")
	body = append(body, m.helpRows(iw)...)

	keys := "enter Edit  d Default  e File  esc Close"
	switch {
	case m.edit == editLine:
		keys = "enter Save  esc Cancel"
	case m.edit == editPick:
		keys = "enter Choose  esc Cancel"
	case m.focus == focusSearch:
		keys = "↓ List  esc Close"
	}
	if m.src.Path() == "" {
		keys = strings.Replace(keys, "  e File", "", 1)
	}
	if f, ok := m.selected(); ok && f.From != "" {
		keys = strings.Replace(keys, "d Default", "d "+f.From, 1)
	}
	pos := m.cur.Pos()
	pos.Skip, pos.Len = 1, m.listH()
	if m.edit == editPick {
		pos = m.pick.Pos()
		pos.Skip, pos.Len = 1, m.listH()
	}
	lines := modal.Frame(th, modal.Box{Title: m.title, Keys: keys, Focused: true, Pos: pos}, body, w)
	return tea.NewView(strings.Join(lines, "\n"))
}

func (m *Model) listRows(iw int) []string {
	th := m.th
	out := make([]string, 0, m.listH())
	from, to := m.cur.Visible()
	query := m.search.String() != ""
	valW := min(iw/2, 44)
	for i := from; i < to; i++ {
		r := m.rows[i]
		if r.header != "" {
			out = append(out, th.Render("ui.text.focus", text.Fit(" "+r.header, iw)))
			continue
		}
		f := r.f
		label := "   " + f.Label
		if query && f.Group != "" {
			label = " " + f.Group + " › " + f.Label
		}
		sel := i == m.cur.At
		var value string
		var plain bool // value has its own styles
		switch e := m.errs[f.Key]; {
		case sel && m.edit == editLine:
			value, _ = m.line.View(valW, th.Lip("ui.text"), th.Lip("ui.cursor"))
			plain = true
		case e != "":
			value = th.Render("error", text.Truncate(e, valW))
			plain = true
		default:
			value = display(f)
		}
		if !plain {
			value = text.Truncate(value, valW)
		}
		row := text.Row(label, value+" ", iw)
		switch {
		case plain:
			row = text.Fit(label, iw-text.Width(value)-2) + " " + value + " "
			if sel {
				row = th.Render("ui.text.focus", text.Fit(label, iw-text.Width(value)-2)) + " " + value + " "
			}
		case sel && m.focus == focusList:
			row = th.Render("ui.menu.selected", row)
		case sel:
			row = th.Render("ui.text.focus", row)
		case f.IsDefault:
			row = th.Render("ui.text", text.Fit(label, iw-text.Width(value)-2)) + " " + th.Render("ui.text.inactive", value) + " "
		default:
			row = th.Render("ui.text", row)
		}
		out = append(out, row)
	}
	if len(m.rows) == 0 {
		out = append(out, th.Render("ui.text.inactive", "   None"))
	}
	for len(out) < m.listH() {
		out = append(out, "")
	}
	return out
}

func (m *Model) pickRows(iw int) []string {
	th := m.th
	f, _ := m.selected()
	out := []string{}
	m.pick.Height = m.listH()
	m.pick.Clamp()
	from, to := m.pick.Visible()
	for i := from; i < to; i++ {
		o := m.options[i]
		label := o
		if o == "" {
			label = "None"
		}
		mark := "   "
		if o == f.Value {
			mark = " • "
		}
		row := text.Fit(mark+label, iw)
		if i == m.pick.At {
			row = th.Render("ui.menu.selected", row)
		} else {
			row = th.Render("ui.text", row)
		}
		out = append(out, row)
	}
	for len(out) < m.listH() {
		out = append(out, "")
	}
	return out
}

func (m *Model) helpRows(iw int) []string {
	th := m.th
	f, ok := m.selected()
	var first, second string
	if ok {
		first = f.Help
		var facts []string
		if f.Kind == settings.Enum && len(f.Options) > 0 && m.edit != editPick {
			lead := "One of "
			if f.Free {
				lead = "For example "
			}
			facts = append(facts, lead+strings.Join(f.Options, ", "))
		}
		if r := f.Range(); r != "" {
			facts = append(facts, r)
		}
		if f.Restart {
			facts = append(facts, "Restart required")
		}
		if !f.IsDefault && !f.Secret {
			d := f.Default
			if d == "" {
				d = "None"
			}
			from := "Default"
			if f.From != "" {
				from = f.From
			}
			facts = append(facts, from+" "+text.Truncate(d, 30))
		}
		second = text.Facts(facts...)
		if first == "" {
			first, second = second, ""
		}
	}
	if m.status != "" {
		second = m.status
	}
	if e := m.errs[f.Key]; ok && e != "" && m.edit != editNone {
		return []string{
			th.Render("ui.text.inactive", text.Fit(" "+first, iw)),
			th.Render("error", text.Fit(" "+e, iw)),
		}
	}
	right := ""
	if m.restart {
		right = "Restart required"
	}
	return []string{
		th.Render("ui.text.inactive", text.Fit(" "+first, iw)),
		th.Render("ui.text.inactive", text.Row(" "+second, right+" ", iw)),
	}
}

// display is a value as shown: None for an unset optional or empty
// list, a masked secret, a long text's first line.
func display(f settings.Field) string {
	v := f.Value
	switch {
	case f.Secret && v != "":
		return "••••••"
	case v == "" && (f.Optional || f.Kind == settings.List):
		return "None"
	case v == "" && (f.Kind == settings.String || f.Kind == settings.Keys):
		return "None"
	case f.Long:
		first := text.FirstLine(v)
		if first != v {
			first += " …"
		}
		return first
	}
	return text.OneLine(v)
}
