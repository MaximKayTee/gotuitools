// Package settings keeps a program's settings in one TOML file holding
// every setting, defaults included, declared as a Go struct.
//
// # Declaring
//
// Nested structs are tables and groups. Tags on a setting:
//
//	toml     key (BurntSushi form)
//	label    name shown; default from the key: "trash_max_age" is "Trash max age"
//	help     one line shown under it and written above it in the file
//	enum     values a string takes: "layered,exploded"; choices is the same
//	options  name of a list given at run time with Store.Options: an enum,
//	         or with free:"true" suggestions for a string
//	min max  bounds; durations as "30s", sizes as "1MiB"
//	restart  "true": takes effect after a restart
//	secret   "true": shown masked
//	long     "true": multi-line text
//	settings "-": not a setting; kept and written back
//
// Settings are bool, ints, floats, string, time.Duration, Bytes and
// []string, or a pointer to one of the scalars for an optional value.
// Fields of other types (maps, slices of structs) are not settings:
// they are read, kept and written back after the settings.
//
// A struct with a Validate() error method is checked whole after each
// change; Store.Validate adds a check on one key.
//
// # The file
//
// Written whole on every change, and at Open or Fill when it lacks a
// setting and read without problems, atomically, mode kept, symlinks
// followed: a header, top-level settings, then one table per group,
// each setting under a comment of its label, help, options or bounds,
// and "restart required". Comments are regenerated. Unknown keys are
// reported at load and dropped at the next write; a bad value keeps the
// default (at open) or the previous value (at reload) and is reported.
//
// # Keys
//
// Store.Keys shows a [keys] table as one setting per command, Kind Keys,
// group "Keys": its keys, set and reset like any setting. The file lists
// the default keys as comments.
//
// # Layers
//
// Store.Over makes the file a layer over another, a project's over a
// person's: it holds only what is set in it.
package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/MaximKayTee/gotuitools/fuzzy"
)

// Store is a program's settings and their file.
type Store[T any] struct {
	mu       sync.Mutex
	path     string
	header   string
	sc       *schema
	def, cur T
	checks   map[string]func(any) error
	options  map[string]func() []string
	stamp    stamp

	keys *keyTable
	// clean: the file read without problems; present: what it holds
	clean   bool
	present map[string]bool

	// a layer: set are the keys in the file, the rest are base's
	base func() T
	from string
	set  map[string]bool
}

type stamp struct {
	mod  time.Time
	size int64
}

// Open reads path over def. A missing file is written with every
// setting at its default, after setup has run: Header, Options,
// Validate. A file without some settings, or without a table def has
// entries in, is written with them when it loads without problems. Path "" keeps the settings in memory only. The Store is usable when err is a *LoadError:
// the problems are reported, the bad values left at their defaults.
func Open[T any](path string, def T, setup ...func(*Store[T])) (*Store[T], error) {
	sc, err := parseSchema(reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	s := &Store[T]{path: path, sc: sc, def: def, cur: def,
		checks: map[string]func(any) error{}, options: map[string]func() []string{}}
	for _, f := range setup {
		f(s)
	}
	if s.base != nil {
		s.def, s.cur, s.set = s.base(), s.base(), map[string]bool{}
	}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if s.base != nil {
			return s, nil
		}
		return s, s.write()
	}
	if err != nil {
		return s, err
	}
	s.stampNow()
	cur, problems, present, err := s.decode(data, s.def)
	if err != nil {
		return s, &LoadError{Path: path, Problems: []Problem{{Err: err.Error()}}}
	}
	s.cur = cur
	if s.base != nil {
		s.set = present
		return s, s.loadErr(problems)
	}
	s.clean, s.present = len(problems) == 0, present
	if err := s.Fill(); err != nil {
		return s, err
	}
	return s, s.loadErr(problems)
}

// Fill writes the file when it read without problems and lacks a
// setting, a table def has entries in, or the default keys of a Keys
// table: so that every setting shows in it. Open fills; call it again
// after Keys.
func (s *Store[T]) Fill() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.clean || s.path == "" || s.base != nil {
		return nil
	}
	lacks := s.lacksLocked(s.present)
	if !lacks && s.keys != nil {
		data, err := os.ReadFile(s.path)
		if err != nil {
			return nil
		}
		lacks = !bytes.Contains(data, []byte(defaultKeysLine(s.keys)))
	}
	if !lacks {
		return nil
	}
	if err := s.syncLocked(); err != nil || !s.clean {
		return err
	}
	return s.writeLocked()
}

