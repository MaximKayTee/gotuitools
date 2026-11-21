package main

import (
	"bytes"
	"image/png"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/guide"
)

// steps are the demo's own quick start.
func steps() []guide.Step {
	var pic bytes.Buffer
	png.Encode(&pic, gradient(imagePx))
	return []guide.Step{
		guide.Welcome,
		{Command: "tab.next", Title: "Tabs", Text: "One tab per component. {tab.prev} goes back; a number goes to its tab."},
		{Command: "palette", Text: "Every command of the tab, found by typing."},
		{Command: "keys", Text: "Every key by group. A key changed in the settings shows here and in this guide."},
		{Command: "settings", Text: "One file, edited here. {modal.reset} on the Modal tab puts every setting back."},
		{Command: "tab.images", Title: "Pictures", Text: "{images.mode} changes how they are drawn: kitty graphics, half blocks, none.", Picture: pic.Bytes()},
	}
}

// newGuide keeps its progress beside the settings and is told of every
// command run.
func (m *model) newGuide() {
	m.guide = guide.Open(filepath.Join(filepath.Dir(m.store.Path()), "guide.toml"), steps())
	m.guide.Queue, m.guide.Cell = m.images.q, m.opts.cell
	m.reg.Ran = m.guide.Ran
}

func (m *model) openGuide() tea.Cmd {
	return m.stack.Open(m.guide.Modal(m.th, m.reg, m.keys, m.mode()))
}
