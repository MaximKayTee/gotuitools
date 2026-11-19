package termimg

import (
	"bytes"
	"compress/zlib"
	"image"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/MaximKayTee/gotuitools/kitty"
)

// Blocks is Encoder drawing pictures as text: each cell two pixels
// stacked, ▀ in the top one's colour on the bottom one's, 24-bit SGR.
// Any terminal with true colour; nothing is written out of band.
// Transmit keeps the picture, VirtualPlace its size, PlaceholderRow draws.
// Pixels under half alpha are left as the terminal's own background.
// Use NewBlocks; safe for concurrent use.
type Blocks struct {
	mu     sync.Mutex
	pics   map[uint32]image.Image
	places map[blockKey]*blockPlace
}

type blockKey struct{ id, pid uint32 }

type blockPlace struct {
	cols, rows int
	px         *image.RGBA // cols x 2*rows, drawn on first use
}

// NewBlocks is an empty Blocks.
func NewBlocks() *Blocks {
	return &Blocks{pics: map[uint32]image.Image{}, places: map[blockKey]*blockPlace{}}
}

// TransmitPNG keeps png as picture id.
func (b *Blocks) TransmitPNG(id uint32, png []byte) string {
	if img, err := Decode(png); err == nil {
		b.keep(id, img)
	}
	return ""
}

// TransmitImage keeps img as picture id.
func (b *Blocks) TransmitImage(id uint32, img Image) string {
	if pic := decodeImage(img); pic != nil {
		b.keep(id, pic)
	}
	return ""
}

func (b *Blocks) keep(id uint32, img image.Image) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pics[id] = img
	for k, p := range b.places {
		if k.id == id {
			p.px = nil
		}
	}
}

// VirtualPlace sizes placement pid of picture id to cols x rows cells.
func (b *Blocks) VirtualPlace(id, pid uint32, cols, rows int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := blockKey{id, pid}
	if p, ok := b.places[k]; ok && p.cols == cols && p.rows == rows {
		return ""
	}
	b.places[k] = &blockPlace{cols: max(cols, 0), rows: max(rows, 0)}
	return ""
}

// PlaceholderRow is row of placement pid of picture id, cols cells wide:
// spaces where there is no such picture, placement or row.
func (b *Blocks) PlaceholderRow(id, pid uint32, row, cols int) string {
	cols = max(cols, 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.places[blockKey{id, pid}]
	pic := b.pics[id]
	if !ok || pic == nil || row < 0 || row >= p.rows || p.cols == 0 {
		return strings.Repeat(" ", cols)
	}
	if p.px == nil {
		p.px = Resize(pic, pic.Bounds(), p.cols, 2*p.rows)
	}
	return blockRow(p.px, row, min(cols, p.cols)) + strings.Repeat(" ", max(cols-p.cols, 0))
}

// Delete forgets picture id and its placements.
func (b *Blocks) Delete(id uint32) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pics, id)
	for k := range b.places {
		if k.id == id {
			delete(b.places, k)
		}
	}
	return ""
}

// blockRow draws cell row y of px, cols cells, ending in a reset.
func blockRow(px *image.RGBA, y, cols int) string {
	var sb strings.Builder
	var fg, bg [4]byte // last set; alpha 0 unset
	for x := range cols {
		top, bot := pixel(px, x, 2*y), pixel(px, x, 2*y+1)
		topOn, botOn := top[3] >= 0x80, bot[3] >= 0x80
		switch {
		case !topOn && !botOn:
			if bg[3] != 0 {
				sb.WriteString("\x1b[49m")
				bg = [4]byte{}
			}
			sb.WriteByte(' ')
		case topOn && !botOn:
			setColour(&sb, &fg, top, 38)
			if bg[3] != 0 {
				sb.WriteString("\x1b[49m")
				bg = [4]byte{}
			}
			sb.WriteString("▀")
		case !topOn && botOn:
			setColour(&sb, &fg, bot, 38)
			if bg[3] != 0 {
				sb.WriteString("\x1b[49m")
				bg = [4]byte{}
			}
			sb.WriteString("▄")
		default:
			setColour(&sb, &fg, top, 38)
			setColour(&sb, &bg, bot, 48)
			sb.WriteString("▀")
		}
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

func pixel(px *image.RGBA, x, y int) [4]byte {
	i := px.PixOffset(px.Rect.Min.X+x, px.Rect.Min.Y+y)
	r, g, b, a := px.Pix[i], px.Pix[i+1], px.Pix[i+2], px.Pix[i+3]
	if a != 0 && a != 0xff {
		r, g, b = unmul(r, a), unmul(g, a), unmul(b, a)
	}
	return [4]byte{r, g, b, a}
}

// setColour writes the SGR for c (38 fg, 48 bg) unless last is c already.
func setColour(sb *strings.Builder, last *[4]byte, c [4]byte, sgr int) {
	c[3] = 0xff
	if *last == c {
		return
	}
	*last = c
	sb.WriteString("\x1b[")
	sb.WriteString(strconv.Itoa(sgr))
	sb.WriteString(";2;")
	sb.WriteString(strconv.Itoa(int(c[0])))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(int(c[1])))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(int(c[2])))
	sb.WriteByte('m')
}

// decodeImage is img's pixels; nil when they cannot be read.
func decodeImage(img Image) image.Image {
	data := img.Data
	if img.Z {
		r, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		data, err = io.ReadAll(r)
		if err != nil {
			return nil
		}
	}
	switch img.Format {
	case kitty.FormatPNG:
		pic, err := Decode(data)
		if err != nil {
			return nil
		}
		return pic
	case kitty.FormatRGB, kitty.FormatRGBA:
		n := 3
		if img.Format == kitty.FormatRGBA {
			n = 4
		}
		if img.W <= 0 || img.H <= 0 || len(data) < img.W*img.H*n {
			return nil
		}
		out := image.NewNRGBA(image.Rect(0, 0, img.W, img.H))
		for i := range img.W * img.H {
			s, d := data[i*n:], out.Pix[i*4:]
			d[0], d[1], d[2], d[3] = s[0], s[1], s[2], 0xff
			if n == 4 {
				d[3] = s[3]
			}
		}
		return out
	}
	return nil
}
