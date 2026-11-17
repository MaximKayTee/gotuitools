package termimg

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"
)

func synth(t *testing.T, w, h int, asJPEG bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 0x80, 0xff})
		}
	}
	var b bytes.Buffer
	var err error
	if asJPEG {
		err = jpeg.Encode(&b, img, nil)
	} else {
		err = png.Encode(&b, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFit(t *testing.T) {
	cell := image.Pt(10, 20)
	for _, c := range []struct {
		w, h, mc, mr int
		up           bool
		cols, rows   int
	}{
		{200, 200, 16, 8, false, 16, 8},
		{200, 100, 16, 8, false, 16, 4},
		{100, 200, 16, 8, false, 8, 8},
		{50, 50, 16, 8, false, 5, 3},
		{50, 50, 16, 8, true, 16, 8},
		{1000, 10, 16, 8, false, 16, 1},
		{0, 10, 16, 8, false, 0, 0},
		{10, 10, 0, 8, false, 0, 0},
	} {
		cols, rows := Fit(c.w, c.h, c.mc, c.mr, cell, c.up)
		if cols != c.cols || rows != c.rows {
			t.Errorf("Fit(%d,%d,%d,%d,%v) = %d,%d want %d,%d", c.w, c.h, c.mc, c.mr, c.up, cols, rows, c.cols, c.rows)
		}
	}
	if c, r := Fit(200, 200, 16, 8, image.Point{}, false); c != 16 || r != 8 {
		t.Errorf("default cell %d,%d", c, r)
	}
}

func TestNormalize(t *testing.T) {
	p := synth(t, 30, 20, false)
	out, w, h, err := Normalize(p)
	if err != nil || w != 30 || h != 20 || !bytes.Equal(out, p) {
		t.Fatalf("png: %v %d %d", err, w, h)
	}
	j := synth(t, 24, 12, true)
	out, w, h, err = Normalize(j)
	if err != nil || w != 24 || h != 12 {
		t.Fatalf("jpeg: %v %d %d", err, w, h)
	}
	if cfg, f, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || f != "png" || cfg.Width != 24 {
		t.Fatalf("jpeg not converted: %v %s", err, f)
	}
	if _, _, _, err := Normalize([]byte("nope")); err == nil {
		t.Fatal("garbage decoded")
	}
}

func TestQueueOrder(t *testing.T) {
	var b bytes.Buffer
	q := NewQueue(&b)
	if q.Flush() != nil {
		t.Fatal("flush on empty")
	}
	q.Push("a")
	f1 := q.Flush()
	q.Push("b")
	q.Push("")
	f2 := q.Flush()
	// second flush first: writes both in push order
	f2()
	f1()
	if b.String() != "ab" {
		t.Fatalf("got %q", b.String())
	}
	var wg sync.WaitGroup
	b.Reset()
	for i := range 50 {
		q.Push(string(rune('A' + i%26)))
		f := q.Flush()
		if f == nil {
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	wg.Wait()
	want := ""
	for i := range 50 {
		want += string(rune('A' + i%26))
	}
	if b.String() != want {
		t.Fatalf("got %q", b.String())
	}
}

func TestBlank(t *testing.T) {
	var e Encoder = Blank{}
	if e.TransmitPNG(1, nil) != "" || e.VirtualPlace(1, 1, 2, 2) != "" || e.Delete(1) != "" || e.PlaceholderRow(1, 1, 0, 3) != "   " {
		t.Fatal("blank")
	}
}
