// Package guide is a program's quick start: a step to a page, each
// ticked once the person has done what it teaches. A modal.Modal.
//
//	right l, left h backspace   the next step, the one before
//	enter space                 the next step; on the last, close
//	esc ctrl+c, the guide's key close
//	click                       a dot: its step
//
// A program lists its Steps, opens the progress kept in a file, has its
// registry tell the guide what runs, adds Command, and shows the guide
// by itself on the first start:
//
//	steps := []guide.Step{guide.Welcome, {Command: "note.new", Text: "…"}}
//	g := guide.Open(filepath.Join(configDir, "guide.toml"), steps)
//	reg.Ran = g.Ran
//	open := func() tea.Cmd { return stack.Open(g.Modal(th, reg, keys, mode)) }
//	reg.MustAdd(guide.Command(open))
//	if g.First() { cmd = open() }
//
// The keys a page shows are the ones bound now. Styles: modal.Frame's,
// ui.text, ui.text.focus, ui.text.inactive, ui.statusline.key,
// ui.menu.selected.
package guide

import (
	"bytes"
	"cmp"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/BurntSushi/toml"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/help"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/termimg"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// Name is the command that opens the guide.
const Name = "guide"

// DefaultImageID is the image id of the picture shown: under
// player's, over pics'.
const DefaultImageID uint32 = 0xFFFFFD

// Command opens the guide: on F12 until the person binds another key.
func Command(open func() tea.Cmd) command.Command {
	return command.Command{Name: Name, Title: "Guide", Key: "f12", Run: func([]string) tea.Cmd { return open() }}
}

// Step is one page.
type Step struct {
	// Command is what the step teaches: its keys are shown, and the step
	// is ticked once it has run. "": ticked once shown.
	Command string
	// Title is the heading; "" the command's title.
	Title string
	// Text is wrapped to the box. {note.new} in it is the first key of
	// that command, its title when it has no key.
	Text string
	// Picture is an image file's bytes, in a format termimg.Decode
	// reads; nil none.
	Picture []byte
	// View draws a screen of the program as it is now, w x h cells, in a
	// box above the text; nil none. It is called when the page shows.
	View func(w, h int) []string
}

func (s Step) id() string { return cmp.Or(s.Command, s.Title) }

// Welcome is the first step of every program's guide: what this is, and
// the key that brings it back.
var Welcome = Step{Title: "Welcome", Text: "Hi! Here's a quick tutorial to get you started. You can revisit it at any time with {" + Name + "}."}

// Guide is a program's steps and the ones the person has done.
type Guide struct {
	Steps []Step
	// Width is the most cells the box is wide; 0 is 72.
	Width int
	// Encoder draws the pictures; nil is termimg.Default. Queue takes
	// its escapes; nil for an encoder that writes none. Cell is a cell
	// in pixels; termimg.DefaultCell. ID is the image id; DefaultImageID.
	Encoder termimg.Encoder
	Queue   *termimg.Queue
	Cell    image.Point
	ID      uint32

	path  string
	mu    sync.Mutex
	state state
}

// state is the file.
type state struct {
	Shown bool     `toml:"shown"`
	Done  []string `toml:"done"`
}

// Open is a guide with the progress kept at path, "" nowhere. A file
// that is not there or cannot be read is a first start.
func Open(path string, steps []Step) *Guide {
	g := &Guide{Steps: steps, path: path}
	if path != "" {
		toml.DecodeFile(path, &g.state)
	}
	return g
}

// change lets f change the state and writes the file if it says it
// did. A failed write is a guide that shows again.
func (g *Guide) change(f func(*state) bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !f(&g.state) || g.path == "" {
		return
	}
	b, err := toml.Marshal(g.state)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(g.path), 0o755)
	if os.WriteFile(g.path+".tmp", b, 0o644) == nil {
		os.Rename(g.path+".tmp", g.path)
	}
}

// First reports a guide never shown, to show it. A guide kept nowhere
// never is: it would show on every start.
func (g *Guide) First() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.state.Shown && g.path != ""
}

// Done reports step i ticked.
func (g *Guide) Done(i int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Contains(g.state.Done, g.Steps[i].id())
}

// Ran ticks the steps that teach the command name. It is what a
// command.Registry's Ran is set to.
func (g *Guide) Ran(name string) {
	if slices.ContainsFunc(g.Steps, func(s Step) bool { return s.Command == name }) {
		g.tick(name)
	}
}

