package files

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/theme"
)

// tree makes a home folder: names ending in "/" are folders.
func tree(t *testing.T, names ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, n := range names {
		p := filepath.Join(home, n)
		if strings.HasSuffix(n, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func open(home string, o Options, chosen *string) *Model {
	if o.Dir == "" {
		o.Dir = home
	}
	m := New(theme.Default(), o, func(path string) tea.Cmd { *chosen = path; return nil })
	m.SetSize(60, 20)
	return m
}

func press(m *Model, keys ...string) {
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp,
		"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "backspace": tea.KeyBackspace}
	for _, k := range keys {
		if c, ok := codes[k]; ok {
			m.Update(tea.KeyPressMsg{Code: c})
			continue
		}
		for _, r := range k {
			m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
}

func want(t *testing.T, m *Model, path string, rows ...string) {
	t.Helper()
	if m.Path() != path || !slices.Equal(m.Shown(), rows) {
		t.Fatalf("path %q rows %q, want %q %q", m.Path(), m.Shown(), path, rows)
	}
}

var notes = []string{"notes/work/", "notes/home/", "notes/todo.md", "music/", ".config/", "readme.md", "Plot.PNG", "cat.png"}

// Choosing a folder: only folders are listed, the first row is the
// folder shown; typing narrows, tab goes in, enter chooses the row.
func TestChooseAFolder(t *testing.T) {
	var got string
	home := tree(t, notes...)
	m := open(home, Options{Title: "Open", Folders: true}, &got)
	want(t, m, "~/", ".", "music/", "notes/")
	press(m, "no")
	want(t, m, "~/no", "notes/")
	press(m, "tab")
	want(t, m, "~/notes/", ".", "home/", "work/")
	press(m, "enter")
	if !m.Done() || got != filepath.Join(home, "notes") {
		t.Fatalf("done %v chose %q", m.Done(), got)
	}

	m = open(home, Options{Folders: true}, &got)
	press(m, "down", "down", "right", "down", "down", "enter")
	if got != filepath.Join(home, "notes", "work") {
		t.Fatalf("chose %q", got)
	}
}

// Going up puts the cursor on the folder left; from a typed name too.
func TestUp(t *testing.T) {
	var got string
	home := tree(t, notes...)
	m := open(home, Options{Dir: filepath.Join(home, "notes"), Folders: true}, &got)
	press(m, "wo", "left")
	want(t, m, "~/", ".", "music/", "notes/")
	press(m, "enter")
	if got != filepath.Join(home, "notes") {
		t.Fatalf("chose %q", got)
	}
	m = open(home, Options{Dir: "/"}, &got)
	press(m, "left")
	if m.Path() != "/" {
		t.Fatalf("above the root: %q", m.Path())
	}
}

// Choosing a file: folders are listed first and enter goes into one;
// only files of the kinds asked for are listed.
func TestChooseAFile(t *testing.T) {
	var got string
	home := tree(t, notes...)
	m := open(home, Options{}, &got)
	want(t, m, "~/", "music/", "notes/", "cat.png", "Plot.PNG", "readme.md")
	press(m, "down", "enter")
	want(t, m, "~/notes/", "home/", "work/", "todo.md")
	if m.Done() {
		t.Fatal("a folder was chosen as a file")
	}
	press(m, "todo", "enter")
	if !m.Done() || got != filepath.Join(home, "notes", "todo.md") {
		t.Fatalf("done %v chose %q", m.Done(), got)
	}

	m = open(home, Options{Ext: []string{".png"}}, &got)
	want(t, m, "~/", "music/", "notes/", "cat.png", "Plot.PNG")
}

// Names starting with a dot are listed once a dot is typed.
func TestHidden(t *testing.T) {
	var got string
	m := open(tree(t, notes...), Options{Folders: true}, &got)
	press(m, ".")
	want(t, m, "~/.", ".config/")
}

// A path typed or pasted whole: the row it names is under the cursor,
// whatever else matches. Typed after the folder shown, a path from the
// root or from home starts again. A path from neither starts at Dir.
func TestTypedPath(t *testing.T) {
	var got string
	home := tree(t, "a/yy/", "a/y/", "a/Yak/")
	m := open(home, Options{Folders: true}, &got)
	m.Update(tea.PasteMsg{Content: filepath.Join(home, "a", "y") + "\n"})
	press(m, "enter")
	if got != filepath.Join(home, "a", "y") {
		t.Fatalf("chose %q", got)
	}
	m = open(home, Options{Dir: filepath.Join(home, "a"), Folders: true}, &got)
	press(m, home+"/a/yy")
	want(t, m, home+"/a/yy", "yy/")
	press(m, "/~/a/Y")
	want(t, m, "~/a/Y", "y/", "Yak/", "yy/")
	m = open(home, Options{Folders: true}, &got)
	m.SetPath("a/")
	want(t, m, "a/", ".", "y/", "Yak/", "yy/")
}

// A name that is not there: chosen as typed when New is set, and never
// when something has the name.
func TestNew(t *testing.T) {
	var got string
	home := tree(t, notes...)
	m := open(home, Options{Folders: true, New: true}, &got)
	press(m, "kitchen")
	want(t, m, "~/kitchen", "kitchen")
	press(m, "enter")
	if got != filepath.Join(home, "kitchen") {
		t.Fatalf("chose %q", got)
	}
	m.SetPath("~/deep/down")
	want(t, m, "~/deep/down", "down")
	m.SetPath("~/readme.md")
	want(t, m, "~/readme.md")
	m.SetPath("~/no")
	want(t, m, "~/no", "notes/", "no")

	m = open(home, Options{Folders: true}, &got)
	got = ""
	press(m, "kitchen", "enter")
	if m.Done() || got != "" {
		t.Fatalf("done %v chose %q", m.Done(), got)
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "None") {
		t.Errorf("no None row:\n%s", v)
	}
}

// The box: the title, the path, the rows, every line as wide. A click
// on a folder goes in; on a file, or the folder shown, it chooses.
func TestViewAndClick(t *testing.T) {
	var got string
	home := tree(t, notes...)
	m := open(home, Options{Title: "Open"}, &got)
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != 19 {
		t.Fatalf("%d lines", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) != 60 {
			t.Errorf("width %d %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	for i, s := range []string{"Open", "~/", "music/", "notes/", "cat.png"} {
		if !strings.Contains(ansi.Strip(lines[i]), s) {
			t.Errorf("line %d %q, want %q", i, ansi.Strip(lines[i]), s)
		}
	}
	m.SetOrigin(5, 1)
	m.Update(tea.MouseClickMsg{X: 10, Y: 1 + 2 + 1, Button: tea.MouseLeft})
	want(t, m, "~/notes/", "home/", "work/", "todo.md")
	m.Update(tea.MouseClickMsg{X: 10, Y: 1 + 2 + 2, Button: tea.MouseLeft})
	if got != filepath.Join(home, "notes", "todo.md") {
		t.Fatalf("click chose %q", got)
	}

	m = open(home, Options{Folders: true}, &got)
	m.SetOrigin(0, 0)
	m.Update(tea.MouseClickMsg{X: 3, Y: 2 + 2, Button: tea.MouseLeft})
	m.Update(tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft})
	if got != filepath.Join(home, "notes") {
		t.Fatalf("click chose %q", got)
	}
	m = open(home, Options{}, &got)
	press(m, "esc")
	if !m.Done() {
		t.Error("esc")
	}
}

// A folder that cannot be read says why.
func TestUnreadable(t *testing.T) {
	var got string
	home := tree(t, "locked/")
	os.Chmod(filepath.Join(home, "locked"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(home, "locked"), 0o755) })
	if _, err := os.ReadDir(filepath.Join(home, "locked")); err == nil {
		t.Skip("everything is readable here")
	}
	m := open(home, Options{}, &got)
	press(m, "enter")
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "permission denied") {
		t.Errorf("no reason:\n%s", v)
	}
}
