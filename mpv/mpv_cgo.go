//go:build cgo && linux

package mpv

/*
#cgo LDFLAGS: -ldl -lpthread
#include <dlfcn.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

// libmpv is opened with dlopen so a missing library is a runtime error,
// not a link failure. Types are declared here; no mpv headers needed.
typedef struct mpv_handle mpv_handle;
typedef struct mpv_render_context mpv_render_context;
typedef struct { int type; void *data; } tm_render_param;
typedef struct { int event_id; int error; uint64_t reply_userdata; void *data; } tm_mpv_event;
typedef struct { const char *name; int format; void *data; } tm_mpv_event_property;
typedef struct { int reason; int error; } tm_mpv_event_end_file;

static void *tm_lib;
static mpv_handle *(*f_create)(void);
static int (*f_initialize)(mpv_handle *);
static void (*f_terminate_destroy)(mpv_handle *);
static int (*f_set_option_string)(mpv_handle *, const char *, const char *);
static int (*f_command)(mpv_handle *, const char **);
static int (*f_command_async)(mpv_handle *, uint64_t, const char **);
static int (*f_observe_property)(mpv_handle *, uint64_t, const char *, int);
static tm_mpv_event *(*f_wait_event)(mpv_handle *, double);
static void (*f_wakeup)(mpv_handle *);
static const char *(*f_error_string)(int);
static int (*f_render_context_create)(mpv_render_context **, mpv_handle *, tm_render_param *);
static int (*f_render_context_render)(mpv_render_context *, tm_render_param *);
static uint64_t (*f_render_context_update)(mpv_render_context *);
static void (*f_render_context_set_update_callback)(mpv_render_context *, void (*)(void *), void *);
static void (*f_render_context_free)(mpv_render_context *);

#define TM_SYM(var, name) \
	*(void **)(&var) = dlsym(h, name); \
	if (!var) { snprintf(err, errlen, "%s: missing %s", lib, name); dlclose(h); return -1; }

// tm_try opens lib and resolves every symbol; installs them when keep.
static int tm_try(const char *lib, int keep, char *err, int errlen) {
	if (keep && tm_lib) return 0;
	void *h = dlopen(lib, RTLD_NOW | RTLD_LOCAL);
	if (!h) {
		const char *e = dlerror();
		snprintf(err, errlen, "%s", e ? e : lib);
		return -1;
	}
	TM_SYM(f_create, "mpv_create");
	TM_SYM(f_initialize, "mpv_initialize");
	TM_SYM(f_terminate_destroy, "mpv_terminate_destroy");
	TM_SYM(f_set_option_string, "mpv_set_option_string");
	TM_SYM(f_command, "mpv_command");
	TM_SYM(f_command_async, "mpv_command_async");
	TM_SYM(f_observe_property, "mpv_observe_property");
	TM_SYM(f_wait_event, "mpv_wait_event");
	TM_SYM(f_wakeup, "mpv_wakeup");
	TM_SYM(f_error_string, "mpv_error_string");
	TM_SYM(f_render_context_create, "mpv_render_context_create");
	TM_SYM(f_render_context_render, "mpv_render_context_render");
	TM_SYM(f_render_context_update, "mpv_render_context_update");
	TM_SYM(f_render_context_set_update_callback, "mpv_render_context_set_update_callback");
	TM_SYM(f_render_context_free, "mpv_render_context_free");
	if (keep) tm_lib = h; else dlclose(h);
	return 0;
}

static mpv_handle *tm_create(void) { return f_create(); }
static int tm_initialize(mpv_handle *h) { return f_initialize(h); }
static void tm_terminate_destroy(mpv_handle *h) { f_terminate_destroy(h); }
static int tm_set_option(mpv_handle *h, const char *k, const char *v) { return f_set_option_string(h, k, v); }
static int tm_observe(mpv_handle *h, uint64_t id, const char *name, int format) { return f_observe_property(h, id, name, format); }
static int tm_command(mpv_handle *h, const char **args) { return f_command(h, args); }
static int tm_command_async(mpv_handle *h, const char **args) { return f_command_async(h, 0, args); }
static void tm_wakeup(mpv_handle *h) { f_wakeup(h); }
static const char *tm_error_string(int e) { return f_error_string(e); }

typedef struct {
	int id, error, format, flag, reason;
	uint64_t userdata;
	double d;
	int64_t i;
	char str[512];
	char *lstr; // a string str is too short for; freed by the caller
	char errmsg[96];
} tm_event;

// tm_next waits for an event and flattens it.
static void tm_next(mpv_handle *h, double timeout, tm_event *out) {
	tm_mpv_event *e = f_wait_event(h, timeout);
	memset(out, 0, sizeof *out);
	out->id = e->event_id;
	out->error = e->error;
	out->userdata = e->reply_userdata;
	if (e->event_id == 22 && e->data) {
		tm_mpv_event_property *p = e->data;
		out->format = p->format;
		if (p->data) {
			switch (p->format) {
			case 1: {
				char *s = *(char **)p->data;
				if (s && strlen(s) >= sizeof out->str) out->lstr = strdup(s);
				else if (s) snprintf(out->str, sizeof out->str, "%s", s);
			} break;
			case 3: out->flag = *(int *)p->data; break;
			case 4: out->i = *(int64_t *)p->data; break;
			case 5: out->d = *(double *)p->data; break;
			}
		}
	} else if (e->event_id == 7 && e->data) {
		tm_mpv_event_end_file *f = e->data;
		out->reason = f->reason;
		out->error = f->error;
		if (f->error < 0) snprintf(out->errmsg, sizeof out->errmsg, "%s", f_error_string(f->error));
	}
}

// tm_signal is set by mpv's render update callback.
typedef struct {
	pthread_mutex_t mu;
	pthread_cond_t cond;
	int pending;
} tm_signal;

static tm_signal *tm_signal_new(void) {
	tm_signal *s = calloc(1, sizeof *s);
	pthread_condattr_t a;
	pthread_condattr_init(&a);
	pthread_condattr_setclock(&a, CLOCK_MONOTONIC);
	pthread_mutex_init(&s->mu, NULL);
	pthread_cond_init(&s->cond, &a);
	pthread_condattr_destroy(&a);
	return s;
}

static void tm_signal_free(tm_signal *s) {
	pthread_cond_destroy(&s->cond);
	pthread_mutex_destroy(&s->mu);
	free(s);
}

static void tm_signal_poke(void *p) {
	tm_signal *s = p;
	pthread_mutex_lock(&s->mu);
	s->pending = 1;
	pthread_cond_signal(&s->cond);
	pthread_mutex_unlock(&s->mu);
}

// tm_signal_wait waits up to ms for a poke; returns and clears it.
static int tm_signal_wait(tm_signal *s, int ms) {
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	ts.tv_sec += ms / 1000;
	ts.tv_nsec += (long)(ms % 1000) * 1000000L;
	if (ts.tv_nsec >= 1000000000L) { ts.tv_sec++; ts.tv_nsec -= 1000000000L; }
	pthread_mutex_lock(&s->mu);
	while (!s->pending) {
		if (pthread_cond_timedwait(&s->cond, &s->mu, &ts) != 0) break;
	}
	int r = s->pending;
	s->pending = 0;
	pthread_mutex_unlock(&s->mu);
	return r;
}

static int tm_render_create(mpv_render_context **out, mpv_handle *h, tm_signal *sig) {
	char api[] = "sw";
	tm_render_param params[] = {{1, api}, {0, NULL}};
	int r = f_render_context_create(out, h, params);
	if (r >= 0) f_render_context_set_update_callback(*out, tm_signal_poke, sig);
	return r;
}

static uint64_t tm_render_update(mpv_render_context *c) { return f_render_context_update(c); }
static void tm_render_free(mpv_render_context *c) { f_render_context_free(c); }

// tm_render_sw draws the current frame as rgb0 and sets alpha opaque.
static int tm_render_sw(mpv_render_context *c, int w, int h, size_t stride, void *pix) {
	int size[2] = {w, h};
	char format[] = "rgb0";
	tm_render_param params[] = {{17, size}, {18, format}, {19, &stride}, {20, pix}, {0, NULL}};
	int r = f_render_context_render(c, params);
	if (r < 0) return r;
	for (int y = 0; y < h; y++) {
		uint8_t *row = (uint8_t *)pix + (size_t)y * stride;
		for (int x = 0; x < w; x++) row[x * 4 + 3] = 255;
	}
	return r;
}
*/
import "C"

