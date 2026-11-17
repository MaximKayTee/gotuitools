package command

import (
	"errors"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRegistry(t *testing.T) {
	var r Registry
	var got []string
	add := func(name string) error {
		return r.Add(Command{Name: name, Run: func(args []string) tea.Cmd {
			got = append(append(got, name), args...)
			return nil
		}})
	}
	for _, name := range []string{"note.new", "canvas.open"} {
		if err := add(name); err != nil {
			t.Fatal(err)
		}
	}
	if add("note.new") == nil || r.Add(Command{Name: "empty"}) == nil {
		t.Error("duplicate or empty accepted")
	}
	r.Run("note.new Title")
	if !reflect.DeepEqual(got, []string{"note.new", "Title"}) {
		t.Errorf("Run: %v", got)
	}
	if msg := r.Run("nope")(); msg == nil {
		t.Error("unknown command ran")
	}
	boom := errors.New("boom")
	r.MustAdd(Command{Name: "fails", Run: Do(func([]string) error { return boom })})
	if msg, _ := r.Run("fails")().(ErrorMsg); !errors.Is(msg.Err, boom) {
		t.Errorf("error lost: %v", msg)
	}
	off := false
	r.MustAdd(Command{Name: "off", Available: func() bool { return off }, Run: func([]string) tea.Cmd { t.Error("ran unavailable"); return nil }})
	r.Run("off")
	all := r.All()
	if len(all) != 4 || all[0].Name != "note.new" || all[1].Name != "canvas.open" {
		t.Errorf("order: %v", all)
	}
	r.Remove("canvas.open")
	if _, ok := r.Get("canvas.open"); ok || len(r.All()) != 3 {
		t.Error("Remove")
	}
	if c, _ := r.Get("off"); c.Label() != "off" {
		t.Error("Label")
	}
}

func TestKeys(t *testing.T) {
	var k Keys
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(k.Bind("normal", "ctrl+p", "palette.open"))
	must(k.Bind("normal", " g   g ", "canvas.top"))
	must(k.Bind("normal", "g e", "canvas.bottom"))
	must(k.Bind("board", "g", "card.grab"))
	if k.Bind("normal", "g", "x") == nil {
		t.Error("prefix of a longer binding accepted")
	}
	for _, c := range []struct {
		mode    string
		pressed []string
		command string
		more    bool
	}{
		{"normal", []string{"ctrl+p"}, "palette.open", false},
		{"normal", []string{"g"}, "", true},
		{"normal", []string{"g", "g"}, "canvas.top", false},
		{"normal", []string{"g", "x"}, "", false},
		{"board", []string{"g"}, "card.grab", false},
	} {
		if command, more := k.Match(c.mode, c.pressed); command != c.command || more != c.more {
			t.Errorf("Match(%s, %v) = %q, %v", c.mode, c.pressed, command, more)
		}
	}
	must(k.Bind("normal", "ctrl+k", "palette.open"))
	if k.For("normal", "palette.open") != "ctrl+k" || len(k.All("normal", "palette.open")) != 2 {
		t.Errorf("For %q", k.For("normal", "palette.open"))
	}
}

func TestLoadDefaultsAndOwn(t *testing.T) {
	cmds := []Command{
		{Name: "palette", Key: ": ctrl+p"},
		{Name: "top", Key: "g,g"},
		{Name: "settings", Key: "ctrl+,"},
		{Name: "grab"},
		{Name: "help"},
	}
	var k Keys
	if err := k.Load("normal", cmds, map[string]string{"ctrl+p": "", "g": "grab", "F1": "help"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{":": "palette", "ctrl+,": "settings", "g": "grab", "F1": "help"}
	if got := k.Bindings("normal"); !reflect.DeepEqual(got, want) {
		t.Errorf("bindings %v", got)
	}
	if err := k.OwnByCommand("normal", map[string]string{"palette": "ctrl+k p"}); err != nil {
		t.Fatal(err)
	}
	if got := k.All("normal", "palette"); !reflect.DeepEqual(got, []string{"p", "ctrl+k"}) {
		t.Errorf("by command %v", got)
	}
}

func TestDispatcher(t *testing.T) {
	var r Registry
	var ran []string
	r.MustAdd(Command{Name: "top", Run: func([]string) tea.Cmd { ran = append(ran, "top"); return nil }},
		Command{Name: "tab", Run: func(a []string) tea.Cmd { ran = append(ran, "tab"+a[0]); return nil }})
	var k Keys
	k.Bind("normal", "g g", "top")
	k.Bind("normal", "2", "tab 2")
	d := Dispatcher{Keys: &k, Registry: &r}
	key := func(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: []rune(s)[0], Text: s} }
	if _, h := d.Key("normal", key("g")); !h || d.Pending() != "g" {
		t.Error("g pending")
	}
	d.Key("normal", key("g"))
	d.Key("normal", key("2"))
	if _, h := d.Key("normal", key("x")); h {
		t.Error("unbound x handled")
	}
	if !reflect.DeepEqual(ran, []string{"top", "tab2"}) {
		t.Errorf("ran %v", ran)
	}
}

func TestLoadPerModeAndUnknown(t *testing.T) {
	nop := func([]string) tea.Cmd { return nil }
	var reg Registry
	reg.MustAdd(Command{Name: "a", Key: "x", Run: nop}, Command{Name: "b", Key: "y", Run: nop})
	own := map[string]string{"z": "b", "q": "nope", "x": ""}
	var k Keys
	if err := k.Load("one", []Command{{Name: "a", Key: "x"}}, own); err != nil {
		t.Fatal(err)
	}
	if got := k.Bindings("one"); len(got) != 0 {
		t.Errorf("other mode's and unknown entries bound: %v", got)
	}
	if u := Unknown(&reg, own); !reflect.DeepEqual(u, []string{"nope"}) {
		t.Errorf("unknown %v", u)
	}
}