func (g *Guide) tick(id string) {
	g.change(func(s *state) bool {
		was := slices.Contains(s.Done, id)
		if !was {
			s.Done = append(s.Done, id)
		}
		return !was
	})
}

func (g *Guide) encoder() termimg.Encoder {
	if g.Encoder != nil {
		return g.Encoder
	}
	return termimg.Default
}

// send writes picture escapes.
func (g *Guide) send(escapes ...string) tea.Cmd {
	if g.Queue == nil {
		return nil
	}
	for _, e := range escapes {
		g.Queue.Push(e)
	}
	return g.Queue.Flush()
}

// page is a step laid out: its picture in cells, 0 x 0 none; the rows
// of its view's box, 0 none.
type page struct {
	title, keys, label string
	text               []string
	cols, rows         int
	view               int
}

// Model is the guide on screen. Use Guide.Modal.
type Model struct {
	g     *Guide
	th    theme.Theme
	reg   *command.Registry
	keys  *command.Keys
	mode  string
	pages []page
	at    int
	body  int      // rows between the borders
	live  []string // the step\'s view, in its box
	sent  [3]int   // the picture sent: its step, cols, rows

	w, h int
	x, y int
	done bool
}

// Modal is the guide on the first step not ticked, the first when all
// are, with the keys bound in mode. From now on it has been shown.
func (g *Guide) Modal(th theme.Theme, reg *command.Registry, keys *command.Keys, mode string) *Model {
	g.change(func(s *state) bool {
		was := s.Shown
		s.Shown = true
		return !was
	})
	m := &Model{g: g, th: th, reg: reg, keys: keys, mode: mode, sent: [3]int{-1}}
	for i := range g.Steps {
		if !g.Done(i) {
			m.at = i
			break
		}
	}
	m.show(m.at)
	return m
}

func (m *Model) Init() tea.Cmd { return nil }

// Done reports closed.
func (m *Model) Done() bool { return m.done }

// SetOrigin is where the box was drawn.
func (m *Model) SetOrigin(x, y int) { m.x, m.y = x, y }

func (m *Model) width() int { return min(m.w, cmp.Or(m.g.Width, 72)) }

var named = regexp.MustCompile(`\{[^{}\s]+\}`)

// SetSize is the box the guide may use: it is as tall as its tallest
// step, a picture or a view as large as the rest leaves room for.
func (m *Model) SetSize(w, h int) tea.Cmd {
	m.w, m.h = w, h
	iw := m.width() - 4
	_, blank := m.g.encoder().(termimg.Blank)
	m.pages, m.body = m.pages[:0], 0
	for _, s := range m.g.Steps {
		p := page{title: s.Title}
		p.text = text.Wrap(named.ReplaceAllStringFunc(s.Text, func(n string) string {
			c, ok := m.reg.Get(n[1 : len(n)-1])
			if !ok {
				return n
			}
			return cmp.Or(help.KeyName(m.keys.For(m.mode, c.Name)), c.Label())
		}), max(iw, 1))
		rows := 2 + len(p.text) + 2 // title, text, dots, a gap between
		if c, ok := m.reg.Get(s.Command); ok {
			all := m.keys.All(m.mode, c.Name)
			for i, k := range all {
				all[i] = help.KeyName(k)
			}
			p.title, p.label, p.keys = cmp.Or(p.title, c.Label()), c.Label(), strings.Join(all, ", ")
			rows += 2
		}
		if c, _, err := image.DecodeConfig(bytes.NewReader(s.Picture)); err == nil && !blank {
			p.cols, p.rows = termimg.Fit(c.Width, c.Height, iw, h-2-rows-1, termimg.Cell(m.g.Cell), false)
		}
		if p.rows > 0 {
			rows += p.rows + 1
		}
		if room := min(h-2-rows-1, max(iw/4, 6)); s.View != nil && room >= 4 {
			p.view = room
			rows += room + 1
		}
		m.pages, m.body = append(m.pages, p), max(m.body, rows)
	}
	m.body = min(m.body, h-2)
	return m.show(m.at)
}