import (
	"fmt"
	"image"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	libOnce sync.Once
	libErr  error
)

// Available loads libmpv once; nil when usable.
func Available() error {
	libOnce.Do(func() { libErr = loadLib(LibmpvNames, true) })
	return libErr
}

// loadLib tries names in order; keep installs the first that resolves.
func loadLib(names []string, keep bool) error {
	var msgs []string
	for _, n := range names {
		cn := C.CString(n)
		var buf [256]C.char
		r := C.tm_try(cn, boolC(keep), &buf[0], C.int(len(buf)))
		C.free(unsafe.Pointer(cn))
		if r == 0 {
			return nil
		}
		msgs = append(msgs, C.GoString(&buf[0]))
	}
	return fmt.Errorf("%w: %v", ErrNoLibmpv, msgs)
}

func boolC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// Player is one libmpv handle playing one source. Methods are safe for
// concurrent use; controls after Close do nothing.
type Player struct {
	h    *C.mpv_handle
	rctx *C.mpv_render_context
	sig  *C.tm_signal
	opts Options
	// target is what loadfile opens.
	target string

	// hmu: commands hold it shared, teardown exclusive.
	hmu    sync.RWMutex
	closed atomic.Bool

	mu         sync.Mutex
	st         mpvState
	bufs       [3]*image.RGBA
	seqs       [3]uint64
	latest     int // newest rendered buffer, -1 none
	shown      int // buffer Frame handed out last, -1 none
	seq        uint64
	rendered   int
	redraw     bool
	lastChange time.Time

	stop       chan struct{}
	renderDone chan struct{}
	eventDone  chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
}

