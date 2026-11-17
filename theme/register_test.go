package theme

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestFindForPicksLightVariant(t *testing.T) {
	dark, err := FindFor("charmtone", true)
	if err != nil || dark.Name() != "charmtone" {
		t.Fatalf("dark: %q %v", dark.Name(), err)
	}
	light, err := FindFor("", false)
	if err != nil || light.Name() != "charmtone-light" {
		t.Fatalf("light: %q %v", light.Name(), err)
	}
	if th, _ := FindFor("terminal", false); th.Name() != "terminal" {
		t.Errorf("no light variant: %q", th.Name())
	}
}

func TestRegisterAndInheritRegistered(t *testing.T) {
	MustRegister("probe", []byte(`"ui.text" = "accent"
"probe.mark" = { fg = "accent", modifiers = ["bold"] }
[palette]
accent = "#112233"
`))
	if th, err := Find("probe"); err != nil || th.Style("probe.mark") != (Style{Fg: "#112233", Bold: true}) {
		t.Fatalf("%+v %v", th.Style("probe.mark"), err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "mine.toml")
	os.WriteFile(file, []byte("inherits = \"probe\"\n\"ui.text\" = \"#445566\"\n"), 0o644)
	th, err := Find("mine", dir)
	if err != nil {
		t.Fatal(err)
	}
	if th.Style("ui.text").Fg != "#445566" || th.Style("probe.mark").Fg != "#112233" {
		t.Errorf("text %+v mark %+v", th.Style("ui.text"), th.Style("probe.mark"))
	}
	if err := Register("bad", []byte(`"ui.text" = "nocolour"`)); err == nil {
		t.Error("bad colour registered")
	}
}

// WCAG contrast: 4.5 for text, 3 for inactive text.
func TestBuiltInContrast(t *testing.T) {
	for _, name := range []string{"charmtone", "charmtone-light"} {
		th := mustBuiltIn(name)
		bg := th.Style("ui.background").Bg
		for scope, min := range map[string]float64{"ui.text": 4.5, "ui.text.focus": 4.5, "error": 4.5, "info": 4.5, "markup.link": 4.5, "ui.text.inactive": 3} {
			if r := contrast(th.Style(scope).Fg, bg); r < min {
				t.Errorf("%s %s on background: contrast %.2f", name, scope, r)
			}
		}
		sel := th.Style("ui.selection")
		if r := contrast(sel.Fg, sel.Bg); r < 4.5 {
			t.Errorf("%s selection contrast %.2f", name, r)
		}
	}
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

func luminance(hex string) float64 {
	var c [3]float64
	for i := range c {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		s := float64(v) / 255
		if s <= 0.03928 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}
