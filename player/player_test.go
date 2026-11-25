package player

import (
	"context"
	"errors"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MaximKayTee/gotuitools/kitty"
	"github.com/MaximKayTee/gotuitools/mpv"
)

// newTest is a pane on fakes: cell 10x20, raw frames, 1000 fps, state
// changes at once.
func newTest(t *testing.T, o Options) (*Pane, *opener, *writes) {
	t.Helper()
	op := &opener{}
	out := &writes{}
	o.Output, o.CellPx, o.open, o.raw, o.every = out, image.Pt(10, 20), op.open, true, time.Nanosecond
	if o.MaxFPS == 0 {
		o.MaxFPS = 1000
	}
	p := New(o)
	t.Cleanup(func() { p.Stop(); p.Wait() })
	return p, op, out
}

// file is a local path source.
func file(path string) Source { return Source{Target: path} }

func TestPlayFrames(t *testing.T) {
	p, op, out := newTest(t, Options{Mute: true, Loop: true, Volume: 70})
	if err := p.Play(context.Background(), file("/v.mp4"), 5, 3, 40, 12); err != nil {
		t.Fatal(err)
	}
	f := op.last(t)
	if f.src.Path != "/v.mp4" || !f.o.Loop || !f.o.Mute || f.o.Volume != 70 {
		t.Fatalf("opened %+v %+v", f.src, f.o)
	}
	// 4:3 in 40x12 cells of 10x20: 32x12 cells, 4 in
	if !f.emit(image.Pt(320, 240)) {
		t.Fatal("no size")
	}
	waitFor(t, "frame", func() bool { return len(frames(t, out.all())) == 1 })
	got := frames(t, out.take())[0]
	want := frame{row: 4, col: 10, w: 320, h: 240, id: int(DefaultImageID), cols: 32, rows: 12, first: 1}
	if got != want {
		t.Fatalf("frame %+v, want %+v", got, want)
	}
	if !p.Playing() || p.State().Target != "/v.mp4" || p.State().Volume != 70 || !p.State().Muted {
		t.Fatalf("state %+v", p.State())
	}
}

