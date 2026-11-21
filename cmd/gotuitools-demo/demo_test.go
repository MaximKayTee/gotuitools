package main

import (
	"image"
	"io"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/text"
)

// testModel is a model over a settings file in a temp dir, no graphics.
func testModel(t *testing.T, w, h int) *model {
	t.Helper()
	store, err := openConfig(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(store, options{cell: image.Pt(10, 20), out: io.Discard})
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(key("esc")) // the guide, open on a first start
	return m
}

// A first start opens the guide; what is done from then on fills its
// dots, and the next start opens without it.
func TestGuide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	start := func() *model {
		store, err := openConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		m := newModel(store, options{cell: image.Pt(10, 20), out: io.Discard})
		m.Init()
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		return m
	}
	m := start()
	if v := text.Strip(m.View().Content); !strings.Contains(v, "╭─ Guide ") || !strings.Contains(v, "at any time with F12.") {
		t.Fatalf("no guide on the first start:\n%s", v)
	}
	typeKeys(m, "esc", "l", "?", "esc")
	m.Update(tea.KeyPressMsg{Code: tea.KeyF12})
	if v := text.Strip(m.View().Content); !strings.Contains(v, "● ● ○ ● ○ ○") || !strings.Contains(v, "Every command of the tab") {
		t.Fatalf("after next tab, keys and the guide:\n%s", v)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF12})
	if m.stack.Len() != 0 {
		t.Fatal("its key did not close it")
	}
	if m = start(); m.stack.Len() != 0 {
		t.Fatal("the guide opened on the second start")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF12})
	if v := text.Strip(m.View().Content); !strings.Contains(v, "● ● ○ ● ○ ○") {
		t.Fatalf("progress not kept:\n%s", v)
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func typeKeys(m *model, keys ...string) {
	for _, k := range keys {
		m.Update(key(k))
	}
}

func TestTabsFit(t *testing.T) {
	for _, size := range [][2]int{{160, 50}, {80, 24}, {minW, minH}} {
		m := testModel(t, size[0], size[1])
		for i, tb := range m.tabs {
			m.active = i
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Errorf("%s at %dx%d: %d lines", tb.name(), size[0], size[1], len(lines))
			}
			for n, l := range lines {
				if w := text.Width(l); w > size[0] {
					t.Errorf("%s at %dx%d: line %d is %d wide", tb.name(), size[0], size[1], n, w)
				}
			}
		}
	}
}

func TestTooSmall(t *testing.T) {
	m := testModel(t, minW-1, minH)
	if !strings.Contains(m.View().Content, "too small") {
		t.Error("no too small screen")
	}
}

func TestInputTakesKeysWhileEditing(t *testing.T) {
	m := testModel(t, 80, 24)
	typeKeys(m, "3", "enter", "q", "u", "i", "t", "esc")
	in := m.tabs[2].(*inputTab)
	if got := in.lines[0].String(); got != "quit" {
		t.Errorf("line %q", got)
	}
	if in.editing {
		t.Error("still editing after esc")
	}
}

func TestInputLimit(t *testing.T) {
	m := testModel(t, 80, 24)
	m.set("input.limit", "3")
	typeKeys(m, "3", "down", "enter", "a", "b", "c", "d", "esc")
	if got := m.tabs[2].(*inputTab).lines[1].String(); got != "abc" {
		t.Errorf("limited line %q", got)
	}
}

func TestFuzzy(t *testing.T) {
	m := testModel(t, 80, 24)
	typeKeys(m, "5", "/", "u", "i", ".", "s", "e", "l")
	f := m.tabs[4].(*fuzzyTab)
	if len(f.shown) == 0 || !strings.HasPrefix(f.items[f.shown[0]], "ui.sel") {
		t.Fatalf("first match %v", f.shown)
	}
}

func TestThemeNext(t *testing.T) {
	m := testModel(t, 80, 24)
	before := m.store.Get().Theme
	typeKeys(m, "1", "t")
	if m.store.Get().Theme == before {
		t.Error("theme unchanged")
	}
}

func TestLayoutWider(t *testing.T) {
	m := testModel(t, 80, 24)
	typeKeys(m, "7", "]")
	if got := m.store.Get().Layout.Split; got != 45 {
		t.Errorf("split %d", got)
	}
}

func TestResetConfirm(t *testing.T) {
	m := testModel(t, 80, 24)
	m.set("list.rows", "7")
	typeKeys(m, "6", "r")
	if m.stack.Len() != 1 {
		t.Fatal("no dialog")
	}
	typeKeys(m, "enter")
	if m.stack.Len() != 0 {
		t.Error("dialog still open")
	}
	if got := m.store.Get().List.Rows; got != defaults().List.Rows {
		t.Errorf("rows %d after reset", got)
	}
}

func TestListCursor(t *testing.T) {
	m := testModel(t, 80, 24)
	typeKeys(m, "4", "j", "j", "G")
	l := m.tabs[3].(*listTab)
	if l.c.At != defaults().List.Rows-1 {
		t.Errorf("at %d", l.c.At)
	}
}

func TestImageModes(t *testing.T) {
	m := testModel(t, 100, 30)
	im := m.images
	view := func() string { return im.view(m.width, m.bodyHeight()) }
	if im.resolved != "blocks" || !strings.Contains(view(), "▀") {
		t.Fatalf("auto without kitty: %s", im.resolved)
	}
	for _, want := range []string{"kitty", "blocks", "none", "auto"} {
		typeKeys(m, "0", "m")
		if im.mode != want {
			t.Fatalf("mode %q, want %q", im.mode, want)
		}
	}
	m.set("images.mode", "none")
	m.Update(nil)
	if strings.ContainsAny(view(), "▀▄") {
		t.Error("none drew blocks")
	}
}