// New starts mpv on src. Errors: ErrNoLibmpv, or mpv setup failures.
// Playback errors show in Status().Err.
func New(src Source, o Options) (*Player, error) {
	if err := Available(); err != nil {
		return nil, err
	}
	if src.target() == "" {
		return nil, fmt.Errorf("mpv: no source")
	}
	if o.MaxSide <= 0 {
		o.MaxSide = DefaultMaxSide
	}
	o.Volume = clampVolume(o.Volume)
	h := C.tm_create()
	if h == nil {
		return nil, fmt.Errorf("mpv: create failed")
	}
	fail := func(what string, r C.int) (*Player, error) {
		C.tm_terminate_destroy(h)
		return nil, fmt.Errorf("mpv: %s: %s", what, C.GoString(C.tm_error_string(r)))
	}
	for _, kv := range optionList(src, o) {
		k, v := C.CString(kv[0]), C.CString(kv[1])
		r := C.tm_set_option(h, k, v)
		C.free(unsafe.Pointer(k))
		C.free(unsafe.Pointer(v))
		if r < 0 {
			return fail("option "+kv[0], r)
		}
	}
	for _, kv := range optional {
		k, v := C.CString(kv[0]), C.CString(kv[1])
		C.tm_set_option(h, k, v)
		C.free(unsafe.Pointer(k))
		C.free(unsafe.Pointer(v))
	}
	if r := C.tm_initialize(h); r < 0 {
		return fail("initialize", r)
	}
	for _, p := range observed {
		n := C.CString(p.name)
		C.tm_observe(h, C.uint64_t(p.id), n, C.int(p.format))
		C.free(unsafe.Pointer(n))
	}
	for i, name := range o.Observe {
		n := C.CString(name)
		C.tm_observe(h, C.uint64_t(uint64(propExtra)+uint64(i)), n, C.int(fmtString))
		C.free(unsafe.Pointer(n))
	}
	sig := C.tm_signal_new()
	var rctx *C.mpv_render_context
	if r := C.tm_render_create(&rctx, h, sig); r < 0 {
		C.tm_signal_free(sig)
		return fail("render context", r)
	}
	p := &Player{
		h: h, rctx: rctx, sig: sig, opts: o, target: src.target(),
		latest: -1, shown: -1,
		stop: make(chan struct{}), renderDone: make(chan struct{}), eventDone: make(chan struct{}), done: make(chan struct{}),
	}
	p.st = mpvState{paused: o.Paused, volume: o.Volume, mute: o.Mute, speed: 1, loop: o.Loop, extra: o.Observe}
	if o.YTDL && src.Path == "" {
		p.st.retries = ytdlRetries
	}
	go p.eventLoop()
	go p.renderLoop()
	if err := p.command(false, "loadfile", p.target); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// command runs an mpv command; async ones return once queued. Nothing
// after Close.
func (p *Player) command(async bool, args ...string) error {
	p.hmu.RLock()
	defer p.hmu.RUnlock()
	if p.closed.Load() {
		return nil
	}
	n := len(args) + 1
	arr := (**C.char)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(uintptr(0)))))
	defer C.free(unsafe.Pointer(arr))
	s := unsafe.Slice(arr, n)
	for i, a := range args {
		s[i] = C.CString(a)
	}
	s[len(args)] = nil
	defer func() {
		for i := range args {
			C.free(unsafe.Pointer(s[i]))
		}
	}()
	var r C.int
	if async {
		r = C.tm_command_async(p.h, arr)
	} else {
		r = C.tm_command(p.h, arr)
	}
	if r < 0 {
		return fmt.Errorf("mpv: %s: %s", args[0], C.GoString(C.tm_error_string(r)))
	}
	return nil
}

