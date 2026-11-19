// Package files is a chooser of a file or a folder on disk: a
// modal.Modal. The line is a path; the rows are what its folder holds,
// narrowed by the name typed after the last slash.
//
//	type, paste     narrow; // starts again at the root, /~/ at home
//	tab right       into the folder under the cursor
//	left            the folder above
//	up down ctrl+p ctrl+n pgup pgdown   move
//	enter           choose; when choosing a file, into a folder
//	esc ctrl+c      close
//	wheel, click    move; into a folder, else choose
//
// Names starting with a dot are listed once a dot is typed.
package files

import (
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/fuzzy"
	"github.com/MaximKayTee/gotuitools/input"
	"github.com/MaximKayTee/gotuitools/list"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// Options say what is chosen.
type Options struct {
	Title string
	// Dir is the folder shown first; "" the working directory.
	Dir string
	// Folders: a folder is chosen and files are not listed. The row "."
	// is the folder shown.
	Folders bool
	// Ext lists only the files ending in one of these: ".png". None: all.
	Ext []string
	// New: a typed name that is not there can be chosen.
	New bool
}

// entry is one row: a name in the folder listed.
type entry struct {
	name     string
	dir, new bool
}

// Model is a chooser. Use New.
type Model struct {
	th     theme.Theme
	o      Options
	line   input.Line
	dir    string // the folder listed
	all    []entry
	err    error // why dir could not be read
	shown  []entry
	cur    list.Cursor
	choose func(string) tea.Cmd
	// MaxWidth caps the box width; 0 is 72.
	MaxWidth int

	w, h int
	x, y int
	done bool
}

// New is a chooser; choose runs on the path chosen.
func New(th theme.Theme, o Options, choose func(path string) tea.Cmd) *Model {
	o.Dir, _ = filepath.Abs(o.Dir)
	m := &Model{th: th, o: o, choose: choose}
	m.show(o.Dir, "")
	return m
}

// SetPath replaces what is typed.
func (m *Model) SetPath(p string) { m.line.Set(p); m.list() }

// Path is what is typed.
func (m *Model) Path() string { return m.line.String() }

// Shown is the rows, folders ending in a slash.
func (m *Model) Shown() []string {
	out := make([]string, len(m.shown))
	for i, e := range m.shown {
		out[i] = e.label()
	}
	return out
}

func (e entry) label() string {
	if e.dir {
		return e.name + "/"
	}
	return e.name
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// show lists dir, the cursor on name.
func (m *Model) show(dir, name string) {
	if rest, ok := strings.CutPrefix(dir, home()); ok && home() != "" && (rest == "" || rest[0] == '/') {
		dir = "~" + rest
	}
	m.line.Set(strings.TrimSuffix(dir, "/") + "/")
	m.list()
	m.cur.At = max(slices.IndexFunc(m.shown, func(e entry) bool { return e.name == name }), 0)
	m.cur.Clamp()
}

// split is the folder the line names and the name typed after it.
func (m *Model) split() (dir, name string) {
	p := m.line.String()
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = home() + p[1:]
	}
	if !filepath.IsAbs(p) {
		p = m.o.Dir + "/" + p
	}
	dir, name = filepath.Split(p)
	return filepath.Clean(dir), name
}

// list makes the rows from the line.
func (m *Model) list() {
	p := m.line.String()
	for _, again := range []string{"//", "/~/"} {
		if i := strings.LastIndex(p, again); i >= 0 {
			p = p[i+1:]
		}
	}
	if p != m.line.String() {
		m.line.Set(p)
	}
	dir, name := m.split()
	if dir != m.dir {
		m.dir = dir
		m.all, m.err = m.read(dir)
	}
	m.shown = nil
	if m.o.Folders && name == "" && m.err == nil {
		m.shown = append(m.shown, entry{name: "."})
	}
	for _, e := range fuzzy.Filter(m.all, name, func(e entry) []string { return []string{e.name} }) {
		if strings.HasPrefix(name, ".") || !strings.HasPrefix(e.name, ".") {
			m.shown = append(m.shown, e)
		}
	}
	if m.o.New && name != "" {
		if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, fs.ErrNotExist) {
			m.shown = append(m.shown, entry{name: name, new: true})
		}
	}
	m.cur.N, m.cur.At, m.cur.Top = len(m.shown), 0, 0
	m.cur.Clamp()
}

