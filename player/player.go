// Package player plays a video with sound inside a cell rect of the
// terminal through libmpv in process (gotuitools/mpv): mpv renders
// into an RGBA buffer of the fitted rect's pixels, on a GPU where
// there is one, in software otherwise or with Options.Software; each
// frame goes out as raw RGB through a Sink under one image id, zlib
// on. Sound through libmpv's own audio output. Targets are local
// paths, fd://N pipes, direct media URLs with request headers, or page
// URLs through mpv's ytdl_hook.
//
// Geometry: Fit of mpv's display size into the rect, rounded to whole
// cells, centred; mpv renders those cells' pixels, so kitty does not
// rescale. A source smaller than its cells is rendered at its own size
// and kitty scales it.
//
// Options.Shm: each frame is a POSIX shared memory object the terminal
// reads (kitty t=s), 32-bit, no zlib, no base64: a terminal on this
// machine. The first frames left unread for a second: frames as above
// from then on.
//
// Pacing: mpv's frame callback wakes one writer goroutine per session;
// it sends the newest frame, at most MaxFPS a second, start to start.
// Frames rendered while a write is in progress are dropped but the
// newest.
//
// Play on the same key: a move re-places the shown frame; a new size
// renders at the new size, position kept; after the end (no Loop) it
// plays again from the start on the same handle. Hide deletes the
// frame and keeps playing; the next Play shows it again.
//
// State: libmpv's observed position, duration, pause, load and error,
// cached; volume, mute, loop and speed are the pane's, kept across
// sessions. Controls change State at once; Changes delivers it at most
// every 250 ms.
//
// Stop deletes the image, pauses, so the sound stops at once, and
// closes the libmpv handle on a goroutine; Wait blocks until every
// closed handle is destroyed.
//
// Output should be the termout.Writer the TUI renders through.
package player

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MaximKayTee/gotuitools/mpv"
)

// DefaultImageID is the kitty image id of the frames.
const DefaultImageID uint32 = 0xFFFFFE

// MaxVolume is mpv's default volume-max.
const MaxVolume = mpv.MaxVolume

const changeEvery = 250 * time.Millisecond

// shmRetry is the wait before a frame the terminal was not ready for
// is tried again.
const shmRetry = 4 * time.Millisecond

// ErrNotPlaying: a control without a running session.
var ErrNotPlaying = errors.New("player: not playing")

// Available reports whether libmpv loads; nil when usable.
func Available() error { return mpv.Available() }

// Options configure a Pane. Zero fields take defaults.
type Options struct {
	Output io.Writer   // terminal, a termout.Writer; os.Stdout
	CellPx image.Point // pixels per cell; 10x20
	Mute   bool        // start muted
	Loop   bool        // loop-file=inf
	// Volume: start volume, 1 to MaxVolume; 0 is 100.
	Volume  int
	MaxFPS  int    // frames sent a second at most; 30
	ImageID uint32 // kitty image id; DefaultImageID
	// AudioOutput is mpv's ao; "" mpv's default.
	AudioOutput string
	// YTDL: URLs resolve through mpv's ytdl_hook, YTDLFormat its format.
	YTDL       bool
	YTDLFormat string
	// AutoCaptions adds YouTube's auto-generated captions in the
	// video's language to the subtitle tracks; with YTDL.
	AutoCaptions bool
	// Shm: frames through shared memory while the terminal reads them.
	Shm bool
	// Software: frames from mpv's software renderer, never a GPU.
	Software bool
	Logger   *slog.Logger // discard

	open    func(mpv.Source, mpv.Options) (engine, error) // tests; mpv.New
	every   time.Duration                                 // tests: Changes interval; changeEvery
	raw     bool                                          // tests: no zlib
	shmWait time.Duration                                 // tests; shmWait
}

// engine is the libmpv player as the pane uses it; *mpv.Player.
type engine interface {
	Frame() (*image.RGBA, uint64, bool)
	Redraw()
	Status() mpv.Status
	SetPause(bool)
	Play()
	SeekBy(time.Duration)
	SeekPercent(float64)
	SetVolume(float64)
	SetMute(bool)
	SetLoop(bool)
	SetSpeed(float64)
	SetSub(int)
	SetAudio(int)
	AddChapter(int)
	SetSpans([]mpv.Span)
	Rendered() int
	Close()
	Done() <-chan struct{}
}

