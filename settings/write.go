package settings

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/MaximKayTee/gotuitools/text"
)

func (s *Store[T]) write() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked()
}

// Render is the file as it would be written now.
func (s *Store[T]) Render() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.render()
}

func (s *Store[T]) render() ([]byte, error) {
	var b bytes.Buffer
	if s.header != "" {
		for _, l := range strings.Split(s.header, "\n") {
			b.WriteString(strings.TrimRight("# "+l, " ") + "\n")
		}
		b.WriteString("\n")
	}
	root := reflect.ValueOf(&s.cur).Elem()
	byGroup := map[string][]*field{}
	for _, f := range s.sc.fields {
		if s.base != nil && !s.set[f.Key] {
			continue
		}
		g := f.Key[:max(strings.LastIndexByte(f.Key, '.'), 0)]
		byGroup[g] = append(byGroup[g], f)
	}
	inline := map[string][]other{}
	var tables []other
	for _, o := range s.sc.others {
		v := root.FieldByIndex(o.path)
		if o.omitempty && empty(v) || v.Kind() == reflect.Pointer && v.IsNil() {
			continue
		}
		if isTable(v.Type()) && !(v.Kind() == reflect.Slice && v.Len() == 0) {
			tables = append(tables, o)
			continue
		}
		g := strings.Join(o.key[:len(o.key)-1], ".")
		inline[g] = append(inline[g], o)
	}
	first := true
	for _, g := range s.sc.groups {
		flds, ins := byGroup[g], inline[g]
		if len(flds) == 0 && len(ins) == 0 {
			continue
		}
		if g != "" {
			if !first {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "[%s]\n", tableKey(strings.Split(g, ".")))
		}
		for i, f := range flds {
			if i > 0 {
				b.WriteString("\n")
			}
			s.writeField(&b, f, f.at(root))
		}
		for _, o := range ins {
			v := root.FieldByIndex(o.path)
			if v.Kind() == reflect.Slice && v.Len() == 0 {
				// written, so a cleared list is not its default again
				b.WriteString(tableKey(o.key[len(o.key)-1:]) + " = []\n")
				continue
			}
			line, err := encodeInline(o.key[len(o.key)-1], v.Interface())
			if err != nil {
				return nil, err
			}
			b.WriteString(line)
		}
		first = false
	}
	for _, o := range tables {
		v := root.FieldByIndex(o.path)
		if v.Kind() == reflect.Map && v.Len() == 0 {
			// an empty table, so a cleared map is not its default again
			fmt.Fprintf(&b, "\n[%s]\n", tableKey(o.key))
			continue
		}
		var nested any = v.Interface()
		for i := len(o.key) - 1; i >= 0; i-- {
			nested = map[string]any{o.key[i]: nested}
		}
		var buf bytes.Buffer
		enc := toml.NewEncoder(&buf)
		enc.Indent = ""
		if err := enc.Encode(nested); err != nil {
			return nil, fmt.Errorf("settings: %s: %w", strings.Join(o.key, "."), err)
		}
		// the parents' headers: written already or implied by the child's
		parents := map[string]bool{}
		for i := 1; i < len(o.key); i++ {
			parents["["+tableKey(o.key[:i])+"]"] = true
		}
		b.WriteString("\n")
		for _, l := range strings.SplitAfter(strings.Trim(buf.String(), "\n")+"\n", "\n") {
			if !parents[strings.TrimSpace(l)] {
				b.WriteString(l)
			}
		}
	}
	s.writeDefaultKeys(&b)
	return b.Bytes(), nil
}

func defaultKeysLine(t *keyTable) string {
	return fmt.Sprintf("# Default keys, for [%s]. \"\" frees a key.", tableKey(t.path))
}

