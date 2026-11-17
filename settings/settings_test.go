package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type provider struct {
	URL string `toml:"url"`
	Key string `toml:"key"`
}

type conf struct {
	Theme  string        `toml:"theme" options:"themes"`
	Volume int           `toml:"volume" min:"0" max:"130" help:"Start volume"`
	Wait   time.Duration `toml:"wait" min:"1s"`
	Layout struct {
		Style   string   `toml:"style" enum:"layered,exploded" label:"Layout style"`
		Spacing int      `toml:"spacing" choices:"" min:"1" max:"5"`
		Width   *float64 `toml:"width"`
		Tags    []string `toml:"tags"`
		Grid    [][]int  `toml:"grid"`
	} `toml:"layout"`
	Cache     Bytes               `toml:"cache" restart:"true"`
	Notes     string              `toml:"notes" long:"true"`
	Providers map[string]provider `toml:"providers"`
	Internal  int                 `toml:"internal" settings:"-"`
}

func (c conf) Validate() error {
	if c.Layout.Style == "exploded" && c.Layout.Spacing > 3 {
		return errors.New("exploded needs spacing 3 or less")
	}
	return nil
}

func defaults() conf {
	var c conf
	c.Theme, c.Volume, c.Wait = "charmtone", 100, 30*time.Second
	c.Layout.Style, c.Layout.Spacing = "layered", 2
	c.Cache = 64 << 20
	c.Providers = map[string]provider{"local": {URL: "http://localhost"}}
	return c
}

func open(t *testing.T, path string) (*Store[conf], error) {
	t.Helper()
	s, err := Open(path, defaults())
	if s != nil {
		s.Options("themes", func() []string { return []string{"charmtone", "terminal"} })
	}
	return s, err
}

func TestMissingFileWrittenWithEverySetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	s, err := open(t, path)
	if err != nil {
		t.Fatal(err)
	}
	s.Header("goview settings", "Written by goview; comments are regenerated.")
	s.Write()
	b, _ := os.ReadFile(path)
	f := string(b)
	for _, want := range []string{
		"# goview settings\n", "theme = \"charmtone\"", "# Volume. Start volume\n# 0 to 130\nvolume = 100",
		"wait = \"30s\"", "[layout]\n", "# Layout style\n# One of: layered, exploded\nstyle = \"layered\"",
		"# width = (unset)", "tags = []", "internal = 0", "cache = \"64MiB\"", "# Restart required\n",
		"[providers.local]", "url = \"http://localhost\"", "# One of: charmtone, terminal",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("file lacks %q:\n%s", want, f)
		}
	}
	for _, fl := range s.Fields() {
		if fl.Key == "internal" {
			t.Error("settings:\"-\" field is a setting")
		}
	}
	again, err := open(t, path)
	if err != nil {
		t.Fatalf("reading own file: %v\n%s", err, f)
	}
	if again.Get().Providers["local"].URL != "http://localhost" || again.Get().Cache != 64<<20 {
		t.Errorf("round trip: %+v", again.Get())
	}
}

func TestLoadReportsAndKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(path, []byte(`
volume = 500
wait = "2m"
bogus = 1
[layout]
style = "exploded"
width = 0.5
tags = ["a", "b, c"]
[providers.remote]
url = "https://x"
key = "$KEY"
`), 0o644)
	s, err := open(t, path)
	var le *LoadError
	if !errors.As(err, &le) || len(le.Problems) != 2 {
		t.Fatalf("problems: %v", err)
	}
	if !strings.Contains(err.Error(), "volume") || !strings.Contains(err.Error(), "bogus: unknown setting") {
		t.Errorf("%v", err)
	}
	c := s.Get()
	if c.Volume != 100 || c.Wait != 2*time.Minute || c.Layout.Style != "exploded" || *c.Layout.Width != 0.5 {
		t.Errorf("%+v", c)
	}
	if len(c.Layout.Tags) != 2 || c.Layout.Tags[1] != "b, c" || c.Providers["remote"].Key != "$KEY" {
		t.Errorf("tags %q providers %v", c.Layout.Tags, c.Providers)
	}
}

func TestSetValidatesAndWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	s, _ := open(t, path)
	if _, err := s.Set("volume", "131"); err == nil || !strings.Contains(err.Error(), "0 to 130") {
		t.Errorf("bounds: %v", err)
	}
	if _, err := s.Set("layout.style", "tiled"); err == nil {
		t.Error("enum")
	}
	if _, err := s.Set("theme", "nope"); err == nil {
		t.Error("runtime options")
	}
	if _, err := s.Set("wait", "500ms"); err == nil {
		t.Error("duration min")
	}
	s.Set("layout.spacing", "5")
	if _, err := s.Set("layout.style", "exploded"); err == nil || !strings.Contains(err.Error(), "spacing 3") {
		t.Errorf("whole check: %v", err)
	}
	s.Validate("layout.tags", func(v any) error {
		if len(v.([]string)) > 2 {
			return errors.New("three tags is too many")
		}
		return nil
	})
	if _, err := s.Set("layout.tags", "a, b, c"); err == nil {
		t.Error("key check")
	}
	f, err := s.Set("cache", "1.5GiB")
	if err != nil || f.Value != "1536MiB" || f.IsDefault {
		t.Errorf("bytes %+v %v", f, err)
	}
	f, _ = s.Set("layout.width", "0.25")
	if f.Value != "0.25" || *s.Get().Layout.Width != 0.25 {
		t.Errorf("optional %+v", f)
	}
	f, _ = s.Set("layout.width", "")
	if f.Value != "" || s.Get().Layout.Width != nil {
		t.Error("optional clear")
	}
	f, _ = s.Reset("cache")
	if !f.IsDefault {
		t.Error("reset")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "spacing = 5") {
		t.Errorf("not written:\n%s", b)
	}
	if fs := s.Search("layout"); len(fs) == 0 || fs[0].Key != "layout.style" {
		t.Errorf("search %+v", fs)
	}
	if fs := s.Search("start vol"); len(fs) != 1 || fs[0].Key != "volume" {
		t.Errorf("search help %+v", fs)
	}
}

func TestReloadOutsideEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	s, _ := open(t, path)
	if changed, err := s.Reload(); changed || err != nil {
		t.Fatal("reload of own write")
	}
	b, _ := os.ReadFile(path)
	edited := strings.Replace(string(b), "volume = 100", "volume = 42", 1)
	edited = strings.Replace(edited, `wait = "30s"`, `wait = "soon"`, 1)
	os.WriteFile(path, []byte(edited+"\n"), 0o644)
	changed, err := s.Reload()
	if !changed || s.Get().Volume != 42 || s.Get().Wait != 30*time.Second || err == nil {
		t.Errorf("changed %v vol %d wait %v err %v", changed, s.Get().Volume, s.Get().Wait, err)
	}
	os.WriteFile(path, []byte("volume = [nope"), 0o644)
	if _, err := s.Reload(); err == nil || s.Get().Volume != 42 {
		t.Error("broken file applied")
	}
	// a Set takes in the outside edit first
	os.WriteFile(path, []byte("volume = 7\n"), 0o644)
	s.Set("layout.spacing", "4")
	if c := s.Get(); c.Volume != 7 || c.Layout.Spacing != 4 {
		t.Errorf("sync before set: %+v", c)
	}
}

func TestFieldsAndModeKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	s, _ := open(t, path)
	os.Chmod(path, 0o640)
	s.Set("volume", "5")
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", st.Mode())
	}
	fs := s.Fields()
	byKey := map[string]Field{}
	for _, f := range fs {
		byKey[f.Key] = f
	}
	if fs[0].Key != "theme" || byKey["theme"].Kind != Enum || len(byKey["theme"].Options) != 2 {
		t.Errorf("theme %+v", byKey["theme"])
	}
	if f := byKey["layout.style"]; f.Group != "Layout" || f.Label != "Layout style" {
		t.Errorf("%+v", f)
	}
	if f := byKey["cache"]; f.Kind != ByteSize || !f.Restart || f.Value != "64MiB" {
		t.Errorf("%+v", f)
	}
	if f := byKey["notes"]; !f.Long || f.Kind != String {
		t.Errorf("%+v", f)
	}
	if _, ok := byKey["layout.grid"]; ok {
		t.Error("[][]int is a setting")
	}
	if f := byKey["wait"]; f.Range() != "at least 1s" {
		t.Errorf("range %q", f.Range())
	}
}

func TestSymlinkFollowed(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "c.toml")
	os.MkdirAll(filepath.Dir(real), 0o755)
	link := filepath.Join(dir, "c.toml")
	s, _ := open(t, real)
	os.Symlink(real, link)
	s2, _ := open(t, link)
	s2.Set("volume", "9")
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced")
	}
	s.Reload()
	if s.Get().Volume != 9 {
		t.Error("target not written")
	}
}

func TestMemory(t *testing.T) {
	s, err := open(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if f, err := s.Set("volume", "9"); err != nil || f.Value != "9" || s.Get().Volume != 9 {
		t.Errorf("%+v %v", f, err)
	}
	if changed, err := s.Reload(); changed || err != nil || s.Path() != "" {
		t.Error("memory reload")
	}
}

func TestUpdateAndClearedTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	var wrote string
	s, err := Open(path, defaults(), func(s *Store[conf]) {
		s.Header("probe header")
		s.Options("themes", func() []string { return []string{"charmtone"} })
		wrote = "set up first"
	})
	if err != nil || wrote == "" {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "# probe header") || !strings.Contains(string(b), "# One of: charmtone") {
		t.Errorf("setup not applied before the first write:\n%s", b)
	}
	if err := s.Update(func(c *conf) { c.Providers["remote"] = provider{URL: "https://x"} }); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *conf) { c.Layout.Spacing = 9 }); err == nil || s.Get().Layout.Spacing != 2 {
		t.Errorf("bounds not checked: %v %d", err, s.Get().Layout.Spacing)
	}
	if err := s.Update(func(c *conf) { c.Layout.Style, c.Layout.Spacing = "exploded", 5 }); err == nil {
		t.Error("whole not checked")
	}
	again, _ := open(t, path)
	if again.Get().Providers["remote"].URL != "https://x" {
		t.Errorf("update not written: %v", again.Get().Providers)
	}
	s.Update(func(c *conf) { clear(c.Providers) })
	again, err = open(t, path)
	if err != nil || len(again.Get().Providers) != 0 {
		t.Errorf("cleared map came back: %v %v", again.Get().Providers, err)
	}
}

type themed struct {
	Theme struct {
		Name   string         `toml:"name"`
		Scopes map[string]any `toml:"scopes"`
	} `toml:"theme"`
	Spaces []provider          `toml:"space,omitempty"`
	Keys   map[string]string   `toml:"keys,omitempty"`
	Deep   map[string]provider `toml:"deep"`
}

func TestNestedTablesAndOmitempty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	var def themed
	def.Theme.Name = "charmtone"
	def.Theme.Scopes = map[string]any{"ui.text": "#ffffff", "markup": map[string]any{"fg": "red"}}
	def.Deep = map[string]provider{"a": {URL: "u"}}
	s, err := Open(path, def)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Count(string(b), "[theme]") != 1 || strings.Contains(string(b), "space") || strings.Contains(string(b), "[keys]") {
		t.Errorf("file:\n%s", b)
	}
	again, err := Open(path, themed{})
	if err != nil {
		t.Fatalf("own file unreadable: %v\n%s", err, b)
	}
	if again.Get().Theme.Scopes["ui.text"] != "#ffffff" || again.Get().Deep["a"].URL != "u" || again.Get().Theme.Name != "charmtone" {
		t.Errorf("%+v", again.Get())
	}
	_ = s
}
