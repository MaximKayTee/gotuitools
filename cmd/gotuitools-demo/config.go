package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MaximKayTee/gotuitools/settings"
	"github.com/MaximKayTee/gotuitools/theme"
)

const program = "gotuitools-demo"

// config is the settings file.
type config struct {
	Theme  string            `toml:"theme" options:"themes"`
	List   listConfig        `toml:"list"`
	Input  inputConfig       `toml:"input"`
	Layout layoutConfig      `toml:"layout"`
	Text   textConfig        `toml:"text"`
	Images imagesConfig      `toml:"images"`
	Keys   map[string]string `toml:"keys"`
}

type listConfig struct {
	Rows int  `toml:"rows" min:"0" max:"1000000"`
	Wrap bool `toml:"wrap" help:"Up on the first row goes to the last"`
}

type inputConfig struct {
	Limit int    `toml:"limit" min:"0" max:"1000" help:"Runes; 0 none"`
	Mask  string `toml:"mask" help:"One character"`
}

type layoutConfig struct {
	Split int `toml:"split" min:"10" max:"90" help:"Left pane, percent of the width"`
}

type textConfig struct {
	Size     settings.Bytes `toml:"size"`
	Duration time.Duration  `toml:"duration"`
}

type imagesConfig struct {
	Mode string `toml:"mode" enum:"auto,kitty,blocks,none" help:"auto: kitty where supported, else blocks"`
}

func defaults() config {
	return config{
		Theme:  "charmtone",
		List:   listConfig{Rows: 500},
		Input:  inputConfig{Limit: 12, Mask: "•"},
		Layout: layoutConfig{Split: 40},
		Text:   textConfig{Size: 1610612736, Duration: 90 * time.Minute},
		Images: imagesConfig{Mode: "auto"},
	}
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, program, "config.toml"), nil
}

// openConfig opens the settings file. The store is usable with a
// *settings.LoadError.
func openConfig(path string) (*settings.Store[config], error) {
	return settings.Open(path, defaults(), func(s *settings.Store[config]) {
		s.Options("themes", themeNames)
		s.Validate("input.mask", func(v any) error {
			if utf8.RuneCountInString(v.(string)) != 1 {
				return errors.New("one character")
			}
			return nil
		})
	})
}

// themeNames are the registered themes and those in the theme directories.
func themeNames() []string {
	names := theme.BuiltIn()
	for _, dir := range theme.Dirs(program) {
		files, _ := filepath.Glob(filepath.Join(dir, "*.toml"))
		for _, f := range files {
			names = append(names, strings.TrimSuffix(filepath.Base(f), ".toml"))
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
