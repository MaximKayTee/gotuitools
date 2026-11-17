package player

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/MaximKayTee/gotuitools/kitty"
)

func TestFit(t *testing.T) {
	c := image.Pt(10, 20)
	for _, tc := range []struct {
		name       string
		src        image.Point
		cols, rows int
		want       Geom
	}{
		{"4:3 in 40x12", image.Pt(320, 240), 40, 12, Geom{DCol: 4, Cols: 32, Rows: 12, W: 320, H: 240}},
		{"16:9 in 40x12", image.Pt(1920, 1080), 40, 12, Geom{Cols: 40, Rows: 11, W: 400, H: 220}},
		{"portrait in 40x12", image.Pt(1080, 1920), 40, 12, Geom{DCol: 13, Cols: 14, Rows: 12, W: 140, H: 240}},
		{"upscale small", image.Pt(32, 24), 80, 24, Geom{DCol: 8, Cols: 64, Rows: 24, W: 32, H: 24}},
		{"wide strip", image.Pt(1000, 10), 10, 10, Geom{DRow: 4, Cols: 10, Rows: 1, W: 100, H: 20}},
		{"one cell", image.Pt(5, 5), 1, 1, Geom{Cols: 1, Rows: 1, W: 5, H: 5}},
		{"no source", image.Pt(0, 5), 4, 4, Geom{}},
		{"no rect", image.Pt(5, 5), 0, 4, Geom{}},
	} {
		if got := Fit(tc.src, tc.cols, tc.rows, c); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestSinkRaw(t *testing.T) {
	out := &writes{}
	s := NewSink(out, 9, 2, 3, true)
	g := Geom{DCol: 1, DRow: 2, Cols: 4, Rows: 2, W: 2, H: 1}
	if !s.Frame([]byte{1, 2, 3, 4, 5, 6}, g) {
		t.Fatal("frame refused")
	}
	ws := out.take()
	if len(ws) != 1 {
		t.Fatalf("%d writes", len(ws))
	}
	got, ok := parseFrame(t, ws[0])
	if want := (frame{row: 6, col: 4, w: 2, h: 1, id: 9, cols: 4, rows: 2, first: 1}); !ok || got != want {
		t.Fatalf("frame %+v, want %+v in %q", got, want, ws[0])
	}
	if !bytes.HasSuffix(ws[0], []byte(syncEnd)) {
		t.Fatalf("no sync end: %q", ws[0])
	}
	// a move re-places the shown frame with its offset
	s.Move(2, 3)
	if w := out.take(); len(w) != 0 {
		t.Fatalf("same origin wrote %q", w)
	}
	s.Move(5, 5)
	if w := out.take(); len(w) != 1 || string(w[0]) != kitty.Place(9, 1, 6, 7, 4, 2, 0, 0, 0, 0, 0) {
		t.Fatalf("move wrote %q", w)
	}
	// a new origin applies to the next frame only
	s.SetOrigin(0, 0)
	if w := out.take(); len(w) != 0 {
		t.Fatalf("origin wrote %q", w)
	}
	s.Frame([]byte{7, 8, 9, 7, 8, 9}, g)
	if f, _ := parseFrame(t, out.take()[0]); f.row != 3 || f.col != 2 || f.first != 7 {
		t.Fatalf("frame at new origin %+v", f)
	}
	// hidden: deleted, frames dropped
	s.SetHidden(true)
	if w := out.take(); len(w) != 1 || string(w[0]) != kitty.Delete(9) {
		t.Fatalf("hide wrote %q", w)
	}
	if !s.Frame([]byte{1, 2, 3, 4, 5, 6}, g) || len(out.take()) != 0 {
		t.Fatal("frame while hidden")
	}
	s.SetHidden(false)
	s.Frame([]byte{1, 2, 3, 4, 5, 6}, g)
	if len(out.take()) != 1 || s.Frames() != 3 {
		t.Fatalf("frames %d", s.Frames())
	}
	// stop: one delete, no frame after
	s.Stop()
	s.Stop()
	if w := out.take(); len(w) != 1 || string(w[0]) != kitty.Delete(9) || !s.Stopped() {
		t.Fatalf("stop wrote %q", w)
	}
	if s.Frame([]byte{1, 2, 3, 4, 5, 6}, g) {
		t.Fatal("frame after stop")
	}
	s.Move(1, 1)
	s.SetHidden(true)
	if w := out.take(); len(w) != 0 {
		t.Fatalf("after stop %q", w)
	}
}

var payloadRe = regexp.MustCompile(`a=T,f=24,s=(\d+),v=(\d+),o=z,[^;]*;([^\x1b]*)\x1b\\`)

func TestSinkZlib(t *testing.T) {
	out := &writes{}
	s := NewSink(out, 9, 0, 0, false)
	rgb := bytes.Repeat([]byte{10, 20, 30}, 64*48)
	for range 2 {
		if !s.Frame(rgb, Geom{Cols: 8, Rows: 3, W: 64, H: 48}) {
			t.Fatal("frame refused")
		}
	}
	ws := out.take()
	if len(ws) != 2 || !bytes.Equal(ws[0], ws[1]) {
		t.Fatalf("%d writes, equal %v", len(ws), len(ws) == 2 && bytes.Equal(ws[0], ws[1]))
	}
	var b64 strings.Builder
	for _, m := range payloadRe.FindAllSubmatch(ws[0], -1) {
		b64.Write(m[3])
	}
	if b64.Len() == 0 {
		t.Fatalf("no o=z frame: %q", ws[0][:min(len(ws[0]), 120)])
	}
	z, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		t.Fatal(err)
	}
	r, err := zlib.NewReader(bytes.NewReader(z))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, rgb) {
		t.Fatalf("payload %d bytes, %v", len(got), err)
	}
}