// send queues a command.
func (p *Player) send(args ...string) { _ = p.command(true, args...) }

// changed runs OnChange; time-pos at most every 100 ms.
func (p *Player) changed(timePos bool) {
	if p.opts.OnChange == nil {
		return
	}
	if timePos {
		p.mu.Lock()
		now := time.Now()
		if now.Sub(p.lastChange) < 100*time.Millisecond {
			p.mu.Unlock()
			return
		}
		p.lastChange = now
		p.mu.Unlock()
	}
	p.opts.OnChange()
}

func (p *Player) eventLoop() {
	defer close(p.eventDone)
	var ce C.tm_event
	for {
		C.tm_next(p.h, -1, &ce)
		str := C.GoString(&ce.str[0])
		if ce.lstr != nil {
			str = C.GoString(ce.lstr)
			C.free(unsafe.Pointer(ce.lstr))
		}
		select {
		case <-p.stop:
			return
		default:
		}
		e := mpvEvent{
			id: int(ce.id), err: int(ce.error), prop: propID(ce.userdata), format: int(ce.format),
			d: float64(ce.d), i: int64(ce.i), flag: ce.flag != 0, str: str,
			reason: int(ce.reason), errMsg: C.GoString(&ce.errmsg[0]),
		}
		if e.id == evShutdown {
			return
		}
		p.mu.Lock()
		retry := p.st.retry(e)
		changed := !retry && p.st.apply(e)
		p.mu.Unlock()
		if retry {
			p.send("loadfile", p.target)
			continue
		}
		if e.id == evPropertyChange && (e.prop == propDWidth || e.prop == propDHeight) {
			C.tm_signal_poke(unsafe.Pointer(p.sig))
		}
		if changed {
			p.changed(e.prop == propTimePos && e.id == evPropertyChange)
		}
	}
}

