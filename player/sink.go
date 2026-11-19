package player

import (
	"bytes"
	"compress/zlib"
	"image"
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MaximKayTee/gotuitools/kitty"
)

// placementID is the kitty placement id of every frame.
const placementID = 1

// Geom is a frame's place in a rect.
type Geom struct {
	DCol, DRow int // top-left cell offset in the rect
	Cols, Rows int // cells covered
	W, H       int // frame pixels: the cells' pixels, src's when smaller
}

// Fit scales src into cols x rows cells, aspect kept to the nearest
// cell, centred. The frame is the cells' pixels; a src smaller than
// its cells stays at its own size and kitty scales it up.
func Fit(src image.Point, cols, rows int, cell image.Point) Geom {
	if src.X <= 0 || src.Y <= 0 || cols <= 0 || rows <= 0 || cell.X <= 0 || cell.Y <= 0 {
		return Geom{}
	}
	s := math.Min(float64(cols*cell.X)/float64(src.X), float64(rows*cell.Y)/float64(src.Y))
	c := min(max(int(math.Round(float64(src.X)*s/float64(cell.X))), 1), cols)
	r := min(max(int(math.Round(float64(src.Y)*s/float64(cell.Y))), 1), rows)
	w, h := c*cell.X, r*cell.Y
	if s > 1 {
		w, h = src.X, src.Y
	}
	return Geom{DCol: (cols - c) / 2, DRow: (rows - r) / 2, Cols: c, Rows: r, W: w, H: h}
}

// Sink writes one session's frames under one image id: each frame a
// kitty transmit-and-display in one write,
//
//	CSI ?2026h, ESC 7, CUP, a=T,f=24,s,v,o=z,t=d,i,p=1,c,r,C=1,q=2 in chunks, ESC 8, CSI ?2026l
//
// at the origin plus the frame's cell offset; o=z unless raw. Same id:
// each frame replaces the last. kitty drops the shown frame at the
// first chunk and paints between chunks of a frame over about 1 MB;
// the synchronized update (mode 2026) keeps the last frame up until
// the new one is whole. Frames, moves, hiding and Stop are ordered by
// one lock: no frame follows Stop. Use NewSink.
//
// frameShm writes a frame as a shared memory object instead, the
// command carrying its name:
//
//	a=T,f=32,s,v,t=s,i,p=1,c,r,C=1,q=2
//
// A new object each frame; the terminal unlinks it after reading. At
// most shmPending are unread at a time, the rest of the frames dropped.
// Hiding and Stop unlink the unread ones.
type Sink struct {
	out io.Writer
	id  uint32
	raw bool

	mu       sync.Mutex
	col, row int
	g        Geom // last frame's
	stopped  bool
	shown    bool
	hidden   bool
	frames   int

	pending []shmFrame // written, not seen unlinked; oldest first
	read    bool       // the terminal unlinked one
	wait    time.Duration

	// Frame's caller only
	zbuf bytes.Buffer
	zw   *zlib.Writer
	buf  []byte
	pack []byte
}

// shmFrame is a frame's shared memory object.
type shmFrame struct {
	name string
	at   time.Time
}

// shmPending is the number of shared memory frames written ahead of
// the terminal.
const shmPending = 2

// shmWait is how long the first shared memory frames may stay unread
// before the terminal counts as not reading them.
const shmWait = time.Second

// shmSeq numbers the process's shared memory objects.
var shmSeq atomic.Uint64

// shmResult is what frameShm did with a frame.
type shmResult int

const (
	shmSent    shmResult = iota // written, or dropped while hidden
	shmBusy                     // shmPending frames unread: not written
	shmUnread                   // the terminal reads no shared memory: not written
	shmStopped                  // after Stop
)

// Synchronized update, mode 2026, around a frame.
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// NewSink writes image id to out at origin cell (col, row), 0-based;
// raw: no zlib.
func NewSink(out io.Writer, id uint32, col, row int, raw bool) *Sink {
	return &Sink{out: out, id: id, col: col, row: row, raw: raw, wait: shmWait}
}