// lacksLocked reports a setting, or a table with entries, that the file
// does not have.
func (s *Store[T]) lacksLocked(present map[string]bool) bool {
	if present == nil { // written by the store
		return false
	}
	for _, f := range s.sc.fields {
		if !present[f.Key] {
			return true
		}
	}
	root := reflect.ValueOf(&s.cur).Elem()
	for _, o := range s.sc.others {
		if !present[strings.Join(o.key, ".")] && !empty(root.FieldByIndex(o.path)) {
			return true
		}
	}
	return false
}

// Over makes the file a layer over base, which from names ("System"):
// it holds only the settings set in it; the others show and take base's
// value. Reset unsets a setting. A missing file is not written until a
// setting is set. Call it in Open's setup.
func (s *Store[T]) Over(from string, base func() T) { s.from, s.base = from, base }

// viewLocked is the settings as they apply: for a layer, base's values
// under the ones set in the file.
func (s *Store[T]) viewLocked() T {
	if s.base == nil {
		return s.cur
	}
	out := s.base()
	dst, src := reflect.ValueOf(&out).Elem(), reflect.ValueOf(&s.cur).Elem()
	for _, f := range s.sc.fields {
		if s.set[f.Key] {
			f.at(dst).Set(f.at(src))
		}
	}
	for _, o := range s.sc.others {
		dst.FieldByIndex(o.path).Set(src.FieldByIndex(o.path))
	}
	return out
}

func (s *Store[T]) defLocked() T {
	if s.base != nil {
		return s.base()
	}
	return s.def
}

// Header sets the file's first comment lines, without "# ".
func (s *Store[T]) Header(lines ...string) { s.header = strings.Join(lines, "\n") }

// Path is the settings file.
func (s *Store[T]) Path() string { return s.path }

// Get returns the settings.
func (s *Store[T]) Get() T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked()
}

// Default returns the defaults; for a layer, base's settings.
func (s *Store[T]) Default() T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.defLocked()
}

// Validate adds a check on key's value, run after type and bounds. The
// value is the Go value: string, int, time.Duration, *float64, ...
func (s *Store[T]) Validate(key string, check func(v any) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks[key] = check
}

// Options gives the values of fields tagged options:"name". list runs
// under the store's lock: it must not call the store.
func (s *Store[T]) Options(name string, list func() []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.options[name] = list
}

// Problem is a key that could not be read.
type Problem struct{ Key, Err string }

// LoadError lists what a load could not read.
type LoadError struct {
	Path     string
	Problems []Problem
}

func (e *LoadError) Error() string {
	var parts []string
	for _, p := range e.Problems {
		if p.Key == "" {
			parts = append(parts, p.Err)
		} else {
			parts = append(parts, p.Key+": "+p.Err)
		}
	}
	return filepath.Base(e.Path) + ": " + strings.Join(parts, "; ")
}

func (s *Store[T]) loadErr(problems []Problem) error {
	if len(problems) == 0 {
		return nil
	}
	return &LoadError{Path: s.path, Problems: problems}
}

// Fields are every setting with its value, in declaration order.
func (s *Store[T]) Fields() []Field {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Field, len(s.sc.fields))
	cur, def := s.viewLocked(), s.defLocked()
	for i, f := range s.sc.fields {
		out[i] = s.fieldWith(f, cur, def)
	}
	return append(out, s.keyFieldsLocked(cur)...)
}

func (s *Store[T]) fieldLocked(f *field) Field {
	return s.fieldWith(f, s.viewLocked(), s.defLocked())
}

func (s *Store[T]) fieldWith(f *field, cur, def T) Field {
	out := f.Field
	out.Options = s.optionsLocked(f)
	out.Value = f.show(f.at(reflect.ValueOf(&cur).Elem()))
	out.Default = f.show(f.at(reflect.ValueOf(&def).Elem()))
	out.IsDefault = out.Value == out.Default
	if s.base != nil {
		out.IsDefault, out.From = !s.set[f.Key], s.from
	}
	return out
}

