// Package settingsui is the settings screen: a search line over every
// setting of a settings.Source, each edited in place by its type, and
// saved on each change. A modal.Modal.
//
// # Rows
//
// No query: every setting under group header rows. A query: matches,
// best first, as "Group › Label". Value on the right, muted at its
// default; a save error in place of the value until the next edit of
// that row. The selected setting's help, options, bounds and "Restart
// required" below the list; "Restart required" on the right after such
// a setting changed.
//
// # Keys
//
//	type, /            search; up, down, enter, tab go to the list
//	tab                search line / list
//	up down ctrl+p ctrl+n pgup pgdown home end   move
//	enter space        edit: bool toggles, enum picks from a list, long
//	                   text opens the editor, others a text line
//	d                  reset to the default
//	e                  open the settings file in the editor; read after
//	esc                cancel an edit; clear the search; close
//	q                  close, from the list
//
// Text lines: numbers, durations ("30s", "1h30m", "2d"), sizes
// ("500MB", "32MiB"), lists comma separated; empty clears an optional
// setting. Enter saves, Esc cancels; a bad value stays open with the
// reason.
package settingsui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/input"
	"github.com/MaximKayTee/gotuitools/list"
	"github.com/MaximKayTee/gotuitools/settings"
	"github.com/MaximKayTee/gotuitools/sys"
	"github.com/MaximKayTee/gotuitools/theme"
)

// ChangedMsg follows a saved change: Field is the setting as saved.
type ChangedMsg struct{ Field settings.Field }

// ReloadedMsg follows an edit of the file in the editor.
type ReloadedMsg struct{ Err error }

type focus int

const (
	focusSearch focus = iota
	focusList
)

type editKind int

const (
	editNone editKind = iota
	editLine
	editPick
)

// row is a group header (key "") or a setting.
type row struct {
	header string
	f      settings.Field
}

// Model is the settings screen. Use New.
type Model struct {
	th    theme.Theme
	src   settings.Source
	title string
	// Editor is the editor command line for long text and the file; ""
	// is $VISUAL, $EDITOR, vi.
	Editor string

	search input.Line
	focus  focus
	rows   []row
	cur    list.Cursor
	seq    int

	edit    editKind
	line    input.Line
	pick    list.Cursor
	options []string

	most int // rows of the whole list: the box's height

	errs    map[string]string
	restart bool
	status  string

	w, h int
	x, y int
	done bool
}

// New is the settings screen over src.
func New(th theme.Theme, src settings.Source, title string) *Model {
	if title == "" {
		title = "Settings"
	}
	return &Model{th: th, src: src, title: title, errs: map[string]string{}}
}

type fieldsMsg struct {
	seq    int
	query  string
	fields []settings.Field
}

type savedMsg struct {
	key   string
	field settings.Field
	err   error
}

type editedMsg struct {
	key  string
	file string
	err  error
}

func (editedMsg) ToModals() {}

func (m *Model) Init() tea.Cmd { return m.query() }

// Done reports closed.
func (m *Model) Done() bool { return m.done }

// SetOrigin is where the box was drawn.
func (m *Model) SetOrigin(x, y int) { m.x, m.y = x, y }

// SetSize is the box the screen may use.
func (m *Model) SetSize(w, h int) tea.Cmd {
	m.w, m.h = w, h
	m.cur.Height = m.listH()
	m.cur.Clamp()
	return nil
}

// listH is the list rows: as many as the whole list or the options
// being picked from, at least 6, at most the box less borders, the
// search row, a blank row and two help rows.
func (m *Model) listH() int {
	n := max(m.most, 6)
	if m.edit == editPick {
		n = max(n, len(m.options))
	}
	return max(min(n, m.h-6), 1)
}

func (m *Model) query() tea.Cmd {
	m.seq++
	seq, q, src := m.seq, m.search.String(), m.src
	return func() tea.Msg {
		if strings.TrimSpace(q) == "" {
			return fieldsMsg{seq, q, src.Fields()}
		}
		return fieldsMsg{seq, q, src.Search(q)}
	}
}

