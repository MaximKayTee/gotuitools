package settings

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Source is settings as the settings screen sees them: a local Store
// or a remote one. Values are text in the form Field.Value shows.
type Source interface {
	Fields() []Field
	Search(q string) []Field
	Set(key, text string) (Field, error)
	Reset(key string) (Field, error)
	// Path is the file to open in an editor; "" none.
	Path() string
	// Reload reads outside edits; changed reports any value differs.
	Reload() (changed bool, err error)
}

var _ Source = (*Store[struct{}])(nil)

// ReloadedMsg follows a Poll that found the file changed. Err is a
// load problem; the store keeps working.
type ReloadedMsg struct {
	Source Source
	Err    error
}

// Poll checks src's file every interval (0 is 2s) and sends
// ReloadedMsg when its values changed or it could not be read. Call it
// again on each pollMsg it returns through Update: see Watch.
func Poll(src Source, every time.Duration) tea.Cmd {
	if every <= 0 {
		every = 2 * time.Second
	}
	return tea.Tick(every, func(time.Time) tea.Msg {
		changed, err := src.Reload()
		return PollMsg{Source: src, Every: every, Changed: changed, Err: err}
	})
}

// PollMsg is a Poll tick. Pass it to Watch.
type PollMsg struct {
	Source  Source
	Every   time.Duration
	Changed bool
	Err     error
}

// Watch handles a PollMsg: the next Poll, and ReloadedMsg when it
// found something. Other messages return nil, false.
func Watch(msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(PollMsg)
	if !ok {
		return nil, false
	}
	next := Poll(m.Source, m.Every)
	if m.Changed || m.Err != nil {
		return tea.Batch(next, func() tea.Msg { return ReloadedMsg{m.Source, m.Err} }), true
	}
	return next, true
}
