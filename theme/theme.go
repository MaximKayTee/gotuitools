package theme

import (
	"embed"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
)

// A Theme gives every named part of the screen a style.
//
// Parts are named by scope: "ui.selection", "markup.heading.1". A scope
// with no style of its own takes the style of the nearest scope above it:
// "markup.heading.1" falls back to "markup.heading", then "markup".
type Theme struct {
	name    string
	scopes  map[string]Style
	under   map[string]Style  // the general parts and a program's own, for scopes the theme does not set
	palette map[string]string // colour names from the theme's [palette]
}

// Name is the name the theme was found or registered under; "" for a
// theme loaded from a path.
func (t Theme) Name() string { return t.name }

// Default is charmtone.
func Default() Theme { return mustBuiltIn("charmtone") }

//go:embed charmtone.toml charmtone-light.toml terminal.toml catppuccin.toml catppuccin-light.toml gruvbox.toml gruvbox-light.toml tokyonight.toml nord.toml dracula.toml
var builtInFiles embed.FS

var (
	regMu    sync.RWMutex
	registry = map[string]Theme{}
	initOnce sync.Once
)

func initBuiltIn() {
	initOnce.Do(func() {
		for _, name := range []string{"charmtone", "charmtone-light", "terminal", "catppuccin", "catppuccin-light", "gruvbox", "gruvbox-light", "tokyonight", "nord", "dracula"} {
			data, err := builtInFiles.ReadFile(name + ".toml")
			if err != nil {
				panic(err)
			}
			if err := register(name, data); err != nil {
				panic(err)
			}
		}
	})
}

func mustBuiltIn(name string) Theme {
	t, ok := builtIn(name)
	if !ok {
		panic("theme: no built-in " + name)
	}
	return t
}

func builtIn(name string) (Theme, bool) {
	initBuiltIn()
	regMu.RLock()
	defer regMu.RUnlock()
	t, ok := registry[name]
	return t, ok
}

// BuiltIn lists the registered theme names, sorted.
func BuiltIn() []string {
	initBuiltIn()
	regMu.RLock()
	defer regMu.RUnlock()
	return slices.Sorted(maps.Keys(registry))
}

// Register adds a program's own theme, a Helix-format TOML file, under
// name. A theme named "<name>-light" is the light variant of "<name>".
// It replaces a registered theme of the same name. No inherits.
func Register(name string, data []byte) error {
	initBuiltIn()
	return register(name, data)
}

// MustRegister is Register, panicking on error. For init and embedded files.
func MustRegister(name string, data []byte) {
	if err := Register(name, data); err != nil {
		panic(err)
	}
}

func register(name string, data []byte) error {
	f, err := decode(name+".toml", data)
	if err != nil {
		return err
	}
	if f.parent != "" {
		return fmt.Errorf("theme %s: inherits not supported in a registered theme", name)
	}
	t, err := build([]themeFile{f})
	if err != nil {
		return err
	}
	t.name = name
	regMu.Lock()
	registry[name] = t
	regMu.Unlock()
	return nil
}

// Load reads a theme file in Helix's format.
func Load(file string) (Theme, error) {
	files, err := lineage(file)
	if err != nil {
		return Theme{}, err
	}
	return build(files)
}

// Find loads the theme with this name: "name.toml" in the first of dirs
// that has it, else Noctalia's kitty colours for "noctalia", else the
// registered theme of that name. "" and "default" are Default.
func Find(name string, dirs ...string) (Theme, error) {
	if name == "" || name == "default" {
		return Default(), nil
	}
	switch file := File(name, dirs...); {
	case file == "":
	case file == NoctaliaFile():
		return noctalia(file)
	default:
		t, err := Load(file)
		t.name = name
		return t, err
	}
	if t, ok := builtIn(name); ok {
		return t, nil
	}
	return Theme{}, fmt.Errorf("theme %q not found in %s", name, strings.Join(append(dirs, "built-in"), ", "))
}

// FindFor is Find for a terminal background: on a light one, the theme
// "<name>-light" when there is one. "" and "default" are charmtone.
func FindFor(name string, dark bool, dirs ...string) (Theme, error) {
	if name == "" || name == "default" {
		name = "charmtone"
	}
	if !dark && !strings.HasSuffix(name, "-light") {
		if t, err := Find(name+"-light", dirs...); err == nil {
			return t, nil
		}
	}
	return Find(name, dirs...)
}

// Dirs are the theme directories for a program: <config>/<app>/themes,
// then <config>/gotuitools/themes, which every program reads.
func Dirs(app string) []string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(dir, app, "themes"), filepath.Join(dir, "gotuitools", "themes")}
}

// Ground returns the colour of plain text and the colour of the screen
// behind it: the styles of "ui.text" and "ui.background". Either is nil
// where the theme leaves the terminal's own.
func (t Theme) Ground() (text, screen color.Color) {
	if fg := t.Style("ui.text").Fg; fg != "" {
		text = lipgloss.Color(fg)
	}
	if bg := t.Style("ui.background").Bg; bg != "" {
		screen = lipgloss.Color(bg)
	}
	return text, screen
}

