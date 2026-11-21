package theme

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// As Noctalia's kitty template writes it.
const noctaliaKitty = `# Noctalia
color0 #212f30
color1 #fd4663
color2 #75ccd7
color3 #6b8fc7
color4 #857dd0
color5 #a8d4ec
color6 #a3b9dc
color7 #afb6b6
color8 #5f6d6e
color15 #f2f3f3

cursor                #f2f3f3
background            #1a2223
foreground            #f2f3f3
selection_foreground  #afb6b6
selection_background  #212f30
active_border_color   #75ccd7
inactive_border_color #212f30
active_tab_foreground   #1b2122
active_tab_background   #75ccd7
inactive_tab_foreground #afb6b6
inactive_tab_background #212f30
`

// "noctalia" is the kitty colours Noctalia writes, read on each Find,
// listed only while the file is there.
func TestNoctalia(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if slices.Contains(Names(), Noctalia) || File(Noctalia) != "" {
		t.Error("listed without the file")
	}
	if _, err := Find(Noctalia); err == nil {
		t.Error("found without the file")
	}
	file := filepath.Join(config, "kitty", "themes", "noctalia.conf")
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte(noctaliaKitty), 0o644)
	if !slices.Contains(Names(), Noctalia) || File(Noctalia) != file {
		t.Errorf("not listed: %v, file %q", Names(), File(Noctalia))
	}
	th, err := Find(Noctalia)
	if err != nil {
		t.Fatal(err)
	}
	c := th.colours()
	if th.Name() != Noctalia || th.Style("ui.background").Bg != "#1a2223" || th.Style("ui.text").Fg != "#f2f3f3" ||
		c["accent"] != "#75ccd7" || c["onaccent"] != "#1b2122" || c["surface"] != "#212f30" || c["bad"] != "#fd4663" {
		t.Errorf("name %q, background %+v, text %+v, colours %v", th.Name(), th.Style("ui.background"), th.Style("ui.text"), c)
	}
	// read again: a new scheme
	os.WriteFile(file, []byte(strings.Replace(noctaliaKitty, "#1a2223", "#000000", 1)), 0o644)
	if th, _ := Find(Noctalia); th.Style("ui.background").Bg != "#000000" {
		t.Errorf("not read again: %+v", th.Style("ui.background"))
	}
	// a file of that name in a theme folder comes first
	dir := t.TempDir()
	mine := filepath.Join(dir, "noctalia.toml")
	os.WriteFile(mine, []byte(`"ui.background" = { bg = "#123456" }`), 0o644)
	if th, _ := Find(Noctalia, dir); th.Style("ui.background").Bg != "#123456" || File(Noctalia, dir) != mine {
		t.Errorf("the folder's file lost: %+v", th.Style("ui.background"))
	}
	// a colour missing
	os.WriteFile(file, []byte("color1 #fd4663\n"), 0o644)
	if _, err := Find(Noctalia); err == nil || !strings.Contains(err.Error(), "foreground") {
		t.Errorf("missing colour: %v", err)
	}
	if File("charmtone") != "" {
		t.Error("a built-in has a file")
	}
}
