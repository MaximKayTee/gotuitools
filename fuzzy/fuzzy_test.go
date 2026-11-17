package fuzzy

import (
	"reflect"
	"testing"
)

func TestScore(t *testing.T) {
	if _, ok := Score("note.new", "nn"); !ok {
		t.Error("nn in note.new")
	}
	if _, ok := Score("note", "ne"); !ok {
		t.Error("ne in note")
	}
	if _, ok := Score("note", "en"); ok {
		t.Error("en out of order")
	}
	a, _ := Score("Open File", "of")
	b, _ := Score("cookoff", "of")
	if a <= b {
		t.Errorf("word starts %d should beat %d", a, b)
	}
}

func TestTier(t *testing.T) {
	for _, c := range []struct {
		q      string
		fields []string
		want   int
	}{
		{"vol", []string{"Volume", "player.volume"}, 0},
		{"lev", []string{"Log level", "logging.level"}, 1},
		{"ume", []string{"Volume", "player.volume"}, 2},
		{"player", []string{"Volume", "player.volume"}, 3},
		{"trash max", []string{"Trash age", "files.trash_max_age"}, 4},
		{"age trash", []string{"Trash age", "files.trash_max_age", "Files"}, 9},
		{"zzz", []string{"Volume"}, -1},
		{"", []string{"x"}, 0},
	} {
		if got := Tier(c.q, c.fields...); got != c.want {
			t.Errorf("Tier(%q, %q) = %d, want %d", c.q, c.fields, got, c.want)
		}
	}
}

func TestFilter(t *testing.T) {
	items := []string{"Settings", "Open settings file", "Quit", "Set theme", "Save session"}
	got := Filter(items, "set", func(s string) []string { return []string{s} })
	want := []string{"Settings", "Set theme", "Open settings file"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tiers: %q", got)
	}
	got = Filter(items, "ss", func(s string) []string { return []string{s} })
	if len(got) == 0 || got[0] != "Save session" {
		t.Errorf("subsequence: %q", got)
	}
	if len(Filter(items, "", func(s string) []string { return []string{s} })) != len(items) {
		t.Error("empty query keeps all")
	}
}
