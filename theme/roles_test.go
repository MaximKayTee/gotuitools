package theme

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const plum = `[palette]
text = "#eeeeee"
background = "#111111"
surface = "#222222"
border = "#333333"
muted = "#888888"
accent = "#aa66ff"
accent2 = "#ff66aa"
good = "#66ff99"
warning = "#ffcc66"
bad = "#ff6666"
info = "#66ccff"
`

// A theme can be a palette of the roles and nothing else: every general
// part of the screen is drawn from them.
func TestAPaletteOfRolesIsAWholeTheme(t *testing.T) {
	MustRegister("plum", []byte(plum))
	th, err := Find("plum")
	if err != nil {
		t.Fatal(err)
	}
	for scope, want := range map[string]Style{
		"ui.text":           {Fg: "#eeeeee"},
		"ui.text.inactive":  {Fg: "#888888"},
		"ui.selection":      {Fg: "#111111", Bg: "#aa66ff"},
		"ui.statusline":     {Fg: "#888888", Bg: "#222222"},
		"ui.window":         {Fg: "#333333"},
		"ui.window.focus":   {Fg: "#aa66ff"},
		"ui.menu.selected":  {Fg: "#111111", Bg: "#aa66ff"},
		"error":             {Fg: "#ff6666"},
		"markup.heading":    {Fg: "#aa66ff", Bold: true},
		"markup.list.other": {Fg: "#888888"}, // falls back to markup.list
	} {
		if got := th.Style(scope); got != want {
			t.Errorf("%s: %+v, want %+v", scope, got, want)
		}
	}
	if text, screen := th.Ground(); text == nil || screen == nil {
		t.Errorf("ground: %v %v", text, screen)
	}
	if len(th.Scopes()) != 0 {
		t.Errorf("the theme sets %v itself", th.Scopes())
	}
}

// A program's own parts are written once, with the roles for colours, and
// laid under whichever theme is chosen. What the theme says of a part, or
// of a wider one, stands.
func TestUnder(t *testing.T) {
	MustRegister("plum", []byte(plum))
	own := []byte(`"probe.node" = { fg = "accent2", modifiers = ["bold"] }
"probe.edge" = "border"
"probe.chip" = { fg = "onaccent", bg = "accent" }
"markup.heading.1" = "good"
`)
	plain, _ := Find("plum")
	th, err := plain.Under(own)
	if err != nil {
		t.Fatal(err)
	}
	if got := th.Style("probe.node"); got != (Style{Fg: "#ff66aa", Bold: true}) {
		t.Errorf("node: %+v", got)
	}
	if got := th.Style("probe.edge.dim"); got != (Style{Fg: "#333333"}) {
		t.Errorf("edge: %+v", got)
	}
	if got := th.Style("probe.chip"); got != (Style{Fg: "#111111", Bg: "#aa66ff"}) {
		t.Errorf("chip: %+v", got)
	}
	if got := th.Style("markup.heading.1"); got != (Style{Fg: "#66ff99"}) {
		t.Errorf("heading 1: %+v", got)
	}
	if plain.Style("probe.node") != (Style{}) {
		t.Error("Under changed the theme it was called on")
	}

	// A theme that does name the program's part, or a wider one, is obeyed.
	MustRegister("plum-own", []byte(`"probe.node" = "#010203"`+"\n"+`"markup.heading" = "#040506"`+"\n"+plum))
	named, _ := Find("plum-own")
	th, _ = named.Under(own)
	if th.Style("probe.node") != (Style{Fg: "#010203"}) || th.Style("probe.edge") != (Style{Fg: "#333333"}) {
		t.Errorf("node %+v edge %+v", th.Style("probe.node"), th.Style("probe.edge"))
	}
	if got := th.Style("markup.heading.1"); got != (Style{Fg: "#040506"}) {
		t.Errorf("heading 1 under a theme that sets headings: %+v", got)
	}

	for _, bad := range []string{`"probe.node" = "nocolour"`, "[palette]\nx = \"#000000\"\n", `inherits = "plum"`} {
		if _, err := plain.Under([]byte(bad)); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
}

// A theme written before there were roles still has them: each is the
// colour of the general part drawn with it.
func TestRolesOfAThemeThatNamesNone(t *testing.T) {
	MustRegister("older", []byte(`"ui.text" = "#dddddd"
"ui.background" = { bg = "#101010" }
"ui.text.inactive" = "#777777"
"ui.window" = "#444444"
"ui.window.focus" = "#5555ff"
"ui.statusline" = { fg = "#777777", bg = "#202020" }
"ui.popup" = "#ff55ff"
"error" = "#ff5555"
`))
	th, _ := Find("older")
	th, err := th.Under([]byte(`"probe.a" = "accent"
"probe.b" = { fg = "muted", bg = "surface" }
"probe.c" = "accent2"
"probe.d" = "bad"
"probe.e" = "good"
`))
	if err != nil {
		t.Fatal(err)
	}
	for scope, want := range map[string]Style{
		"probe.a": {Fg: "#5555ff"}, "probe.b": {Fg: "#777777", Bg: "#202020"},
		"probe.c": {Fg: "#ff55ff"}, "probe.d": {Fg: "#ff5555"}, "probe.e": {},
	} {
		if got := th.Style(scope); got != want {
			t.Errorf("%s: %+v, want %+v", scope, got, want)
		}
	}
	if th.Style("ui.text") != (Style{Fg: "#dddddd"}) || th.Colour("accent") == nil || th.Colour("good") != nil {
		t.Errorf("text %+v accent %v good %v", th.Style("ui.text"), th.Colour("accent"), th.Colour("good"))
	}
}

// What is written on the accent is whichever of the text and the
// background reads better there.
func TestOnAccent(t *testing.T) {
	MustRegister("pale", []byte("[palette]\ntext = \"#eeeeee\"\nbackground = \"#111111\"\naccent = \"#ccddff\"\n"))
	MustRegister("deep", []byte("[palette]\ntext = \"#eeeeee\"\nbackground = \"#111111\"\naccent = \"#3322aa\"\n"))
	pale, _ := Find("pale")
	deep, _ := Find("deep")
	if pale.Style("ui.selection").Fg != "#111111" || deep.Style("ui.selection").Fg != "#eeeeee" {
		t.Errorf("on a pale accent %s, on a deep one %s", pale.Style("ui.selection").Fg, deep.Style("ui.selection").Fg)
	}
}

// A theme that inherits a palette and changes a role is drawn with its own.
func TestRolesAreInherited(t *testing.T) {
	MustRegister("plum", []byte(plum))
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "grape.toml"), []byte("inherits = \"plum\"\n[palette]\naccent = \"#00ff00\"\n"), 0o644)
	th, err := Find("grape", dir)
	if err != nil {
		t.Fatal(err)
	}
	if th.Style("ui.window.focus").Fg != "#00ff00" || th.Style("ui.text").Fg != "#eeeeee" {
		t.Errorf("focus %+v text %+v", th.Style("ui.window.focus"), th.Style("ui.text"))
	}
}