func (s *Store[T]) optionsLocked(f *field) []string {
	if f.options == "" {
		return slices.Clone(f.Options)
	}
	if list := s.options[f.options]; list != nil {
		return list()
	}
	return nil
}

// Field is one setting by key.
func (s *Store[T]) Field(key string) (Field, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.sc.byKey[key]
	if !ok {
		for _, kf := range s.keyFieldsLocked(s.viewLocked()) {
			if kf.Key == key {
				return kf, true
			}
		}
		return Field{}, false
	}
	return s.fieldLocked(f), true
}

// Search returns settings matching q, best first: label, key, group,
// help, then options.
func (s *Store[T]) Search(q string) []Field {
	all := s.Fields()
	return fuzzy.Filter(all, q, func(f Field) []string {
		return []string{f.Label, f.Key, f.Group, f.Help, strings.Join(f.Options, " ")}
	})
}

// Set parses text as key's value, checks it, writes the file and keeps
// it. On error nothing changes.
func (s *Store[T]) Set(key, text string) (Field, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.sc.byKey[key]
	if c, modes, isKeys := s.keyCmdLocked(key); !ok && isKeys {
		return s.setKeysLocked(c, modes, text, false)
	}
	if !ok {
		return Field{}, fmt.Errorf("no setting %q", key)
	}
	v, err := f.parse(text)
	if err != nil {
		return s.fieldLocked(f), err
	}
	return s.commitLocked(f, v)
}

// SetValue is Set with a Go value of the field's type.
func (s *Store[T]) SetValue(key string, value any) (Field, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.sc.byKey[key]
	if !ok {
		return Field{}, fmt.Errorf("no setting %q", key)
	}
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		v = reflect.Zero(f.typ)
	}
	if !v.Type().ConvertibleTo(f.typ) {
		return s.fieldLocked(f), fmt.Errorf("%s: %T is not a %s", key, value, f.typ)
	}
	v = v.Convert(f.typ)
	if err := f.check(v); err != nil {
		return s.fieldLocked(f), err
	}
	return s.commitLocked(f, v)
}

// Update changes the settings through fn, checks the whole, writes
// the file and keeps the result: for fields that are not settings,
// such as maps of entries. Maps and slices in fn's argument are shared
// with the store: on error the store reads its file back.
func (s *Store[T]) Update(fn func(*T)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.syncLocked(); err != nil {
		return err
	}
	next := s.viewLocked()
	fn(&next)
	root := reflect.ValueOf(&next).Elem()
	err := whole(next)
	for _, f := range s.sc.fields {
		if err != nil {
			break
		}
		v := f.at(root)
		if err = f.check(v); err == nil {
			err = s.checkLocked(f, v)
		}
		if err != nil {
			err = fmt.Errorf("%s: %w", f.Key, err)
		}
	}
	if err == nil {
		prev, prevSet := s.cur, maps.Clone(s.set)
		if s.base != nil {
			was := reflect.ValueOf(s.viewLocked())
			for _, f := range s.sc.fields {
				if !reflect.DeepEqual(f.at(root).Interface(), f.at(was).Interface()) {
					s.set[f.Key] = true
				}
			}
		}
		s.cur = next
		if err = s.writeLocked(); err == nil {
			return nil
		}
		s.cur, s.set = prev, prevSet
	}
	s.stamp = stamp{}
	s.reloadLocked()
	return err
}

// Reset sets key to its default.
func (s *Store[T]) Reset(key string) (Field, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.sc.byKey[key]
	if c, modes, isKeys := s.keyCmdLocked(key); !ok && isKeys {
		return s.setKeysLocked(c, modes, "", true)
	}
	if !ok {
		return Field{}, fmt.Errorf("no setting %q", key)
	}
	if s.base == nil {
		return s.commitLocked(f, f.at(reflect.ValueOf(&s.def).Elem()))
	}
	if err := s.syncLocked(); err != nil || !s.set[key] {
		return s.fieldLocked(f), err
	}
	delete(s.set, key)
	if err := whole(s.viewLocked()); err != nil {
		s.set[key] = true
		return s.fieldLocked(f), err
	}
	if err := s.writeLocked(); err != nil {
		s.set[key] = true
		return s.fieldLocked(f), err
	}
	return s.fieldLocked(f), nil
}

