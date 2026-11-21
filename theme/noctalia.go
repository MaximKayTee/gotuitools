package theme

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Noctalia is the theme from the kitty colours the Noctalia shell writes:
// NoctaliaFile, read again on each Find.
const Noctalia = "noctalia"

// NoctaliaFile is <config>/kitty/themes/noctalia.conf; "" without a
// config directory.
func NoctaliaFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kitty", "themes", "noctalia.conf")
}

// noctaliaRoles are the roles from kitty's colour names, as Noctalia's
// kitty template fills them: the first name the file has.
var noctaliaRoles = [][]string{
	{"text", "foreground"},
	{"background", "background"},
	{"surface", "inactive_tab_background", "color0"},
	{"border", "color8"},
	{"muted", "inactive_tab_foreground", "color7"},
	{"accent", "active_border_color", "active_tab_background", "color4"},
	{"accent2", "color3"},
	{"good", "color2"},
	{"warning", "color3"},
	{"bad", "color1"},
	{"info", "color6"},
	{"onaccent", "active_tab_foreground"},
}

// noctalia builds the theme from the kitty colours in file.
func noctalia(file string) (Theme, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Theme{}, err
	}
	colours := map[string]string{}
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		key, value, _ := strings.Cut(strings.TrimSpace(lines.Text()), " ")
		if key != "" && key[0] != '#' {
			colours[key] = strings.TrimSpace(value)
		}
	}
	palette := map[string]any{}
	for _, r := range noctaliaRoles {
		for _, key := range r[1:] {
			if c, ok := colours[key]; ok {
				palette[r[0]] = c
				break
			}
		}
		if _, ok := palette[r[0]]; !ok && r[0] != "onaccent" {
			return Theme{}, fmt.Errorf("%s: no %s", file, strings.Join(r[1:], " or "))
		}
	}
	t, err := build([]themeFile{{path: file, palette: palette}})
	t.name = Noctalia
	return t, err
}

// File is the file Find loads the theme name from with dirs; "" for a
// registered theme or none.
func File(name string, dirs ...string) string {
	for _, dir := range dirs {
		file := filepath.Join(dir, name+".toml")
		if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
			return file
		}
	}
	if f := NoctaliaFile(); name == Noctalia && f != "" {
		if _, err := os.Stat(f); !errors.Is(err, fs.ErrNotExist) {
			return f
		}
	}
	return ""
}

// Names lists the themes Find can load from dirs, sorted: the files in
// them, the registered themes, and Noctalia when its file is there.
func Names(dirs ...string) []string {
	names := BuiltIn()
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.toml"))
		for _, f := range files {
			names = append(names, strings.TrimSuffix(filepath.Base(f), ".toml"))
		}
	}
	if File(Noctalia) != "" {
		names = append(names, Noctalia)
	}
	slices.Sort(names)
	return slices.Compact(names)
}