// show goes to step i: ticks it if it has nothing to do, and sends its
// picture unless that is the one sent.
func (m *Model) show(i int) tea.Cmd {
	m.at = min(max(i, 0), len(m.g.Steps)-1)
	if m.at < 0 {
		return nil
	}
	s := m.g.Steps[m.at]
	if s.Command == "" {
		m.g.tick(s.id())
	}
	if m.live = nil; m.at >= len(m.pages) {
		return nil
	}
	p := m.pages[m.at]
	if w := m.width() - 4; p.view > 0 {
		m.live = modal.Frame(m.th, modal.Box{}, text.Block(s.View(w-2, p.view-2), w-2, p.view-2), w)
	}
	if p.rows == 0 {
		return nil
	}
	sent := [3]int{m.at, p.cols, p.rows}
	img, err := termimg.Decode(m.g.Steps[m.at].Picture)
	if err != nil || sent == m.sent {
		return nil
	}
	m.sent = sent
	cell, enc, id := termimg.Cell(m.g.Cell), m.g.encoder(), cmp.Or(m.g.ID, DefaultImageID)
	return m.g.send(enc.TransmitImage(id, termimg.Scale(img, img.Bounds(), p.cols*cell.X, p.rows*cell.Y)),
		enc.VirtualPlace(id, 1, p.cols, p.rows))
}

// close takes the picture away.
func (m *Model) close() tea.Cmd {
	m.done = true
	if m.sent[0] < 0 {
		return nil
	}
	return m.g.send(m.g.encoder().Delete(cmp.Or(m.g.ID, DefaultImageID)))
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		last := m.at == len(m.g.Steps)-1
		switch k := msg.String(); {
		case k == "esc", k == "ctrl+c", slices.Contains(m.keys.All(m.mode, Name), k),
			last && (k == "enter" || k == "space"):
			return m, m.close()
		case k == "right", k == "l", k == "enter", k == "space":
			return m, m.show(m.at + 1)
		case k == "left", k == "h", k == "backspace":
			return m, m.show(m.at - 1)
		}
	case tea.MouseClickMsg:
		// the dots: the last row in the borders, a space between
		n := len(m.g.Steps)
		x := msg.X - m.x - 1 - (m.width()-2-(2*n-1))/2
		if msg.Button == tea.MouseLeft && msg.Y == m.y+m.body && x >= 0 && x%2 == 0 && x/2 < n {
			return m, m.show(x / 2)
		}
	}
	return m, nil
}

// View is the box.
func (m *Model) View() tea.View {
	w := m.width()
	if w < 12 || m.body < 3 || m.at >= len(m.pages) {
		return tea.NewView("")
	}
	iw, th, p := w-2, m.th, m.pages[m.at]
	done := ""
	if m.g.Done(m.at) {
		done = "Done"
	}
	body := []string{text.Row(" "+th.Render("ui.text.focus", p.title), th.Render("ui.text.inactive", done)+" ", iw), ""}
	enc, id := m.g.encoder(), cmp.Or(m.g.ID, DefaultImageID)
	for r := range p.rows {
		body = append(body, text.Spaces((iw-p.cols)/2)+enc.PlaceholderRow(id, 1, r, p.cols))
	}
	if p.rows > 0 {
		body = append(body, "")
	}
	for _, l := range m.live {
		body = append(body, " "+l)
	}
	if m.live != nil {
		body = append(body, "")
	}
	for _, l := range p.text {
		body = append(body, " "+th.Render("ui.text", l))
	}
	if row := th.Render("ui.text", p.label); p.keys != "" {
		body = append(body, "", " "+th.Render("ui.statusline.key", p.keys)+"  "+row)
	} else if p.label != "" {
		body = append(body, "", " "+row)
	}
	body = text.Block(body, iw, m.body-1)
	dots := make([]string, len(m.g.Steps))
	for i := range dots {
		dot, style := "○", "ui.text.inactive"
		if m.g.Done(i) {
			dot, style = "●", "ui.text"
		}
		if i == m.at {
			style = "ui.menu.selected"
		}
		dots[i] = th.Render(style, dot)
	}
	body = append(body, text.Center(strings.Join(dots, " "), iw))
	title := "Guide"
	if c, ok := m.reg.Get(Name); ok {
		title = c.Label()
	}
	return tea.NewView(strings.Join(modal.Frame(th, modal.Box{Title: title, Keys: "right Next  left Back  esc Close", Focused: true}, body, w), "\n"))
}