// frameShm writes img, g.W x g.H pixels with alpha opaque, as a shared
// memory object at the origin offset by g. One caller at a time, the
// same as Frame's.
func (s *Sink) frameShm(img *image.RGBA, g Geom) shmResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return shmStopped
	}
	if s.hidden {
		return shmSent
	}
	unread := s.pending[:0]
	for _, f := range s.pending {
		if shmExists(f.name) {
			unread = append(unread, f)
		} else {
			s.read = true
		}
	}
	s.pending = unread
	if len(s.pending) >= shmPending {
		if s.read || time.Since(s.pending[0].at) < s.wait {
			return shmBusy
		}
		s.unlinkPending()
		return shmUnread
	}
	name := "/gotuitools-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatUint(shmSeq.Add(1), 10)
	if err := shmWrite(name, s.packed(img)); err != nil {
		s.unlinkPending()
		return shmUnread
	}
	s.pending = append(s.pending, shmFrame{name: name, at: time.Now()})
	s.g = g
	s.buf = append(s.buf[:0], syncBegin...)
	s.buf = kitty.AppendDisplayShm(s.buf, s.id, placementID, s.col+g.DCol, s.row+g.DRow, g.Cols, g.Rows, g.W, g.H, name)
	s.buf = append(s.buf, syncEnd...)
	_, _ = s.out.Write(s.buf)
	s.shown = true
	s.frames++
	return shmSent
}

// packed is img's pixels without the stride's padding: img's own when
// it has none.
func (s *Sink) packed(img *image.RGBA) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if img.Stride == w*4 {
		return img.Pix[:w*4*h]
	}
	if cap(s.pack) < w*4*h {
		s.pack = make([]byte, w*4*h)
	}
	s.pack = s.pack[:w*4*h]
	for y := range h {
		copy(s.pack[y*w*4:(y+1)*w*4], img.Pix[y*img.Stride:])
	}
	return s.pack
}

// unlinkPending removes the shared memory frames the terminal has not
// read; under mu.
func (s *Sink) unlinkPending() {
	for _, f := range s.pending {
		shmRemove(f.name)
	}
	s.pending = s.pending[:0]
}

// Frame writes rgb, g.W x g.H 24-bit pixels, at the origin offset by
// g; false once stopped. Dropped while hidden. One caller at a time.
func (s *Sink) Frame(rgb []byte, g Geom) bool {
	data, z := rgb, !s.raw
	if z {
		s.zbuf.Reset()
		if s.zw == nil {
			s.zw, _ = zlib.NewWriterLevel(&s.zbuf, zlib.BestSpeed)
		} else {
			s.zw.Reset(&s.zbuf)
		}
		_, _ = s.zw.Write(rgb)
		_ = s.zw.Close()
		data = s.zbuf.Bytes()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	if s.hidden {
		return true
	}
	s.g = g
	s.buf = append(s.buf[:0], syncBegin...)
	s.buf = kitty.AppendDisplayRGB(s.buf, s.id, placementID, s.col+g.DCol, s.row+g.DRow, g.Cols, g.Rows, g.W, g.H, data, z)
	s.buf = append(s.buf, syncEnd...)
	_, _ = s.out.Write(s.buf)
	s.shown = true
	s.frames++
	return true
}

// Move re-places the shown frame at origin (col, row).
func (s *Sink) Move(col, row int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.col == col && s.row == row {
		return
	}
	s.col, s.row = col, row
	if !s.shown || s.stopped {
		return
	}
	_, _ = io.WriteString(s.out, kitty.Place(s.id, placementID, col+s.g.DCol, row+s.g.DRow, s.g.Cols, s.g.Rows, 0, 0, 0, 0, 0))
}

// SetOrigin sets the origin of the next frame; the shown one stays.
func (s *Sink) SetOrigin(col, row int) {
	s.mu.Lock()
	s.col, s.row = col, row
	s.mu.Unlock()
}

// SetHidden deletes the shown frame and drops frames while hidden.
func (s *Sink) SetHidden(hidden bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hidden = hidden
	if hidden {
		s.unlinkPending()
	}
	if hidden && s.shown && !s.stopped {
		_, _ = io.WriteString(s.out, kitty.Delete(s.id))
		s.shown = false
	}
}

// Stop deletes the image once shown; no frame after. Idempotent.
func (s *Sink) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	s.unlinkPending()
	if s.shown {
		_, _ = io.WriteString(s.out, kitty.Delete(s.id))
		s.shown = false
	}
}

// Stopped reports Stop.
func (s *Sink) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// Frames is the count of frames written.
func (s *Sink) Frames() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames
}