func (m *Model) setRows(q string, fields []settings.Field) {
	keep := ""
	if f, ok := m.selected(); ok {
		keep = f.Key
	}
	m.rows = m.rows[:0]
	group := "\x00"
	for _, f := range fields {
		if q == "" && f.Group != group {
			group = f.Group
			if group != "" {
				m.rows = append(m.rows, row{header: group})
			}
		}
		m.rows = append(m.rows, row{f: f})
	}
	m.cur.N = len(m.rows)
	if q == "" {
		m.most = max(m.most, len(m.rows))
	}
	m.cur.Height = m.listH()
	m.cur.At = 0
	for i, r := range m.rows {
		if r.header == "" && r.f.Key == keep && keep != "" {
			m.cur.At = i
		}
	}
	if q != "" {
		m.cur.At, m.cur.Top = 0, 0
	}
	m.skipHeader(1)
	m.cur.Clamp()
}

// skipHeader moves off a header row in direction d.
func (m *Model) skipHeader(d int) {
	for m.cur.At >= 0 && m.cur.At < len(m.rows) && m.rows[m.cur.At].header != "" {
		m.cur.At += d
	}
	if m.cur.At >= len(m.rows) || m.cur.At < 0 {
		m.cur.At = min(max(m.cur.At, 0), len(m.rows)-1)
		if d > 0 {
			m.skipHeader(-1)
		}
	}
	// show the group header above the first setting of a group
	if m.cur.At > 0 && m.rows[m.cur.At-1].header != "" && m.cur.Top == m.cur.At {
		m.cur.Top--
	}
}

func (m *Model) selected() (settings.Field, bool) {
	if m.cur.At < 0 || m.cur.At >= len(m.rows) || m.rows[m.cur.At].header != "" {
		return settings.Field{}, false
	}
	return m.rows[m.cur.At].f, true
}

func (m *Model) move(d int) {
	m.cur.Move(d)
	dir := 1
	if d < 0 {
		dir = -1
	}
	m.skipHeader(dir)
	m.cur.Clamp()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fieldsMsg:
		if msg.seq == m.seq {
			m.setRows(msg.query, msg.fields)
		}
	case savedMsg:
		return m, m.saved(msg)
	case editedMsg:
		return m, m.edited(msg)
	case sys.EditedMsg:
		return m, m.fileEdited(msg)
	case tea.PasteMsg:
		switch {
		case m.edit == editLine:
			m.line.Update(msg)
		case m.focus == focusSearch:
			m.search.Update(msg)
			return m, m.query()
		}
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.cur.Scroll(-3)
		case tea.MouseWheelDown:
			m.cur.Scroll(3)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.edit == editNone {
			i := m.cur.Hit(msg.Y - m.y - 2)
			if i < 0 || m.rows[i].header != "" {
				return m, nil
			}
			m.focus = focusList
			if i == m.cur.At {
				return m, m.startEdit()
			}
			m.cur.At = i
		}
	}
	return m, nil
}

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	switch m.edit {
	case editLine:
		return m.lineKey(k)
	case editPick:
		return m.pickKey(k)
	}
	s := k.String()
	if m.focus == focusSearch {
		switch s {
		case "esc":
			if m.search.String() == "" {
				m.done = true
				return nil
			}
			m.search.Set("")
			return m.query()
		case "tab", "down", "enter", "ctrl+n", "pgdown":
			m.focus = focusList
			if s == "enter" {
				return m.startEdit()
			}
			if s == "pgdown" {
				m.move(m.cur.Page())
			}
			return nil
		case "up", "ctrl+p":
			return nil
		}
		if m.search.Key(k) {
			return m.query()
		}
		return nil
	}
	switch s {
	case "esc":
		if m.search.String() != "" {
			m.search.Set("")
			return m.query()
		}
		m.done = true
	case "q":
		m.done = true
	case "tab", "/":
		m.focus = focusSearch
	case "up", "ctrl+p", "k":
		if m.cur.At == m.firstSetting() {
			m.focus = focusSearch
			return nil
		}
		m.move(-1)
	case "down", "ctrl+n", "j":
		m.move(1)
	case "pgup":
		m.move(-m.cur.Page())
	case "pgdown":
		m.move(m.cur.Page())
	case "home", "g":
		m.cur.At, m.cur.Top = 0, 0
		m.skipHeader(1)
	case "end", "G":
		m.cur.At = len(m.rows) - 1
		m.cur.Clamp()
	case "enter", "space":
		return m.startEdit()
	case "d":
		return m.reset()
	case "e":
		if p := m.src.Path(); p != "" {
			return sys.Edit(m.Editor, p, 0)
		}
	default:
		if k.Text != "" {
			m.focus = focusSearch
			m.search.Key(k)
			return m.query()
		}
	}
	return nil
}

