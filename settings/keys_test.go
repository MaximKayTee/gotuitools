package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaximKayTee/gotuitools/command"
)

type withKeys struct {
	Theme string            `toml:"theme"`
	Keys  map[string]string `toml:"keys"`
}

type withModes struct {
	Keys *map[string]map[string]string `toml:"keys"`
}

var keyCmds = []command.Command{
	{Name: "palette", Title: "Commands", Key: "ctrl+p :"},
	{Name: "top", Title: "Top", Group: "Move", Key: "g,g"},
	{Name: "quit", Title: "Quit", Key: "q"},
	{Name: "secret", Hidden: true, Key: "x"},
}

func keyModes() []KeyMode { return []KeyMode{{"normal", keyCmds}} }

func TestKeysAsSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	st, err := Open(path, withKeys{Theme: "a", Keys: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	st.Keys("keys", keyModes)
	if err := st.Fill(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`# "ctrl+p" = "palette"`, `# "g,g" = "top"`, `# q = "quit"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("defaults lack %s:\n%s", want, data)
		}
	}
	f, ok := st.Field("keys.top")
	if !ok || f.Value != "g,g" || f.Group != "Keys › Move" || f.Label != "Top" || !f.IsDefault || f.Kind != Keys {
		t.Errorf("field %+v", f)
	}
	if f, ok := st.Field("keys.secret"); !ok || f.Value != "x" {
		t.Error("hidden command not shown")
	}
	if got := st.Search("quit"); len(got) == 0 || got[0].Key != "keys.quit" {
		t.Errorf("search %v", got)
	}
	shared := st.Get().Keys
	if _, err := st.Set("keys.palette", "ctrl+k"); err != nil {
		t.Fatal(err)
	}
	if len(shared) != 0 {
		t.Errorf("map handed out changed: %v", shared)
	}
	own := st.Get().Keys
	if own["ctrl+k"] != "palette" || own["ctrl+p"] != "" || own[":"] != "" || len(own) != 3 {
		t.Errorf("own %v", own)
	}
	var k command.Keys
	if err := k.Load("normal", keyCmds, own); err != nil || k.For("normal", "palette") != "ctrl+k" || k.For("normal", "quit") != "q" {
		t.Errorf("loaded %v %v", k.Bindings("normal"), err)
	}
	if _, err := st.Set("keys.quit", "ctrl+k"); err == nil || !strings.Contains(err.Error(), "Commands") {
		t.Errorf("taken keys: %v", err)
	}
	if _, err := st.Set("keys.quit", "g"); err == nil {
		t.Error("prefix of g,g accepted")
	}
	if f, err := st.Set("keys.quit", "q ctrl+q"); err != nil || f.Value != "q ctrl+q" || f.IsDefault {
		t.Errorf("two keys: %+v %v", f, err)
	}
	if f, _ := st.Reset("keys.palette"); f.Value != ": ctrl+p" || !f.IsDefault {
		t.Errorf("reset %+v", f)
	}
	if own := st.Get().Keys; len(own) != 1 || own["ctrl+q"] != "quit" {
		t.Errorf("after reset %v", own)
	}
	if f, _ := st.Set("keys.top", ""); f.Value != "" {
		t.Errorf("unbound %+v", f)
	}
	again, err := Open(path, withKeys{Keys: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	again.Keys("keys", keyModes)
	if f, _ := again.Field("keys.top"); f.Value != "" || f.Default != "g,g" {
		t.Errorf("read back %+v", f)
	}
}

func TestKeysInAModeTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[keys.board]\nz = \"quit\"\n"), 0o644)
	st, err := Open(path, withModes{})
	if err != nil {
		t.Fatal(err)
	}
	st.Keys("keys.normal", keyModes)
	if _, err := st.Set("keys.normal.quit", "ctrl+q"); err != nil {
		t.Fatal(err)
	}
	m := *st.Get().Keys
	if m["normal"]["ctrl+q"] != "quit" || m["normal"]["q"] != "" || m["board"]["z"] != "quit" {
		t.Errorf("tables %v", m)
	}
}

func TestKeysByCommand(t *testing.T) {
	st, _ := Open("", withKeys{Keys: map[string]string{}})
	st.KeysByCommand("keys", keyModes)
	if f, err := st.Set("keys.top", "t g,t"); err != nil || f.Value != "t g,t" {
		t.Errorf("%+v %v", f, err)
	}
	if own := st.Get().Keys; own["top"] != "t g,t" || len(own) != 1 {
		t.Errorf("own %v", own)
	}
	if f, _ := st.Reset("keys.top"); f.Value != "g,g" || len(st.Get().Keys) != 0 {
		t.Errorf("reset %+v", f)
	}
}
