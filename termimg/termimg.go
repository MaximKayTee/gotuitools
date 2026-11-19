// Package termimg is terminal image plumbing: the encoder seam (kitty
// graphics, half blocks, none), the out-of-band writer, decoding and normalising,
// resizing and packing, fitting to cells, an LRU and image ids.
package termimg

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/kitty"
)

// Image is a picture ready to transmit: PNG bytes, or raw RGB or RGBA
// pixels, zlib compressed when Z.
type Image struct {
	Format int // kitty f: 100 PNG, 24 RGB, 32 RGBA
	W, H   int
	Data   []byte
	Z      bool
}

// PNG is png bytes of w x h pixels as an Image.
func PNG(png []byte, w, h int) Image { return Image{Format: 100, W: w, H: h, Data: png} }

// Encoder produces kitty graphics escape strings: Kitty, or Blank in
// tests and on terminals without graphics.
type Encoder interface {
	TransmitPNG(id uint32, png []byte) string
	// TransmitImage is kitty.TransmitData of img.
	TransmitImage(id uint32, img Image) string
	VirtualPlace(id, pid uint32, cols, rows int) string
	PlaceholderRow(id, pid uint32, row, cols int) string
	Delete(id uint32) string
}

// Blank emits nothing; placeholder rows are spaces.
type Blank struct{}

func (Blank) TransmitPNG(uint32, []byte) string               { return "" }
func (Blank) TransmitImage(uint32, Image) string              { return "" }
func (Blank) VirtualPlace(uint32, uint32, int, int) string    { return "" }
func (Blank) PlaceholderRow(_, _ uint32, _ int, c int) string { return strings.Repeat(" ", max(c, 0)) }
func (Blank) Delete(uint32) string                            { return "" }

// Kitty is Encoder over package kitty.
type Kitty struct{}

func (Kitty) TransmitPNG(id uint32, png []byte) string { return kitty.TransmitPNG(id, png) }
func (Kitty) TransmitImage(id uint32, img Image) string {
	return kitty.TransmitData(id, img.Format, img.W, img.H, img.Data, img.Z)
}
func (Kitty) VirtualPlace(id, pid uint32, cols, rows int) string {
	return kitty.VirtualPlace(id, pid, cols, rows)
}
func (Kitty) PlaceholderRow(id, pid uint32, row, cols int) string {
	return kitty.PlaceholderRow(id, pid, row, cols)
}
func (Kitty) Delete(id uint32) string { return kitty.Delete(id) }

// Modes are the picture modes Choose takes, for a setting's enum.
var Modes = []string{"auto", "kitty", "blocks", "none"}

// Choose is the encoder for a mode and the mode it resolves to: auto is
// kitty when kittyOK (kitty.Supported), else blocks; an unknown mode is
// auto. A new Blocks each call.
func Choose(mode string, kittyOK bool) (Encoder, string) {
	switch mode {
	case "kitty":
		return Kitty{}, mode
	case "blocks":
		return NewBlocks(), mode
	case "none":
		return Blank{}, mode
	}
	if kittyOK {
		return Kitty{}, "kitty"
	}
	return NewBlocks(), "blocks"
}

// Default is the encoder components use unless given one: Blank until
// a program sets it, Kitty when kitty.Supported.
var Default Encoder = Blank{}

// DefaultCell is the cell pixel size used when unknown.
var DefaultCell = image.Pt(10, 20)

// Cell returns c, or DefaultCell when c has a non-positive side.
func Cell(c image.Point) image.Point {
	if c.X <= 0 || c.Y <= 0 {
		return DefaultCell
	}
	return c
}

// Queue collects escape strings in push order and writes them from a
// tea.Cmd. Concurrent flushes keep push order.
type Queue struct {
	w   io.Writer
	mu  sync.Mutex // buf
	wmu sync.Mutex // writes
	buf []byte
}

// NewQueue writes to w, os.Stdout when nil.
func NewQueue(w io.Writer) *Queue {
	if w == nil {
		w = os.Stdout
	}
	return &Queue{w: w}
}

// Push appends s.
func (q *Queue) Push(s string) {
	if s == "" {
		return
	}
	q.mu.Lock()
	q.buf = append(q.buf, s...)
	q.mu.Unlock()
}

// Flush returns a Cmd writing everything pending, nil when nothing is.
func (q *Queue) Flush() tea.Cmd {
	q.mu.Lock()
	empty := len(q.buf) == 0
	q.mu.Unlock()
	if empty {
		return nil
	}
	return func() tea.Msg {
		q.wmu.Lock()
		defer q.wmu.Unlock()
		q.mu.Lock()
		b := q.buf
		q.buf = nil
		q.mu.Unlock()
		if len(b) > 0 {
			_, _ = q.w.Write(b)
		}
		return nil
	}
}

// Normalize decodes a picture in any format Decode reads and returns
// PNG bytes and pixel size. PNG input is returned as is.
func Normalize(b []byte) (pngData []byte, w, h int, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, 0, 0, err
	}
	if format == "png" {
		return b, cfg.Width, cfg.Height, nil
	}
	img, err := Decode(b)
	if err != nil {
		return nil, 0, 0, err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), cfg.Width, cfg.Height, nil
}

// Fit returns the cell size of a w x h pixel image scaled into at most
// maxCols x maxRows cells, aspect kept. upscale false caps scale at 1.
// Zero when any input is non-positive.
func Fit(w, h, maxCols, maxRows int, cell image.Point, upscale bool) (cols, rows int) {
	if w <= 0 || h <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 0, 0
	}
	cell = Cell(cell)
	s := math.Min(float64(maxCols*cell.X)/float64(w), float64(maxRows*cell.Y)/float64(h))
	if !upscale && s > 1 {
		s = 1
	}
	cols = min(max(int(math.Round(float64(w)*s/float64(cell.X))), 1), maxCols)
	rows = min(max(int(math.Round(float64(h)*s/float64(cell.Y))), 1), maxRows)
	return cols, rows
}