func (m *Model) firstSetting() int {
	for i, r := range m.rows {
		if r.header == "" {
			return i
		}
	}
	return 0
}

func (m *Model) startEdit() tea.Cmd {
	f, ok := m.selected()
	if !ok {
		return nil
	}
	delete(m.errs, f.Key)
	switch {
	case f.Kind == settings.Bool:
		next := "true"
		if f.Value == "true" {
			next = "false"
		}
		return m.save(f.Key, next)
	case f.Kind == settings.Enum && !f.Free:
		m.options = f.Options
		if f.Optional {
			m.options = append([]string{""}, m.options...)
		}
		m.pick = list.Cursor{N: len(m.options), Height: m.listH(), Wrap: true}
		for i, o := range m.options {
			if o == f.Value {
				m.pick.At = i
			}
		}
		m.pick.Clamp()
		m.edit = editPick
	case f.Long:
		return m.editLong(f)
	default:
		m.line = input.Of(f.Value)
		if f.Secret {
			m.line.Mask = '•'
		}
		m.edit = editLine
	}
	return nil
}

func (m *Model) lineKey(k tea.KeyPressMsg) tea.Cmd {
	f, _ := m.selected()
	switch k.String() {
	case "esc":
		m.edit = editNone
		delete(m.errs, f.Key)
	case "enter":
		return m.save(f.Key, m.line.String())
	case "tab":
		// complete from suggestions
		if f.Free {
			q := strings.ToLower(m.line.String())
			for _, o := range f.Options {
				if strings.HasPrefix(strings.ToLower(o), q) {
					m.line.Set(o)
					break
				}
			}
		}
	default:
		m.line.Key(k)
	}
	return nil
}

func (m *Model) pickKey(k tea.KeyPressMsg) tea.Cmd {
	f, _ := m.selected()
	switch s := k.String(); s {
	case "esc", "q":
		m.edit = editNone
	case "enter", "space":
		if m.pick.At < len(m.options) {
			return m.save(f.Key, m.options[m.pick.At])
		}
	default:
		m.pick.Key(s, true)
	}
	return nil
}

func (m *Model) save(key, text string) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		f, err := src.Set(key, text)
		return savedMsg{key, f, err}
	}
}

func (m *Model) reset() tea.Cmd {
	f, ok := m.selected()
	if !ok || f.IsDefault {
		return nil
	}
	src := m.src
	return func() tea.Msg {
		nf, err := src.Reset(f.Key)
		return savedMsg{f.Key, nf, err}
	}
}

func (m *Model) saved(msg savedMsg) tea.Cmd {
	if msg.err != nil {
		m.errs[msg.key] = msg.err.Error()
		return nil
	}
	delete(m.errs, msg.key)
	m.edit = editNone
	for i, r := range m.rows {
		if r.header == "" && r.f.Key == msg.key {
			m.rows[i].f = msg.field
		}
	}
	if msg.field.Restart {
		m.restart = true
	}
	f := msg.field
	return func() tea.Msg { return ChangedMsg{f} }
}

// editLong opens a long text in the editor through a temp file.
func (m *Model) editLong(f settings.Field) tea.Cmd {
	tmp, err := os.CreateTemp("", "setting-*.txt")
	if err != nil {
		m.errs[f.Key] = err.Error()
		return nil
	}
	tmp.WriteString(f.Value)
	tmp.Close()
	key, name := f.Key, tmp.Name()
	cmd := sys.EditorCommand(m.Editor, name, 0)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editedMsg{key, name, err} })
}

func (m *Model) edited(msg editedMsg) tea.Cmd {
	defer os.Remove(msg.file)
	if msg.err != nil {
		m.errs[msg.key] = msg.err.Error()
		return nil
	}
	b, err := os.ReadFile(msg.file)
	if err != nil {
		m.errs[msg.key] = err.Error()
		return nil
	}
	return m.save(msg.key, strings.TrimRight(string(b), "\n"))
}

func (m *Model) fileEdited(msg sys.EditedMsg) tea.Cmd {
	if msg.Err != nil {
		m.status = msg.Err.Error()
		return nil
	}
	_, err := m.src.Reload()
	m.status = ""
	if err != nil {
		m.status = err.Error()
	}
	return tea.Batch(m.query(), func() tea.Msg { return ReloadedMsg{err} })
}
