package settingsui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/settings"
	"github.com/MaximKayTee/gotuitools/theme"
)

type conf struct {
	Mute   bool          `toml:"mute" label:"Mute at start"`
	Volume int           `toml:"volume" min:"0" max:"130" help:"Start volume"`
	Wait   time.Duration `toml:"wait"`
	Layout struct {
		Style string `toml:"style" enum:"layered,exploded"`
		Key   string `toml:"key" secret:"true"`
	} `toml:"layout"`
}

type harness struct {
	t     *testing.T
	m     *Model
	store *settings.Store[conf]
	path  string
	out   []tea.Msg
}

func newHarness(t *testing.T) *harness {
	path := filepath.Join(t.TempDir(), "c.toml")
	var def conf
	def.Volume, def.Wait, def.Layout.Style = 100, 30*time.Second, "layered"
	st, err := settings.Open(path, def)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, store: st, path: path, m: New(theme.Default(), st, "")}
	h.m.SetSize(70, 20)
	h.run(h.m.Init())
	return h
}

// run executes cmd and feeds its messages back, keeping the ones the
// model sends out.
func (h *harness) run(cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case ChangedMsg, ReloadedMsg:
			h.out = append(h.out, msg)
		default:
			_, next := h.m.Update(msg)
			queue = append(queue, next)
		}
	}
}

func (h *harness) keys(keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		_, cmd := h.m.Update(msg)
		h.run(cmd)
	}
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.keys(string(r))
	}
}

func (h *harness) screen() string { return ansi.Strip(h.m.View().Content) }

func (h *harness) file() string {
	b, _ := os.ReadFile(h.path)
	return string(b)
}

func TestRowsAndBoxShape(t *testing.T) {
	h := newHarness(t)
	s := h.screen()
	for _, want := range []string{"Settings", "Search", "Mute at start", "false", "Layout", "Style", "layered", "None", "↓ List"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	lines := strings.Split(h.m.View().Content, "\n")
	// as tall as the list: borders, search, rows, a blank and two help rows
	if want := len(h.m.rows) + 6; len(lines) != max(want, 12) || len(lines) >= 20 {
		t.Errorf("%d lines for %d rows", len(lines), len(h.m.rows))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) != 70 {
			t.Errorf("width %d: %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}
}

func TestToggleAndPickWriteFile(t *testing.T) {
	h := newHarness(t)
	h.keys("down", "enter")
	if !h.store.Get().Mute || !strings.Contains(h.file(), "mute = true") {
		t.Fatalf("toggle not saved:\n%s", h.file())
	}
	if len(h.out) != 1 || h.out[0].(ChangedMsg).Field.Key != "mute" {
		t.Errorf("ChangedMsg %v", h.out)
	}
	h.keys("down", "down", "down", "enter")
	if !strings.Contains(h.screen(), "• layered") {
		t.Fatalf("picker:\n%s", h.screen())
	}
	h.keys("down", "enter")
	if h.store.Get().Layout.Style != "exploded" {
		t.Errorf("pick %q", h.store.Get().Layout.Style)
	}
	h.keys("d")
	if h.store.Get().Layout.Style != "layered" {
		t.Error("reset")
	}
}

func TestLineEditErrorStaysOpen(t *testing.T) {
	h := newHarness(t)
	h.keys("down", "down", "enter")
	for range 3 {
		h.keys("backspace")
	}
	h.typeText("500")
	h.keys("enter")
	s := h.screen()
	if !strings.Contains(s, "out of range") || !strings.Contains(s, "enter Save") {
		t.Fatalf("error not shown in the open edit:\n%s", s)
	}
	for range 3 {
		h.keys("backspace")
	}
	h.typeText("42")
	h.keys("enter")
	if h.store.Get().Volume != 42 || strings.Contains(h.screen(), "out of range") {
		t.Errorf("volume %d\n%s", h.store.Get().Volume, h.screen())
	}
	if !strings.Contains(h.screen(), "Default 100") {
		t.Errorf("default fact:\n%s", h.screen())
	}
}

func TestSearchAndSecret(t *testing.T) {
	h := newHarness(t)
	h.typeText("key")
	s := h.screen()
	if !strings.Contains(s, "Layout › Key") || strings.Contains(s, "Mute") {
		t.Fatalf("search:\n%s", s)
	}
	h.keys("enter")
	h.typeText("hunter2")
	if strings.Contains(h.screen(), "hunter2") {
		t.Error("secret shown while typing")
	}
	h.keys("enter")
	if h.store.Get().Layout.Key != "hunter2" || strings.Contains(h.screen(), "hunter2") || !strings.Contains(h.screen(), "••••••") {
		t.Errorf("secret:\n%s", h.screen())
	}
	h.keys("esc")
	if strings.Contains(h.screen(), "Layout › Key") || h.m.Done() {
		t.Error("esc clears the search first")
	}
	h.keys("esc")
	if !h.m.Done() {
		t.Error("esc closes")
	}
}

func TestReloadAfterOutsideEdit(t *testing.T) {
	h := newHarness(t)
	os.WriteFile(h.path, []byte("volume = 7\n"), 0o644)
	_, cmd := h.m.Update(fileEdited(h.path))
	h.run(cmd)
	if h.store.Get().Volume != 7 || !strings.Contains(h.screen(), "7") {
		t.Errorf("reload:\n%s", h.screen())
	}
}

func fileEdited(path string) tea.Msg { return sysEdited(path) }
