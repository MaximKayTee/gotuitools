package theme_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/theme"
)

func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const helix = `
"ui.background" = { bg = "bg0" }
"ui.text" = "fg1"
"ui.selection" = { bg = "#3c3836", modifiers = ["bold"] }
"ui.cursor" = { modifiers = ["reversed"] }
"markup" = { fg = "fg1" }
"markup.heading" = { fg = "yellow1", modifiers = ["bold", "italic"] }
"markup.heading.1" = "red"
"markup.strikethrough" = { modifiers = ["crossed_out", "dim"] }
"markup.link.url" = { fg = "light-blue", underline = { style = "line" } }
"markup.raw" = { fg = "gray", modifiers = ["underlined", "slow_blink", "hidden"] }
"diagnostic.error" = { underline = { color = "red", style = "curl" } }
"canvas.header" = { fg = "250", bg = "17" }

[palette]
bg0 = "#282828"
fg1 = "#ebdbb2"
yellow1 = "#fabd2f"
`

func TestLoad(t *testing.T) {
	th, err := theme.Load(write(t, t.TempDir(), "mine.toml", helix))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]theme.Style{
		"ui.background":        {Bg: "#282828"},
		"ui.text":              {Fg: "#ebdbb2"},
		"ui.selection":         {Bg: "#3c3836", Bold: true},
		"ui.cursor":            {Reverse: true},
		"markup.heading":       {Fg: "#fabd2f", Bold: true, Italic: true},
		"markup.heading.1":     {Fg: "1"},                                 // a terminal colour by name
		"markup.heading.2":     {Fg: "#fabd2f", Bold: true, Italic: true}, // falls back to markup.heading
		"markup.bold":          {Fg: "#ebdbb2"},                           // falls back to markup
		"markup.strikethrough": {Strikethrough: true, Dim: true},
		"markup.link.url":      {Fg: "12", Underline: true},
		"markup.raw":           {Fg: "8", Underline: true}, // modifiers that cannot be drawn are skipped
		"markup.raw.block":     {Fg: "8", Underline: true},
		"diagnostic.error":     {Underline: true},
		"canvas.header":        {Fg: "250", Bg: "17"},
		"nothing.like.this":    {},
		"":                     {},
	}
	for scope, want := range cases {
		if got := th.Style(scope); got != want {
			t.Errorf("%s = %+v, want %+v", scope, got, want)
		}
	}
}

func TestTerminalColourNames(t *testing.T) {
	names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "gray",
		"light-red", "light-green", "light-yellow", "light-blue", "light-magenta", "light-cyan", "light-gray", "white"}
	want := []string{"0", "1", "2", "3", "4", "5", "6", "8", "9", "10", "11", "12", "13", "14", "7", "15"}
	var file strings.Builder
	for _, n := range names {
		file.WriteString(`"c.` + n + `" = "` + n + "\"\n")
	}
	th, err := theme.Load(write(t, t.TempDir(), "names.toml", file.String()))
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range names {
		if got := th.Style("c." + n).Fg; got != want[i] {
			t.Errorf("%s = %q, want %q", n, got, want[i])
		}
	}
}

func TestInherits(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "base.toml", helix)
	child := write(t, dir, "child.toml", `
inherits = "base"
"ui.text" = "accent"
"markup.heading" = { fg = "fg1" }

[palette]
accent = "#ff8800"
`)
	th, err := theme.Load(child)
	if err != nil {
		t.Fatal(err)
	}
	for scope, want := range map[string]theme.Style{
		"ui.text":        {Fg: "#ff8800"},
		"ui.background":  {Bg: "#282828"},
		"markup.heading": {Fg: "#ebdbb2"}, // a scope the child sets replaces the parent's whole style
		"ui.selection":   {Bg: "#3c3836", Bold: true},
	} {
		if got := th.Style(scope); got != want {
			t.Errorf("%s = %+v, want %+v", scope, got, want)
		}
	}

	write(t, dir, "loop-a.toml", `inherits = "loop-b"`)
	write(t, dir, "loop-b.toml", `inherits = "loop-a"`)
	if _, err := theme.Load(filepath.Join(dir, "loop-a.toml")); err == nil {
		t.Error("themes that inherit from each other loaded")
	}
	if _, err := theme.Load(write(t, dir, "orphan.toml", `inherits = "no-such-theme"`)); err == nil {
		t.Error("a theme with a missing parent loaded")
	}
}

