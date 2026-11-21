package guide

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/termimg"
	"github.com/MaximKayTee/gotuitools/theme"
)

// desk is a program with two commands to learn and a guide of three
// steps: one to read, two to do.
type desk struct {
	t    *testing.T
	path string
	reg  *command.Registry
	keys *command.Keys
	g    *Guide
}

func steps() []Step {
	return []Step{
		{Title: "Notes are files", Text: "Each note is a file in the folder. {pane.split} shows two at once, {quit} leaves, {nothing} is left."},
		{Command: "note.new", Text: "The note is made in the folder shown."},
		{Command: "pane.split", Title: "Side by side", Text: "Two notes next to each other."},
	}
}

func newDesk(t *testing.T) *desk {
	d := &desk{t: t, path: filepath.Join(t.TempDir(), "app", "guide.toml"), reg: &command.Registry{}, keys: &command.Keys{}}
	run := func([]string) tea.Cmd { return nil }
	d.reg.MustAdd(
		command.Command{Name: "note.new", Title: "New note", Key: "n ctrl+n", Run: run},
		command.Command{Name: "pane.split", Title: "Split pane right", Key: "ctrl+b,v", Run: run},
		command.Command{Name: "quit", Title: "Quit", Run: run},
		command.Command{Name: "locked", Title: "Locked", Available: func() bool { return false }, Run: run},
		Command(func() tea.Cmd { return nil }),
	)
	if err := d.keys.Defaults("normal", d.reg.All()); err != nil {
		t.Fatal(err)
	}
	d.open()
	return d
}

// open is the program starting: the progress read, the registry reporting.
func (d *desk) open() {
	d.g = Open(d.path, steps())
	d.reg.Ran = d.g.Ran
}

func (d *desk) modal() *Model {
	m := d.g.Modal(theme.Default(), d.reg, d.keys, "normal")
	m.SetSize(60, 30)
	return m
}

func press(m *Model, keys ...string) {
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "left": tea.KeyLeft, "right": tea.KeyRight,
		"f9": tea.KeyF9, "f12": tea.KeyF12}
	for _, k := range keys {
		if c, ok := codes[k]; ok {
			m.Update(tea.KeyPressMsg{Code: c})
			continue
		}
		m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
	}
}

func screen(m *Model) string { return ansi.Strip(m.View().Content) }

func (d *desk) shows(m *Model, texts ...string) {
	d.t.Helper()
	for _, s := range texts {
		if !strings.Contains(screen(m), s) {
			d.t.Fatalf("no %q in:\n%s", s, screen(m))
		}
	}
}

func (d *desk) hides(m *Model, texts ...string) {
	d.t.Helper()
	for _, s := range texts {
		if strings.Contains(screen(m), s) {
			d.t.Fatalf("%q in:\n%s", s, screen(m))
		}
	}
}

// The first start: the guide has never been shown, so the program opens
// it. From then on it does not, on this start or the next.
func TestFirstStart(t *testing.T) {
	d := newDesk(t)
	if !d.g.First() {
		t.Fatal("not the first start")
	}
	d.modal()
	if d.g.First() {
		t.Fatal("shown, and still the first start")
	}
	d.open()
	if d.g.First() {
		t.Fatal("the next start is the first again")
	}
}

// Learning by doing: a step is ticked once its command has run, by a
// key, the palette or anything else, and stays ticked the next start. A
// step with nothing to do is ticked once it has been shown. The guide
// opens on the first step not ticked.
func TestTickedByDoing(t *testing.T) {
	d := newDesk(t)
	d.reg.Run("pane.split")
	d.reg.Run("locked")
	d.reg.Run("quit")
	if d.g.Done(0) || d.g.Done(1) || !d.g.Done(2) {
		t.Fatalf("done %v %v %v", d.g.Done(0), d.g.Done(1), d.g.Done(2))
	}
	m := d.modal()
	d.shows(m, "Notes are files", "● ○ ●")
	d.open()
	if !d.g.Done(0) || d.g.Done(1) || !d.g.Done(2) {
		t.Fatalf("next start: done %v %v %v", d.g.Done(0), d.g.Done(1), d.g.Done(2))
	}
	m = d.modal()
	d.shows(m, "New note", "● ○ ●")
	d.hides(m, "Done")
	d.reg.Run("note.new extra words")
	d.shows(m, "● ● ●", "Done")
	d.shows(d.modal(), "Notes are files")
}

