package theme

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestTerminalThemeUsesOnlyTerminalColours(t *testing.T) {
	for scope, s := range mustBuiltIn("terminal").scopes {
		for _, c := range []string{s.Fg, s.Bg} {
			if n, err := strconv.Atoi(c); c != "" && (err != nil || n > 15) {
				t.Errorf("%s uses %q, not one of the sixteen terminal colours", scope, c)
			}
		}
	}
}

func TestBuiltInThemesLoadAsCopies(t *testing.T) {
	for _, name := range []string{"charmtone", "charmtone-light", "terminal"} {
		want := mustBuiltIn(name)
		data, err := builtInFiles.ReadFile(name + ".toml")
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "mine.toml")
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
		th, err := Load(file)
		if err != nil {
			t.Fatal(err)
		}
		for scope, style := range want.scopes {
			if got := th.Style(scope); got != style {
				t.Errorf("%s: %s = %+v, want %+v", name, scope, got, style)
			}
		}
	}
}