// nativeSize is mpv's display size from the dwidth, dheight events;
// zero before. Never mpv_get_property: it blocks while mpv waits for
// the first render.
func (p *Player) nativeSize() image.Point {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.st.w > 0 && p.st.h > 0 {
		return image.Pt(p.st.w, p.st.h)
	}
	return image.Point{}
}

func (p *Player) frameSize(native image.Point) image.Point {
	if p.opts.Size != nil {
		if native.X <= 0 || native.Y <= 0 {
			return image.Point{}
		}
		s := p.opts.Size(native)
		return image.Pt(min(max(s.X, 0), maxFrameSide), min(max(s.Y, 0), maxFrameSide))
	}
	return RenderSize(native, 1, p.opts.MaxSide)
}

func (p *Player) renderLoop() {
	defer close(p.renderDone)
	defer C.tm_render_free(p.rctx)
	var last image.Point
	// scratch takes the first frame before dwidth is known: mpv shows
	// no frame, and sets no dwidth, until one is rendered.
	scratch := make([]byte, 2*2*4)
	for {
		C.tm_signal_wait(p.sig, 100)
		select {
		case <-p.stop:
			return
		default:
		}
		flags := C.tm_render_update(p.rctx)
		native := p.nativeSize()
		want := p.frameSize(native)
		p.mu.Lock()
		force := p.redraw
		p.redraw = false
		p.mu.Unlock()
		if native == (image.Point{}) {
			if flags&1 != 0 {
				C.tm_render_sw(p.rctx, 2, 2, 8, unsafe.Pointer(&scratch[0]))
			}
			continue
		}
		if want.X <= 0 || want.Y <= 0 || (flags&1 == 0 && want == last && !force) {
			continue
		}
		i, buf := p.backBuffer(want)
		if C.tm_render_sw(p.rctx, C.int(want.X), C.int(want.Y), C.size_t(buf.Stride), unsafe.Pointer(&buf.Pix[0])) < 0 {
			continue
		}
		last = want
		p.mu.Lock()
		p.seq++
		p.latest, p.seqs[i] = i, p.seq
		p.rendered++
		p.mu.Unlock()
		if p.opts.OnFrame != nil {
			p.opts.OnFrame()
		}
	}
}

// backBuffer returns a buffer that is neither held nor latest, sized to
// size with a 64-byte aligned stride.
func (p *Player) backBuffer(size image.Point) (int, *image.RGBA) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := 0
	for i == p.latest || i == p.shown {
		i++
	}
	b := p.bufs[i]
	if b == nil || b.Rect.Size() != size {
		stride := (size.X*4 + 63) &^ 63
		b = &image.RGBA{Pix: make([]byte, stride*size.Y+64), Stride: stride, Rect: image.Rectangle{Max: size}}
		p.bufs[i] = b
	}
	return i, b
}

// Redraw renders the current frame again, at Options.Size asked anew.
func (p *Player) Redraw() {
	p.mu.Lock()
	p.redraw = true
	p.mu.Unlock()
	C.tm_signal_poke(unsafe.Pointer(p.sig))
}

// Rendered is the count of frames rendered so far.
func (p *Player) Rendered() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rendered
}

// Frame is the newest rendered frame and its sequence number, rgb0
// with alpha opaque; false before the first. It stays unchanged until
// the next Frame call.
func (p *Player) Frame() (*image.RGBA, uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.latest < 0 {
		return nil, 0, false
	}
	p.shown = p.latest
	return p.bufs[p.shown], p.seqs[p.shown], true
}

// Status is the cached state.
func (p *Player) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.st.status()
}

// SetPause pauses or resumes; resuming at the end restarts from 0.
func (p *Player) SetPause(paused bool) {
	p.mu.Lock()
	restart := !paused && p.st.eof
	p.st.paused = paused
	if restart {
		p.st.eof, p.st.pos = false, 0
	}
	p.mu.Unlock()
	if restart {
		p.send("seek", "0", "absolute+exact")
	}
	p.send("set", "pause", yesNo(paused))
}