func openMPV(src mpv.Source, o mpv.Options) (engine, error) {
	p, err := mpv.New(src, o)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// State is the player state. Pos and Dur are 0 until mpv reports them.
type State struct {
	Target        string // the session's key
	Pos, Dur      time.Duration
	Paused, Muted bool
	Volume        int
	Loop          bool
	Speed         float64 // 1 normal
	Loaded        bool
	// Cached is where what is buffered ahead ends; 0 unknown.
	Cached time.Duration
	// Playing: a session runs; false after the end, Stop, or before
	// Play.
	Playing bool
	// Sub is the ID of the subtitle track shown, a Subs ID; 0 none.
	Sub int
	// Audio is the ID of the audio track playing, an Audios ID; 0 none.
	Audio int
	// Chapter is the index in Chapters of the one playing; -1 before
	// the first or none.
	Chapter int
	// Skips counts the spans skipped this session, Skipped is the
	// index of the last one in SetSpans; -1 none.
	Skips, Skipped int
	Err            error
}

// Span is a stretch of a video to skip or play muted: playback that
// runs into a skip span goes on from its end; a seek into one plays it.
type Span = mpv.Span

// Sub is a subtitle track.
type Sub = mpv.Sub

// Audio is an audio track.
type Audio = mpv.Audio

// Chapter is a chapter of a file.
type Chapter = mpv.Chapter

// Source is what to play: Target is a local path or a URL, Headers
// its request headers ("Name: value"). Key names the session so a
// re-resolved Target counts as the same playback; "" means Target.
type Source struct {
	Key     string
	Target  string
	Headers []string
	// Format is the ytdl-format; "" Options.YTDLFormat.
	Format string
	// Start is the position playback starts at.
	Start time.Duration
	// Paused: playback starts paused, the frame at Start shown.
	Paused bool
	// Close runs once when the session ends: a pipe's producer.
	Close func()
}

func (s Source) key() string {
	if s.Key != "" {
		return s.Key
	}
	return s.Target
}

// mpvSource is src as an mpv source: a URL when it has a scheme.
func mpvSource(src Source) mpv.Source {
	if strings.Contains(src.Target, "://") {
		return mpv.Source{URL: src.Target, Headers: src.Headers}
	}
	return mpv.Source{Path: src.Target}
}

// Pane plays one source at a time. Use New. Methods are safe for
// concurrent use and on a nil Pane, where controls report
// ErrNotPlaying.
type Pane struct {
	o Options

	mu      sync.Mutex // sess, vol, mute, loop, speed, langs; session done, ended, eng
	sess    *session
	vol     int
	mute    bool
	loop    bool
	speed   float64
	subLang string // language of the subtitles picked; "" none
	subOff  bool   // subtitles turned off: none in the sessions after
	// audioLang is the language of the audio track picked; "" none
	audioLang string
	// audioPref and subPref are SetLangs'
	audioPref, subPref string
	// spans are SetSpans', for the session of spanKey
	spanKey string
	spans   []Span

	shm atomic.Bool // frames through shared memory

	wg sync.WaitGroup // handles closing

	nmu   sync.Mutex // sent, timer; deliveries to ch
	ch    chan State
	sent  time.Time
	timer *time.Timer
}

type session struct {
	key  string
	src  Source
	sink *Sink
	wake chan struct{} // frame callback; one pending
	quit chan struct{} // closed by stop
	stop sync.Once

	// under Pane.mu
	eng   engine
	done  chan struct{}
	ended bool

	gmu        sync.Mutex
	cols, rows int
	native     image.Point // mpv's display size, from the render goroutine
	hidden     bool

	// writer goroutine
	sent int
}

var closed = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// New returns an idle pane.
func New(o Options) *Pane {
	if o.Output == nil {
		o.Output = os.Stdout
	}
	if o.CellPx.X <= 0 || o.CellPx.Y <= 0 {
		o.CellPx = image.Pt(10, 20)
	}
	if o.Volume <= 0 {
		o.Volume = 100
	}
	o.Volume = min(o.Volume, MaxVolume)
	if o.MaxFPS <= 0 {
		o.MaxFPS = 30
	}
	if o.ImageID == 0 {
		o.ImageID = DefaultImageID
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.open == nil {
		o.open = openMPV
	}
	if o.every <= 0 {
		o.every = changeEvery
	}
	p := &Pane{o: o, vol: o.Volume, mute: o.Mute, loop: o.Loop, speed: 1, ch: make(chan State, 1)}
	p.shm.Store(o.Shm)
	return p
}

// Play plays src in the rect at 0-based cell (col, row), cols x rows
// cells. The same key as now playing: moves, resizes or shows the
// frame, position kept; ended, it plays again from the start.
// Otherwise stops what plays and opens src. ctx bounds the session:
// cancelling it stops as Stop. The end without Loop, a playback error
// or Stop closes Done.
func (p *Pane) Play(ctx context.Context, src Source, col, row, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("player: empty rect %dx%d", cols, rows)
	}
	if src.Target == "" {
		return errors.New("player: no target")
	}
	key := src.key()
	p.mu.Lock()
	if s := p.sess; s != nil {
		if s.key == key {
			replay := s.ended
			if replay {
				s.ended, s.done = false, make(chan struct{})
			}
			eng := s.eng
			p.mu.Unlock()
			if replay {
				eng.Play()
			}
			p.place(s, eng, col, row, cols, rows)
			p.notify()
			return nil
		}
		p.sess = nil
		p.mu.Unlock()
		p.stop(s)
		p.mu.Lock()
	}
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return err
	}
	s := &session{key: key, src: src, cols: cols, rows: rows, done: make(chan struct{}),
		wake: make(chan struct{}, 1), quit: make(chan struct{}),
		sink: NewSink(p.o.Output, p.o.ImageID, col, row, p.o.raw)}
	if p.o.shmWait > 0 {
		s.sink.wait = p.o.shmWait
	}
	vol, mute, loop, speed := p.vol, p.mute, p.loop, p.speed
	audioLang, subLang, subOff := langs(p.audioLang, p.audioPref), langs(p.subLang, p.subPref), p.subOff
	var spans []Span
	if p.spanKey == key {
		spans = p.spans
	}
	p.mu.Unlock()
	eng, err := p.o.open(mpvSource(src), mpv.Options{
		Loop: loop, Speed: speed, Mute: mute, Paused: src.Paused, Volume: float64(vol), AudioOutput: p.o.AudioOutput,
		YTDL: p.o.YTDL, YTDLFormat: cmp.Or(src.Format, p.o.YTDLFormat), Start: src.Start,
		AutoCaptions: p.o.AutoCaptions, AudioLang: audioLang, SubLang: subLang, SubOff: subOff, Spans: spans, Software: p.o.Software,
		Size:     s.size(p.o.CellPx),
		OnFrame:  s.poke,
		OnChange: func() { p.changed(s) },
	})
	if err != nil {
		if src.Close != nil {
			src.Close()
		}
		return fmt.Errorf("player: %w", err)
	}
	p.mu.Lock()
	if p.sess != nil {
		// a concurrent Play won
		p.mu.Unlock()
		eng.Close()
		if src.Close != nil {
			src.Close()
		}
		return errors.New("player: superseded")
	}
	s.eng = eng
	p.sess = s
	p.mu.Unlock()
	log := p.o.Logger.With("target", key)
	renderer := ""
	if r, ok := eng.(interface{ Renderer() string }); ok {
		renderer = r.Renderer()
	}
	log.Debug("mpv opened", "cols", cols, "rows", rows, "renderer", cmp.Or(renderer, "software"))
	go p.write(s, eng, log)
	go func() {
		select {
		case <-ctx.Done():
			p.stopSession(s)
		case <-s.quit:
		}
	}()
	p.changed(s)
	return nil
}

