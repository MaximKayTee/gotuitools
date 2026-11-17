package command

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Keys says which keys run which command line, mode by mode. A binding
// is one key or several pressed in turn, written with spaces between:
// "ctrl+p", "g g". The command line may carry arguments: "tab.go 3".
type Keys struct {
	modes map[string]map[string]string // mode → keys → command line
}

// Bind makes keys run a command line in a mode; an empty command frees
// them. Keys that are the start of a longer binding, or continue a
// shorter one, are refused: nothing ever waits to see what comes next.
func (k *Keys) Bind(mode, keys, command string) error {
	keys = strings.Join(strings.Fields(keys), " ")
	if keys == "" {
		return fmt.Errorf("bind %q: no keys", command)
	}
	bound := k.modes[mode]
	if command == "" {
		delete(bound, keys)
		return nil
	}
	for other, has := range bound {
		if other != keys && (strings.HasPrefix(other, keys+" ") || strings.HasPrefix(keys, other+" ")) {
			return fmt.Errorf("bind %q to %s: %q is already bound to %s", keys, command, other, has)
		}
	}
	if k.modes == nil {
		k.modes = map[string]map[string]string{}
	}
	if bound == nil {
		bound = map[string]string{}
		k.modes[mode] = bound
	}
	bound[keys] = command
	return nil
}

// Unbind frees every binding of a command in a mode.
func (k *Keys) Unbind(mode, command string) {
	for keys, has := range k.modes[mode] {
		if has == command {
			delete(k.modes[mode], keys)
		}
	}
}

// Match takes the keys pressed so far in a mode. It returns the command
// line they run, or reports that they are the start of a longer binding.
func (k *Keys) Match(mode string, pressed []string) (command string, more bool) {
	keys := strings.Join(pressed, " ")
	bound := k.modes[mode]
	if command, ok := bound[keys]; ok {
		return command, false
	}
	for other := range bound {
		if strings.HasPrefix(other, keys+" ") {
			return "", true
		}
	}
	return "", false
}

// For returns the keys bound to a command line in a mode, "" if none.
// Of several bindings it gives the shortest.
func (k *Keys) For(mode, command string) string {
	var found string
	for keys, has := range k.modes[mode] {
		shorter := len(keys) < len(found) || len(keys) == len(found) && keys < found
		if has == command && (found == "" || shorter) {
			found = keys
		}
	}
	return found
}

// All returns every binding of a command in a mode, shortest first.
func (k *Keys) All(mode, command string) []string {
	var out []string
	for keys, has := range k.modes[mode] {
		if has == command {
			out = append(out, keys)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if len(a) != len(b) {
			return len(a) - len(b)
		}
		return strings.Compare(a, b)
	})
	return out
}

// Bindings returns a mode's bindings: keys → command line.
func (k *Keys) Bindings(mode string) map[string]string { return maps.Clone(k.modes[mode]) }

// inTurn finds the commas between keys pressed in turn, as in "ctrl+b,v".
// A comma after a + is a key: "ctrl+,".
var inTurn = regexp.MustCompile(`([^+]),`)

// Sequence turns a Command.Key entry into a binding: "g,g" is "g g".
func Sequence(key string) string { return inTurn.ReplaceAllString(key, "$1 ") }

// Defaults binds every command's Key in mode.
func (k *Keys) Defaults(mode string, cmds []Command) error {
	for _, c := range cmds {
		for _, key := range strings.Fields(c.Key) {
			if err := k.Bind(mode, Sequence(key), c.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Own lays a user's bindings over a mode: keys → command line, "" to
// free the keys. Keys may be written "g,g" or "g g".
func (k *Keys) Own(mode string, own map[string]string) error {
	for _, keys := range slices.Sorted(maps.Keys(own)) {
		seq := Sequence(keys)
		if err := k.Bind(mode, seq, ""); err != nil {
			return err
		}
		if own[keys] == "" {
			continue
		}
		// a binding that would collide with a default prefix replaces it
		for other := range k.modes[mode] {
			if strings.HasPrefix(other, seq+" ") || strings.HasPrefix(seq, other+" ") {
				delete(k.modes[mode], other)
			}
		}
		if err := k.Bind(mode, seq, own[keys]); err != nil {
			return err
		}
	}
	return nil
}

// OwnByCommand lays a user's bindings over a mode written command →
// keys ("up k", "g,g"): the command's default keys are replaced.
func (k *Keys) OwnByCommand(mode string, own map[string]string) error {
	for _, command := range slices.Sorted(maps.Keys(own)) {
		k.Unbind(mode, command)
		for _, key := range strings.Fields(own[command]) {
			if err := k.Own(mode, map[string]string{key: command}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load binds cmds' default keys in mode, then own (keys → command
// line) over them: the entries naming one of cmds, or "" to free keys.
// Nothing changes if any binding is refused.
func (k *Keys) Load(mode string, cmds []Command, own map[string]string) error {
	var fresh Keys
	if err := fresh.Defaults(mode, cmds); err != nil {
		return err
	}
	names := map[string]bool{}
	for _, c := range cmds {
		names[c.Name] = true
	}
	mine := map[string]string{}
	for keys, line := range own {
		if f := strings.Fields(line); len(f) == 0 || names[f[0]] {
			mine[keys] = line
		}
	}
	if err := fresh.Own(mode, mine); err != nil {
		return err
	}
	if k.modes == nil {
		k.modes = map[string]map[string]string{}
	}
	k.modes[mode] = fresh.modes[mode]
	return nil
}

// Unknown are the command names in own (keys → command line) that reg
// does not have, sorted.
func Unknown(reg *Registry, own map[string]string) []string {
	var out []string
	for _, line := range own {
		if f := strings.Fields(line); len(f) > 0 {
			if _, ok := reg.Get(f[0]); !ok && !slices.Contains(out, f[0]) {
				out = append(out, f[0])
			}
		}
	}
	slices.Sort(out)
	return out
}

// Dispatcher turns key presses into commands: it keeps the keys of an
// unfinished binding.
type Dispatcher struct {
	Keys     *Keys
	Registry *Registry
	pressed  []string
}

// Pending is the keys pressed of an unfinished binding: "g".
func (d *Dispatcher) Pending() string { return strings.Join(d.pressed, " ") }

// Reset forgets an unfinished binding.
func (d *Dispatcher) Reset() { d.pressed = nil }

// Key runs the command a key completes in mode. handled is false for a
// first key bound to nothing, which the caller may use itself.
func (d *Dispatcher) Key(mode string, key tea.KeyPressMsg) (cmd tea.Cmd, handled bool) {
	d.pressed = append(d.pressed, key.String())
	line, more := d.Keys.Match(mode, d.pressed)
	if more {
		return nil, true
	}
	alone := len(d.pressed) == 1
	d.pressed = nil
	if line == "" {
		return nil, !alone
	}
	return d.Registry.Run(line), true
}