// Style returns how the named part of the screen is drawn: as the theme
// says of it or of a scope above it, else as the general parts and the
// program's own (see Under) say.
func (t Theme) Style(scope string) Style {
	if s, ok := nearest(t.scopes, scope); ok {
		return s
	}
	s, _ := nearest(t.under, scope)
	return s
}

// nearest is the style of scope, or of the nearest scope above it that
// has one.
func nearest(styles map[string]Style, scope string) (Style, bool) {
	for {
		if s, ok := styles[scope]; ok {
			return s, true
		}
		i := strings.LastIndexByte(scope, '.')
		if i < 0 {
			return Style{}, false
		}
		scope = scope[:i]
	}
}

// Render draws text in the style of scope.
func (t Theme) Render(scope, text string) string { return t.Style(scope).Render(text) }

// Lip is the style of scope as a lipgloss.Style.
func (t Theme) Lip(scope string) lipgloss.Style { return t.Style(scope).Lip() }

// Colour is the colour named in the theme's palette, or the colour of a
// role (see Roles), or nil.
func (t Theme) Colour(name string) color.Color {
	if c := t.colours()[name]; c != "" {
		return lipgloss.Color(c)
	}
	return nil
}

// Fg is the foreground colour of scope, or nil.
func (t Theme) Fg(scope string) color.Color {
	if c := t.Style(scope).Fg; c != "" {
		return lipgloss.Color(c)
	}
	return nil
}

// Bg is the background colour of scope, or nil.
func (t Theme) Bg(scope string) color.Color {
	if c := t.Style(scope).Bg; c != "" {
		return lipgloss.Color(c)
	}
	return nil
}

// Over returns the theme with these styles laid on top. Each value is
// written as in a theme file: a colour, or a table with fg, bg, modifiers.
func (t Theme) Over(scopes map[string]any) (Theme, error) {
	styles, err := t.styles(scopes)
	if err != nil {
		return Theme{}, err
	}
	over := Theme{name: t.name, scopes: make(map[string]Style, len(t.scopes)), under: t.under, palette: t.palette}
	maps.Copy(over.scopes, t.scopes)
	for scope, s := range styles {
		over.scopes[scope] = s.Over(over.scopes[scope])
	}
	return over, nil
}

// Scopes lists the scopes the theme sets itself, sorted.
func (t Theme) Scopes() []string { return slices.Sorted(maps.Keys(t.scopes)) }

// lineage reads file and the files it inherits from, furthest parent first.
func lineage(file string) ([]themeFile, error) {
	var files []themeFile
	seen := make(map[string]bool)
	path, child := filepath.Clean(file), ""
	for path != "" {
		if seen[path] {
			return nil, fmt.Errorf("%s: themes inherit from each other", path)
		}
		seen[path] = true
		f, err := read(path)
		if err != nil && child != "" {
			// a parent that is not beside it: a registered theme of that name
			name := strings.TrimSuffix(filepath.Base(path), ".toml")
			if errors.Is(err, fs.ErrNotExist) {
				if t, ok := builtIn(name); ok {
					files = append([]themeFile{t.asFile()}, files...)
					break
				}
			}
			return nil, fmt.Errorf("%s: inherits: %w", child, err)
		}
		if err != nil {
			return nil, err
		}
		files = append([]themeFile{f}, files...)
		path, child = f.parent, path
	}
	return files, nil
}

// asFile is a built theme as a file whose values are already looked up.
func (t Theme) asFile() themeFile {
	f := themeFile{path: t.name, palette: map[string]any{}, built: maps.Clone(t.scopes)}
	for k, v := range t.palette {
		f.palette[k] = v
	}
	return f
}

// build makes a theme from files given furthest parent first. Palettes
// add up; a scope set in a later file replaces its style from earlier ones.
// Every file's scopes see the whole palette, as in Helix.
func build(files []themeFile) (Theme, error) {
	t := Theme{scopes: make(map[string]Style), palette: make(map[string]string)}
	for _, f := range files {
		for name, value := range f.palette {
			c, err := colour(value, nil)
			if err != nil {
				return Theme{}, fmt.Errorf("%s: palette %s: %w", f.path, name, err)
			}
			t.palette[name] = c
		}
	}
	for _, f := range files {
		maps.Copy(t.scopes, f.built)
		styles, err := t.styles(f.scopes)
		if err != nil {
			return Theme{}, fmt.Errorf("%s: %w", f.path, err)
		}
		maps.Copy(t.scopes, styles)
	}
	under, err := t.styles(base())
	if err != nil {
		return Theme{}, fmt.Errorf("base.toml: %w", err)
	}
	t.under = under
	return t, nil
}

// styles reads each scope's value with the theme's palette and its roles.
func (t Theme) styles(values map[string]any) (map[string]Style, error) {
	styles := make(map[string]Style, len(values))
	colours := t.colours()
	for _, scope := range slices.Sorted(maps.Keys(values)) {
		s, err := style(values[scope], colours)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", scope, err)
		}
		styles[scope] = s
	}
	return styles, nil
}