// size is the render size callback: the fit of the native size.
func (s *session) size(cell image.Point) func(image.Point) image.Point {
	return func(native image.Point) image.Point {
		s.gmu.Lock()
		defer s.gmu.Unlock()
		s.native = native
		g := Fit(native, s.cols, s.rows, cell)
		return image.Pt(g.W, g.H)
	}
}

// geom is the fit for the current rect; zero before mpv's size.
func (s *session) geom(cell image.Point) Geom {
	s.gmu.Lock()
	defer s.gmu.Unlock()
	return Fit(s.native, s.cols, s.rows, cell)
}

func (s *session) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// place moves, resizes or shows s at the rect.
func (p *Pane) place(s *session, eng engine, col, row, cols, rows int) {
	s.gmu.Lock()
	resized := s.cols != cols || s.rows != rows
	shown := s.hidden
	s.cols, s.rows, s.hidden = cols, rows, false
	s.gmu.Unlock()
	if shown {
		s.sink.SetHidden(false)
	}
	if resized || shown {
		s.sink.SetOrigin(col, row)
		eng.Redraw()
		return
	}
	s.sink.Move(col, row)
}

// Place moves, resizes or shows the open session's frame at the rect,
// as Play of the same key does; an ended one is not played again.
// ErrNotPlaying without an open session.
func (p *Pane) Place(col, row, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("player: empty rect %dx%d", cols, rows)
	}
	if p == nil {
		return ErrNotPlaying
	}
	p.mu.Lock()
	s := p.sess
	var eng engine
	if s != nil {
		eng = s.eng
	}
	p.mu.Unlock()
	if eng == nil {
		return ErrNotPlaying
	}
	p.place(s, eng, col, row, cols, rows)
	p.notify()
	return nil
}

