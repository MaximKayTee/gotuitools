//go:build linux

package player

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/MaximKayTee/gotuitools/kitty"
)

// shmRe matches a shared memory frame write: CUP, a=T keys, the name.
var shmRe = regexp.MustCompile(`^\x1b\[\?2026h\x1b7\x1b\[(\d+);(\d+)H\x1b_Ga=T,f=32,s=(\d+),v=(\d+),t=s,i=(\d+),p=1,c=(\d+),r=(\d+),C=1,q=2;([^\x1b]*)\x1b\\\x1b8\x1b\[\?2026l$`)

// shmFrameOf is a parsed shared memory frame write and its object's
// name.
func shmFrameOf(t *testing.T, b []byte) (frame, string, bool) {
	t.Helper()
	m := shmRe.FindSubmatch(b)
	if m == nil {
		return frame{}, "", false
	}
	n := func(i int) int { v, _ := strconv.Atoi(string(m[i])); return v }
	name, err := base64.StdEncoding.DecodeString(string(m[8]))
	if err != nil || len(name) == 0 || name[0] != '/' {
		t.Fatalf("name %q: %v", m[8], err)
	}
	return frame{row: n(1), col: n(2), w: n(3), h: n(4), id: n(5), cols: n(6), rows: n(7)}, string(name), true
}

// shmObjects are this process's shared memory objects.
func shmObjects(t *testing.T) []string {
	t.Helper()
	got, err := filepath.Glob(shmDir + "/gotuitools-" + strconv.Itoa(os.Getpid()) + "-*")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// padded is a w x h picture with a stride wider than its rows, every
// byte different from its neighbours, the padding 0xEE.
func padded(w, h int) *image.RGBA {
	stride := (w*4 + 63) &^ 63
	img := &image.RGBA{Pix: bytes.Repeat([]byte{0xEE}, stride*h+64), Stride: stride, Rect: image.Rect(0, 0, w, h)}
	for y := range h {
		for x := range w {
			o := y*stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = byte(x*7+y), byte(x+y*13), byte(x*3+y*5+1), 255
		}
	}
	return img
}

// TestSinkShm: a frame is a shared memory object of the picture's
// pixels, the same as the inline frame's; two unread at most; hiding
// and Stop unlink the unread.
func TestSinkShm(t *testing.T) {
	t.Cleanup(func() {
		for _, o := range shmObjects(t) {
			_ = os.Remove(o)
		}
	})
	out := &writes{}
	s := NewSink(out, 9, 2, 3, true)
	g := Geom{DCol: 1, DRow: 2, Cols: 4, Rows: 2, W: 37, H: 5}
	img := padded(37, 5)
	if r := s.frameShm(img, g); r != shmSent {
		t.Fatalf("frame: %v", r)
	}
	ws := out.take()
	if len(ws) != 1 {
		t.Fatalf("%d writes", len(ws))
	}
	f, name, ok := shmFrameOf(t, ws[0])
	if want := (frame{row: 6, col: 4, w: 37, h: 5, id: 9, cols: 4, rows: 2}); !ok || f != want {
		t.Fatalf("frame %+v, want %+v in %q", f, want, ws[0])
	}
	got, err := os.ReadFile(shmDir + name)
	if err != nil {
		t.Fatal(err)
	}
	// the reference: the inline frame's pixels
	ref := toRGB(nil, img)
	if len(got) != 37*5*4 {
		t.Fatalf("object %d bytes, want %d", len(got), 37*5*4)
	}
	worst := 0
	for i := range 37 * 5 {
		for c := range 3 {
			d := int(got[i*4+c]) - int(ref[i*3+c])
			worst = max(worst, d, -d)
		}
		if got[i*4+3] != 255 {
			t.Fatalf("pixel %d alpha %d", i, got[i*4+3])
		}
	}
	t.Logf("shared memory frame against the inline one, %d pixels: worst difference %d", 37*5, worst)
	if worst != 0 {
		t.Fatalf("worst difference %d", worst)
	}
	// a stride without padding: the same pixels
	tight := image.NewRGBA(image.Rect(0, 0, 37, 5))
	copy(tight.Pix, got)
	if !bytes.Equal(s.packed(tight), got) {
		t.Fatal("tight stride differs")
	}

	// unread: a second frame, then none
	if r := s.frameShm(img, g); r != shmSent {
		t.Fatalf("second frame: %v", r)
	}
	if r := s.frameShm(img, g); r != shmBusy || len(out.take()) != 1 || len(shmObjects(t)) != 2 {
		t.Fatalf("third frame: %v, %d objects", r, len(shmObjects(t)))
	}
	// the terminal reads one
	_ = os.Remove(shmDir + name)
	if r := s.frameShm(img, g); r != shmSent || len(out.take()) != 1 {
		t.Fatalf("after a read: %v", r)
	}
	// read once: unread frames are waited for, however long
	s.wait = time.Nanosecond
	if r := s.frameShm(img, g); r != shmBusy {
		t.Fatalf("unread after a read: %v", r)
	}
	// hidden: unread unlinked, the frame deleted, frames dropped
	s.SetHidden(true)
	if w := out.take(); len(shmObjects(t)) != 0 || len(w) != 1 || string(w[0]) != kitty.Delete(9) {
		t.Fatalf("hide: %d objects, wrote %q", len(shmObjects(t)), w)
	}
	if r := s.frameShm(img, g); r != shmSent || len(out.take()) != 0 || len(shmObjects(t)) != 0 {
		t.Fatalf("frame while hidden: %v", r)
	}
	s.SetHidden(false)
	if r := s.frameShm(img, g); r != shmSent || len(shmObjects(t)) != 1 {
		t.Fatalf("frame after hide: %v", r)
	}
	// stop: unread unlinked, no frame after
	s.Stop()
	if len(shmObjects(t)) != 0 {
		t.Fatalf("stop left %v", shmObjects(t))
	}
	if r := s.frameShm(img, g); r != shmStopped || len(shmObjects(t)) != 0 {
		t.Fatalf("frame after stop: %v", r)
	}
}

// TestSinkShmUnread: frames no terminal reads are given up after the
// wait and unlinked.
func TestSinkShmUnread(t *testing.T) {
	out := &writes{}
	s := NewSink(out, 9, 0, 0, true)
	s.wait = 30 * time.Millisecond
	g := Geom{Cols: 4, Rows: 2, W: 8, H: 4}
	img := padded(8, 4)
	for i := range shmPending {
		if r := s.frameShm(img, g); r != shmSent {
			t.Fatalf("frame %d: %v", i, r)
		}
	}
	if r := s.frameShm(img, g); r != shmBusy {
		t.Fatalf("before the wait: %v", r)
	}
	time.Sleep(40 * time.Millisecond)
	if r := s.frameShm(img, g); r != shmUnread || len(shmObjects(t)) != 0 {
		t.Fatalf("after the wait: %v, objects %v", r, shmObjects(t))
	}
}

// reader is a terminal that reads shared memory frames: each write's
// object is read and unlinked.
type reader struct {
	writes
	mu    sync.Mutex
	first []byte // each frame's first byte
}

func (r *reader) Write(p []byte) (int, error) {
	if m := shmRe.FindSubmatch(p); m != nil {
		name, _ := base64.StdEncoding.DecodeString(string(m[8]))
		if b, err := os.ReadFile(shmDir + string(name)); err == nil && len(b) > 0 {
			r.mu.Lock()
			r.first = append(r.first, b[0])
			r.mu.Unlock()
		}
		_ = os.Remove(shmDir + string(name))
	}
	return r.writes.Write(p)
}

func (r *reader) seen() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return bytes.Clone(r.first)
}