func TestFind(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	write(t, second, "gruvbox.toml", `"ui.text" = "red"`)
	write(t, second, "both.toml", `"ui.text" = "green"`)
	write(t, first, "both.toml", `"ui.text" = "blue"`)

	for name, want := range map[string]string{"gruvbox": "1", "both": "4"} {
		th, err := theme.Find(name, first, second)
		if err != nil || th.Style("ui.text").Fg != want {
			t.Errorf("Find(%s): %+v, %v", name, th.Style("ui.text"), err)
		}
	}
	for _, name := range []string{"", "default"} {
		th, err := theme.Find(name, first, second)
		if err != nil || th.Style("ui.text") != theme.Default().Style("ui.text") {
			t.Errorf("Find(%q) is not the built-in theme: %v", name, err)
		}
	}
	// Built-in themes are found by name, unless a file takes the name.
	if th, err := theme.Find("terminal", first, second); err != nil || th.Style("ui.selection") != (theme.Style{Reverse: true}) {
		t.Errorf("Find(terminal): %+v, %v", th.Style("ui.selection"), err)
	}
	if th, err := theme.Find("charmtone", first, second); err != nil || th.Style("ui.background").Bg != "#201f26" {
		t.Errorf("Find(charmtone): %+v, %v", th.Style("ui.background"), err)
	}
	write(t, first, "terminal.toml", `"ui.selection" = "red"`)
	if th, _ := theme.Find("terminal", first, second); th.Style("ui.selection").Fg != "1" {
		t.Error("a theme file named like a built-in theme was not used")
	}
	if text, screen := theme.Default().Ground(); text == nil || screen == nil {
		t.Error("the default theme should set the colour of text and of the screen")
	}
	if th, _ := theme.Find("terminal", second); th.Style("ui.background").Bg != "" {
		t.Error("the terminal theme should leave the screen's colour alone")
	} else if text, screen := th.Ground(); text != nil || screen != nil {
		t.Error("Ground of the terminal theme should be nil, nil")
	}
	if _, err := theme.Find("missing", first, second); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("Find of an unknown theme: %v", err)
	}
}

func TestMistakesAreNamed(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"syntax.toml":   `"ui.text" = {`,
		"colour.toml":   `"ui.text" = "not-a-colour"`,
		"hex.toml":      `"ui.text" = "#12345"`,
		"shape.toml":    `"ui.text" = 3`,
		"modifier.toml": `"ui.text" = { modifiers = "bold" }`,
	} {
		_, err := theme.Load(write(t, dir, name, text))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error %v, want one naming the file", name, err)
		}
	}
	if _, err := theme.Load(filepath.Join(dir, "absent.toml")); err == nil {
		t.Error("loading a file that does not exist succeeded")
	}
}

// The built-in theme styles every part the app draws.
func TestDefault(t *testing.T) {
	th := theme.Default()
	for _, scope := range []string{
		"ui.text.focus", "ui.text.inactive", "ui.selection", "ui.cursor", "ui.statusline", "ui.window", "ui.popup", "ui.menu", "ui.menu.selected",
		"markup.heading", "markup.bold", "markup.italic", "markup.strikethrough", "markup.link", "markup.quote", "markup.raw", "markup.list",
		"markup.link.broken", "markup.tag", "markup.callout", "markup.table.header", "markup.separator",
		"canvas.header", "canvas.separator", "canvas.card.focused",
		"widget.checkbox", "widget.date", "widget.tag",
	} {
		if th.Style(scope) == (theme.Style{}) {
			t.Errorf("the built-in theme has no style for %s", scope)
		}
	}
	if th.Style("markup.bold").Bold != true || th.Style("markup.italic").Italic != true || th.Style("markup.strikethrough").Strikethrough != true {
		t.Error("bold, italic and strikethrough should be drawn as such")
	}
}