// Hide deletes the shown frame; playback goes on. The next Play of the
// same key or Place shows it again.
func (p *Pane) Hide() {
	if p == nil {
		return
	}
	p.mu.Lock()
	s := p.sess
	p.mu.Unlock()
	if s == nil {
		return
	}
	s.gmu.Lock()
	s.hidden = true
	s.gmu.Unlock()
	s.sink.SetHidden(true)
}

// write sends the newest frame on each frame callback, at most MaxFPS a
// second, until stop.
func (p *Pane) write(s *session, eng engine, log *slog.Logger) {
	interval := time.Second / time.Duration(p.o.MaxFPS)
	var last time.Time
	var seen uint64
	var rgb []byte
	for {
		select {
		case <-s.wake:
		case <-s.quit:
			return
		}
		if wait := interval - time.Since(last); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-s.quit:
				t.Stop()
				return
			}
		}
		start := time.Now()
		img, seq, ok := eng.Frame()
		if !ok || seq == seen {
			continue
		}
		g := s.geom(p.o.CellPx)
		if g.W != img.Rect.Dx() || g.H != img.Rect.Dy() {
			continue // an older size; the new one is on its way
		}
		sent := false
		if p.shm.Load() {
			switch s.sink.frameShm(img, g) {
			case shmStopped:
				return
			case shmBusy:
				// the newest frame again once the terminal has read
				t := time.NewTimer(shmRetry)
				select {
				case <-t.C:
				case <-s.quit:
					t.Stop()
					return
				}
				s.poke()
				continue
			case shmUnread:
				p.shm.Store(false)
				log.Warn("shared memory frames unread")
			case shmSent:
				sent = true
			}
		}
		if !sent {
			rgb = toRGB(rgb, img)
			if !s.sink.Frame(rgb, g) {
				return
			}
		}
		seen, last = seq, start
		if s.sent == 0 {
			log.Debug("first frame", "w", g.W, "h", g.H)
		}
		s.sent++
	}
}

// toRGB packs img's pixels as 24-bit RGB into dst, reused when large
// enough.
func toRGB(dst []byte, img *image.RGBA) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	n := w * h * 3
	if cap(dst) < n {
		dst = make([]byte, n)
	}
	dst = dst[:n]
	o := 0
	for y := range h {
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x+4 <= len(row); x += 4 {
			dst[o], dst[o+1], dst[o+2] = row[x], row[x+1], row[x+2]
			o += 3
		}
	}
	return dst
}