// TestShmFrames: with Shm and a terminal reading, every frame is a
// shared memory one.
func TestShmFrames(t *testing.T) {
	op, out := &opener{}, &reader{}
	p := New(Options{Output: out, CellPx: image.Pt(10, 20), open: op.open, raw: true, every: time.Nanosecond, MaxFPS: 1000, Shm: true})
	t.Cleanup(func() { p.Stop(); p.Wait() })
	if err := p.Play(context.Background(), file("/v.mp4"), 5, 3, 40, 12); err != nil {
		t.Fatal(err)
	}
	f := op.last(t)
	for i := range 3 {
		f.emit(image.Pt(320, 240))
		waitFor(t, "frame", func() bool { return len(out.seen()) == i+1 })
	}
	ws := out.take()
	if len(ws) != 3 || !bytes.Equal(out.seen(), []byte{1, 2, 3}) {
		t.Fatalf("%d writes, frames %v", len(ws), out.seen())
	}
	for _, w := range ws {
		got, _, ok := shmFrameOf(t, w)
		if want := (frame{row: 4, col: 10, w: 320, h: 240, id: int(DefaultImageID), cols: 32, rows: 12}); !ok || got != want {
			t.Fatalf("frame %+v, want %+v in %q", got, want, w)
		}
	}
	p.Stop()
	if o := shmObjects(t); len(o) != 0 {
		t.Fatalf("left %v", o)
	}
}

// TestShmFallback: a terminal that reads no shared memory gets inline
// frames after the wait, the newest first.
func TestShmFallback(t *testing.T) {
	op, out := &opener{}, &writes{}
	p := New(Options{Output: out, CellPx: image.Pt(10, 20), open: op.open, raw: true, every: time.Nanosecond, MaxFPS: 1000,
		Shm: true, shmWait: 30 * time.Millisecond})
	t.Cleanup(func() { p.Stop(); p.Wait() })
	if err := p.Play(context.Background(), file("/v.mp4"), 0, 0, 40, 12); err != nil {
		t.Fatal(err)
	}
	f := op.last(t)
	for i := range 3 {
		f.emit(image.Pt(320, 240))
		if i < shmPending {
			waitFor(t, "shared memory frame", func() bool { return len(out.all()) == i+1 })
		}
	}
	waitFor(t, "inline frame", func() bool { return len(frames(t, out.all())) == 1 })
	if got := frames(t, out.all())[0]; got.first != 3 {
		t.Fatalf("inline frame %+v, want the third", got)
	}
	if o := shmObjects(t); len(o) != 0 {
		t.Fatalf("left %v", o)
	}
	f.emit(image.Pt(320, 240))
	waitFor(t, "second inline frame", func() bool { return len(frames(t, out.all())) == 2 })
	if o := shmObjects(t); len(o) != 0 {
		t.Fatalf("shared memory after the fallback: %v", o)
	}
}
