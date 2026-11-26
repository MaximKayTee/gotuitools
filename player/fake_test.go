package player

import (
	"bytes"
	"encoding/base64"
	"image"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/MaximKayTee/gotuitools/mpv"
)

// fakeEngine stands in for libmpv: emit renders a frame through the
// Size callback, filled with the frame's sequence number.
type fakeEngine struct {
	src mpv.Source
	o   mpv.Options

	mu      sync.Mutex
	st      mpv.Status
	img     *image.RGBA
	seq     uint64
	native  image.Point
	calls   []string
	redraws int
	done    chan struct{}
	closes  int
}

func (f *fakeEngine) call(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

// emit renders one frame at native; false when Size gives none.
func (f *fakeEngine) emit(native image.Point) bool {
	sz := f.o.Size(native)
	if sz.X <= 0 || sz.Y <= 0 {
		return false
	}
	f.mu.Lock()
	f.native = native
	f.seq++
	img := image.NewRGBA(image.Rectangle{Max: sz})
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(f.seq), 7, 9, 255
	}
	f.img = img
	f.mu.Unlock()
	f.o.OnFrame()
	return true
}

// set changes the status and runs OnChange.
func (f *fakeEngine) set(fn func(*mpv.Status)) {
	f.mu.Lock()
	fn(&f.st)
	f.mu.Unlock()
	f.o.OnChange()
}

func (f *fakeEngine) Frame() (*image.RGBA, uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.img, f.seq, f.img != nil
}

func (f *fakeEngine) Redraw() {
	f.mu.Lock()
	f.redraws++
	n := f.native
	f.mu.Unlock()
	if n != (image.Point{}) {
		f.emit(n)
	}
}

func (f *fakeEngine) Status() mpv.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st
}

func (f *fakeEngine) SetPause(v bool) {
	f.call("pause " + strconv.FormatBool(v))
	f.set(func(s *mpv.Status) { s.Paused = v })
}

func (f *fakeEngine) Play() {
	f.call("play")
	f.set(func(s *mpv.Status) { s.Paused, s.EOF, s.Pos = false, false, 0 })
}

func (f *fakeEngine) SeekBy(d time.Duration) {
	f.call("seek " + d.String())
	f.set(func(s *mpv.Status) { s.Pos += d })
}

func (f *fakeEngine) SeekPercent(frac float64) {
	f.call("seekto " + strconv.FormatFloat(frac, 'f', -1, 64))
	f.set(func(s *mpv.Status) { s.Pos = time.Duration(float64(s.Dur) * frac) })
}

func (f *fakeEngine) SetVolume(v float64) {
	f.call("volume " + strconv.FormatFloat(v, 'f', -1, 64))
	f.set(func(s *mpv.Status) { s.Volume = v })
}

func (f *fakeEngine) SetMute(m bool) {
	f.call("mute " + strconv.FormatBool(m))
	f.set(func(s *mpv.Status) { s.Mute = m })
}

func (f *fakeEngine) SetLoop(on bool) {
	f.call("loop " + strconv.FormatBool(on))
	f.set(func(s *mpv.Status) { s.Loop = on })
}

func (f *fakeEngine) SetSpeed(x float64) {
	f.call("speed " + strconv.FormatFloat(x, 'f', -1, 64))
	f.set(func(s *mpv.Status) { s.Speed = x })
}

func (f *fakeEngine) SetSub(id int) {
	f.call("sub " + strconv.Itoa(id))
	f.set(func(s *mpv.Status) { s.Sub = id })
}

func (f *fakeEngine) SetAudio(id int) {
	f.call("audio " + strconv.Itoa(id))
	f.set(func(s *mpv.Status) { s.Audio = id })
}

func (f *fakeEngine) AddChapter(d int) {
	f.call("chapter " + strconv.Itoa(d))
}

func (f *fakeEngine) SetSpans(spans []mpv.Span) {
	f.call("spans " + strconv.Itoa(len(spans)))
}

func (f *fakeEngine) Rendered() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int(f.seq)
}

func (f *fakeEngine) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	if f.closes == 1 {
		close(f.done)
	}
}

func (f *fakeEngine) Done() <-chan struct{} { return f.done }

func (f *fakeEngine) took() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

// opener records engines.
type opener struct {
	mu   sync.Mutex
	engs []*fakeEngine
	err  error
}

func (op *opener) open(src mpv.Source, o mpv.Options) (engine, error) {
	if op.err != nil {
		return nil, op.err
	}
	f := &fakeEngine{src: src, o: o, done: make(chan struct{}),
		st: mpv.Status{Loaded: true, Volume: o.Volume, Mute: o.Mute, Loop: o.Loop, Dur: 90 * time.Second}}
	op.mu.Lock()
	op.engs = append(op.engs, f)
	op.mu.Unlock()
	return f, nil
}

func (op *opener) last(t *testing.T) *fakeEngine {
	t.Helper()
	op.mu.Lock()
	defer op.mu.Unlock()
	if len(op.engs) == 0 {
		t.Fatal("no engine")
	}
	return op.engs[len(op.engs)-1]
}

func (op *opener) count() int {
	op.mu.Lock()
	defer op.mu.Unlock()
	return len(op.engs)
}

// writes records each Write; delay blocks every write.
type writes struct {
	mu    sync.Mutex
	w     [][]byte
	delay time.Duration
}

func (r *writes) Write(p []byte) (int, error) {
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.w = append(r.w, bytes.Clone(p))
	return len(p), nil
}

func (r *writes) all() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.w...)
}

func (r *writes) take() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.w
	r.w = nil
	return out
}

// frameRe matches a frame write: CUP, a=T keys, the payload of the
// single chunk.
var frameRe = regexp.MustCompile(`^\x1b\[\?2026h\x1b7\x1b\[(\d+);(\d+)H\x1b_Ga=T,f=24,s=(\d+),v=(\d+),t=d,i=(\d+),p=1,c=(\d+),r=(\d+),C=1,q=2(?:,m=[01])?;([^\x1b]*)\x1b\\`)

// frame is a parsed frame write: 1-based CUP, size, cells, first pixel.
type frame struct {
	row, col, w, h, id, cols, rows int
	first                          byte
}

func parseFrame(t *testing.T, b []byte) (frame, bool) {
	t.Helper()
	m := frameRe.FindSubmatch(b)
	if m == nil {
		return frame{}, false
	}
	n := func(i int) int { v, _ := strconv.Atoi(string(m[i])); return v }
	raw, err := base64.StdEncoding.DecodeString(string(m[8]))
	if err != nil || len(raw) == 0 {
		t.Fatalf("payload: %v", err)
	}
	return frame{row: n(1), col: n(2), w: n(3), h: n(4), id: n(5), cols: n(6), rows: n(7), first: raw[0]}, true
}

func frames(t *testing.T, ws [][]byte) []frame {
	var out []frame
	for _, w := range ws {
		if f, ok := parseFrame(t, w); ok {
			out = append(out, f)
		}
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}
