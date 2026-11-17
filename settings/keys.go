package settings

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/MaximKayTee/gotuitools/command"
)

// Keys is the Kind of a command's keys: "ctrl+p :", a sequence "g,g".
const Keys Kind = "keys"

// KeyMode is the commands of one command.Keys mode that a [keys] table
// binds.
type KeyMode struct {
	Mode     string
	Commands []command.Command
}

// keyTable is a [keys] table shown as one setting per command.
type keyTable struct {
	path      []string // "keys", or "keys.normal" in a table of modes
	modes     func() []KeyMode
	byCommand bool // command → keys, not keys → command line
}

const keysHelp = "Space between keys; g,g is g then g"

// Keys shows the table at path, keys → command line as command.Keys.Load
// reads it, as one setting per command of modes: its keys. path is a
// field that is not a setting, map[string]string or a pointer to one, or
// a mode's table in such a map of maps: "keys.normal". Hidden commands
// are shown too: they are out of the palette, not unbindable. modes runs
// under the store's lock: it must not call the store.
func (s *Store[T]) Keys(path string, modes func() []KeyMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = &keyTable{path: strings.Split(path, "."), modes: modes}
}

// KeysByCommand is Keys for a table of command → keys, as
// command.Keys.OwnByCommand reads it.
func (s *Store[T]) KeysByCommand(path string, modes func() []KeyMode) {
	s.Keys(path, modes)
	s.keys.byCommand = true
}

// keyCmd is a command shown, and the modes it is in.
type keyCmd struct {
	command.Command
	modes []string
}

func (t *keyTable) commands() ([]keyCmd, []KeyMode) {
	modes := t.modes()
	var out []keyCmd
	at := map[string]int{}
	for _, m := range modes {
		for _, c := range m.Commands {
			if c.Name == "" {
				continue
			}
			if i, ok := at[c.Name]; ok {
				out[i].modes = append(out[i].modes, m.Mode)
				continue
			}
			at[c.Name] = len(out)
			out = append(out, keyCmd{c, []string{m.Mode}})
		}
	}
	return out, modes
}

func (t *keyTable) fieldKey(name string) string { return strings.Join(t.path, ".") + "." + name }

// written is a binding as a table writes it: "g,g".
func written(seq string) string { return strings.ReplaceAll(seq, " ", ",") }

// bound are a table's bindings, mode by mode.
func (t *keyTable) bound(modes []KeyMode, own map[string]string) (map[string]*command.Keys, error) {
	out := map[string]*command.Keys{}
	for _, m := range modes {
		k := &command.Keys{}
		var err error
		if t.byCommand {
			if err = k.Load(m.Mode, m.Commands, nil); err == nil {
				err = k.OwnByCommand(m.Mode, filterNames(own, m.Commands))
			}
		} else {
			err = k.Load(m.Mode, m.Commands, own)
		}
		if err != nil {
			return nil, err
		}
		out[m.Mode] = k
	}
	return out, nil
}

func filterNames(own map[string]string, cmds []command.Command) map[string]string {
	out := map[string]string{}
	for _, c := range cmds {
		if v, ok := own[c.Name]; ok {
			out[c.Name] = v
		}
	}
	return out
}

// shown is a command's keys in its first mode, written as a table does.
func shown(k *command.Keys, mode, name string) string {
	all := k.All(mode, name)
	for i, b := range all {
		all[i] = written(b)
	}
	return strings.Join(all, " ")
}

// keyFieldsLocked are the table's settings.
func (s *Store[T]) keyFieldsLocked(cur T) []Field {
	t := s.keys
	if t == nil {
		return nil
	}
	cmds, modes := t.commands()
	own := s.ownLocked(cur)
	now, err := t.bound(modes, own)
	if err != nil {
		now, _ = t.bound(modes, nil)
	}
	def, _ := t.bound(modes, nil)
	out := make([]Field, 0, len(cmds))
	for _, c := range cmds {
		f := Field{Key: t.fieldKey(c.Name), Group: "Keys", Label: c.Label(), Help: keysHelp, Kind: Keys}
		if c.Group != "" {
			f.Group = "Keys › " + c.Group
		}
		f.Value = shown(now[c.modes[0]], c.modes[0], c.Name)
		f.Default = shown(def[c.modes[0]], c.modes[0], c.Name)
		f.IsDefault = f.Value == f.Default
		out = append(out, f)
	}
	return out
}

// ownLocked is a copy of the table in v.
func (s *Store[T]) ownLocked(v T) map[string]string {
	out := map[string]string{}
	cur := reflect.ValueOf(&v).Elem()
	o, ok := s.keyOther()
	if !ok {
		return out
	}
	m := cur.FieldByIndex(o.path)
	for _, k := range append([]string{""}, s.keys.path[len(o.key):]...) {
		if m.Kind() == reflect.Pointer {
			if m.IsNil() {
				return out
			}
			m = m.Elem()
		}
		if k == "" {
			continue
		}
		if m.Kind() != reflect.Map || m.IsNil() {
			return out
		}
		if m = m.MapIndex(reflect.ValueOf(k)); !m.IsValid() {
			return out
		}
	}
	if mm, ok := m.Interface().(map[string]string); ok {
		maps.Copy(out, mm)
	}
	return out
}

// keyOther is the field that holds the table.
func (s *Store[T]) keyOther() (other, bool) {
	for _, o := range s.sc.others {
		if len(o.key) <= len(s.keys.path) && slices.Equal(o.key, s.keys.path[:len(o.key)]) {
			return o, true
		}
	}
	return other{}, false
}

