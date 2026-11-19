// Package pics shows fetched pictures by key in boxes of cells: icons
// cut to circles, covers, thumbnails.
//
// A draw calls Show for each picture on screen and lays the Placed
// rows out as text; Sweep after it frees the transmitted pictures no
// longer shown, fetches the missing ones and flushes the escapes.
// A fetched picture comes back as Msg for Take; the next draw shows it.
//
// Pictures go through a termimg.Encoder: kitty virtual placements under
// unicode placeholder cells, or half blocks.
package pics

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/termimg"
	"github.com/MaximKayTee/gotuitools/text"
)

// placementID is the placement id of every picture.
const placementID uint32 = 1

// Kind is how a picture is cut before it is kept. A key shown as two
// kinds is fetched and kept once for each.
type Kind struct {
	// Name tells kinds apart.
	Name string
	// Cut makes the picture kept from the decoded one; nil keeps it.
	Cut func(image.Image) image.Image
}

// Circle is a kind cut to a circle (termimg.Circle), neither side over
// maxSide pixels; 0 no cap.
func Circle(name string, maxSide int) Kind {
	return Kind{Name: name, Cut: func(img image.Image) image.Image {
		return termimg.Circle(within(img, maxSide))
	}}
}

// Whole is a kind kept uncut, neither side over maxSide pixels; 0 no
// cap.
func Whole(name string, maxSide int) Kind {
	return Kind{Name: name, Cut: func(img image.Image) image.Image { return within(img, maxSide) }}
}

func within(img image.Image, maxSide int) image.Image {
	if maxSide <= 0 {
		return img
	}
	return termimg.FitWithin(img, maxSide)
}

// Options configure a Set.
type Options struct {
	// On: pictures are shown; false and Show reports none.
	On bool
	// Cell is the pixel size of a cell; termimg.DefaultCell.
	Cell image.Point
	// Queue takes the escapes, flushed by Sweep.
	Queue *termimg.Queue
	// Encoder: nil is termimg.Kitty.
	Encoder termimg.Encoder
	// Fetch is the picture's bytes, in a format termimg.Decode reads.
	// Called off the update loop, MaxFetch at a time.
	Fetch func(ctx context.Context, kind, key, url string) ([]byte, error)
	// Max is the number of decoded pictures kept; 600.
	Max int
	// MaxFetch is the number of fetches in flight; 6.
	MaxFetch int
	// Timeout ends a fetch; 20 s.
	Timeout time.Duration
	// IDs are the image ids used; nil is 1 up to 0xFFFF00.
	IDs *termimg.IDs
}

// Msg is a fetched picture, for Take.
type Msg struct {
	Key string
	Err error

	ck  string
	pic pic
}

// pic is a cut picture as PNG.
type pic struct {
	png  []byte
	w, h int
}

type live struct {
	id         uint32
	cols, rows int
}

type want struct {
	ck, key, url string
	kind         Kind
}

// Set shows pictures by key. Use New; one goroutine, the update loop's.
type Set struct {
	o   Options
	enc termimg.Encoder
	ids *termimg.IDs

	cache    *termimg.LRU[string, pic]
	live     map[string]live
	used     map[string]bool
	want     []want
	inflight map[string]bool
	failed   map[string]bool
	ctx      context.Context
	cancel   context.CancelFunc
}

