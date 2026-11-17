package settings

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MaximKayTee/gotuitools/text"
)

// Bytes is a byte count, written "2GiB", "500MB"; read also as a number.
type Bytes int64

// Kind is how a setting's value is typed and edited.
type Kind string

const (
	Bool     Kind = "bool"
	Int      Kind = "int"
	Float    Kind = "float"
	String   Kind = "string"
	Enum     Kind = "enum" // a string, one of Options
	Duration Kind = "duration"
	ByteSize Kind = "bytes"
	List     Kind = "list" // []string
	JSON     Kind = "json" // raw text, a remote source's structured value
)

// Field is one setting: its schema, and its value and default as text.
type Field struct {
	Key   string // "layout.style"
	Group string // "Layout"
	Label string
	Help  string
	Kind  Kind
	// Options are an enum's values; with Free, suggestions for a string.
	Options []string
	Free    bool
	// Min, Max bound numbers, durations (seconds) and byte sizes.
	Min, Max *float64
	Restart  bool // takes effect after a restart
	Secret   bool // shown masked
	Long     bool // multi-line text
	Optional bool // may be unset: "" clears it

	Value, Default string
	IsDefault      bool   // for a layer: not set in its file
	From           string // for a layer: where Default comes from, "System"
}

// Range is the bounds in words, "1 to 5", "at least 1MiB"; "" none.
func (f Field) Range() string {
	n := func(v float64) string {
		switch f.Kind {
		case Duration:
			return text.Duration(time.Duration(v * float64(time.Second)))
		case ByteSize:
			return text.ExactBytes(int64(v))
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	switch {
	case f.Min != nil && f.Max != nil:
		return n(*f.Min) + " to " + n(*f.Max)
	case f.Min != nil:
		return "at least " + n(*f.Min)
	case f.Max != nil:
		return "at most " + n(*f.Max)
	}
	return ""
}

// field is a Field with where its value lives in the struct.
type field struct {
	Field
	path    []int
	typ     reflect.Type
	options string // name of runtime options
}

// other is a struct field that is not a setting: kept and written back;
// with omitempty, not written when empty.
type other struct {
	key       []string
	path      []int
	omitempty bool
}

type schema struct {
	fields []*field
	byKey  map[string]*field
	others []other
	groups []string // group keys in order, "" top level
}

var (
	durationType = reflect.TypeFor[time.Duration]()
	bytesType    = reflect.TypeFor[Bytes]()
)

// kindOf is the Kind of a Go type, false for types that are not settings.
func kindOf(t reflect.Type) (Kind, bool) {
	switch t {
	case durationType:
		return Duration, true
	case bytesType:
		return ByteSize, true
	}
	switch t.Kind() {
	case reflect.Bool:
		return Bool, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Int, true
	case reflect.Float32, reflect.Float64:
		return Float, true
	case reflect.String:
		return String, true
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return List, true
		}
	}
	return "", false
}

// Label is a key in words: "trash_max_age" is "Trash max age".
func Label(key string) string {
	s := strings.NewReplacer("_", " ", "-", " ").Replace(key)
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

func tomlName(sf reflect.StructField) (name string, skip bool) {
	tag := sf.Tag.Get("toml")
	if tag == "-" || !sf.IsExported() {
		return "", true
	}
	name, _, _ = strings.Cut(tag, ",")
	if name == "" {
		name = sf.Name
	}
	return name, false
}

func parseSchema(t reflect.Type) (*schema, error) {
	s := &schema{byKey: map[string]*field{}}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("settings: %s is not a struct", t)
	}
	s.groups = append(s.groups, "")
	return s, s.walk(t, nil, nil, "")
}

func (s *schema) walk(t reflect.Type, keys []string, path []int, group string) error {
	for i := range t.NumField() {
		sf := t.Field(i)
		name, skip := tomlName(sf)
		if skip {
			continue
		}
		k := append(slices.Clone(keys), name)
		p := append(slices.Clone(path), i)
		ft := sf.Type
		omit := strings.Contains(sf.Tag.Get("toml"), ",omitempty")
		if sf.Tag.Get("settings") == "-" {
			s.others = append(s.others, other{k, p, omit})
			continue
		}
		if ft.Kind() == reflect.Struct && ft != durationType {
			label := sf.Tag.Get("label")
			if label == "" {
				label = Label(name)
			}
			g := label
			if group != "" {
				g = group + " › " + label
			}
			s.groups = append(s.groups, strings.Join(k, "."))
			if err := s.walk(ft, k, p, g); err != nil {
				return err
			}
			continue
		}
		optional := false
		base := ft
		if ft.Kind() == reflect.Pointer {
			base, optional = ft.Elem(), true
		}
		kind, ok := kindOf(base)
		if !ok || (optional && kind == List) {
			s.others = append(s.others, other{k, p, omit})
			continue
		}
		f := &field{path: p, typ: ft}
		f.Key = strings.Join(k, ".")
		f.Group = group
		f.Label = sf.Tag.Get("label")
		if f.Label == "" {
			f.Label = Label(name)
		}
		f.Help = sf.Tag.Get("help")
		f.Kind = kind
		f.Optional = optional
		f.Restart = sf.Tag.Get("restart") == "true"
		f.Secret = sf.Tag.Get("secret") == "true"
		f.Long = sf.Tag.Get("long") == "true"
		f.options = sf.Tag.Get("options")
		enum := sf.Tag.Get("enum")
		if enum == "" {
			enum = sf.Tag.Get("choices")
		}
		if enum != "" {
			if kind != String {
				return fmt.Errorf("settings: %s: enum on a %s", f.Key, kind)
			}
			f.Kind = Enum
			for _, o := range strings.Split(enum, ",") {
				f.Options = append(f.Options, strings.TrimSpace(o))
			}
		}
		if f.options != "" && f.Kind == String {
			f.Kind, f.Free = Enum, sf.Tag.Get("free") == "true"
		}
		for tag, dst := range map[string]**float64{"min": &f.Min, "max": &f.Max} {
			v := sf.Tag.Get(tag)
			if v == "" {
				continue
			}
			n, err := bound(f.Kind, v)
			if err != nil {
				return fmt.Errorf("settings: %s: %s: %w", f.Key, tag, err)
			}
			*dst = &n
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return fmt.Errorf("settings: %s: min above max", f.Key)
		}
		s.fields = append(s.fields, f)
		s.byKey[f.Key] = f
	}
	return nil
}

// bound reads a min or max tag in the field's units: seconds for a
// duration, bytes for a size.
func bound(k Kind, v string) (float64, error) {
	switch k {
	case Duration:
		d, err := text.ParseDuration(v)
		return d.Seconds(), err
	case ByteSize:
		b, err := text.ParseBytes(v)
		return float64(b), err
	case Int, Float:
		return strconv.ParseFloat(v, 64)
	}
	return 0, fmt.Errorf("bounds on a %s", k)
}

// at is the field's value in root, a struct value.
func (f *field) at(root reflect.Value) reflect.Value { return root.FieldByIndex(f.path) }

// show is v as the text Parse reads back; "" for an unset optional.
func (f *field) show(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	switch f.Kind {
	case Bool:
		return strconv.FormatBool(v.Bool())
	case Int:
		if v.CanUint() {
			return strconv.FormatUint(v.Uint(), 10)
		}
		return strconv.FormatInt(v.Int(), 10)
	case Float:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case Duration:
		return text.Duration(time.Duration(v.Int()))
	case ByteSize:
		return text.ExactBytes(v.Int())
	case List:
		return strings.Join(v.Interface().([]string), ", ")
	}
	return v.String()
}

// parse reads text into a new value of the field's Go type, bounds
// checked. Options are checked by the caller, who knows them.
func (f *field) parse(s string) (reflect.Value, error) {
	t := f.typ
	if f.Optional {
		if strings.TrimSpace(s) == "" {
			return reflect.Zero(t), nil
		}
		t = t.Elem()
	}
	v := reflect.New(t).Elem()
	s = strings.TrimSpace(s)
	var n float64
	switch f.Kind {
	case Bool:
		switch strings.ToLower(s) {
		case "true", "yes", "on", "1":
			v.SetBool(true)
		case "false", "no", "off", "0":
		default:
			return v, fmt.Errorf("%q is not true or false", s)
		}
	case Int:
		if v.CanUint() {
			u, err := strconv.ParseUint(s, 10, t.Bits())
			if err != nil {
				return v, fmt.Errorf("%q is not a whole number", s)
			}
			v.SetUint(u)
			n = float64(u)
		} else {
			i, err := strconv.ParseInt(s, 10, t.Bits())
			if err != nil {
				return v, fmt.Errorf("%q is not a whole number", s)
			}
			v.SetInt(i)
			n = float64(i)
		}
	case Float:
		x, err := strconv.ParseFloat(s, t.Bits())
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return v, fmt.Errorf("%q is not a number", s)
		}
		v.SetFloat(x)
		n = x
	case Duration:
		d, err := text.ParseDuration(s)
		if err != nil {
			return v, err
		}
		v.SetInt(int64(d))
		n = d.Seconds()
	case ByteSize:
		b, err := text.ParseBytes(s)
		if err != nil {
			return v, err
		}
		v.SetInt(b)
		n = float64(b)
	case List:
		var items []string
		for _, it := range strings.Split(s, ",") {
			if it = strings.TrimSpace(it); it != "" {
				items = append(items, it)
			}
		}
		v.Set(reflect.ValueOf(items).Convert(t))
	default:
		v.SetString(s)
	}
	if f.Min != nil && n < *f.Min || f.Max != nil && n > *f.Max {
		return v, fmt.Errorf("%s is out of range: %s", s, f.Range())
	}
	if f.Optional {
		p := reflect.New(t)
		p.Elem().Set(v)
		return p, nil
	}
	return v, nil
}

// check is the bounds check of a value already in the struct.
func (f *field) check(v reflect.Value) error {
	_, err := f.parse(f.show(v))
	return err
}