// changed takes a property change: the end without Loop or an error
// ends the session.
func (p *Pane) changed(s *session) {
	p.mu.Lock()
	eng := s.eng
	cur := p.sess == s
	p.mu.Unlock()
	if eng == nil {
		return
	}
	st := eng.Status()
	p.mu.Lock()
	end := !s.ended && ((st.EOF && !p.loop) || st.Err != nil)
	if end {
		s.ended = true
		close(s.done)
	}
	p.mu.Unlock()
	if end && st.Err != nil {
		p.o.Logger.Warn("mpv", "target", s.key, "err", st.Err)
	}
	if cur {
		p.notify()
	}
}

// Stop deletes the image and closes the libmpv handle; no frame
// follows the delete. Idempotent.
func (p *Pane) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	s := p.sess
	p.sess = nil
	p.mu.Unlock()
	if s != nil {
		p.stop(s)
		p.notify()
	}
}

// stopSession stops s, also clearing it when still current.
func (p *Pane) stopSession(s *session) {
	p.mu.Lock()
	cur := p.sess == s
	if cur {
		p.sess = nil
	}
	p.mu.Unlock()
	p.stop(s)
	if cur {
		p.notify()
	}
}

func (p *Pane) stop(s *session) {
	s.stop.Do(func() {
		s.sink.Stop()
		close(s.quit)
		p.mu.Lock()
		eng := s.eng
		if !s.ended {
			s.ended = true
			close(s.done)
		}
		p.mu.Unlock()
		if eng == nil {
			if s.src.Close != nil {
				s.src.Close()
			}
			return
		}
		eng.Close()
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			<-eng.Done()
			if s.src.Close != nil {
				s.src.Close()
			}
			p.o.Logger.Debug("mpv closed", "target", s.key, "sent", s.sink.Frames(), "rendered", eng.Rendered())
		}()
	})
}

// Wait blocks until every stopped session's libmpv handle is destroyed
// and its Source.Close ran.
func (p *Pane) Wait() {
	if p == nil {
		return
	}
	p.wg.Wait()
}

// Playing reports a running session.
func (p *Pane) Playing() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sess != nil && !p.sess.ended
}

// Target is the current session's key, "" when none.
func (p *Pane) Target() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sess == nil {
		return ""
	}
	return p.sess.key
}

