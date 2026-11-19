package main

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/layout"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/text"
)

type layoutTab struct {
	m     *model
	zones layout.Zones
	drag  int // split while dragging; 0 none
}

func (t *layoutTab) name() string  { return "layout" }
func (t *layoutTab) title() string { return "Layout" }

func (t *layoutTab) status() string {
	return "Split " + text.Percent(float64(t.split())/100) + ", layout.split"
}

func (t *layoutTab) commands() []command.Command {
	return []command.Command{
		{Name: "layout.narrower", Title: "Narrower", Key: "[", Run: do(func() { t.setSplit(t.split() - 5) })},
		{Name: "layout.wider", Title: "Wider", Key: "]", Run: do(func() { t.setSplit(t.split() + 5) })},
	}
}

func (t *layoutTab) split() int {
	if t.drag > 0 {
		return t.drag
	}
	return t.m.store.Get().Layout.Split
}

func (t *layoutTab) setSplit(pct int) {
	t.m.set("layout.split", strconv.Itoa(layout.Clamp(pct, 10, 90)))
}

func (t *layoutTab) update(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
		if t.m.tabs[t.m.active] != tab(t) {
			return nil
		}
		cmd, _ := t.zones.Update(msg)
		return cmd
	}
	return nil
}

func (t *layoutTab) view(w, h int) string {
	th := t.m.th
	r := layout.Rect{W: w, H: h}
	left, rest := r.Left(layout.Share(w, t.split(), 10, w-11), 0)
	div, right := rest.Left(1, 0)
	top, bottom := right.Top(right.H/2, 0)

	t.zones.Reset()
	t.zones.Add(layout.Divider(div, false, func(x int) {
		if w > 0 {
			t.drag = layout.Clamp(x*100/w, 10, 90)
		}
	}, func() {
		pct := t.drag
		t.drag = 0
		if pct > 0 {
			t.setSplit(pct)
		}
	}))

	pane := func(title string, p layout.Rect) []string {
		body := []string{
			" x " + strconv.Itoa(p.X) + "  y " + strconv.Itoa(p.Y),
			" w " + strconv.Itoa(p.W) + "  h " + strconv.Itoa(p.H),
		}
		body = text.Block(body, max(p.W-2, 0), max(p.H-2, 0))
		return modal.Frame(th, modal.Box{Title: title}, body, p.W)
	}
	l := pane("Left", left)
	rt := append(pane("Top", top), pane("Bottom", bottom)...)
	rule := th.Render("ui.window", "│")
	if t.drag > 0 {
		rule = th.Render("ui.window.focus", "┃")
	}
	lines := make([]string, h)
	for y := range lines {
		lines[y] = text.Pad(at(l, y), left.W) + rule + text.Pad(at(rt, y), right.W)
	}
	return strings.Join(lines, "\n")
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}
