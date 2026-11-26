package player

import (
	"context"
	"slices"
	"testing"

	"github.com/MaximKayTee/gotuitools/mpv"
)

// A track picked is found in the files after by language, name and
// place: of two English subtitle tracks the one picked, not mpv's first;
// renamed, the one at its place; an untagged audio track too. Off stays
// off. A file without the language is left to mpv.
func TestPicksHeld(t *testing.T) {
	p, op, _ := newTest(t, Options{})
	ctx := context.Background()
	subs := []Sub{{ID: 1, Lang: "eng", Title: "Signs & Songs"}, {ID: 2, Lang: "eng", Title: "Dialogue"}, {ID: 3, Lang: "spa"}}
	audios := []Audio{{ID: 1, Title: "Stereo"}, {ID: 2, Title: "Commentary"}}
	play := func(name string, subs []Sub, audios []Audio) *fakeEngine {
		t.Helper()
		if err := p.Play(ctx, file(name), 0, 0, 40, 12); err != nil {
			t.Fatal(err)
		}
		f := op.last(t)
		f.took()
		// mpv's own picks: the first of each
		f.set(func(s *mpv.Status) { s.Loaded, s.Subs, s.Sub, s.Audios, s.Audio = true, subs, 1, audios, 1 })
		return f
	}
	f := play("/1.mkv", subs, audios)
	if got := f.took(); len(got) != 0 {
		t.Fatalf("nothing picked: %q", got)
	}
	if err := p.SetSub(2); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAudio(2); err != nil {
		t.Fatal(err)
	}

	f = play("/2.mkv", subs, audios)
	if got := f.took(); !slices.Equal(got, []string{"sub 2", "audio 2"}) || f.o.SubLang != "eng" {
		t.Fatalf("same tracks: %q, slang %q", got, f.o.SubLang)
	}
	f.set(func(s *mpv.Status) { s.Pos++ })
	if got := f.took(); len(got) != 0 {
		t.Fatalf("set again: %q", got)
	}
	// the tracks in another order: by name
	f = play("/3.mkv", []Sub{{ID: 1, Lang: "spa"}, {ID: 2, Lang: "eng", Title: "Dialogue"}, {ID: 3, Lang: "eng", Title: "Signs & Songs"}},
		[]Audio{{ID: 1, Title: "Commentary"}, {ID: 2, Title: "Stereo"}})
	if got := f.took(); !slices.Equal(got, []string{"sub 2"}) {
		t.Fatalf("by name: %q", got)
	}
	// renamed: by place among its language's
	f = play("/4.mkv", []Sub{{ID: 1, Lang: "spa"}, {ID: 2, Lang: "eng", Title: "S&S"}, {ID: 3, Lang: "eng", Title: "Full"}}, []Audio{{ID: 1}, {ID: 2}})
	if got := f.took(); !slices.Equal(got, []string{"sub 3", "audio 2"}) {
		t.Fatalf("by place: %q", got)
	}
	// without the language: mpv's
	f = play("/5.mkv", []Sub{{ID: 1, Lang: "spa"}}, []Audio{{ID: 1, Lang: "jpn"}})
	if got := f.took(); len(got) != 0 {
		t.Fatalf("no such language: %q", got)
	}
	// off stays off
	if err := p.SetSub(0); err != nil {
		t.Fatal(err)
	}
	f = play("/6.mkv", subs, audios)
	if got := f.took(); !slices.Equal(got, []string{"audio 2"}) || !f.o.SubOff {
		t.Fatalf("off: %q, SubOff %v", got, f.o.SubOff)
	}
	// kept past the pane: another one, given them, finds the same
	kept := p.Picks()
	if kept.Sub != nil || !kept.SubOff || kept.Audio == nil || *kept.Audio != (Pick{Title: "Commentary", Nth: 1}) {
		t.Fatalf("picks %+v, audio %+v", kept, kept.Audio)
	}
	p2, op2, _ := newTest(t, Options{})
	p2.SetLangs("ja", "en")
	p2.SetPicks(Picks{Audio: kept.Audio, Sub: &Pick{Lang: "eng", Title: "Dialogue", Nth: 1}})
	if err := p2.Play(ctx, file("/1.mkv"), 0, 0, 40, 12); err != nil {
		t.Fatal(err)
	}
	f2 := op2.last(t)
	f2.took()
	f2.set(func(s *mpv.Status) { s.Loaded, s.Subs, s.Sub, s.Audios, s.Audio = true, subs, 1, audios, 1 })
	if got := f2.took(); !slices.Equal(got, []string{"sub 2", "audio 2"}) || f2.o.SubLang != "eng,en" || f2.o.SubOff {
		t.Fatalf("another pane: %q, slang %q, off %v", got, f2.o.SubLang, f2.o.SubOff)
	}
	// forgotten with new languages
	p.SetLangs("", "")
	f = play("/7.mkv", subs, audios)
	if got := f.took(); len(got) != 0 || p.Picks() != (Picks{}) {
		t.Fatalf("after SetLangs: %q, picks %+v", got, p.Picks())
	}
}
