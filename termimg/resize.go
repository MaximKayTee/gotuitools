package termimg

import (
	"image"
	"math"
	"sync"
	"sync/atomic"

	"golang.org/x/image/draw"
)

// Filter is the resize filter.
type Filter int

const (
	// FilterCatmullRom: the source rect as premultiplied RGBA, box
	// averaged by an integer factor when the reduction is over 4x (every
	// source pixel counted once; the remaining ratio is 2x to 4x), then
	// a separable Catmull-Rom pass whose kernel widens with the
	// reduction. x/image/draw's kernel scaler is 20x to 150x slower:
	// float64 per channel over the whole source per call.
	FilterCatmullRom Filter = iota
	// FilterBilinear: x/image/draw ApproxBiLinear; for more than a 2x
	// reduction to twice the size, then 2x2 box averaged. Soft, 3x to
	// 15x faster.
	FilterBilinear
)

// Filter names, as settings take them.
var filterNames = map[string]Filter{"catmull-rom": FilterCatmullRom, "bilinear": FilterBilinear}

// ParseFilter: FilterCatmullRom for an unknown name, false.
func ParseFilter(name string) (Filter, bool) {
	f, ok := filterNames[name]
	return f, ok
}

var current atomic.Int32

// SetFilter sets the filter Resize uses; FilterCatmullRom at start.
func SetFilter(f Filter) { current.Store(int32(f)) }

// CurrentFilter is the filter Resize uses.
func CurrentFilter() Filter { return Filter(current.Load()) }

// ResizeWith is Resize with filter f.
func ResizeWith(f Filter, src image.Image, sr image.Rectangle, w, h int) *image.RGBA {
	sr = sr.Intersect(src.Bounds())
	w, h = max(w, 1), max(h, 1)
	if sr.Empty() {
		return image.NewRGBA(image.Rect(0, 0, w, h))
	}
	if f == FilterBilinear {
		return bilinear(src, sr, w, h)
	}
	img := toRGBA(src, sr)
	if img.Rect.Size() == image.Pt(w, h) {
		return img
	}
	if kx, ky := sr.Dx()/w/2, sr.Dy()/h/2; kx > 1 || ky > 1 {
		img = box(img, max(kx, 1), max(ky, 1))
	}
	return catmullRom(img, w, h)
}

// bilinear is FilterBilinear: ApproxBiLinear in parallel bands of rows
// sharing one transform, 2x2 box averaged after a reduction over 2x.
func bilinear(src image.Image, sr image.Rectangle, w, h int) *image.RGBA {
	two := sr.Dx() > 2*w && sr.Dy() > 2*h
	if two {
		w, h = 2*w, 2*h
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if sr.Size() == dst.Rect.Size() {
		draw.Copy(dst, image.Point{}, src, sr, draw.Src, nil)
		return dst
	}
	// dst pixel (x, y) samples src at sr.Min + (x, y) * sr/dst
	sx, sy := float64(sr.Dx())/float64(w), float64(sr.Dy())/float64(h)
	m := [6]float64{1 / sx, 0, -float64(sr.Min.X) / sx, 0, 1 / sy, -float64(sr.Min.Y) / sy}
	bands(h, 64, func(y0, y1 int) {
		sub := dst.SubImage(image.Rect(0, y0, w, y1)).(*image.RGBA)
		draw.ApproxBiLinear.Transform(sub, m, src, sr, draw.Src, nil)
	})
	if two {
		return box(dst, 2, 2)
	}
	return dst
}

// toRGBA is sr of src as RGBA at origin; src itself when it already is.
func toRGBA(src image.Image, sr image.Rectangle) *image.RGBA {
	if r, ok := src.(*image.RGBA); ok && sr == r.Rect && sr.Min == (image.Point{}) {
		return r
	}
	dst := image.NewRGBA(image.Rect(0, 0, sr.Dx(), sr.Dy()))
	bands(sr.Dy(), 64, func(y0, y1 int) {
		band := image.Rect(0, y0, dst.Rect.Dx(), y1)
		draw.Copy(dst.SubImage(band).(*image.RGBA), band.Min, src, image.Rect(sr.Min.X, sr.Min.Y+y0, sr.Max.X, sr.Min.Y+y1), draw.Src, nil)
	})
	return dst
}

// bands calls f on up to strips row ranges of h rows, each at least
// minRows, in parallel.
func bands(h, minRows int, f func(y0, y1 int)) {
	n := min(strips, max(h/minRows, 1))
	if n == 1 {
		f(0, h)
		return
	}
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { f(h*i/n, h*(i+1)/n) })
	}
	wg.Wait()
}