// A page: the command's title unless the step has its own, the text
// with the keys the person has now, every key of the command, the dots.
func TestPage(t *testing.T) {
	d := newDesk(t)
	d.keys.Bind("normal", "ctrl+b v", "")
	d.keys.Bind("normal", "alt+s", "pane.split")
	m := d.modal()
	d.shows(m, "╭─ Guide ", "Notes are files", "Alt+S shows two", "Quit leaves, {nothing} is left.", "● ○ ○", "right Next  left Back")
	d.hides(m, "Split pane right")
	press(m, "right")
	d.shows(m, "New note", "The note is made in the folder shown.", "n, Ctrl+N  New note", "● ○ ○")
	press(m, "enter")
	d.shows(m, "Side by side", "Alt+S  Split pane right")
	lines := strings.Split(m.View().Content, "\n")
	for _, l := range lines {
		if ansi.StringWidth(l) != 60 {
			t.Errorf("width %d %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	// as tall as the tallest step: title, text, keys, dots, a gap between
	if len(lines) != 9 {
		t.Errorf("%d lines:\n%s", len(lines), screen(m))
	}
}

// Moving: back and forth, not past either end; enter on the last step
// closes, and so do esc and the key the guide is on, whichever it is.
func TestKeys(t *testing.T) {
	d := newDesk(t)
	m := d.modal()
	press(m, "left")
	d.shows(m, "Notes are files")
	press(m, "right", "right", "right")
	d.shows(m, "Side by side")
	if m.Done() {
		t.Fatal("right closed it")
	}
	press(m, "left", "l", "h", " ", "enter")
	if !m.Done() {
		t.Fatal("enter on the last step")
	}
	m = d.modal()
	press(m, "f12")
	if !m.Done() {
		t.Fatal("f12")
	}
	d.keys.Unbind("normal", Name)
	d.keys.Bind("normal", "f9", Name)
	m = d.modal()
	press(m, "f12")
	if m.Done() {
		t.Fatal("f12 is no longer its key")
	}
	press(m, "f9")
	if !m.Done() {
		t.Fatal("f9")
	}
	m = d.modal()
	press(m, "esc")
	if !m.Done() {
		t.Fatal("esc")
	}
}

// A click on a dot goes to its step.
func TestClickADot(t *testing.T) {
	d := newDesk(t)
	m := d.modal()
	m.SetOrigin(4, 2)
	lines := strings.Split(screen(m), "\n")
	row := len(lines) - 2
	col := ansi.StringWidth(lines[row][:strings.Index(lines[row], "●")])
	m.Update(tea.MouseClickMsg{X: 4 + col + 4, Y: 2 + row, Button: tea.MouseLeft})
	d.shows(m, "Side by side")
	m.Update(tea.MouseClickMsg{X: 4 + col + 1, Y: 2 + row, Button: tea.MouseLeft})
	m.Update(tea.MouseClickMsg{X: 4 + col + 2, Y: 1 + row, Button: tea.MouseLeft})
	d.shows(m, "Side by side")
	m.Update(tea.MouseClickMsg{X: 4 + col + 2, Y: 2 + row, Button: tea.MouseLeft})
	d.shows(m, "New note")
}

func picture(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(0xff * (y / 10 % 2)), 0, 0xff, 0xff})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// A step with a picture: drawn above the text where the terminal draws
// pictures, sized to the box; without, the step is its text. The picture
// is sent when its step shows and taken away when the guide closes.
func TestPicture(t *testing.T) {
	d := newDesk(t)
	d.g.Steps[1].Picture = picture(200, 100) // 20 x 5 cells of 10 x 20
	m := d.modal()
	plain := len(strings.Split(screen(m), "\n"))

	d.g.Encoder = termimg.NewBlocks()
	m = d.modal()
	if got := len(strings.Split(screen(m), "\n")); got != plain+6 {
		t.Fatalf("%d lines, %d without pictures", got, plain)
	}
	press(m, "left")
	d.hides(m, "▀")
	press(m, "right")
	d.shows(m, "▀", "New note", "The note is made")
	for _, l := range strings.Split(m.View().Content, "\n") {
		if ansi.StringWidth(l) != 60 {
			t.Errorf("width %d %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}

	var out bytes.Buffer
	d.g.Encoder, d.g.Queue = termimg.Kitty{}, termimg.NewQueue(&out)
	m = d.g.Modal(theme.Default(), d.reg, d.keys, "normal")
	run := func(c tea.Cmd) {
		if c != nil {
			c()
		}
	}
	run(m.SetSize(60, 30))
	if !strings.Contains(out.String(), "\x1b_G") {
		t.Fatalf("no picture sent: %q", out.String())
	}
	for _, l := range strings.Split(m.View().Content, "\n") {
		if ansi.StringWidth(l) != 60 {
			t.Errorf("width %d %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	out.Reset()
	run(m.SetSize(60, 30))
	for _, k := range []rune{tea.KeyLeft, tea.KeyRight} {
		_, c := m.Update(tea.KeyPressMsg{Code: k})
		run(c)
	}
	if out.Len() != 0 {
		t.Fatalf("sent again at the same size: %q", out.String())
	}
	_, c := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	run(c)
	if !strings.Contains(out.String(), "\x1b_Ga=d") {
		t.Fatalf("not taken away: %q", out.String())
	}
}

// A step with a live view: the program draws its screen as it is now,
// at the size the box has, each time the page shows.
func TestLiveView(t *testing.T) {
	d := newDesk(t)
	notes := []string{"Harbour"}
	var w, h int
	d.g.Steps[1].View = func(vw, vh int) []string {
		w, h = vw, vh
		return append([]string{"  Files"}, notes...)
	}
	d.g.Width = 50
	m := d.modal()
	d.hides(m, "Harbour")
	plain := len(strings.Split(screen(m), "\n"))
	press(m, "right")
	d.shows(m, "│ ╭──", "│ │  Files", "│ │Harbour", "The note is made")
	// 50 wide: 46 between the borders and the gaps, 44 in the view's box;
	// 46/4 rows, 9 in the box
	if w != 44 || h != 9 || plain != 9+12 {
		t.Fatalf("view %d x %d, %d lines", w, h, plain)
	}
	notes = append(notes, "Varnish the stairs")
	d.hides(m, "Varnish the stairs")
	press(m, "left", "right")
	d.shows(m, "│ │Varnish the stairs")
	for _, l := range strings.Split(m.View().Content, "\n") {
		if ansi.StringWidth(l) != 50 {
			t.Errorf("width %d %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	// no room for it: the step is its text
	m.SetSize(50, 11)
	d.hides(m, "Harbour")
	d.shows(m, "The note is made")
}

// Progress kept nowhere: a guide with no file works for the run, and
// does not open by itself.
func TestNoFile(t *testing.T) {
	g := Open("", steps())
	g.Ran("note.new")
	if g.First() || !g.Done(1) {
		t.Fatalf("first %v done %v", g.First(), g.Done(1))
	}
}

// The first step of every guide says what it is and how to come back
// to it, by the key the guide is on now.
func TestWelcome(t *testing.T) {
	d := newDesk(t)
	d.g.Steps = append([]Step{Welcome}, d.g.Steps...)
	m := d.modal()
	d.shows(m, "Welcome", "Hi! Here's a quick tutorial to get you started.", "at any time with F12.", "● ○ ○ ○")
	d.keys.Unbind("normal", Name)
	d.keys.Bind("normal", "f9", Name)
	d.shows(d.modal(), "Notes are files")
	press(m, "left")
	m.SetSize(60, 30)
	d.shows(m, "at any time with F9.")
}

// The command every program adds: the guide on F12.
func TestCommand(t *testing.T) {
	opened := false
	c := Command(func() tea.Cmd { opened = true; return nil })
	c.Run(nil)
	if c.Name != "guide" || c.Key != "f12" || !opened {
		t.Fatalf("%+v opened %v", c, opened)
	}
}
