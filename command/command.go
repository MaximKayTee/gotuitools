// Package command is the list of things a program can be told to do,
// and the keys that do them.
//
// Keys, the palette, the help screen and the hints all read the same
// Registry: nothing can be bound or shown that is not registered.
package command

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// A Command is one thing the program can be told to do.
type Command struct {
	Name  string // "note.new"
	Title string // shown in the palette and help: "New note"
	Group string // help screen section
	// Key is the keys it starts out on: "up k" is either key, "g,g" is
	// g then g; "" for none. "ctrl+," is the key ctrl+comma.
	Key string
	// Hidden keeps it out of the palette.
	Hidden bool
	// Available reports whether it can run now; nil always. Unavailable
	// commands are left out of the palette and do not run.
	Available func() bool
	Run       func(args []string) tea.Cmd
}

// Can reports whether c can run now.
func (c Command) Can() bool { return c.Available == nil || c.Available() }

// Label is Title, else Name.
func (c Command) Label() string {
	if c.Title != "" {
		return c.Title
	}
	return c.Name
}

// ErrorMsg is a command's failure.
type ErrorMsg struct{ Err error }

func (e ErrorMsg) Error() string { return e.Err.Error() }

// Do adapts a function returning an error: a non-nil error comes back
// as ErrorMsg.
func Do(f func(args []string) error) func(args []string) tea.Cmd {
	return func(args []string) tea.Cmd {
		if err := f(args); err != nil {
			return func() tea.Msg { return ErrorMsg{err} }
		}
		return nil
	}
}

// A Registry holds commands by name, in the order added.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]int
	all    []Command
}

// Add registers commands. Names are unique; each needs a Run.
func (r *Registry) Add(cs ...Command) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range cs {
		if c.Name == "" || c.Run == nil {
			return fmt.Errorf("command %q: needs a name and something to run", c.Name)
		}
		if _, taken := r.byName[c.Name]; taken {
			return fmt.Errorf("command %q is already registered", c.Name)
		}
		if r.byName == nil {
			r.byName = map[string]int{}
		}
		r.byName[c.Name] = len(r.all)
		r.all = append(r.all, c)
	}
	return nil
}

// MustAdd is Add, panicking on error.
func (r *Registry) MustAdd(cs ...Command) {
	if err := r.Add(cs...); err != nil {
		panic(err)
	}
}

// Remove drops the named command.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, ok := r.byName[name]
	if !ok {
		return
	}
	r.all = slices.Delete(r.all, i, i+1)
	delete(r.byName, name)
	for j := i; j < len(r.all); j++ {
		r.byName[r.all[j].Name] = j
	}
}

// Get returns the named command.
func (r *Registry) Get(name string) (Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.byName[name]
	if !ok {
		return Command{}, false
	}
	return r.all[i], true
}

// Run runs a command line: its name, then arguments separated by spaces.
func (r *Registry) Run(line string) tea.Cmd {
	words := strings.Fields(line)
	if len(words) == 0 {
		return nil
	}
	c, ok := r.Get(words[0])
	if !ok {
		return func() tea.Msg { return ErrorMsg{fmt.Errorf("no command %q", words[0])} }
	}
	if !c.Can() {
		return nil
	}
	return c.Run(words[1:])
}

// All returns every command in the order added.
func (r *Registry) All() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.all)
}