// writeDefaultKeys lists the default bindings of a Keys table, as
// comments.
func (s *Store[T]) writeDefaultKeys(b *bytes.Buffer) {
	t := s.keys
	if t == nil {
		return
	}
	cmds, modes := t.commands()
	def, err := t.bound(modes, nil)
	if err != nil || len(cmds) == 0 {
		return
	}
	b.WriteString("\n" + defaultKeysLine(t) + "\n")
	for _, c := range cmds {
		k := def[c.modes[0]]
		for _, keys := range k.All(c.modes[0], c.Name) {
			if t.byCommand {
				fmt.Fprintf(b, "# %s = %s\n", tableKey([]string{c.Name}), tomlString(shown(k, c.modes[0], c.Name)))
				break
			}
			fmt.Fprintf(b, "# %s = %s\n", tableKey([]string{written(keys)}), tomlString(c.Name))
		}
	}
}

// empty reports a nil pointer or an empty map, slice or string.
func empty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	case reflect.Map, reflect.Slice, reflect.String, reflect.Array:
		return v.Len() == 0
	}
	return false
}

func (s *Store[T]) writeField(b *bytes.Buffer, f *field, v reflect.Value) {
	comment := f.Label
	if f.Help != "" {
		comment += ". " + f.Help
	}
	for _, l := range text.Wrap(comment, 76) {
		b.WriteString("# " + l + "\n")
	}
	if opts := s.optionsLocked(f); len(opts) > 0 && f.Kind == Enum {
		lead := "One of: "
		if f.Free {
			lead = "For example: "
		}
		b.WriteString("# " + lead + strings.Join(opts, ", ") + "\n")
	}
	if r := f.Range(); r != "" {
		b.WriteString("# " + strings.ToUpper(r[:1]) + r[1:] + "\n")
	}
	if f.Restart {
		b.WriteString("# Restart required\n")
	}
	name := tableKey([]string{f.Key[strings.LastIndexByte(f.Key, '.')+1:]})
	if v.Kind() == reflect.Pointer && v.IsNil() {
		b.WriteString("# " + name + " = (unset)\n")
		return
	}
	b.WriteString(name + " = " + f.tomlValue(v) + "\n")
}

// tomlValue is v written as TOML.
func (f *field) tomlValue(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	switch f.Kind {
	case Bool, Int:
		return f.show(v)
	case Float:
		s := strconv.FormatFloat(v.Float(), 'f', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s
	case Duration:
		return tomlString(text.Duration(time.Duration(v.Int())))
	case ByteSize:
		return tomlString(text.ExactBytes(v.Int()))
	case List:
		items := v.Interface()
		var parts []string
		for i := range reflect.ValueOf(items).Len() {
			parts = append(parts, tomlString(reflect.ValueOf(items).Index(i).String()))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return tomlString(v.String())
}

// tomlString is s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tableKey writes a dotted key, quoting parts that need it.
func tableKey(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		bare := p != ""
		for _, r := range p {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				bare = false
			}
		}
		if bare {
			out[i] = p
		} else {
			out[i] = tomlString(p)
		}
	}
	return strings.Join(out, ".")
}

// isTable reports values TOML writes as tables or arrays of tables.
func isTable(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Map, reflect.Struct:
		return true
	case reflect.Slice, reflect.Array:
		e := t.Elem()
		for e.Kind() == reflect.Pointer {
			e = e.Elem()
		}
		return e.Kind() == reflect.Struct || e.Kind() == reflect.Map
	}
	return false
}

func encodeInline(key string, v any) (string, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(map[string]any{key: v}); err != nil {
		return "", fmt.Errorf("settings: %s: %w", key, err)
	}
	return buf.String(), nil
}

// writeLocked renders and replaces the file atomically: a temp file in
// the same directory, the old mode, fsync, rename over the target of a
// symlink.
func (s *Store[T]) writeLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := s.render()
	if err != nil {
		return err
	}
	target := s.path
	if t, err := filepath.EvalSymlinks(s.path); err == nil {
		target = t
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(target); err == nil {
		mode = st.Mode().Perm()
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(b)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	var st fs.FileInfo
	if err == nil {
		st, err = tmp.Stat()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, target)
	}
	if err != nil {
		os.Remove(name)
		return fmt.Errorf("settings file: %w", err)
	}
	s.stamp = stamp{st.ModTime(), st.Size()}
	s.clean, s.present = true, nil
	return nil
}

// Write writes the file now: after Header or Options change what it says.
func (s *Store[T]) Write() error { return s.write() }