// Done is closed when the current session ends: the end without Loop,
// a playback error, Stop. Closed when there is none; a replay makes a
// new one. The last frame stays until Stop.
func (p *Pane) Done() <-chan struct{} {
	if p == nil {
		return closed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sess == nil {
		return closed
	}
	return p.sess.done
}

// State is the cached player state.
func (p *Pane) State() State {
	if p == nil {
		return State{}
	}
	p.mu.Lock()
	s, vol, mute, loop, speed := p.sess, p.vol, p.mute, p.loop, p.speed
	var eng engine
	ended := true
	if s != nil {
		eng, ended = s.eng, s.ended
	}
	p.mu.Unlock()
	if s == nil || eng == nil {
		return State{Volume: vol, Muted: mute, Loop: loop, Speed: speed, Skipped: -1, Chapter: -1}
	}
	st := eng.Status()
	return State{Target: s.key, Pos: st.Pos, Dur: st.Dur, Paused: st.Paused, Muted: mute,
		Volume: vol, Loop: loop, Speed: speed, Loaded: st.Loaded, Cached: st.Cached, Playing: !ended, Sub: st.Sub, Audio: st.Audio, Chapter: st.Chapter,
		Skips: st.Skips, Skipped: st.Skipped, Err: st.Err}
}

// Changes receives State after it changed: at once, then at most every
// 250 ms, the latest one only. Never closed; Stop and the end of a
// session send one with Playing false. nil on a nil Pane.
func (p *Pane) Changes() <-chan State {
	if p == nil {
		return nil
	}
	return p.ch
}

// notify schedules a State delivery; never with p.mu held.
func (p *Pane) notify() {
	p.nmu.Lock()
	defer p.nmu.Unlock()
	if p.timer != nil {
		return
	}
	if wait := p.o.every - time.Since(p.sent); wait > 0 {
		p.timer = time.AfterFunc(wait, func() {
			p.nmu.Lock()
			defer p.nmu.Unlock()
			p.timer = nil
			p.deliver()
		})
		return
	}
	p.deliver()
}

// deliver replaces an unread State with the current one; under nmu.
func (p *Pane) deliver() {
	st := p.State()
	p.sent = time.Now()
	select {
	case <-p.ch:
	default:
	}
	p.ch <- st
}

// live is the running session's engine.
func (p *Pane) live() (engine, error) {
	if p == nil {
		return nil, ErrNotPlaying
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.sess
	if s == nil || s.ended || s.eng == nil {
		return nil, ErrNotPlaying
	}
	return s.eng, nil
}

func (p *Pane) control(fn func(e engine)) error {
	e, err := p.live()
	if err != nil {
		return err
	}
	fn(e)
	p.notify()
	return nil
}

// Toggle pauses or resumes.
func (p *Pane) Toggle() error {
	return p.control(func(e engine) { e.SetPause(!e.Status().Paused) })
}

// Pause holds the shown frame and the sound.
func (p *Pane) Pause() error { return p.control(func(e engine) { e.SetPause(true) }) }

// Resume continues after Pause.
func (p *Pane) Resume() error { return p.control(func(e engine) { e.SetPause(false) }) }

// Seek moves by d, back when negative.
func (p *Pane) Seek(d time.Duration) error { return p.control(func(e engine) { e.SeekBy(d) }) }

// SeekTo moves to frac of the duration, clamped to 0..1.
func (p *Pane) SeekTo(frac float64) error {
	return p.control(func(e engine) { e.SeekPercent(min(max(frac, 0), 1)) })
}

// set changes a setting kept across sessions and applies it to the
// open handle, ended or not. running: ErrNotPlaying without a running
// session, nothing changed. The pane's value is the truth; mpv's
// report of it is not read back.
func (p *Pane) set(running bool, change func() func(engine)) error {
	if p == nil {
		return ErrNotPlaying
	}
	p.mu.Lock()
	var eng engine
	s := p.sess
	if s != nil {
		eng = s.eng
	}
	if running && (eng == nil || s.ended) {
		p.mu.Unlock()
		return ErrNotPlaying
	}
	apply := change()
	p.mu.Unlock()
	if eng != nil {
		apply(eng)
	}
	p.notify()
	return nil
}

// SetVolume sets the volume, clamped to 0..MaxVolume, now and for the
// sessions after.
func (p *Pane) SetVolume(v int) {
	v = min(max(v, 0), MaxVolume)
	_ = p.set(false, func() func(engine) {
		p.vol = v
		return func(e engine) { e.SetVolume(float64(v)) }
	})
}

// Volume changes the volume by delta, clamped to 0..MaxVolume;
// ErrNotPlaying without a running session.
func (p *Pane) Volume(delta int) error {
	return p.set(true, func() func(engine) {
		v := min(max(p.vol+delta, 0), MaxVolume)
		p.vol = v
		return func(e engine) { e.SetVolume(float64(v)) }
	})
}

// SetMute mutes or unmutes, now and for the sessions after.
func (p *Pane) SetMute(on bool) {
	_ = p.set(false, func() func(engine) {
		p.mute = on
		return func(e engine) { e.SetMute(on) }
	})
}

// ToggleMute mutes or unmutes; ErrNotPlaying without a running
// session.
func (p *Pane) ToggleMute() error {
	return p.set(true, func() func(engine) {
		on := !p.mute
		p.mute = on
		return func(e engine) { e.SetMute(on) }
	})
}

// SetLoop plays the video again at its end, now and for the sessions
// after.
func (p *Pane) SetLoop(on bool) {
	_ = p.set(false, func() func(engine) {
		p.loop = on
		return func(e engine) { e.SetLoop(on) }
	})
}

// SetSpeed sets the playback speed, clamped to 0.25..4, now and for
// the sessions after.
func (p *Pane) SetSpeed(x float64) {
	x = min(max(x, 0.25), 4)
	_ = p.set(false, func() func(engine) {
		p.speed = x
		return func(e engine) { e.SetSpeed(x) }
	})
}

// open is the open session's engine, ended or not; nil without one.
func (p *Pane) open() engine {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sess == nil {
		return nil
	}
	return p.sess.eng
}

// Subs are the open session's subtitle tracks. Not to be changed.
func (p *Pane) Subs() []Sub {
	eng := p.open()
	if eng == nil {
		return nil
	}
	return eng.Status().Subs
}

// SetSub shows the subtitle track id, a Subs ID; 0 none. Its language
// is shown in the sessions after too; 0 turns them off for the sessions
// after, until a track is set or SetLangs. ErrNotPlaying without an
// open session.
func (p *Pane) SetSub(id int) error {
	eng := p.open()
	if eng == nil {
		return ErrNotPlaying
	}
	lang := ""
	if id != 0 {
		subs := eng.Status().Subs
		i := slices.IndexFunc(subs, func(s Sub) bool { return s.ID == id })
		if i < 0 {
			return fmt.Errorf("player: no subtitle track %d", id)
		}
		lang = subs[i].Lang
	}
	p.mu.Lock()
	p.subLang, p.subOff = lang, id == 0
	p.mu.Unlock()
	eng.SetSub(id)
	p.notify()
	return nil
}

// Audios are the open session's audio tracks. Not to be changed.
func (p *Pane) Audios() []Audio {
	eng := p.open()
	if eng == nil {
		return nil
	}
	return eng.Status().Audios
}

// SetAudio plays the audio track id, an Audios ID. Its language comes
// first in the sessions after. ErrNotPlaying without an open session.
func (p *Pane) SetAudio(id int) error {
	eng := p.open()
	if eng == nil {
		return ErrNotPlaying
	}
	audios := eng.Status().Audios
	i := slices.IndexFunc(audios, func(a Audio) bool { return a.ID == id })
	if i < 0 {
		return fmt.Errorf("player: no audio track %d", id)
	}
	p.mu.Lock()
	p.audioLang = audios[i].Lang
	p.mu.Unlock()
	eng.SetAudio(id)
	p.notify()
	return nil
}

// SetLangs sets the languages of the audio and subtitle tracks picked
// at the start of each session after, comma separated, first wanted
// first ("ja, en"); "" mpv's default. The tracks picked with SetAudio
// and SetSub before are forgotten, subtitles turned off too.
func (p *Pane) SetLangs(audio, sub string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.audioPref, p.subPref = audio, sub
	p.audioLang, p.subLang, p.subOff = "", "", false
	p.mu.Unlock()
}

// Chapters are the open session's chapters, by start. Not to be
// changed.
func (p *Pane) Chapters() []Chapter {
	eng := p.open()
	if eng == nil {
		return nil
	}
	return eng.Status().Chapters
}

// AddChapter jumps by d chapters: 1 the next, -1 the start of the one
// playing or the one before near its start; past the last, the end.
// ErrNotPlaying without an open session.
func (p *Pane) AddChapter(d int) error {
	eng := p.open()
	if eng == nil {
		return ErrNotPlaying
	}
	eng.AddChapter(d)
	return nil
}

// langs is the language picked, then the ones preferred.
func langs(picked, pref string) string {
	if picked == "" || pref == "" {
		return picked + pref
	}
	return picked + "," + pref
}

// SetSpans sets the spans of the source key: for its session, now or
// the next one. They replace those set before, for any key.
func (p *Pane) SetSpans(key string, spans []Span) {
	if p == nil {
		return
	}
	spans = slices.Clone(spans)
	p.mu.Lock()
	p.spanKey, p.spans = key, spans
	var eng engine
	if s := p.sess; s != nil && s.key == key {
		eng = s.eng
	}
	p.mu.Unlock()
	if eng != nil {
		eng.SetSpans(spans)
	}
}