func (p *Player) Play()  { p.SetPause(false) }
func (p *Player) Pause() { p.SetPause(true) }

// TogglePause pauses a running file, else plays.
func (p *Player) TogglePause() {
	p.mu.Lock()
	playing := !p.st.paused && !p.st.eof
	p.mu.Unlock()
	p.SetPause(playing)
}

// Seek moves to pos; exact false allows a faster keyframe seek.
func (p *Player) Seek(pos time.Duration, exact bool) {
	mode := "absolute+keyframes"
	if exact {
		mode = "absolute+exact"
	}
	pos = max(pos, 0)
	p.mu.Lock()
	p.st.pos, p.st.eof = pos.Seconds(), false
	p.mu.Unlock()
	p.send("seek", strconv.FormatFloat(pos.Seconds(), 'f', 3, 64), mode)
}

// SeekBy moves by d, back when negative.
func (p *Player) SeekBy(d time.Duration) {
	p.mu.Lock()
	pos := max(p.st.pos+d.Seconds(), 0)
	if p.st.dur > 0 {
		pos = min(pos, p.st.dur)
	}
	p.st.pos = pos
	p.mu.Unlock()
	p.send("seek", strconv.FormatFloat(d.Seconds(), 'f', 3, 64), "relative")
}

// SeekPercent moves to frac of the duration, clamped to 0..1.
func (p *Player) SeekPercent(frac float64) {
	frac = min(max(frac, 0), 1)
	p.mu.Lock()
	p.st.pos, p.st.eof = p.st.dur*frac, false
	p.mu.Unlock()
	p.send("seek", strconv.FormatFloat(frac*100, 'f', 3, 64), "absolute-percent")
}

// Step moves n frames and pauses.
func (p *Player) Step(n int) {
	p.mu.Lock()
	p.st.paused = true
	p.mu.Unlock()
	cmd := "frame-step"
	if n < 0 {
		cmd, n = "frame-back-step", -n
	}
	for range n {
		p.send(cmd)
	}
}

func (p *Player) SetLoop(on bool) {
	p.mu.Lock()
	p.st.loop = on
	p.mu.Unlock()
	p.send("set", "loop-file", loopValue(on))
}

// SetSpeed clamps s to 0.1..8.
func (p *Player) SetSpeed(s float64) {
	s = min(max(s, 0.1), 8)
	p.mu.Lock()
	p.st.speed = s
	p.mu.Unlock()
	p.send("set", "speed", strconv.FormatFloat(s, 'f', -1, 64))
}

// SetVolume clamps v to 0..MaxVolume.
func (p *Player) SetVolume(v float64) {
	v = clampVolume(v)
	p.mu.Lock()
	p.st.volume = v
	p.mu.Unlock()
	p.send("set", "volume", strconv.FormatFloat(v, 'f', -1, 64))
}

func (p *Player) SetMute(on bool) {
	p.mu.Lock()
	p.st.mute = on
	p.mu.Unlock()
	p.send("set", "mute", yesNo(on))
}

// SetSub shows the subtitle track id, a Status.Subs ID; 0 none.
func (p *Player) SetSub(id int) {
	p.mu.Lock()
	p.st.sub = id
	p.mu.Unlock()
	v := "no"
	if id > 0 {
		v = strconv.Itoa(id)
	}
	p.send("set", "sid", v)
}

// Close stops playback; teardown finishes on a goroutine, then Done
// closes. Idempotent.
func (p *Player) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.stop)
		go func() {
			defer close(p.done)
			C.tm_signal_poke(unsafe.Pointer(p.sig))
			<-p.renderDone // the render context goes first, as mpv requires
			C.tm_wakeup(p.h)
			<-p.eventDone
			p.hmu.Lock()
			C.tm_terminate_destroy(p.h)
			p.hmu.Unlock()
			C.tm_signal_free(p.sig)
		}()
	})
}

// Done is closed once Close has destroyed the mpv handle.
func (p *Player) Done() <-chan struct{} { return p.done }
