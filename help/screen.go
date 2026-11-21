package help

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/list"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// Column widths: two columns when both fit at least minCol.
const (
	minCol = 38
	maxCol = 54
	colGap = 3
)

// Screen is the keys screen: groups in one or two scrolling columns, a
// modal.Modal. up down j k pgup pgdown home end scroll; esc q ? close.
type Screen struct {
	th     theme.Theme
	title  string
	groups []Group
	open   string

	w, h int
	cols [][]string
	colW int
	top  int
	done bool
}

// Command opens the keys screen: on ? and F1 until the person binds
// other keys.
func Command(open func() tea.Cmd) command.Command {
	return command.Command{Name: "keys", Title: "Keys", Key: "? f1", Run: func([]string) tea.Cmd { return open() }}
}

// New is a keys screen scrolled to group open, "" the top.
func New(th theme.Theme, title string, groups []Group, open string) *Screen {
	return &Screen{th: th, title: title, groups: groups, open: open}
}

func (s *Screen) Init() tea.Cmd { return nil }
func (s *Screen) Done() bool    { return s.done }

// SetSize lays the groups out for a w x h box.
func (s *Screen) SetSize(w, h int) tea.Cmd {
	s.w, s.h = w, h
	s.layout()
	return nil
}

func (s *Screen) viewH() int { return max(s.h-2, 0) }

func (s *Screen) layout() {
	inner := s.w - 4
	n := 1
	if inner >= 2*minCol+colGap {
		n = 2
	}
	s.colW = min((inner-(n-1)*colGap)/n, maxCol)
	if s.colW <= 0 {
		s.cols = nil
		return
	}
	keyW := 0
	for _, g := range s.groups {
		for _, r := range g.Rows {
			keyW = max(keyW, ansi.StringWidth(r.Keys))
		}
	}
	keyW = min(keyW, s.colW/2)
	var blocks [][]string
	starts := map[string]int{}
	for _, g := range s.groups {
		b := []string{text.Fit(s.th.Render("ui.text.focus", g.Name), s.colW)}
		for _, r := range g.Rows {
			act := text.Wrap(r.Action, max(s.colW-keyW-2, 1))
			for i, a := range act {
				k := ""
				if i == 0 {
					k = r.Keys
				}
				b = append(b, text.Fit(s.th.Render("ui.statusline.key", text.Fit(k, keyW))+"  "+s.th.Render("ui.text", a), s.colW))
			}
		}
		b = append(b, text.Spaces(s.colW))
		blocks = append(blocks, b)
		starts[g.Name] = len(blocks) - 1
	}
	total := 0
	for _, b := range blocks {
		total += len(b)
	}
	s.cols = make([][]string, n)
	c := 0
	var openAt int
	for i, b := range blocks {
		if n == 2 && c == 0 && len(s.cols[0]) > 0 && len(s.cols[0])+len(b)/2 > total/2 {
			c = 1
		}
		if s.groups[i].Name == s.open {
			openAt = len(s.cols[c])
		}
		s.cols[c] = append(s.cols[c], b...)
	}
	if s.open != "" {
		s.top = openAt
		s.open = ""
	}
	s.top = min(max(s.top, 0), s.maxTop())
}

func (s *Screen) rows() int {
	n := 0
	for _, c := range s.cols {
		n = max(n, len(c))
	}
	return n
}

func (s *Screen) maxTop() int { return max(s.rows()-s.viewH(), 0) }

func (s *Screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "?", "f1", "ctrl+c":
			s.done = true
		case "up", "k":
			s.top--
		case "down", "j":
			s.top++
		case "pgup":
			s.top -= max(s.viewH()-1, 1)
		case "pgdown", "space":
			s.top += max(s.viewH()-1, 1)
		case "home", "g":
			s.top = 0
		case "end", "G":
			s.top = s.maxTop()
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			s.top -= 3
		case tea.MouseWheelDown:
			s.top += 3
		}
	}
	s.top = min(max(s.top, 0), s.maxTop())
	return s, nil
}

func (s *Screen) View() tea.View {
	if s.w < 6 || s.h < 3 || len(s.cols) == 0 {
		return tea.NewView("")
	}
	vh := min(s.viewH(), s.rows())
	cw := len(s.cols)*s.colW + (len(s.cols)-1)*colGap
	body := make([]string, vh)
	blank := text.Spaces(s.colW)
	for i := range body {
		var b strings.Builder
		b.WriteString(" ")
		for c, col := range s.cols {
			if c > 0 {
				b.WriteString(text.Spaces(colGap))
			}
			if j := s.top + i; j < len(col) {
				b.WriteString(col[j])
			} else {
				b.WriteString(blank)
			}
		}
		body[i] = b.String()
	}
	pos := list.Pos{Top: s.top, Shown: vh, Total: s.rows()}
	lines := modal.Frame(s.th, modal.Box{Title: s.title, Keys: "esc Close", Focused: true, Pos: pos}, body, cw+4)
	return tea.NewView(strings.Join(lines, "\n"))
}
