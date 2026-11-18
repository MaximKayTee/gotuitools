package theme

import (
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"
	"sync"
)

// The roles are the colours a theme names in its palette for the general
// parts of a screen. A program writes its own parts with them, so that a
// theme which has never heard of the program still draws all of it.
//
// A theme that does not name a role has it taken from the scope that is
// drawn with it.
var roles = []struct {
	name  string
	scope string // where a theme that does not name the role has it from
	bg    bool   // from the scope's background
}{
	{"text", "ui.text", false},
	{"background", "ui.background", true},
	{"surface", "ui.statusline", true}, // raised: bars, menus, blocks
	{"border", "ui.window", false},
	{"muted", "ui.text.inactive", false},
	{"accent", "ui.window.focus", false},
	{"accent2", "ui.popup", false},
	{"good", "diff.plus", false},
	{"warning", "warning", false},
	{"bad", "error", false},
	{"info", "info", false},
}

// Roles lists the names of the roles. There is one more, "onaccent": the
// one of text and background that reads better on accent.
func Roles() []string {
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = r.name
	}
	return names
}

//go:embed base.toml
var baseFile []byte

// base is base.toml: the general parts, written with the roles.
var base = sync.OnceValue(func() map[string]any {
	f, err := decode("base.toml", baseFile)
	if err != nil {
		panic(err)
	}
	return f.scopes
})

// colours are the names a style can use for a colour: the theme's
// palette, and every role, "" for one the theme gives no colour.
func (t Theme) colours() map[string]string {
	all := maps.Clone(t.palette)
	if all == nil {
		all = map[string]string{}
	}
	for _, r := range roles {
		if _, named := all[r.name]; named {
			continue
		}
		s, _ := nearest(t.scopes, r.scope)
		all[r.name] = s.Fg
		if r.bg {
			all[r.name] = s.Bg
		}
	}
	if _, named := all["onaccent"]; !named {
		all["onaccent"] = all["background"]
		if contrast(all["text"], all["accent"]) > contrast(all["background"], all["accent"]) {
			all["onaccent"] = all["text"]
		}
	}
	return all
}

// Under returns the theme with a program's own parts beneath it. data is
// written as a theme file is, scopes only, with the roles for colours:
//
//	"gotube.mark" = { fg = "accent", modifiers = ["bold"] }
//
// What the theme says of a scope, or of a scope above it, stands.
func (t Theme) Under(data []byte) (Theme, error) {
	f, err := decode("under", data)
	if err != nil {
		return Theme{}, err
	}
	if f.parent != "" || f.palette != nil {
		return Theme{}, errors.New("theme: Under takes scopes only, no inherits and no palette")
	}
	styles, err := t.styles(f.scopes)
	if err != nil {
		return Theme{}, fmt.Errorf("theme: under: %w", err)
	}
	t.under = maps.Clone(t.under)
	if t.under == nil {
		t.under = map[string]Style{}
	}
	maps.Copy(t.under, styles)
	return t, nil
}

// contrast is the WCAG contrast of two "#rrggbb" colours, 0 if either is
// not one.
func contrast(a, b string) float64 {
	la, aok := luminance(a)
	lb, bok := luminance(b)
	if !aok || !bok {
		return 0
	}
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

func luminance(hex string) (float64, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, false
	}
	var c [3]float64
	for i := range c {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return 0, false
		}
		s := float64(v) / 255
		if s <= 0.03928 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2], true
}
