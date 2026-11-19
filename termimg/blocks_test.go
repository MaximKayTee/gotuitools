package termimg

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var _ Encoder = (*Blocks)(nil)

// quad is 2 x 2: red over blue on the left, clear over green on the right.
func quad() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	img.Set(0, 1, color.NRGBA{0, 0, 255, 255})
	img.Set(1, 0, color.NRGBA{})
	img.Set(1, 1, color.NRGBA{0, 255, 0, 255})
	return img
}

const quadRow = "\x1b[38;2;255;0;0m\x1b[48;2;0;0;255m▀" + "\x1b[38;2;0;255;0m\x1b[49m▄" + "\x1b[0m"

func TestBlocksImage(t *testing.T) {
	b := NewBlocks()
	rgba := image.NewRGBA(image.Rect(0, 0, 2, 2))
	q := quad()
	for y := range 2 {
		for x := range 2 {
			rgba.Set(x, y, q.At(x, y))
		}
	}
	if s := b.TransmitImage(7, Pack(rgba)) + b.VirtualPlace(7, 1, 2, 1); s != "" {
		t.Fatalf("wrote %q", s)
	}
	if got := b.PlaceholderRow(7, 1, 0, 2); got != quadRow {
		t.Errorf("row %q, want %q", got, quadRow)
	}
}

func TestBlocksPNG(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, quad()); err != nil {
		t.Fatal(err)
	}
	b := NewBlocks()
	b.TransmitPNG(3, buf.Bytes())
	b.VirtualPlace(3, 1, 2, 1)
	if got := b.PlaceholderRow(3, 1, 0, 2); got != quadRow {
		t.Errorf("row %q, want %q", got, quadRow)
	}
}

func TestBlocksWidths(t *testing.T) {
	b := NewBlocks()
	b.TransmitPNG(1, mustPNG(t, 40, 30))
	b.VirtualPlace(1, 1, 8, 3)
	for row, cols := range map[int]int{0: 8, 2: 12, 3: 8, -1: 5} {
		if w := ansi.StringWidth(b.PlaceholderRow(1, 1, row, cols)); w != cols {
			t.Errorf("row %d of %d cols is %d wide", row, cols, w)
		}
	}
	if got := b.PlaceholderRow(1, 2, 0, 4); got != "    " {
		t.Errorf("unplaced %q", got)
	}
	b.Delete(1)
	if got := b.PlaceholderRow(1, 1, 0, 4); got != "    " {
		t.Errorf("deleted %q", got)
	}
}

func TestBlocksReplace(t *testing.T) {
	b := NewBlocks()
	b.TransmitPNG(1, mustPNG(t, 4, 4))
	b.VirtualPlace(1, 1, 2, 1)
	before := b.PlaceholderRow(1, 1, 0, 2)
	var buf bytes.Buffer
	png.Encode(&buf, quad())
	b.TransmitPNG(1, buf.Bytes())
	if after := b.PlaceholderRow(1, 1, 0, 2); after == before || after != quadRow {
		t.Errorf("not redrawn: %q", after)
	}
	if strings.Contains(before, "\x1b[49m") {
		t.Errorf("opaque picture drew clear cells: %q", before)
	}
}

func mustPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