func TestMoveResizeKeepSession(t *testing.T) {
	p, op, out := newTest(t, Options{})
	ctx := context.Background()
	p.Play(ctx, file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	f.emit(image.Pt(320, 240))
	waitFor(t, "frame", func() bool { return len(frames(t, out.all())) == 1 })
	out.take()
	// a move: re-placed, no frame, no reopen
	p.Play(ctx, file("/v.mp4"), 2, 1, 40, 12)
	ws := out.take()
	if len(ws) != 1 || string(ws[0]) != kitty.Place(DefaultImageID, 1, 6, 1, 32, 12, 0, 0, 0, 0, 0) {
		t.Fatalf("move wrote %q", ws)
	}
	// a resize: the same handle renders at the new size at the new origin
	f.SeekBy(7 * time.Second)
	f.took()
	p.Play(ctx, file("/v.mp4"), 1, 2, 20, 6)
	waitFor(t, "resized frame", func() bool { return len(frames(t, out.all())) == 1 })
	got := frames(t, out.take())[0]
	if got.w != 160 || got.h != 120 || got.cols != 16 || got.rows != 6 || got.col != 4 || got.row != 3 {
		t.Fatalf("resized %+v", got)
	}
	if op.count() != 1 || p.State().Pos != 7*time.Second || len(f.took()) != 0 {
		t.Fatalf("reopened %d, pos %v", op.count(), p.State().Pos)
	}
	// a frame of the old size after the resize is not sent
	f.mu.Lock()
	f.img = image.NewRGBA(image.Rect(0, 0, 320, 240))
	f.seq++
	f.mu.Unlock()
	f.o.OnFrame()
	time.Sleep(20 * time.Millisecond)
	if n := len(frames(t, out.take())); n != 0 {
		t.Fatalf("%d stale frames", n)
	}
}

func TestStopDeletes(t *testing.T) {
	p, op, out := newTest(t, Options{})
	p.Play(context.Background(), file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	done := p.Done()
	f.emit(image.Pt(320, 240))
	waitFor(t, "frame", func() bool { return len(frames(t, out.all())) == 1 })
	out.take()
	p.Stop()
	p.Wait()
	ws := out.take()
	if len(ws) != 1 || string(ws[0]) != kitty.Delete(DefaultImageID) {
		t.Fatalf("stop wrote %q", ws)
	}
	select {
	case <-done:
	default:
		t.Fatal("done open")
	}
	select {
	case <-f.Done():
	default:
		t.Fatal("engine not closed")
	}
	if p.Playing() || p.State().Playing {
		t.Fatal("playing after stop")
	}
	f.emit(image.Pt(320, 240))
	time.Sleep(20 * time.Millisecond)
	if ws := out.take(); len(ws) != 0 {
		t.Fatalf("after stop: %q", ws)
	}
	p.Stop()
	var nilPane *Pane
	nilPane.Stop()
	nilPane.Hide()
}

func TestNewTargetReopens(t *testing.T) {
	p, op, out := newTest(t, Options{})
	ctx := context.Background()
	p.Play(ctx, file("/a.mp4"), 0, 0, 40, 12)
	a := op.last(t)
	a.emit(image.Pt(320, 240))
	waitFor(t, "frame", func() bool { return len(frames(t, out.all())) == 1 })
	p.Volume(-30)
	p.ToggleMute()
	out.take()
	p.Play(ctx, file("/b.mp4"), 0, 0, 40, 12)
	b := op.last(t)
	if op.count() != 2 || b.src.Path != "/b.mp4" {
		t.Fatal("not reopened")
	}
	select {
	case <-a.Done():
	default:
		t.Fatal("old engine open")
	}
	if ws := out.take(); len(ws) != 1 || string(ws[0]) != kitty.Delete(DefaultImageID) {
		t.Fatalf("switch wrote %q", ws)
	}
	// volume and mute carried over
	if b.o.Volume != 70 || !b.o.Mute {
		t.Fatalf("carried %v %v", b.o.Volume, b.o.Mute)
	}
}

func TestEndAndReplay(t *testing.T) {
	p, op, out := newTest(t, Options{})
	ctx := context.Background()
	p.Play(ctx, file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	f.emit(image.Pt(320, 240))
	waitFor(t, "frame", func() bool { return len(frames(t, out.all())) == 1 })
	done := p.Done()
	f.set(func(s *mpv.Status) { s.EOF, s.Paused, s.Pos = true, true, s.Dur })
	select {
	case <-done:
	default:
		t.Fatal("done open at the end")
	}
	if p.Playing() || p.State().Playing || p.State().Pos != 90*time.Second {
		t.Fatalf("ended: %+v", p.State())
	}
	if err := p.Toggle(); !errors.Is(err, ErrNotPlaying) {
		t.Fatalf("toggle after end: %v", err)
	}
	// the last frame stays
	for _, w := range out.all() {
		if strings.Contains(string(w), "a=d") {
			t.Fatal("deleted at the end")
		}
	}
	f.took()
	// Place hides and shows the ended frame without playing it again
	out.take()
	p.Hide()
	redraws := f.redraws
	if err := p.Place(2, 1, 40, 12); err != nil {
		t.Fatal(err)
	}
	if got := f.took(); len(got) != 0 || p.Playing() || f.redraws != redraws+1 {
		t.Fatalf("place at the end: calls %q, playing %v, redraws %d", got, p.Playing(), f.redraws-redraws)
	}
	if err := p.Play(ctx, file("/v.mp4"), 0, 0, 40, 12); err != nil {
		t.Fatal(err)
	}
	if op.count() != 1 || !slices.Equal(f.took(), []string{"play"}) || !p.Playing() {
		t.Fatalf("replay: %d opens, playing %v", op.count(), p.Playing())
	}
	select {
	case <-p.Done():
		t.Fatal("replay done")
	default:
	}
	// with Loop the end does not end it
	q, qop, _ := newTest(t, Options{Loop: true})
	q.Play(ctx, file("/v.mp4"), 0, 0, 40, 12)
	qop.last(t).set(func(s *mpv.Status) { s.EOF = true })
	if !q.Playing() {
		t.Fatal("loop ended")
	}
	// a playback error ends it
	r, rop, _ := newTest(t, Options{})
	r.Play(ctx, file("/bad.mp4"), 0, 0, 40, 12)
	rop.last(t).set(func(s *mpv.Status) { s.Err = errors.New("mpv: unrecognized file format") })
	if r.Playing() {
		t.Fatal("error plays")
	}
}

func TestControls(t *testing.T) {
	p, op, _ := newTest(t, Options{})
	for _, err := range []error{p.Toggle(), p.Pause(), p.Resume(), p.Seek(time.Second), p.SeekTo(0.5), p.Volume(5), p.ToggleMute()} {
		if !errors.Is(err, ErrNotPlaying) {
			t.Fatalf("idle control: %v", err)
		}
	}
	if p.SetMute(true); !p.State().Muted {
		t.Fatalf("idle mute: %+v", p.State())
	}
	p.Play(context.Background(), file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	if !f.o.Mute {
		t.Fatal("mute not carried")
	}
	f.took()
	p.Toggle()
	p.Toggle()
	p.Pause()
	p.Resume()
	p.Seek(-5 * time.Second)
	p.SeekTo(1.5)
	p.Volume(50)
	p.Volume(-200)
	p.ToggleMute()
	p.SetMute(true)
	want := []string{"pause true", "pause false", "pause true", "pause false", "seek -5s", "seekto 1",
		"volume 130", "volume 0", "mute false", "mute true"}
	if got := f.took(); !slices.Equal(got, want) {
		t.Fatalf("calls\n%q\nwant\n%q", got, want)
	}
	if st := p.State(); st.Volume != 0 || !st.Muted || st.Pos != 90*time.Second {
		t.Fatalf("state %+v", st)
	}
}

func TestChanges(t *testing.T) {
	p, op, _ := newTest(t, Options{})
	p.o.every = 50 * time.Millisecond
	p.Play(context.Background(), file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	st := <-p.Changes()
	if !st.Playing || st.Target != "/v.mp4" {
		t.Fatalf("first %+v", st)
	}
	start := time.Now()
	for i := range 10 {
		f.set(func(s *mpv.Status) { s.Pos = time.Duration(i) * time.Second })
	}
	st = <-p.Changes()
	if d := time.Since(start); d < 30*time.Millisecond || st.Pos != 9*time.Second {
		t.Fatalf("after %v: %+v", d, st)
	}
	p.Stop()
	waitFor(t, "stopped state", func() bool {
		select {
		case st = <-p.Changes():
			return !st.Playing
		default:
			return false
		}
	})
}

func TestPacingCap(t *testing.T) {
	p, op, out := newTest(t, Options{MaxFPS: 20})
	p.Play(context.Background(), file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	start := time.Now()
	for time.Since(start) < 300*time.Millisecond {
		f.emit(image.Pt(320, 240))
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	fs := frames(t, out.all())
	if len(fs) < 4 || len(fs) > 8 {
		t.Fatalf("%d frames in 300 ms at 20 fps", len(fs))
	}
	if last := fs[len(fs)-1].first; last != byte(f.Rendered()) {
		t.Fatalf("last sent %d, newest %d", last, f.Rendered())
	}
}

func TestDropWhileWriting(t *testing.T) {
	p, op, out := newTest(t, Options{})
	out.delay = 40 * time.Millisecond
	p.Play(context.Background(), file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	for range 20 {
		f.emit(image.Pt(320, 240))
		time.Sleep(5 * time.Millisecond)
	}
	waitFor(t, "newest frame", func() bool {
		fs := frames(t, out.all())
		return len(fs) > 0 && fs[len(fs)-1].first == 20
	})
	if n := len(frames(t, out.all())); n > 6 {
		t.Fatalf("%d frames written for 20 rendered under slow writes", n)
	}
}

func TestHide(t *testing.T) {
	p, op, out := newTest(t, Options{})
	ctx := context.Background()
	p.Play(ctx, file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	f.emit(image.Pt(320, 240))
	waitFor(t, "frame", func() bool { return len(frames(t, out.all())) == 1 })
	out.take()
	p.Hide()
	f.emit(image.Pt(320, 240))
	time.Sleep(20 * time.Millisecond)
	ws := out.take()
	if len(ws) != 1 || string(ws[0]) != kitty.Delete(DefaultImageID) || !p.Playing() {
		t.Fatalf("hidden wrote %q", ws)
	}
	// shown again, same rect: a redraw
	p.Play(ctx, file("/v.mp4"), 0, 0, 40, 12)
	waitFor(t, "shown", func() bool { return len(frames(t, out.all())) == 1 })
	if op.count() != 1 {
		t.Fatal("reopened")
	}
}

func TestPlayErrors(t *testing.T) {
	p, op, _ := newTest(t, Options{})
	ctx := context.Background()
	if err := p.Play(ctx, file("/v.mp4"), 0, 0, 0, 4); err == nil {
		t.Fatal("empty rect")
	}
	if err := p.Play(ctx, file(""), 0, 0, 4, 4); err == nil {
		t.Fatal("no target")
	}
	op.err = mpv.ErrNoLibmpv
	if err := p.Play(ctx, file("/v.mp4"), 0, 0, 4, 4); !errors.Is(err, mpv.ErrNoLibmpv) || p.Playing() {
		t.Fatalf("open error %v", err)
	}
	op.err = nil
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := p.Play(cctx, file("/v.mp4"), 0, 0, 4, 4); err == nil {
		t.Fatal("cancelled ctx")
	}
}

func TestContextCancelStops(t *testing.T) {
	p, op, _ := newTest(t, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	p.Play(ctx, file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	cancel()
	waitFor(t, "stopped", func() bool { return !p.Playing() })
	<-f.Done()
}

func TestToRGB(t *testing.T) {
	img := &image.RGBA{Rect: image.Rect(0, 0, 2, 2), Stride: 12, Pix: []byte{
		1, 2, 3, 255, 4, 5, 6, 255, 0, 0, 0, 0,
		7, 8, 9, 255, 10, 11, 12, 255, 0, 0, 0, 0,
	}}
	if got := toRGB(nil, img); !slices.Equal(got, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}) {
		t.Fatalf("%v", got)
	}
}

func TestSourceToMPV(t *testing.T) {
	p, op, _ := newTest(t, Options{YTDL: true, YTDLFormat: "best"})
	ctx := context.Background()
	closes := 0
	src := Source{Key: "https://youtu.be/x", Target: "https://cdn/x.mp4", Headers: []string{"Referer: r"},
		Start: 42 * time.Second, Paused: true, Close: func() { closes++ }}
	if err := p.Play(ctx, src, 0, 0, 40, 12); err != nil {
		t.Fatal(err)
	}
	f := op.last(t)
	if f.src.URL != "https://cdn/x.mp4" || f.src.Path != "" || !slices.Equal(f.src.Headers, src.Headers) ||
		!f.o.YTDL || f.o.YTDLFormat != "best" || f.o.Start != 42*time.Second || f.o.Speed != 1 || !f.o.Paused {
		t.Fatalf("opened %+v %+v", f.src, f.o)
	}
	if p.Target() != "https://youtu.be/x" || p.State().Target != "https://youtu.be/x" {
		t.Fatalf("target %q", p.Target())
	}
	// a re-resolved target under the same key: same session
	src.Target = "https://cdn/y.mp4"
	if err := p.Play(ctx, src, 1, 1, 40, 12); err != nil || op.count() != 1 {
		t.Fatalf("same key reopened: %v %d", err, op.count())
	}
	p.Play(ctx, Source{Target: "/v.mp4", Format: "worst"}, 0, 0, 40, 12)
	g := op.last(t)
	if g.src.Path != "/v.mp4" || g.o.YTDLFormat != "worst" {
		t.Fatalf("opened %+v %+v", g.src, g.o)
	}
	p.Wait()
	if closes != 1 {
		t.Fatalf("close ran %d times", closes)
	}
	// Close also on an open error
	op.err = errors.New("no")
	if err := p.Play(ctx, Source{Target: "/w.mp4", Close: func() { closes++ }}, 0, 0, 4, 4); err == nil || closes != 2 {
		t.Fatalf("open error: %v, closes %d", err, closes)
	}
}

func TestSettings(t *testing.T) {
	p, op, _ := newTest(t, Options{Loop: true})
	if st := p.State(); !st.Loop || st.Speed != 1 || st.Volume != 100 || st.Playing {
		t.Fatalf("idle %+v", st)
	}
	p.SetVolume(200)
	p.SetMute(true)
	p.SetLoop(false)
	p.SetSpeed(9)
	if st := p.State(); st.Volume != MaxVolume || !st.Muted || st.Loop || st.Speed != 4 {
		t.Fatalf("idle set %+v", st)
	}
	ctx := context.Background()
	p.Play(ctx, file("/v.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	if f.o.Volume != MaxVolume || !f.o.Mute || f.o.Loop || f.o.Speed != 4 {
		t.Fatalf("carried %+v", f.o)
	}
	f.took()
	p.SetVolume(-3)
	p.SetMute(false)
	p.SetLoop(true)
	p.SetSpeed(0.1)
	if got, want := f.took(), []string{"volume 0", "mute false", "loop true", "speed 0.25"}; !slices.Equal(got, want) {
		t.Fatalf("calls %q, want %q", got, want)
	}
	if st := p.State(); st.Volume != 0 || st.Muted || !st.Loop || st.Speed != 0.25 || !st.Loaded {
		t.Fatalf("state %+v", st)
	}
	// with Loop set live the end does not end it
	f.set(func(s *mpv.Status) { s.EOF = true })
	if !p.Playing() {
		t.Fatal("loop ended")
	}
	// ended: settings reach the open handle for the replay
	p.SetLoop(false)
	f.set(func(s *mpv.Status) { s.EOF = true })
	if p.Playing() {
		t.Fatal("not ended")
	}
	f.took()
	p.SetVolume(40)
	if err := p.Volume(5); !errors.Is(err, ErrNotPlaying) {
		t.Fatalf("volume after end: %v", err)
	}
	if got := f.took(); !slices.Equal(got, []string{"volume 40"}) || p.State().Volume != 40 {
		t.Fatalf("ended calls %q", got)
	}
}

func TestPlaybackError(t *testing.T) {
	p, op, _ := newTest(t, Options{})
	p.Play(context.Background(), file("/bad.mp4"), 0, 0, 40, 12)
	bad := errors.New("mpv: unrecognized file format")
	op.last(t).set(func(s *mpv.Status) { s.Err = bad })
	select {
	case <-p.Done():
	default:
		t.Fatal("done open")
	}
	if st := p.State(); st.Playing || st.Err != bad {
		t.Fatalf("state %+v", st)
	}
}

func TestNilPane(t *testing.T) {
	var p *Pane
	p.Stop()
	p.Hide()
	p.Wait()
	p.SetVolume(5)
	p.SetMute(true)
	p.SetLoop(true)
	p.SetSpeed(2)
	if p.Playing() || p.Target() != "" || p.State() != (State{}) || p.Changes() != nil || p.Subs() != nil ||
		!errors.Is(p.SetSub(1), ErrNotPlaying) || !errors.Is(p.Place(0, 0, 4, 4), ErrNotPlaying) {
		t.Fatal("nil pane state")
	}
	for _, err := range []error{p.Toggle(), p.Pause(), p.Resume(), p.Seek(time.Second), p.SeekTo(0.5), p.Volume(5), p.ToggleMute()} {
		if !errors.Is(err, ErrNotPlaying) {
			t.Fatalf("nil control: %v", err)
		}
	}
	select {
	case <-p.Done():
	default:
		t.Fatal("nil done open")
	}
}

func TestSubs(t *testing.T) {
	p, op, _ := newTest(t, Options{AutoCaptions: true})
	if err := p.SetSub(1); !errors.Is(err, ErrNotPlaying) {
		t.Fatalf("idle: %v", err)
	}
	ctx := context.Background()
	p.Play(ctx, file("/a.mp4"), 0, 0, 40, 12)
	f := op.last(t)
	if !f.o.AutoCaptions || f.o.SubLang != "" {
		t.Fatalf("options %+v", f.o)
	}
	subs := []Sub{{ID: 1, Lang: "de"}, {ID: 2, Lang: "en-US"}, {ID: 3, Lang: "en", Auto: true}}
	f.set(func(s *mpv.Status) { s.Subs = subs })
	if st := p.State(); !slices.Equal(p.Subs(), subs) || st.Sub != 0 {
		t.Fatalf("subs %+v, state %+v", p.Subs(), st)
	}
	f.took()
	if err := p.SetSub(9); err == nil {
		t.Fatal("track 9 set")
	}
	if err := p.SetSub(2); err != nil {
		t.Fatal(err)
	}
	if got := f.took(); !slices.Equal(got, []string{"sub 2"}) || p.State().Sub != 2 {
		t.Fatalf("calls %q, state %+v", got, p.State())
	}
	// the language holds for the session after
	p.Play(ctx, file("/b.mp4"), 0, 0, 40, 12)
	if f = op.last(t); f.o.SubLang != "en-US" {
		t.Fatalf("carried %q", f.o.SubLang)
	}
	f.set(func(s *mpv.Status) { s.Subs, s.Sub = subs, 2 })
	if err := p.SetSub(0); err != nil {
		t.Fatal(err)
	}
	p.Play(ctx, file("/c.mp4"), 0, 0, 40, 12)
	if f = op.last(t); f.o.SubLang != "" {
		t.Fatalf("off carried %q", f.o.SubLang)
	}
}

func TestSpans(t *testing.T) {
	p, op, _ := newTest(t, Options{})
	ctx := context.Background()
	spans := []Span{{Start: time.Second, End: 2 * time.Second}}
	// set before the session: given at open
	p.SetSpans("/a.mp4", spans)
	p.Play(ctx, file("/a.mp4"), 0, 0, 40, 12)
	a := op.last(t)
	if !slices.Equal(a.o.Spans, spans) {
		t.Fatalf("open spans %+v", a.o.Spans)
	}
	// set during it: handed to the engine
	a.took()
	p.SetSpans("/a.mp4", nil)
	if got := a.took(); !slices.Equal(got, []string{"spans 0"}) {
		t.Fatalf("calls %q", got)
	}
	// another key's: not this session's, nor carried to the next
	p.SetSpans("/x.mp4", spans)
	if got := a.took(); len(got) != 0 {
		t.Fatalf("calls %q", got)
	}
	p.Play(ctx, file("/b.mp4"), 0, 0, 40, 12)
	if b := op.last(t); b.o.Spans != nil {
		t.Fatalf("b spans %+v", b.o.Spans)
	}
	a.set(func(s *mpv.Status) {})
	b := op.last(t)
	b.set(func(s *mpv.Status) { s.Skips, s.Skipped = 2, 1 })
	if st := p.State(); st.Skips != 2 || st.Skipped != 1 {
		t.Fatalf("state %+v", st)
	}
}