func (s *Store[T]) commitLocked(f *field, v reflect.Value) (Field, error) {
	if err := s.syncLocked(); err != nil {
		return s.fieldLocked(f), err
	}
	if err := s.checkLocked(f, v); err != nil {
		return s.fieldLocked(f), err
	}
	next := s.cur
	f.at(reflect.ValueOf(&next).Elem()).Set(v)
	prev, wasSet := s.cur, s.set[f.Key]
	s.cur = next
	if s.base != nil {
		s.set[f.Key] = true
	}
	undo := func() {
		s.cur = prev
		if s.base != nil && !wasSet {
			delete(s.set, f.Key)
		}
	}
	if err := whole(s.viewLocked()); err != nil {
		undo()
		return s.fieldLocked(f), err
	}
	if err := s.writeLocked(); err != nil {
		undo()
		return s.fieldLocked(f), err
	}
	return s.fieldLocked(f), nil
}

func (s *Store[T]) checkLocked(f *field, v reflect.Value) error {
	if f.Kind == Enum && !f.Free {
		val := f.show(v)
		opts := s.optionsLocked(f)
		if !(f.Optional && val == "") && len(opts) > 0 && !slices.Contains(opts, val) {
			return fmt.Errorf("%q is not one of %s", val, strings.Join(opts, ", "))
		}
	}
	if check := s.checks[f.Key]; check != nil {
		if err := check(v.Interface()); err != nil {
			return err
		}
	}
	return nil
}

// whole runs T's Validate method, on a value or pointer receiver.
func whole[T any](v T) error {
	if c, ok := any(v).(interface{ Validate() error }); ok {
		return c.Validate()
	}
	if c, ok := any(&v).(interface{ Validate() error }); ok {
		return c.Validate()
	}
	return nil
}

// syncLocked takes in outside edits to the file before a write.
func (s *Store[T]) syncLocked() error {
	if !s.changedLocked() {
		return nil
	}
	_, err := s.reloadLocked()
	return err
}

func (s *Store[T]) changedLocked() bool {
	if s.path == "" {
		return false
	}
	st, err := os.Stat(s.path)
	if err != nil {
		// a layer's file removed: back to base
		return s.base != nil && errors.Is(err, fs.ErrNotExist) && s.stamp != (stamp{})
	}
	return st.ModTime() != s.stamp.mod || st.Size() != s.stamp.size
}

func (s *Store[T]) stampNow() {
	if st, err := os.Stat(s.path); err == nil {
		s.stamp = stamp{st.ModTime(), st.Size()}
	}
}

// Reload reads the file again if it changed on disk since last read or
// written. Bad values keep their previous value and are reported;
// changed reports any value differs.
func (s *Store[T]) Reload() (changed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.changedLocked() {
		return false, nil
	}
	return s.reloadLocked()
}

func (s *Store[T]) reloadLocked() (bool, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		if s.base != nil {
			changed := len(s.set) > 0
			s.cur, s.set, s.stamp = s.base(), map[string]bool{}, stamp{}
			return changed, nil
		}
		return false, s.writeLocked()
	}
	if err != nil {
		return false, err
	}
	s.stampNow()
	if len(bytes.TrimSpace(data)) == 0 {
		return false, &LoadError{Path: s.path, Problems: []Problem{{Err: "empty file, not applied"}}}
	}
	over := s.cur
	if s.base != nil {
		over = s.base()
	}
	cur, problems, present, err := s.decode(data, over)
	if err != nil {
		return false, &LoadError{Path: s.path, Problems: []Problem{{Err: err.Error() + ", not applied"}}}
	}
	if err := whole(cur); err != nil {
		return false, &LoadError{Path: s.path, Problems: []Problem{{Err: err.Error() + ", not applied"}}}
	}
	changed := !reflect.DeepEqual(cur, s.viewLocked())
	s.cur = cur
	s.clean, s.present = len(problems) == 0, present
	if s.base != nil {
		changed = changed || !maps.Equal(present, s.set)
		s.set = present
	}
	return changed, s.loadErr(problems)
}

