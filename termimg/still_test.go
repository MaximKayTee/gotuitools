package termimg

import (
	"bytes"
	"compress/zlib"
	"image"
	"image/color"
	"image/png"
	"io"
	"slices"
	"testing"

	"github.com/MaximKayTee/gotuitools/kitty"
)

func inflate(t *testing.T, b []byte) []byte {
	t.Helper()
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDecodable(t *testing.T) {
	for _, m := range []string{"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp", "image/tiff"} {
		if !Decodable(m) {
			t.Errorf("%s", m)
		}
	}
	for _, m := range []string{"image/vnd.adobe.photoshop", "image/heic", "image/avif", "image/jxl", "image/svg+xml", "application/x-krita", "image/x-xcf", ""} {
		if Decodable(m) {
			t.Errorf("%s decodable", m)
		}
	}
}

func TestResizeSizes(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 2100, 3500), image.YCbCrSubsampleRatio420)
	for _, c := range []struct {
		sr   image.Rectangle
		w, h int
	}{
		{src.Rect, 648, 1080}, // more than 2x
		{src.Rect, 1500, 2500},
		{src.Rect, 2100, 3500}, // copy
		{image.Rect(100, 200, 600, 400), 1000, 400},
		{image.Rect(-50, -50, 10, 10), 3, 3}, // clamped
		{src.Rect, 0, 0},                     // at least 1x1
	} {
		for _, f := range []Filter{FilterCatmullRom, FilterBilinear} {
			got := ResizeWith(f, src, c.sr, c.w, c.h).Rect.Size()
			if want := image.Pt(max(c.w, 1), max(c.h, 1)); got != want {
				t.Errorf("filter %d: %v to %dx%d: %v", f, c.sr, c.w, c.h, got)
			}
		}
	}
	if got := Resize(src, image.Rect(5000, 5000, 6000, 6000), 4, 4).Rect.Size(); got != image.Pt(4, 4) {
		t.Errorf("outside: %v", got)
	}
}

func TestResizeAverages(t *testing.T) {
	// 1 px stripes, 8x reduction: the kernel averages them to grey
	src := image.NewGray(image.Rect(0, 0, 400, 400))
	for y := range 400 {
		for x := range 400 {
			if x%2 == 0 {
				src.SetGray(x, y, color.Gray{255})
			}
		}
	}
	for _, f := range []Filter{FilterCatmullRom, FilterBilinear} {
		dst := ResizeWith(f, src, src.Rect, 50, 50)
		for _, p := range [][2]int{{0, 0}, {25, 25}, {49, 10}} {
			if v := dst.RGBAAt(p[0], p[1]).R; v < 100 || v > 155 {
				t.Fatalf("filter %d pixel %v: %d", f, p, v)
			}
		}
	}
	// parallel bands join without seams: equal to one band
	g := image.NewRGBA(image.Rect(0, 0, 300, 1000))
	for i := range g.Pix {
		g.Pix[i] = uint8(i*7 + i/300*13)
	}
	d := Resize(g, g.Rect, 220, 900)
	save := strips
	strips = 1
	one := Resize(g, g.Rect, 220, 900)
	strips = save
	if !bytes.Equal(d.Pix, one.Pix) {
		t.Fatal("bands differ from one band")
	}
	// a step edge stays sharp: within 2 px of the edge after a 3x reduction
	e := image.NewGray(image.Rect(0, 0, 300, 30))
	for y := range 30 {
		for x := 150; x < 300; x++ {
			e.SetGray(x, y, color.Gray{255})
		}
	}
	r := Resize(e, e.Rect, 100, 10)
	if l, h := r.RGBAAt(47, 5).R, r.RGBAAt(52, 5).R; l != 0 || h != 255 {
		t.Fatalf("edge not sharp: %d %d", l, h)
	}
}

func TestPack(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Pix = []byte{10, 20, 30, 255, 40, 50, 60, 255}
	p := Pack(img)
	if p.Format != kitty.FormatRGB || p.W != 2 || p.H != 1 || !p.Z || !slices.Equal(inflate(t, p.Data), []byte{10, 20, 30, 40, 50, 60}) {
		t.Fatalf("opaque %+v", p)
	}
	// premultiplied in, straight alpha out
	img.Pix = []byte{50, 0, 100, 128, 0, 0, 0, 0}
	p = Pack(img)
	if p.Format != kitty.FormatRGBA || !slices.Equal(inflate(t, p.Data), []byte{100, 0, 199, 128, 0, 0, 0, 0}) {
		t.Fatalf("alpha %+v %v", p, inflate(t, p.Data))
	}
}

func pngBytes(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func TestFilters(t *testing.T) {
	for name, want := range map[string]Filter{"catmull-rom": FilterCatmullRom, "bilinear": FilterBilinear} {
		if f, ok := ParseFilter(name); !ok || f != want {
			t.Errorf("%s: %d %v", name, f, ok)
		}
	}
	if _, ok := ParseFilter("lanczos"); ok {
		t.Error("unknown name parsed")
	}
	defer SetFilter(CurrentFilter())
	src := image.NewGray(image.Rect(0, 0, 300, 30))
	for y := range 30 {
		for x := 150; x < 300; x++ {
			src.SetGray(x, y, color.Gray{255})
		}
	}
	SetFilter(FilterBilinear)
	if CurrentFilter() != FilterBilinear {
		t.Fatal("not set")
	}
	// bilinear 3x: the edge spreads over the sampled pixels
	soft := Resize(src, src.Rect, 100, 10)
	SetFilter(FilterCatmullRom)
	sharp := Resize(src, src.Rect, 100, 10)
	if bytes.Equal(soft.Pix, sharp.Pix) {
		t.Fatal("filters agree")
	}
}