func TestOver(t *testing.T) {
	base, err := theme.Load(write(t, t.TempDir(), "base.toml", helix))
	if err != nil {
		t.Fatal(err)
	}
	top, err := base.Over(map[string]any{
		"ui.selection":   map[string]any{"fg": "yellow1", "modifiers": []any{"italic"}},
		"markup.heading": "#ffffff",
		"canvas.card":    map[string]any{"bg": "236"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for scope, want := range map[string]theme.Style{
		"ui.selection":     {Fg: "#fabd2f", Bg: "#3c3836", Bold: true, Italic: true}, // laid over what was there
		"markup.heading":   {Fg: "#ffffff", Bold: true, Italic: true},
		"markup.heading.1": {Fg: "1"}, // a more exact scope underneath still wins
		"canvas.card":      {Bg: "236"},
		"ui.text":          {Fg: "#ebdbb2"},
	} {
		if got := top.Style(scope); got != want {
			t.Errorf("%s = %+v, want %+v", scope, got, want)
		}
	}
	if base.Style("ui.selection") != (theme.Style{Bg: "#3c3836", Bold: true}) {
		t.Error("Over changed the theme underneath")
	}
	if same, err := base.Over(nil); err != nil || same.Style("ui.text") != base.Style("ui.text") {
		t.Errorf("Over(nil): %v", err)
	}
	if _, err := base.Over(map[string]any{"ui.text": "no-such-colour"}); err == nil || !strings.Contains(err.Error(), "ui.text") {
		t.Errorf("a bad style was accepted: %v", err)
	}
}

func TestStyle(t *testing.T) {
	under := theme.Style{Fg: "1", Bg: "2", Bold: true}
	if got := (theme.Style{Fg: "3", Italic: true}).Over(under); got != (theme.Style{Fg: "3", Bg: "2", Bold: true, Italic: true}) {
		t.Errorf("Style.Over = %+v", got)
	}
	if got := (theme.Style{}).Over(under); got != under {
		t.Errorf("zero Style over something = %+v", got)
	}

	if got := (theme.Style{}).Render("plain"); got != "plain" {
		t.Errorf("the zero Style changed the text: %q", got)
	}
	for _, s := range []theme.Style{
		{Fg: "#ff0000"}, {Bg: "17"}, {Bold: true}, {Dim: true}, {Italic: true},
		{Underline: true}, {Strikethrough: true}, {Reverse: true},
		{Fg: "#ebdbb2", Bg: "#282828", Bold: true, Underline: true},
	} {
		got := s.Render("héllo wörld")
		if got == "héllo wörld" || ansi.Strip(got) != "héllo wörld" {
			t.Errorf("%+v rendered %q", s, got)
		}
	}
	if got := (theme.Style{Bold: true}).Render("x"); !strings.Contains(got, "\x1b[1m") {
		t.Errorf("bold rendered %q", got)
	}
	if got := (theme.Style{Fg: "#ff0000"}).Render("x"); !strings.Contains(got, "38;2;255;0;0") {
		t.Errorf("a hex colour rendered %q", got)
	}
	if got := (theme.Style{Fg: "1"}).Render("x"); !strings.Contains(got, "\x1b[31m") {
		t.Errorf("terminal colour 1 rendered %q", got)
	}
	if got := (theme.Style{Bg: "250"}).Render("x"); !strings.Contains(got, "48;5;250") {
		t.Errorf("palette colour 250 rendered %q", got)
	}
}
