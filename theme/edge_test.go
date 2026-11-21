package theme_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/MaximKayTee/gotuitools/theme"
)

func TestColours(t *testing.T) {
	cases := map[string]string{
		"#ABCDEF":   "#abcdef",
		"#11223344": "#112233", // alpha dropped, as Helix does
		"007":       "7",
		"255":       "255",
		"red":       "#ff0000", // the palette wins over a terminal colour name
		"default":   "",        // the terminal's own
		"none":      "",
	}
	dir := t.TempDir()
	for in, want := range cases {
		th, err := theme.Load(write(t, dir, "c.toml", `"x" = "`+in+`"`+"\n[palette]\nred = \"#FF0000\"\n"))
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if got := th.Style("x").Fg; got != want {
			t.Errorf("%s = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"256", "-1", "#1234567", "#ggg000", "", "Red"} {
		if _, err := theme.Load(write(t, dir, "c.toml", `"x" = "`+in+`"`)); err == nil {
			t.Errorf("colour %q was accepted", in)
		}
	}
}

func TestStyleTables(t *testing.T) {
	th, err := theme.Load(write(t, t.TempDir(), "t.toml", `
"ui" = "red"
"ui.text" = { }
"ui.blink" = { modifiers = ["slow_blink", "rapid_blink", "hidden"] }
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := th.Style("ui.text"); got != (theme.Style{}) {
		t.Errorf("an empty table fell back to its parent scope: %+v", got)
	}
	if got := th.Style("ui.blink"); got != (theme.Style{}) {
		t.Errorf("modifiers that cannot be drawn gave %+v", got)
	}
}

func TestMistakesInTables(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"attribute.toml":     `"x" = { colour = "red" }`,
		"modifier.toml":      `"x" = { modifiers = ["blinking"] }`,
		"modifier-type.toml": `"x" = { modifiers = [1] }`,
		"underline.toml":     `"x" = { underline = true }`,
		"fg.toml":            `"x" = { fg = 1 }`,
		"inherits.toml":      `inherits = 1`,
		"palette.toml":       `palette = "red"`,
		"palette-value.toml": "[palette]\nfg = \"nope\"",
		"nested.toml":        "[ui]\ntext = \"red\"",
	} {
		_, err := theme.Load(write(t, dir, name, text))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error %v, want one naming the file", name, err)
		}
	}
}

func TestInheritance(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "base.toml", "\"ui.text\" = \"fg\"\n[palette]\nfg = \"#111111\"\n")
	child := write(t, dir, "child.toml", "inherits = \"base\"\n[palette]\nfg = \"#222222\"\n")
	th, err := theme.Load(child)
	if err != nil {
		t.Fatal(err)
	}
	if got := th.Style("ui.text").Fg; got != "#222222" {
		t.Errorf("the child's palette did not reach the parent's scopes: %q", got)
	}

	orphan := write(t, dir, "orphan.toml", `inherits = "gone"`)
	if _, err := theme.Load(orphan); err == nil || !strings.Contains(err.Error(), "orphan.toml") {
		t.Errorf("a missing parent: %v, want an error naming the child", err)
	}

	write(t, dir, "bad-parent.toml", `"x" = "nope"`)
	if _, err := theme.Load(write(t, dir, "kid.toml", `inherits = "bad-parent"`)); err == nil || !strings.Contains(err.Error(), "bad-parent.toml") {
		t.Errorf("a mistake in a parent: %v, want an error naming the parent", err)
	}

	self := write(t, dir, "self.toml", `inherits = "self"`)
	if _, err := theme.Load(self); err == nil {
		t.Error("a theme that inherits from itself loaded")
	}
}

func TestFindEdges(t *testing.T) {
	if _, err := theme.Find("anything"); err == nil {
		t.Error("Find with no directories found a theme")
	}
	dir := t.TempDir()
	write(t, dir, "broken.toml", `"x" = "nope"`)
	write(t, t.TempDir(), "broken.toml", `"x" = "red"`)
	if _, err := theme.Find("broken", dir); err == nil {
		t.Error("Find skipped a broken theme")
	}
}

func TestOverEdges(t *testing.T) {
	var zero theme.Theme
	th, err := zero.Over(map[string]any{"ui": "blue", "markup": map[string]any{"modifiers": []any{"bold"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := th.Style("ui.text"); got != (theme.Style{Fg: "4"}) {
		t.Errorf("Over on the zero Theme: %+v", got)
	}
	if got := th.Style("markup.bold"); got != (theme.Style{Bold: true}) {
		t.Errorf("Over on the zero Theme: %+v", got)
	}
	if _, err := zero.Over(map[string]any{"ui": 7}); err == nil || !strings.Contains(err.Error(), "ui") {
		t.Errorf("a value that is not a style: %v", err)
	}
}

func TestRenderKeepsText(t *testing.T) {
	for _, text := range []string{"a\tb", "", "  spaced  "} {
		if got := (theme.Style{}).Render(text); got != text {
			t.Errorf("zero Style rendered %q as %q", text, got)
		}
	}
	if got := (theme.Style{Bold: true}).Render("a\tb"); !strings.Contains(got, "a\tb") {
		t.Errorf("a tab was changed: %q", got)
	}
}

func TestConcurrentUse(t *testing.T) {
	th, err := theme.Load(write(t, t.TempDir(), "c.toml", helix))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for _, scope := range []string{"ui.selection", "markup.heading.1", "markup.raw.inline", "canvas.header"} {
				th.Style(scope).Render(scope)
			}
			theme.Default().Style("ui.cursor").Render("x")
		})
	}
	wg.Wait()
}