// decode reads data over base: settings value by value, the rest as
// BurntSushi decodes it. A bad setting keeps base's value. present are
// the settings read from data.
func (s *Store[T]) decode(data []byte, base T) (T, []Problem, map[string]bool, error) {
	var raw map[string]any
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return base, nil, nil, err
	}
	present := map[string]bool{}
	out := base
	root := reflect.ValueOf(&out).Elem()
	var problems []Problem
	known := map[string]bool{}
	for _, f := range s.sc.fields {
		known[f.Key] = true
		val, ok := lookup(raw, strings.Split(f.Key, "."))
		if !ok {
			continue
		}
		v, err := f.fromTOML(val)
		if err == nil {
			err = s.checkLocked(f, v)
		}
		if err != nil {
			problems = append(problems, Problem{f.Key, err.Error()})
			continue
		}
		f.at(root).Set(v)
		present[f.Key] = true
	}
	for _, o := range s.sc.others {
		val, ok := lookup(raw, o.key)
		known[strings.Join(o.key, ".")] = true
		if !ok {
			continue
		}
		present[strings.Join(o.key, ".")] = true
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(map[string]any{"v": val}); err != nil {
			problems = append(problems, Problem{strings.Join(o.key, "."), err.Error()})
			continue
		}
		dst := reflect.New(root.FieldByIndex(o.path).Type())
		var wrap = reflect.New(reflect.StructOf([]reflect.StructField{{Name: "V", Type: dst.Type().Elem(), Tag: `toml:"v"`}}))
		if _, err := toml.Decode(buf.String(), wrap.Interface()); err != nil {
			problems = append(problems, Problem{strings.Join(o.key, "."), err.Error()})
			continue
		}
		root.FieldByIndex(o.path).Set(wrap.Elem().Field(0))
	}
	groups := map[string]bool{}
	for _, g := range s.sc.groups {
		groups[g] = true
	}
	for _, k := range md.Keys() {
		key := k.String()
		if known[key] || groups[key] || underOther(key, s.sc.others) {
			continue
		}
		problems = append(problems, Problem{key, "unknown setting"})
	}
	return out, problems, present, nil
}

func underOther(key string, others []other) bool {
	for _, o := range others {
		if strings.HasPrefix(key, strings.Join(o.key, ".")+".") {
			return true
		}
	}
	return false
}

func lookup(m map[string]any, keys []string) (any, bool) {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = mm[k]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// fromTOML converts a decoded TOML value to the field's Go type.
func (f *field) fromTOML(val any) (reflect.Value, error) {
	switch x := val.(type) {
	case string:
		if f.Kind == List {
			return reflect.Value{}, fmt.Errorf("want a list of strings")
		}
		if f.Kind == Bool || f.Kind == Int || f.Kind == Float {
			return reflect.Value{}, fmt.Errorf("want a %s, not a string", f.Kind)
		}
		if f.Kind == String || f.Kind == Enum || f.Kind == JSON {
			// kept as written: no trimming
			base := f.typ
			if f.Optional {
				base = base.Elem()
			}
			v := reflect.New(base).Elem()
			v.SetString(x)
			if f.Optional {
				p := reflect.New(base)
				p.Elem().Set(v)
				return p, nil
			}
			return v, nil
		}
		return f.parse(x)
	case bool:
		if f.Kind != Bool {
			return reflect.Value{}, fmt.Errorf("want a %s, not true or false", f.Kind)
		}
		return f.parse(fmt.Sprint(x))
	case int64:
		switch f.Kind {
		case Int, Float, ByteSize, Duration:
			return f.parse(fmt.Sprint(x))
		}
	case float64:
		switch f.Kind {
		case Float, Duration, ByteSize:
			return f.parse(fmt.Sprint(x))
		}
	case []any:
		if f.Kind != List {
			break
		}
		items := make([]string, len(x))
		for i, it := range x {
			s, ok := it.(string)
			if !ok {
				return reflect.Value{}, fmt.Errorf("want a list of strings")
			}
			items[i] = s
		}
		return reflect.ValueOf(items).Convert(f.typ), nil
	}
	return reflect.Value{}, fmt.Errorf("want a %s", f.Kind)
}