// box averages kx x ky blocks of src; the remainder under a block is
// dropped. Premultiplied, so alpha stays right.
func box(src *image.RGBA, kx, ky int) *image.RGBA {
	w, h := src.Rect.Dx()/kx, src.Rect.Dy()/ky
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint32(kx * ky)
	bands(h, 32, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			d := dst.Pix[y*dst.Stride : y*dst.Stride+4*w]
			for x := range w {
				var sum [4]uint32
				for yy := range ky {
					row := src.Pix[(y*ky+yy)*src.Stride+4*x*kx:]
					for xx := range kx {
						p := row[4*xx : 4*xx+4]
						sum[0] += uint32(p[0])
						sum[1] += uint32(p[1])
						sum[2] += uint32(p[2])
						sum[3] += uint32(p[3])
					}
				}
				for c := range 4 {
					d[4*x+c] = uint8((sum[c] + n/2) / n)
				}
			}
		}
	})
	return dst
}

// taps is the sampling of one axis: for dst index i, source indices
// first[i] .. first[i]+count-1 weighted by weight[i*count:], padded
// with zeros, normalised to 1. Indices are clamped to the source, so
// edges repeat.
type taps struct {
	first  []int
	weight []float32
	count  int
}

// catmullRomKernel at distance t, support 2.
func catmullRomKernel(t float32) float32 {
	if t < 0 {
		t = -t
	}
	switch {
	case t < 1:
		return (1.5*t-2.5)*t*t + 1
	case t < 2:
		return ((-0.5*t+2.5)*t-4)*t + 2
	}
	return 0
}

// newTaps maps dn dst samples onto sn source samples; the kernel widens
// by the reduction so every source sample contributes.
func newTaps(dn, sn int) taps {
	scale := float32(sn) / float32(dn)
	widen := max(scale, 1)
	support := 2 * widen
	count := int(math.Ceil(float64(2*support))) + 1
	t := taps{first: make([]int, dn), weight: make([]float32, dn*count), count: count}
	for i := range dn {
		centre := (float32(i)+0.5)*scale - 0.5
		lo := int(math.Ceil(float64(centre - support)))
		t.first[i] = lo
		w := t.weight[i*count : (i+1)*count]
		var sum float32
		for k := range count {
			if j := lo + k; float32(j) <= centre+support {
				w[k] = catmullRomKernel((float32(j) - centre) / widen)
				sum += w[k]
			}
		}
		for k := range w {
			w[k] /= sum
		}
	}
	return t
}

// catmullRom is src at w x h: a horizontal pass of the needed source
// rows into float32 per band, then the vertical pass into dst rows.
func catmullRom(src *image.RGBA, w, h int) *image.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	tx, ty := newTaps(w, sw), newTaps(h, sh)
	bands(h, 16, func(y0, y1 int) {
		// source rows the band's dst rows sample
		r0 := max(ty.first[y0], 0)
		r1 := min(ty.first[y1-1]+ty.count, sh)
		tmp := make([]float32, (r1-r0)*w*4)
		for r := r0; r < r1; r++ {
			row := src.Pix[r*src.Stride : r*src.Stride+4*sw]
			out := tmp[(r-r0)*w*4 : (r-r0+1)*w*4]
			for x := range w {
				var acc [4]float32
				wt := tx.weight[x*tx.count : (x+1)*tx.count]
				for k, wk := range wt {
					if wk == 0 {
						continue
					}
					j := min(max(tx.first[x]+k, 0), sw-1)
					p := row[4*j : 4*j+4]
					acc[0] += wk * float32(p[0])
					acc[1] += wk * float32(p[1])
					acc[2] += wk * float32(p[2])
					acc[3] += wk * float32(p[3])
				}
				copy(out[4*x:4*x+4], acc[:])
			}
		}
		for y := y0; y < y1; y++ {
			d := dst.Pix[y*dst.Stride : y*dst.Stride+4*w]
			wt := ty.weight[y*ty.count : (y+1)*ty.count]
			acc := make([]float32, 4*w)
			for k, wk := range wt {
				if wk == 0 {
					continue
				}
				r := min(max(ty.first[y]+k, 0), sh-1)
				row := tmp[(r-r0)*w*4 : (r-r0+1)*w*4]
				for i, v := range row {
					acc[i] += wk * v
				}
			}
			for i := 0; i < 4*w; i += 4 {
				a := clamp8(acc[i+3])
				d[i] = min(clamp8(acc[i]), a)
				d[i+1] = min(clamp8(acc[i+1]), a)
				d[i+2] = min(clamp8(acc[i+2]), a)
				d[i+3] = a
			}
		}
	})
	return dst
}

func clamp8(v float32) uint8 { return uint8(min(max(v+0.5, 0), 255)) }
