package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type layered struct {
	Editor struct {
		Default string `toml:"default" enum:"pty,textarea"`
		Command string `toml:"command"`
	} `toml:"editor"`
	Programs *map[string]string `toml:"programs"`
}

func TestLayer(t *testing.T) {
	dir := t.TempDir()
	var def layered
	def.Editor.Default = "pty"
	sys, err := Open(filepath.Join(dir, "system.toml"), def)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "store", ".app", "config.toml")
	local, err := Open(path, layered{}, func(s *Store[layered]) { s.Over("System", sys.Get) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing layer written at open")
	}
	f, _ := local.Field("editor.default")
	if f.Value != "pty" || !f.IsDefault || f.From != "System" {
		t.Errorf("unset: %+v", f)
	}
	sys.Set("editor.default", "textarea")
	if f, _ := local.Field("editor.default"); f.Value != "textarea" || !f.IsDefault {
		t.Errorf("follows base: %+v", f)
	}
	if _, err := local.Set("editor.command", "hx"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `command = "hx"`) || strings.Contains(string(data), "default") {
		t.Errorf("file holds only what is set:\n%s", data)
	}
	if g := local.Get(); g.Editor.Command != "hx" || g.Editor.Default != "textarea" {
		t.Errorf("Get %+v", g)
	}
	if _, err := local.Set("editor.default", "nope"); err == nil {
		t.Error("bad enum accepted")
	}
	if err := local.Update(func(c *layered) { c.Programs = &map[string]string{"lg": "lazygit"} }); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "default") || !strings.Contains(string(data), "lazygit") {
		t.Errorf("Update set an unchanged setting:\n%s", data)
	}
	if f, _ := local.Reset("editor.command"); f.Value != "" || !f.IsDefault {
		t.Errorf("Reset: %+v", f)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "hx") {
		t.Errorf("reset still written:\n%s", data)
	}
	os.WriteFile(path, []byte("[editor]\ndefault = \"pty\"\n"), 0o644)
	if changed, err := local.Reload(); !changed || err != nil {
		t.Fatal("reload", changed, err)
	}
	if f, _ := local.Field("editor.default"); f.Value != "pty" || f.IsDefault {
		t.Errorf("reloaded: %+v", f)
	}
	os.Remove(path)
	if changed, _ := local.Reload(); !changed {
		t.Error("removal not seen")
	}
	if f, _ := local.Field("editor.default"); f.Value != "textarea" || !f.IsDefault {
		t.Errorf("after removal: %+v", f)
	}
}

func TestOpenFillsMissing(t *testing.T) {
	dir := t.TempDir()
	var def layered
	def.Editor.Default = "pty"
	def.Programs = &map[string]string{"top": "htop"}
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[editor]\ncommand = \"hx\"\n"), 0o644)
	if _, err := Open(path, def); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`command = "hx"`, `default = "pty"`, `top = "htop"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("not filled with %s:\n%s", want, data)
		}
	}
	odd := "[editor]\ncommand = \"hx\"\nmystery = 1\n"
	os.WriteFile(path, []byte(odd), 0o644)
	if _, err := Open(path, def); err == nil {
		t.Error("unknown key not reported")
	}
	if data, _ := os.ReadFile(path); string(data) != odd {
		t.Errorf("file with a problem rewritten:\n%s", data)
	}
}