// The themes that come with gotuitools name every role, and look as they
// did before there were roles.
func TestBuiltInRoles(t *testing.T) {
	for _, name := range []string{"charmtone", "charmtone-light", "catppuccin", "catppuccin-light", "gruvbox", "gruvbox-light", "tokyonight", "nord", "dracula"} {
		th := mustBuiltIn(name)
		bg := th.Style("ui.background").Bg
		if r := contrast(th.Style("ui.text").Fg, bg); r < 4.5 {
			t.Errorf("%s text contrast %.2f", name, r)
		}
		if r := contrast(th.Style("ui.text.inactive").Fg, bg); r < 3 {
			t.Errorf("%s muted contrast %.2f", name, r)
		}
		for _, role := range Roles() {
			if th.Colour(role) == nil {
				t.Errorf("%s has no %s", name, role)
			}
		}
	}
	th := mustBuiltIn("charmtone")
	if th.Style("ui.selection") != (Style{Fg: "#fffaf1", Bg: "#6b50ff"}) || th.Style("ui.text.focus") != (Style{Fg: "#fffaf1", Bold: true}) {
		t.Errorf("selection %+v focus %+v", th.Style("ui.selection"), th.Style("ui.text.focus"))
	}
}

// One folder holds the themes every program can use, after a program's
// own folder.
func TestDirsHasASharedFolder(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	want := []string{filepath.Join(config, "probe", "themes"), filepath.Join(config, "gotuitools", "themes")}
	if got := Dirs("probe"); !slices.Equal(got, want) {
		t.Errorf("dirs %v", got)
	}
	shared := filepath.Join(config, "gotuitools", "themes")
	os.MkdirAll(shared, 0o755)
	os.WriteFile(filepath.Join(shared, "plum2.toml"), []byte(plum), 0o644)
	for _, program := range []string{"probe", "another"} {
		if th, err := Find("plum2", Dirs(program)...); err != nil || th.Style("ui.window.focus").Fg != "#aa66ff" {
			t.Errorf("%s: %+v %v", program, th.Style("ui.window.focus"), err)
		}
	}
}
