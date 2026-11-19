package pics

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/termimg"
	"github.com/MaximKayTee/gotuitools/text"
)

func synth(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 0x80, 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// msgs runs cmd and the commands of its batches, returning the pics
// messages.
func msgs(cmd tea.Cmd) []Msg {
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case Msg:
		return []Msg{m}
	case tea.BatchMsg:
		var out []Msg
		for _, c := range m {
			out = append(out, msgs(c)...)
		}
		return out
	}
	return nil
}

func newSet(t *testing.T, out *bytes.Buffer, fetched *[]string) *Set {
	data := synth(t, 400, 200)
	return New(Options{On: true, Cell: image.Pt(10, 20), Queue: termimg.NewQueue(out), Encoder: termimg.Kitty{},
		Fetch: func(_ context.Context, kind, key, url string) ([]byte, error) {
			*fetched = append(*fetched, kind+" "+url)
			if strings.HasSuffix(url, "/bad") {
				return nil, errors.New("gone")
			}
			return data, nil
		}})
}

func TestShowFetchPlaceFree(t *testing.T) {
	var out bytes.Buffer
	var fetched []string
	s := newSet(t, &out, &fetched)
	icon, cover := Circle("icon", 64), Whole("cover", 0)

	if pl, ok := s.Show("a", "http://x/a", icon, 4, 2); pl != nil || !ok {
		t.Fatalf("before the fetch: %v %v", pl, ok)
	}
	s.Show("a", "http://x/a", icon, 4, 2) // the same picture twice in a draw: one fetch
	s.Show("a", "http://x/a", cover, 20, 10)
	got := msgs(s.Sweep())
	if len(got) != 2 || len(fetched) != 2 {
		t.Fatalf("fetches %v, messages %d", fetched, len(got))
	}
	if msgs(s.Sweep()) != nil {
		t.Error("a fetch in flight was started again")
	}
	for _, m := range got {
		if m.Err != nil || m.Key != "a" {
			t.Fatalf("message %+v", m)
		}
		s.Take(m)
	}

	ic, ok := s.Show("a", "http://x/a", icon, 4, 2)
	if ic == nil || !ok {
		t.Fatal("icon not placed")
	}
	// a circle is the centred square: 2 rows of 20 px are 4 columns
	if ic.PicCols != 4 || ic.PicRows != 2 {
		t.Errorf("icon cells %dx%d", ic.PicCols, ic.PicRows)
	}
	cv, _ := s.Show("a", "http://x/a", cover, 20, 10)
	if cv == nil || cv.PicCols != 20 || cv.PicRows != 5 {
		t.Fatalf("cover %+v", cv)
	}
	lines := cv.Lines()
	if len(lines) != 10 {
		t.Fatalf("%d lines", len(lines))
	}
	for y, l := range lines {
		if w := text.Width(l); w != 20 {
			t.Errorf("line %d is %d cells", y, w)
		}
		if blank := strings.TrimSpace(l) == ""; blank != (y < 2 || y >= 7) {
			t.Errorf("line %d blank %v", y, blank)
		}
	}
	msgs(s.Sweep())
	sent := out.String()
	if n := strings.Count(sent, "a=T") + strings.Count(sent, "a=t"); n < 2 {
		t.Errorf("%d transmits in %d bytes", n, len(sent))
	}
	if len(s.live) != 2 {
		t.Fatalf("%d live", len(s.live))
	}

	// a draw without them frees both
	out.Reset()
	msgs(s.Sweep())
	if len(s.live) != 0 || !strings.Contains(out.String(), "a=d") {
		t.Errorf("live %d, sent %q", len(s.live), out.String())
	}
	// shown again from the cache: no fetch
	if pl, _ := s.Show("a", "http://x/a", icon, 4, 2); pl == nil {
		t.Error("not shown from the cache")
	}
	if msgs(s.Sweep()) != nil || len(fetched) != 2 {
		t.Errorf("fetched again: %v", fetched)
	}
}

func TestFailedAndOff(t *testing.T) {
	var out bytes.Buffer
	var fetched []string
	s := newSet(t, &out, &fetched)
	kind := Whole("cover", 0)
	s.Show("b", "http://x/bad", kind, 4, 2)
	for _, m := range msgs(s.Sweep()) {
		if m.Err == nil {
			t.Fatal("no error")
		}
		s.Take(m)
	}
	if pl, ok := s.Show("b", "http://x/bad", kind, 4, 2); pl != nil || ok {
		t.Errorf("failed picture: %v %v", pl, ok)
	}
	s.Retry()
	if _, ok := s.Show("b", "http://x/bad", kind, 4, 2); !ok {
		t.Error("not tried again after Retry")
	}
	if _, ok := s.Show("c", "", kind, 4, 2); ok {
		t.Error("no url reported as coming")
	}

	off := New(Options{})
	if pl, ok := off.Show("a", "http://x/a", kind, 4, 2); pl != nil || ok || off.On() {
		t.Error("pictures off still shown")
	}
	var none *Set
	if none.On() || none.Sweep() != nil {
		t.Error("nil set")
	}
	none.Close()
}

func TestBlocksEncoder(t *testing.T) {
	var out bytes.Buffer
	var fetched []string
	s := newSet(t, &out, &fetched)
	s.enc = termimg.NewBlocks()
	kind := Whole("cover", 0)
	s.Show("a", "http://x/a", kind, 8, 4)
	for _, m := range msgs(s.Sweep()) {
		s.Take(m)
	}
	pl, _ := s.Show("a", "http://x/a", kind, 8, 4)
	if pl == nil {
		t.Fatal("not placed")
	}
	if l := pl.Line(1); text.Width(l) != 8 || !strings.Contains(l, "▀") {
		t.Errorf("line %q", l)
	}
	msgs(s.Sweep())
	if out.Len() != 0 {
		t.Errorf("blocks wrote %d bytes out of band", out.Len())
	}
}