// read is what dir holds of the kinds asked for: folders, then files,
// by name.
func (m *Model) read(dir string) ([]entry, error) {
	ents, err := os.ReadDir(dir)
	var all []entry
	for _, e := range ents {
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			st, err := os.Stat(filepath.Join(dir, e.Name()))
			isDir = err == nil && st.IsDir()
		}
		kind := len(m.o.Ext) == 0 || slices.ContainsFunc(m.o.Ext, func(x string) bool {
			return strings.EqualFold(filepath.Ext(e.Name()), x)
		})
		if isDir || !m.o.Folders && kind {
			all = append(all, entry{name: e.Name(), dir: isDir})
		}
	}
	slices.SortFunc(all, func(a, b entry) int {
		if a.dir != b.dir {
			return cmp.Compare(btoi(b.dir), btoi(a.dir))
		}
		return cmp.Or(strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)), strings.Compare(a.name, b.name))
	})
	return all, err
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (m *Model) Init() tea.Cmd { return nil }

// Done reports closed.
func (m *Model) Done() bool { return m.done }

// SetOrigin is where the box was drawn.
func (m *Model) SetOrigin(x, y int) { m.x, m.y = x, y }

// SetSize is the box the chooser may use.
func (m *Model) SetSize(w, h int) tea.Cmd {
	m.w, m.h = w, h
	m.cur.Height = max(min(h-3, 16), 1)
	m.cur.Clamp()
	return nil
}

func (m *Model) width() int {
	mw := m.MaxWidth
	if mw <= 0 {
		mw = 72
	}
	return min(m.w, mw)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch k := msg.String(); k {
		case "esc", "ctrl+c":
			m.done = true
		case "enter":
			if m.o.Folders || !m.into(m.cur.At) {
				return m, m.pick(m.cur.At)
			}
		case "tab", "right":
			m.into(m.cur.At)
		case "left":
			m.show(filepath.Dir(m.dir), filepath.Base(m.dir))
		case "up", "down", "ctrl+p", "ctrl+n", "pgup", "pgdown":
			m.cur.Key(k, false)
		default:
			if m.line.Key(msg) {
				m.list()
			}
		}
	case tea.PasteMsg:
		m.line.Insert(strings.TrimSpace(msg.Content))
		m.list()
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.cur.Scroll(-3)
		case tea.MouseWheelDown:
			m.cur.Scroll(3)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			// border, path row, then the list
			if i := m.cur.Hit(msg.Y - m.y - 2); i >= 0 && msg.X > m.x && msg.X < m.x+m.width()-1 && !m.into(i) {
				return m, m.pick(i)
			}
		}
	}
	return m, nil
}

// into shows the folder on row i and reports whether it is one.
func (m *Model) into(i int) bool {
	if i < 0 || i >= len(m.shown) || !m.shown[i].dir {
		return false
	}
	m.show(filepath.Join(m.dir, m.shown[i].name), "")
	return true
}

// pick chooses row i.
func (m *Model) pick(i int) tea.Cmd {
	if i < 0 || i >= len(m.shown) {
		return nil
	}
	m.done = true
	if m.choose == nil {
		return nil
	}
	return m.choose(filepath.Join(m.dir, m.shown[i].name))
}

// View is the box.
func (m *Model) View() tea.View {
	w := m.width()
	if w < 6 || m.h < 4 {
		return tea.NewView("")
	}
	iw := w - 2
	body := make([]string, 0, m.cur.Height+1)
	line, _ := m.line.View(iw-2, m.th.Lip("ui.text"), m.th.Lip("ui.cursor"))
	body = append(body, " "+line+" ")
	from, to := m.cur.Visible()
	for i := from; i < to; i++ {
		e, note := m.shown[i], ""
		if e.new {
			note = "New"
		}
		if i == m.cur.At {
			body = append(body, m.th.Render("ui.menu.selected", text.Row(" "+e.label(), note+" ", iw)))
		} else {
			body = append(body, m.th.Render("ui.menu", text.Row(" "+e.label(), m.th.Render("ui.text.inactive", note)+" ", iw)))
		}
	}
	if len(m.shown) == 0 {
		none := "None"
		var pe *fs.PathError
		if errors.As(m.err, &pe) {
			none = pe.Err.Error()
		}
		body = append(body, m.th.Render("ui.text.inactive", " "+none))
	}
	for len(body) < m.cur.Height+1 {
		body = append(body, "")
	}
	keys := "enter Choose  left Up  esc Close"
	if m.o.Folders {
		keys = "enter Choose  tab Open  left Up  esc Close"
	}
	pos := m.cur.Pos()
	pos.Skip = 1
	lines := modal.Frame(m.th, modal.Box{Title: m.o.Title, Keys: keys, Focused: true, Pos: pos}, body, w)
	return tea.NewView(strings.Join(lines, "\n"))
}
