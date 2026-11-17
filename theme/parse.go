package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// A themeFile is one theme file as written, its colours not yet looked up.
type themeFile struct {
	path    string
	parent  string // the file named by inherits, or ""
	palette map[string]any
	scopes  map[string]any
	built   map[string]Style // scopes already looked up: a registered parent
}

func read(path string) (themeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return themeFile{}, err
	}
	return decode(path, data)
}

// decode splits a theme file into its parent, its palette and its scopes.
func decode(path string, data []byte) (themeFile, error) {
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return themeFile{}, fmt.Errorf("%s: %w", path, err)
	}
	f := themeFile{path: path, scopes: doc}
	if value, ok := doc["inherits"]; ok {
		name, ok := value.(string)
		if !ok {
			return themeFile{}, fmt.Errorf("%s: inherits is %v, not a theme name", path, value)
		}
		f.parent = filepath.Join(filepath.Dir(path), name+".toml")
		delete(doc, "inherits")
	}
	if value, ok := doc["palette"]; ok {
		palette, ok := value.(map[string]any)
		if !ok {
			return themeFile{}, fmt.Errorf("%s: palette is %v, not a table", path, value)
		}
		f.palette = palette
		delete(doc, "palette")
	}
	return f, nil
}

// style reads a scope's value: a colour for the foreground, or a table.
func style(value any, palette map[string]string) (Style, error) {
	switch value := value.(type) {
	case string:
		fg, err := colour(value, palette)
		return Style{Fg: fg}, err
	case map[string]any:
		return table(value, palette)
	}
	return Style{}, fmt.Errorf("%v is neither a colour nor a table", value)
}

func table(attributes map[string]any, palette map[string]string) (Style, error) {
	var s Style
	for name, value := range attributes {
		var err error
		switch name {
		case "fg":
			s.Fg, err = colour(value, palette)
		case "bg":
			s.Bg, err = colour(value, palette)
		case "modifiers":
			err = modify(&s, value)
		case "underline":
			// Underline colour and shape cannot be drawn; any table means underlined.
			if _, ok := value.(map[string]any); !ok {
				err = fmt.Errorf("underline is %v, not a table", value)
			}
			s.Underline = true
		default:
			err = fmt.Errorf("unknown attribute %q", name)
		}
		if err != nil {
			return Style{}, err
		}
	}
	return s, nil
}

// modify sets the attributes named in a list of Helix modifiers.
func modify(s *Style, modifiers any) error {
	list, ok := modifiers.([]any)
	if !ok {
		return fmt.Errorf("modifiers is %v, not a list", modifiers)
	}
	for _, m := range list {
		name, _ := m.(string)
		switch name {
		case "bold":
			s.Bold = true
		case "dim":
			s.Dim = true
		case "italic":
			s.Italic = true
		case "underlined":
			s.Underline = true
		case "crossed_out":
			s.Strikethrough = true
		case "reversed":
			s.Reverse = true
		case "slow_blink", "rapid_blink", "hidden":
			// Helix modifiers that are not drawn.
		default:
			return fmt.Errorf("unknown modifier %v", m)
		}
	}
	return nil
}

// terminal maps Helix's names for the sixteen terminal colours to palette numbers.
var terminal = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3",
	"blue": "4", "magenta": "5", "cyan": "6", "light-gray": "7",
	"gray": "8", "light-red": "9", "light-green": "10", "light-yellow": "11",
	"light-blue": "12", "light-magenta": "13", "light-cyan": "14", "white": "15",
}

// colour reads a colour as "#rrggbb" or a palette number, looking names up
// in the palette first, then among the terminal colours. An alpha byte, as
// in "#rrggbbaa", is dropped, as Helix does.
func colour(value any, palette map[string]string) (string, error) {
	name, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("colour %v is not a string", value)
	}
	if c, ok := palette[name]; ok {
		return c, nil
	}
	if c, ok := terminal[name]; ok {
		return c, nil
	}
	if (len(name) == 7 || len(name) == 9) && name[0] == '#' {
		if _, err := strconv.ParseUint(name[1:], 16, 32); err == nil {
			return strings.ToLower(name[:7]), nil
		}
	}
	if n, err := strconv.Atoi(name); err == nil && n >= 0 && n <= 255 {
		return strconv.Itoa(n), nil
	}
	return "", fmt.Errorf("unknown colour %q", name)
}
