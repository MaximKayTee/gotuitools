package termimg

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif"  // Decode
	_ "image/jpeg" // Decode
	_ "image/png"  // Decode
	"math"
	"runtime"

	_ "golang.org/x/image/bmp" // Decode
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff" // Decode
	_ "golang.org/x/image/webp" // Decode

	"github.com/MaximKayTee/gotuitools/kitty"
)

// ErrUndecodable is a format Decode does not read.
var ErrUndecodable = errors.New("undecodable image")

// Decodable reports a MIME type Decode reads.
func Decodable(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp", "image/tiff":
		return true
	}
	return false
}

// Decode reads jpeg, png, gif (first frame), webp, bmp, tiff.
func Decode(b []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if errors.Is(err, image.ErrFormat) {
		return nil, ErrUndecodable
	}
	return img, err
}

// FitWithin downscales img so neither side is over maxSide, bilinear.
func FitWithin(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSide && h <= maxSide {
		return img
	}
	if w >= h {
		h, w = max(1, h*maxSide/w), maxSide
	} else {
		w, h = max(1, w*maxSide/h), maxSide
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// Scale resizes src's rect sr (clamped to src) to w x h and packs it.
func Scale(src image.Image, sr image.Rectangle, w, h int) Image {
	return Pack(Resize(src, sr, w, h))
}

// strips: parallel resize bands.
var strips = min(runtime.GOMAXPROCS(0), 8)

// Resize is src's rect sr (clamped to src) at w x h, at least 1 x 1,
// with CurrentFilter.
func Resize(src image.Image, sr image.Rectangle, w, h int) *image.RGBA {
	return ResizeWith(CurrentFilter(), src, sr, w, h)
}

// Pack is img as RGB when every pixel is opaque, else RGBA with
// straight alpha; zlib compressed.
func Pack(img *image.RGBA) Image {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	opaque := true
	for y := 0; y < h && opaque; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+4*w]
		for x := 3; x < len(row); x += 4 {
			if row[x] != 0xff {
				opaque = false
				break
			}
		}
	}
	if opaque {
		rgb := make([]byte, 0, w*h*3)
		for y := range h {
			row := img.Pix[y*img.Stride : y*img.Stride+4*w]
			for x := 0; x < len(row); x += 4 {
				rgb = append(rgb, row[x], row[x+1], row[x+2])
			}
		}
		return Image{Format: kitty.FormatRGB, W: w, H: h, Data: kitty.Deflate(rgb), Z: true}
	}
	rgba := make([]byte, 0, w*h*4)
	for y := range h {
		row := img.Pix[y*img.Stride : y*img.Stride+4*w]
		for x := 0; x < len(row); x += 4 {
			r, g, b, a := row[x], row[x+1], row[x+2], row[x+3]
			if a != 0 && a != 0xff {
				r, g, b = unmul(r, a), unmul(g, a), unmul(b, a)
			}
			rgba = append(rgba, r, g, b, a)
		}
	}
	return Image{Format: kitty.FormatRGBA, W: w, H: h, Data: kitty.Deflate(rgba), Z: true}
}

func unmul(c, a uint8) uint8 { return uint8(min((uint32(c)*0xff+uint32(a)/2)/uint32(a), 0xff)) }

// Circle is the centred square of img with a soft round edge.
func Circle(img image.Image) image.Image {
	r := img.Bounds()
	n := min(r.Dx(), r.Dy())
	x0, y0 := r.Min.X+(r.Dx()-n)/2, r.Min.Y+(r.Dy()-n)/2
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	c := float64(n) / 2
	for y := range n {
		for x := range n {
			d := math.Hypot(float64(x)+0.5-c, float64(y)+0.5-c)
			a := min(max(c-d, 0), 1)
			if a == 0 {
				continue
			}
			px := color.NRGBAModel.Convert(img.At(x0+x, y0+y)).(color.NRGBA)
			px.A = uint8(float64(px.A) * a)
			dst.SetNRGBA(x, y, px)
		}
	}
	return dst
}

// Cover is the rect of a w x h source that fills a box of aspect bw:bh,
// centred: a crop. Contain is all of it.
func Cover(w, h, bw, bh int) image.Rectangle {
	if w <= 0 || h <= 0 || bw <= 0 || bh <= 0 {
		return image.Rect(0, 0, max(w, 0), max(h, 0))
	}
	if w*bh > h*bw { // wider than the box
		cw := h * bw / bh
		return image.Rect((w-cw)/2, 0, (w-cw)/2+cw, h)
	}
	ch := w * bh / bw
	return image.Rect(0, (h-ch)/2, w, (h-ch)/2+ch)
}
