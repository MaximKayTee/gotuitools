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

typedef struct { void *(*get_proc_address)(void *, const char *); void *get_proc_address_ctx; } tm_gl_init;
typedef struct { int fbo, w, h, internal_format; } tm_gl_fbo;

// tm_render_create_gl is tm_render_create for the GL context current on
// this thread; getproc is its address lookup.
static int tm_render_create_gl(mpv_render_context **out, mpv_handle *h, tm_signal *sig, void *getproc) {
	char api[] = "opengl";
	tm_gl_init init = {(void *(*)(void *, const char *))getproc, NULL};
	tm_render_param params[] = {{1, api}, {2, &init}, {0, NULL}};
	int r = f_render_context_create(out, h, params);
	if (r >= 0) f_render_context_set_update_callback(*out, tm_signal_poke, sig);
	return r;
}

// tm_render_gl draws the current frame into the framebuffer fbo, 8-bit
// RGB, its first row the top.
static int tm_render_gl(mpv_render_context *c, int fbo, int w, int h) {
	tm_gl_fbo target = {fbo, w, h, 0x8051};
	int flip = 0;
	tm_render_param params[] = {{3, &target}, {4, &flip}, {0, NULL}};
	return f_render_context_render(c, params);
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
	"runtime"
	"slices"
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
	gl   *glContext // render goroutine; nil: the software renderer
	sig  *C.tm_signal
	opts Options
	// target is what loadfile opens.
	target string

	// hmu: commands hold it shared, teardown exclusive.
	hmu    sync.RWMutex
	closed atomic.Bool

	mu         sync.Mutex
	gpu        string // the GPU's name; "" the software renderer
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
	renderStop chan struct{} // closed once the video output is gone
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
	for _, kv := range optional {
		k, v := C.CString(kv[0]), C.CString(kv[1])
		C.tm_set_option(h, k, v)
		C.free(unsafe.Pointer(k))
		C.free(unsafe.Pointer(v))
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
	p := &Player{
		h: h, sig: C.tm_signal_new(), opts: o, target: src.target(),
		latest: -1, shown: -1,
		stop: make(chan struct{}), renderStop: make(chan struct{}), renderDone: make(chan struct{}), eventDone: make(chan struct{}), done: make(chan struct{}),
	}
	p.st = mpvState{chapter: -1, paused: o.Paused, volume: o.Volume, mute: o.Mute, userMute: o.Mute, speed: 1, loop: o.Loop, extra: o.Observe,
		spans: slices.Clone(o.Spans), landed: -1, skipped: -1}
	if o.YTDL && src.Path == "" {
		p.st.retries = ytdlRetries
	}
	created := make(chan C.int, 1)
	go p.renderLoop(created)
	if r := <-created; r < 0 {
		C.tm_signal_free(p.sig)
		return fail("render context", r)
	}
	// the event goroutine after loadfile: OnChange not before New returns
	err := p.command(false, "loadfile", p.target)
	go p.eventLoop()
	if err != nil {
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
	return p.run(async, args...)
}

// run runs an mpv command on the handle, hmu held.
func (p *Player) run(async bool, args ...string) error {
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

// Command queues an mpv command, its name and arguments as mpv's
// client API takes them: "loadfile", url, "append" plays url after what
// plays, with no gap in the sound under gapless-audio. An error when
// mpv does not take the command; what running it comes to is not
// waited for.
func (p *Player) Command(args ...string) error {
	if len(args) == 0 {
		return nil
	}
	return p.command(true, args...)
}

// SetProperty sets an mpv property or option by name while it plays,
// its value as text: "af", "volume-gain". Not for what Player has a
// method for: Status would not follow.
func (p *Player) SetProperty(name, value string) { p.send("set", name, value) }

// changed runs OnChange; time-pos and demuxer-cache-time at most every
// 100 ms.
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
		to, remute := -1.0, false
		if !retry {
			to, remute = p.st.follow(e)
		}
		mute := p.st.muted()
		p.mu.Unlock()
		if retry {
			p.send("loadfile", p.target)
			continue
		}
		if to >= 0 {
			p.send("seek", strconv.FormatFloat(to, 'f', 3, 64), "absolute+exact")
		}
		if remute {
			p.send("set", "mute", yesNo(mute))
		}
		if e.id == evPropertyChange && (e.prop == propDWidth || e.prop == propDHeight) {
			C.tm_signal_poke(unsafe.Pointer(p.sig))
		}
		if changed {
			p.changed((e.prop == propTimePos || e.prop == propCacheTime) && e.id == evPropertyChange)
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

// createRender makes the render context on the calling thread: the
// GPU's, on a GL context with no window, the thread locked to it; else,
// or with Options.Software, the software renderer's.
func (p *Player) createRender() C.int {
	if !p.opts.Software {
		runtime.LockOSThread()
		if gl, err := glOpen(); err == nil {
			if C.tm_render_create_gl(&p.rctx, p.h, p.sig, gl.getProc()) >= 0 {
				p.gl = gl
				p.mu.Lock()
				p.gpu = gl.renderer()
				p.mu.Unlock()
				return 0
			}
			gl.close()
		}
		runtime.UnlockOSThread()
	}
	return C.tm_render_create(&p.rctx, p.h, p.sig)
}

// freeRender frees the render context and the GL context under it, on
// the thread that made them.
func (p *Player) freeRender() {
	if p.rctx != nil {
		C.tm_render_free(p.rctx)
		p.rctx = nil
	}
	if p.gl != nil {
		p.gl.close()
		p.gl = nil
		runtime.UnlockOSThread()
	}
	p.mu.Lock()
	p.gpu = ""
	p.mu.Unlock()
}

// glFails is the count of frames running a GPU may fail before the
// software renderer takes over.
const glFails = 3

// toSoftware gives a failing GPU's place to the software renderer. mpv
// drops the video with its render context: the first video track is
// chosen again. A video without sound ends there with mpv's error.
func (p *Player) toSoftware() bool {
	p.freeRender()
	if C.tm_render_create(&p.rctx, p.h, p.sig) < 0 {
		p.rctx = nil
		return false
	}
	p.send("set", "vid", "1")
	return true
}

// draw renders the current frame into buf: on the GPU, read back, or
// by the software renderer.
func (p *Player) draw(buf *image.RGBA) bool {
	w, h := buf.Rect.Dx(), buf.Rect.Dy()
	if p.gl == nil {
		return C.tm_render_sw(p.rctx, C.int(w), C.int(h), C.size_t(buf.Stride), unsafe.Pointer(&buf.Pix[0])) >= 0
	}
	fbo, ok := p.gl.size(buf.Rect.Size())
	return ok && C.tm_render_gl(p.rctx, C.int(fbo), C.int(w), C.int(h)) >= 0 && p.gl.read(buf)
}

func (p *Player) renderLoop(created chan<- C.int) {
	r := p.createRender()
	created <- r
	if r < 0 {
		return
	}
	defer close(p.renderDone)
	defer p.freeRender()
	fails := 0
	// draw is p.draw; false also when the software renderer could not
	// take a failing GPU's place: no frames from then on.
	draw := func(buf *image.RGBA) (drawn, ok bool) {
		if p.draw(buf) {
			fails = 0
			return true, true
		}
		if p.gl == nil {
			return false, true
		}
		if fails++; fails < glFails {
			return false, true
		}
		fails = 0
		ok = p.toSoftware()
		p.Redraw()
		return false, ok
	}
	var last image.Point
	// scratch takes the first frame before dwidth is known: mpv shows
	// no frame, and sets no dwidth, until one is rendered.
	scratch := &image.RGBA{Pix: make([]byte, 2*2*4), Stride: 8, Rect: image.Rect(0, 0, 2, 2)}
	for {
		C.tm_signal_wait(p.sig, 100)
		select {
		case <-p.renderStop:
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
				if _, ok := draw(scratch); !ok {
					return
				}
			}
			continue
		}
		if want.X <= 0 || want.Y <= 0 || (flags&1 == 0 && want == last && !force) {
			continue
		}
		i, buf := p.backBuffer(want)
		drawn, ok := draw(buf)
		if !ok {
			return
		}
		if !drawn {
			if p.gl != nil {
				p.Redraw() // the GPU again, glFails times at most
			}
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

// Renderer is the name of the GPU the frames are rendered on; "" mpv's
// software renderer.
func (p *Player) Renderer() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gpu
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

// SetMute mutes or unmutes; a mute span keeps it muted.
func (p *Player) SetMute(on bool) {
	p.mu.Lock()
	p.st.userMute = on
	on = p.st.muted()
	p.st.mute = on
	p.mu.Unlock()
	p.send("set", "mute", yesNo(on))
}

// SetSpans replaces the spans to skip or play muted.
func (p *Player) SetSpans(spans []Span) {
	p.mu.Lock()
	p.st.spans = slices.Clone(spans)
	p.mu.Unlock()
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

// AddChapter jumps by d chapters: 1 the next, -1 the start of the one
// playing, or the one before within mpv's chapter-seek-threshold of its
// start. Past the last: the end.
func (p *Player) AddChapter(d int) { p.send("add", "chapter", strconv.Itoa(d)) }

// SetAudio plays the audio track id, a Status.Audios ID; 0 none.
func (p *Player) SetAudio(id int) {
	p.mu.Lock()
	p.st.audio = id
	p.mu.Unlock()
	v := "no"
	if id > 0 {
		v = strconv.Itoa(id)
	}
	p.send("set", "aid", v)
}

// Close pauses, so the sound stops at once, and tears down on a
// goroutine; then Done closes. Idempotent.
func (p *Player) Close() {
	p.closeOnce.Do(func() {
		p.send("set", "pause", "yes")
		p.closed.Store(true)
		close(p.stop)
		go func() {
			defer close(p.done)
			// the video output goes while its frames are still taken: one
			// left untaken holds mpv 200 ms
			p.hmu.RLock()
			_ = p.run(false, "set", "vid", "no")
			p.hmu.RUnlock()
			close(p.renderStop)
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