// putKeys sets the table in v to own: fresh maps on the way, so values
// handed out before keep theirs.
func (s *Store[T]) putKeys(v *T, own map[string]string) error {
	o, ok := s.keyOther()
	if !ok {
		return fmt.Errorf("settings: no field for %s", strings.Join(s.keys.path, "."))
	}
	field := reflect.ValueOf(v).Elem().FieldByIndex(o.path)
	put, err := putIn(field.Type(), field, s.keys.path[len(o.key):], own)
	if err != nil {
		return fmt.Errorf("settings: %s: %w", strings.Join(s.keys.path, "."), err)
	}
	field.Set(put)
	return nil
}

// putIn is old, of type t, with the map at path set to own.
func putIn(t reflect.Type, old reflect.Value, path []string, own map[string]string) (reflect.Value, error) {
	if t.Kind() == reflect.Pointer {
		var inner reflect.Value
		if old.IsValid() && !old.IsNil() {
			inner = old.Elem()
		}
		v, err := putIn(t.Elem(), inner, path, own)
		if err != nil {
			return v, err
		}
		p := reflect.New(t.Elem())
		p.Elem().Set(v)
		return p, nil
	}
	if t.Kind() != reflect.Map || t.Key().Kind() != reflect.String {
		return reflect.Value{}, fmt.Errorf("%s is not a map", t)
	}
	if len(path) == 0 {
		if t != reflect.TypeFor[map[string]string]() {
			return reflect.Value{}, fmt.Errorf("%s is not a map[string]string", t)
		}
		return reflect.ValueOf(maps.Clone(own)), nil
	}
	m := reflect.MakeMap(t)
	if old.IsValid() && !old.IsNil() {
		for it := old.MapRange(); it.Next(); {
			m.SetMapIndex(it.Key(), it.Value())
		}
	}
	var inner reflect.Value
	if old.IsValid() && !old.IsNil() {
		inner = old.MapIndex(reflect.ValueOf(path[0]))
	}
	v, err := putIn(t.Elem(), inner, path[1:], own)
	if err != nil {
		return v, err
	}
	m.SetMapIndex(reflect.ValueOf(path[0]), v)
	return m, nil
}

// keyCmdLocked is the command a key field is for.
func (s *Store[T]) keyCmdLocked(key string) (keyCmd, []KeyMode, bool) {
	if s.keys == nil {
		return keyCmd{}, nil, false
	}
	cmds, modes := s.keys.commands()
	for _, c := range cmds {
		if s.keys.fieldKey(c.Name) == key {
			return c, modes, true
		}
	}
	return keyCmd{}, nil, false
}

// setKeysLocked binds text's keys to c and nothing else, or with reset
// gives c its default keys back.
func (s *Store[T]) setKeysLocked(c keyCmd, modes []KeyMode, text string, reset bool) (Field, error) {
	t := s.keys
	field := func() Field {
		for _, f := range s.keyFieldsLocked(s.viewLocked()) {
			if f.Key == t.fieldKey(c.Name) {
				return f
			}
		}
		return Field{}
	}
	if err := s.syncLocked(); err != nil {
		return field(), err
	}
	own := s.ownLocked(s.cur)
	before, err := t.bound(modes, own)
	if err != nil {
		before, _ = t.bound(modes, nil)
	}
	var want []string // "g g"
	for _, k := range strings.Fields(text) {
		want = append(want, command.Sequence(k))
	}
	next := maps.Clone(own)
	if t.byCommand {
		delete(next, c.Name)
		if !reset {
			var parts []string
			for _, w := range want {
				parts = append(parts, written(w))
			}
			next[c.Name] = strings.Join(parts, " ")
		}
	} else {
		var defaults []string
		for _, k := range strings.Fields(c.Key) {
			defaults = append(defaults, command.Sequence(k))
		}
		for k, line := range own {
			seq := command.Sequence(k)
			if line == c.Name || line == "" && slices.Contains(defaults, seq) {
				delete(next, k)
			}
		}
		if !reset {
			for _, d := range defaults {
				if !slices.Contains(want, d) {
					next[written(d)] = ""
				}
			}
			for _, w := range want {
				for k := range next {
					if command.Sequence(k) == w {
						delete(next, k)
					}
				}
				if !slices.Contains(defaults, w) {
					next[written(w)] = c.Name
				}
			}
		}
	}
	if !reset {
		for _, w := range want {
			for _, m := range c.modes {
				if other, _ := before[m].Match(m, strings.Fields(w)); other != "" && other != c.Name {
					return field(), fmt.Errorf("%s is bound to %s", written(w), s.labelOf(modes, other))
				}
			}
		}
	}
	after, err := t.bound(modes, next)
	if err != nil {
		return field(), err
	}
	// a binding may not take another command's keys away
	for _, m := range modes {
		for keys, line := range before[m.Mode].Bindings(m.Mode) {
			if line != c.Name && after[m.Mode].Bindings(m.Mode)[keys] != line {
				return field(), fmt.Errorf("%s would end %s's %s", text, s.labelOf(modes, line), written(keys))
			}
		}
	}
	prev := s.cur
	if err := s.putKeys(&s.cur, next); err != nil {
		return field(), err
	}
	if err := whole(s.viewLocked()); err != nil {
		s.cur = prev
		return field(), err
	}
	if err := s.writeLocked(); err != nil {
		s.cur = prev
		return field(), err
	}
	return field(), nil
}

func (s *Store[T]) labelOf(modes []KeyMode, line string) string {
	name, _, _ := strings.Cut(line, " ")
	for _, m := range modes {
		for _, c := range m.Commands {
			if c.Name == name {
				return c.Label()
			}
		}
	}
	return line
}