// New is an empty Set.
func New(o Options) *Set {
	o.Cell = termimg.Cell(o.Cell)
	if o.Max <= 0 {
		o.Max = 600
	}
	if o.MaxFetch <= 0 {
		o.MaxFetch = 6
	}
	if o.Timeout <= 0 {
		o.Timeout = 20 * time.Second
	}
	s := &Set{o: o, enc: o.Encoder, ids: o.IDs, cache: termimg.NewLRU[string, pic](o.Max),
		live: map[string]live{}, used: map[string]bool{}, inflight: map[string]bool{}, failed: map[string]bool{}}
	if s.enc == nil {
		s.enc = termimg.Kitty{}
	}
	if s.ids == nil {
		s.ids = &termimg.IDs{Lo: 1, Hi: 0xFFFF00}
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s
}

// On reports pictures shown.
func (s *Set) On() bool { return s != nil && s.o.On }

// Placed is a picture placed in a box of cells, centred, aspect kept.
type Placed struct {
	// Cols, Rows: the box. PicCols, PicRows: the picture's cells in it.
	Cols, Rows       int
	PicCols, PicRows int

	id  uint32
	enc termimg.Encoder
}

// Line is box row y: the picture's cells, spaces around them.
func (p *Placed) Line(y int) string {
	top, left := (p.Rows-p.PicRows)/2, (p.Cols-p.PicCols)/2
	if y < top || y >= top+p.PicRows {
		return text.Spaces(p.Cols)
	}
	return text.Spaces(left) + p.enc.PlaceholderRow(p.id, placementID, y-top, p.PicCols) + text.Spaces(p.Cols-left-p.PicCols)
}

// Lines is the whole box.
func (p *Placed) Lines() []string {
	out := make([]string, p.Rows)
	for y := range out {
		out[y] = p.Line(y)
	}
	return out
}

// Show is the picture key, cut as kind, in a box of cols x rows cells.
// nil while the picture is not there; ok false when it never will be:
// pictures off, no url, a failed fetch.
func (s *Set) Show(key, url string, kind Kind, cols, rows int) (pl *Placed, ok bool) {
	ck := kind.Name + "|" + key
	if !s.On() || key == "" || cols <= 0 || rows <= 0 || s.failed[ck] {
		return nil, false
	}
	t, cached := s.cache.Get(ck)
	if !cached {
		if url == "" {
			return nil, false
		}
		if !s.inflight[ck] {
			s.want = append(s.want, want{ck: ck, key: key, url: url, kind: kind})
		}
		return nil, true
	}
	c, r := termimg.Fit(t.w, t.h, cols, rows, s.o.Cell, true)
	if c == 0 {
		return nil, false
	}
	im, shown := s.live[ck]
	switch {
	case !shown:
		id := s.ids.Take()
		if id == 0 {
			return nil, true
		}
		im = live{id: id, cols: c, rows: r}
		s.push(s.enc.TransmitPNG(im.id, t.png))
		s.push(s.enc.VirtualPlace(im.id, placementID, c, r))
		s.live[ck] = im
	case im.cols != c || im.rows != r:
		im.cols, im.rows = c, r
		s.push(s.enc.VirtualPlace(im.id, placementID, c, r))
		s.live[ck] = im
	}
	s.used[ck] = true
	return &Placed{Cols: cols, Rows: rows, PicCols: c, PicRows: r, id: im.id, enc: s.enc}, true
}

func (s *Set) push(esc string) {
	if esc != "" && s.o.Queue != nil {
		s.o.Queue.Push(esc)
	}
}

// Sweep ends a draw: transmitted pictures not shown in it are freed,
// the missing ones fetched, the escapes flushed.
func (s *Set) Sweep() tea.Cmd {
	if s == nil {
		return nil
	}
	for ck, im := range s.live {
		if !s.used[ck] {
			s.push(s.enc.Delete(im.id))
			s.ids.Free(im.id)
			delete(s.live, ck)
		}
	}
	clear(s.used)
	var cmds []tea.Cmd
	for _, w := range s.want {
		if len(s.inflight) >= s.o.MaxFetch || s.inflight[w.ck] || s.o.Fetch == nil {
			continue
		}
		s.inflight[w.ck] = true
		cmds = append(cmds, s.fetch(w))
	}
	s.want = s.want[:0]
	if s.o.Queue != nil {
		if f := s.o.Queue.Flush(); f != nil {
			cmds = append(cmds, f)
		}
	}
	return tea.Batch(cmds...)
}

func (s *Set) fetch(w want) tea.Cmd {
	ctx, fetch, timeout := s.ctx, s.o.Fetch, s.o.Timeout
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		msg := Msg{Key: w.key, ck: w.ck}
		b, err := fetch(ctx, w.kind.Name, w.key, w.url)
		if err == nil {
			msg.pic, err = cut(b, w.kind)
		}
		msg.Err = err
		return msg
	}
}

// Take keeps a fetched picture; the next draw shows it. A failed one is
// not fetched again until Retry.
func (s *Set) Take(m Msg) {
	delete(s.inflight, m.ck)
	if m.Err != nil {
		s.failed[m.ck] = true
		return
	}
	s.cache.Put(m.ck, m.pic)
}

// Retry forgets the failed fetches.
func (s *Set) Retry() { clear(s.failed) }

// Close ends the fetches in flight.
func (s *Set) Close() {
	if s != nil {
		s.cancel()
	}
}

// cut decodes b, cuts it as kind and encodes PNG.
func cut(b []byte, kind Kind) (pic, error) {
	img, err := termimg.Decode(b)
	if err != nil {
		return pic{}, err
	}
	if kind.Cut != nil {
		img = kind.Cut(img)
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return pic{}, err
	}
	return pic{png: buf.Bytes(), w: img.Bounds().Dx(), h: img.Bounds().Dy()}, nil
}
