//go:build cgo && linux

package mpv

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

// libEGL is opened with dlopen, as libmpv is; GL entry points come from
// eglGetProcAddress. Types and constants are declared here; no EGL or
// GL headers needed.
#define TG_PLATFORM_DEVICE      0x313F
#define TG_PLATFORM_SURFACELESS 0x31DD
#define TG_EXTENSIONS           0x3055
#define TG_SURFACE_TYPE         0x3033
#define TG_PBUFFER_BIT          0x0001
#define TG_RENDERABLE_TYPE      0x3040
#define TG_OPENGL_BIT           0x0008
#define TG_OPENGL_ES3_BIT       0x0040
#define TG_RED_SIZE             0x3024
#define TG_GREEN_SIZE           0x3023
#define TG_BLUE_SIZE            0x3022
#define TG_NONE                 0x3038
#define TG_OPENGL_API           0x30A2
#define TG_OPENGL_ES_API        0x30A0
#define TG_CONTEXT_MAJOR        0x3098
#define TG_CONTEXT_MINOR        0x30FB
#define TG_CONTEXT_PROFILE_MASK 0x30FD
#define TG_CONTEXT_CORE_BIT     0x0001
#define TG_WIDTH                0x3057
#define TG_HEIGHT               0x3056

#define TG_FRAMEBUFFER          0x8D40
#define TG_READ_FRAMEBUFFER     0x8CA8
#define TG_RENDERBUFFER         0x8D41
#define TG_COLOR_ATTACHMENT0    0x8CE0
#define TG_FRAMEBUFFER_COMPLETE 0x8CD5
#define TG_RGB8                 0x8051
#define TG_RGBA                 0x1908
#define TG_UNSIGNED_BYTE        0x1401
#define TG_PACK_ROW_LENGTH      0x0D02
#define TG_RENDERER             0x1F01

static void *tg_lib;
static void *(*e_GetProcAddress)(const char *);
static const char *(*e_QueryString)(void *, int32_t);
static unsigned (*e_Initialize)(void *, int32_t *, int32_t *);
static unsigned (*e_BindAPI)(unsigned);
static unsigned (*e_ChooseConfig)(void *, const int32_t *, void **, int32_t, int32_t *);
static void *(*e_CreateContext)(void *, void *, void *, const int32_t *);
static void *(*e_CreatePbufferSurface)(void *, void *, const int32_t *);
static unsigned (*e_MakeCurrent)(void *, void *, void *, void *);
static unsigned (*e_DestroyContext)(void *, void *);
static unsigned (*e_DestroySurface)(void *, void *);
static unsigned (*e_ReleaseThread)(void);
static unsigned (*e_QueryDevices)(int32_t, void **, int32_t *);
static void *(*e_GetPlatformDisplay)(unsigned, void *, const int32_t *);

static void (*g_GenFramebuffers)(int, unsigned *);
static void (*g_DeleteFramebuffers)(int, const unsigned *);
static void (*g_BindFramebuffer)(unsigned, unsigned);
static void (*g_GenRenderbuffers)(int, unsigned *);
static void (*g_DeleteRenderbuffers)(int, const unsigned *);
static void (*g_BindRenderbuffer)(unsigned, unsigned);
static void (*g_RenderbufferStorage)(unsigned, unsigned, int, int);
static void (*g_FramebufferRenderbuffer)(unsigned, unsigned, unsigned, unsigned);
static unsigned (*g_CheckFramebufferStatus)(unsigned);
static void (*g_ReadPixels)(int, int, int, int, unsigned, unsigned, void *);
static void (*g_PixelStorei)(unsigned, int);
static const unsigned char *(*g_GetString)(unsigned);
static unsigned (*g_GetError)(void);

// The display and API the first context was made on; every later one
// is made on it.
static void *tg_display;
static unsigned tg_api;

typedef struct {
	void *ctx, *surf;
	unsigned fbo, rbo;
	int w, h;
	char renderer[128];
} tg_gl;

#define TG_ESYM(var, name) \
	*(void **)(&var) = dlsym(h, name); \
	if (!var) { snprintf(err, errlen, "libEGL: missing %s", name); dlclose(h); return -1; }
#define TG_GSYM(var, name) \
	*(void **)(&var) = e_GetProcAddress(name); \
	if (!var) { snprintf(err, errlen, "GL: missing %s", name); return -1; }

static int tg_load(char *err, int errlen) {
	if (tg_lib) return 0;
	void *h = dlopen("libEGL.so.1", RTLD_NOW | RTLD_GLOBAL);
	if (!h) {
		const char *e = dlerror();
		snprintf(err, errlen, "%s", e ? e : "libEGL.so.1");
		return -1;
	}
	TG_ESYM(e_GetProcAddress, "eglGetProcAddress");
	TG_ESYM(e_QueryString, "eglQueryString");
	TG_ESYM(e_Initialize, "eglInitialize");
	TG_ESYM(e_BindAPI, "eglBindAPI");
	TG_ESYM(e_ChooseConfig, "eglChooseConfig");
	TG_ESYM(e_CreateContext, "eglCreateContext");
	TG_ESYM(e_CreatePbufferSurface, "eglCreatePbufferSurface");
	TG_ESYM(e_MakeCurrent, "eglMakeCurrent");
	TG_ESYM(e_DestroyContext, "eglDestroyContext");
	TG_ESYM(e_DestroySurface, "eglDestroySurface");
	TG_ESYM(e_ReleaseThread, "eglReleaseThread");
	tg_lib = h;
	return 0;
}

static int tg_gl_syms(char *err, int errlen) {
	TG_GSYM(g_GenFramebuffers, "glGenFramebuffers");
	TG_GSYM(g_DeleteFramebuffers, "glDeleteFramebuffers");
	TG_GSYM(g_BindFramebuffer, "glBindFramebuffer");
	TG_GSYM(g_GenRenderbuffers, "glGenRenderbuffers");
	TG_GSYM(g_DeleteRenderbuffers, "glDeleteRenderbuffers");
	TG_GSYM(g_BindRenderbuffer, "glBindRenderbuffer");
	TG_GSYM(g_RenderbufferStorage, "glRenderbufferStorage");
	TG_GSYM(g_FramebufferRenderbuffer, "glFramebufferRenderbuffer");
	TG_GSYM(g_CheckFramebufferStatus, "glCheckFramebufferStatus");
	TG_GSYM(g_ReadPixels, "glReadPixels");
	TG_GSYM(g_PixelStorei, "glPixelStorei");
	TG_GSYM(g_GetString, "glGetString");
	TG_GSYM(g_GetError, "glGetError");
	return 0;
}

static void tg_drop(void *dpy, tg_gl *g) {
	e_MakeCurrent(dpy, NULL, NULL, NULL);
	if (g->surf) e_DestroySurface(dpy, g->surf);
	if (g->ctx) e_DestroyContext(dpy, g->ctx);
	g->ctx = g->surf = NULL;
}

// tg_context makes a context of api on dpy current on this thread: a
// GPU's, never a software rasteriser's.
static int tg_context(void *dpy, unsigned api, tg_gl *g, char *err, int errlen) {
	int es = api == TG_OPENGL_ES_API;
	int32_t cattr[] = {TG_SURFACE_TYPE, TG_PBUFFER_BIT, TG_RENDERABLE_TYPE, es ? TG_OPENGL_ES3_BIT : TG_OPENGL_BIT,
		TG_RED_SIZE, 8, TG_GREEN_SIZE, 8, TG_BLUE_SIZE, 8, TG_NONE};
	int32_t core[] = {TG_CONTEXT_MAJOR, 3, TG_CONTEXT_MINOR, 3, TG_CONTEXT_PROFILE_MASK, TG_CONTEXT_CORE_BIT, TG_NONE};
	int32_t es3[] = {TG_CONTEXT_MAJOR, 3, TG_NONE};
	int32_t one[] = {TG_WIDTH, 1, TG_HEIGHT, 1, TG_NONE};
	void *cfg = NULL;
	int32_t n = 0;
	if (!e_BindAPI(api)) { snprintf(err, errlen, "EGL: API not supported"); return -1; }
	if (!e_ChooseConfig(dpy, cattr, &cfg, 1, &n) || n < 1) { snprintf(err, errlen, "EGL: no config"); return -1; }
	g->ctx = e_CreateContext(dpy, cfg, NULL, es ? es3 : core);
	if (!g->ctx) { snprintf(err, errlen, "EGL: no context"); return -1; }
	g->surf = e_CreatePbufferSurface(dpy, cfg, one);
	if (!e_MakeCurrent(dpy, g->surf, g->surf, g->ctx)) {
		snprintf(err, errlen, "EGL: context not made current");
		tg_drop(dpy, g);
		return -1;
	}
	if (tg_gl_syms(err, errlen) < 0) { tg_drop(dpy, g); return -1; }
	const char *r = (const char *)g_GetString(TG_RENDERER);
	snprintf(g->renderer, sizeof g->renderer, "%s", r ? r : "");
	if (!r || strstr(r, "llvmpipe") || strstr(r, "softpipe") || strstr(r, "SwiftShader") || strstr(r, "Software")) {
		snprintf(err, errlen, "GL: software renderer %s", g->renderer);
		tg_drop(dpy, g);
		return -1;
	}
	return 0;
}

// tg_try initializes dpy and makes a context on it, OpenGL before
// OpenGL ES.
static int tg_try(void *dpy, tg_gl *g, char *err, int errlen) {
	int32_t major, minor;
	if (!dpy || !e_Initialize(dpy, &major, &minor)) { snprintf(err, errlen, "EGL: display not initialized"); return -1; }
	unsigned apis[] = {TG_OPENGL_API, TG_OPENGL_ES_API};
	for (int i = 0; i < 2; i++) {
		if (tg_context(dpy, apis[i], g, err, errlen) == 0) {
			tg_display = dpy;
			tg_api = apis[i];
			return 0;
		}
	}
	return -1;
}

// tg_open makes a GL context current on this thread, with no window:
// on the display found before, else on the first EGL device with a
// GPU, else on Mesa's surfaceless platform. One caller at a time.
static int tg_open(tg_gl *g, char *err, int errlen) {
	memset(g, 0, sizeof *g);
	if (tg_load(err, errlen) < 0) return -1;
	if (tg_display) return tg_context(tg_display, tg_api, g, err, errlen);
	const char *ext = e_QueryString(NULL, TG_EXTENSIONS);
	if (!ext) { snprintf(err, errlen, "EGL: no client extensions"); return -1; }
	*(void **)(&e_GetPlatformDisplay) = e_GetProcAddress("eglGetPlatformDisplayEXT");
	if (!e_GetPlatformDisplay) { snprintf(err, errlen, "EGL: no eglGetPlatformDisplayEXT"); return -1; }
	snprintf(err, errlen, "EGL: no device or surfaceless platform");
	if (strstr(ext, "EGL_EXT_platform_device") && (strstr(ext, "EGL_EXT_device_enumeration") || strstr(ext, "EGL_EXT_device_base"))) {
		*(void **)(&e_QueryDevices) = e_GetProcAddress("eglQueryDevicesEXT");
		void *devs[16];
		int32_t n = 0;
		if (e_QueryDevices && e_QueryDevices(16, devs, &n)) {
			for (int i = 0; i < n; i++) {
				if (tg_try(e_GetPlatformDisplay(TG_PLATFORM_DEVICE, devs[i], NULL), g, err, errlen) == 0) return 0;
			}
		}
	}
	if (strstr(ext, "EGL_MESA_platform_surfaceless")) {
		if (tg_try(e_GetPlatformDisplay(TG_PLATFORM_SURFACELESS, NULL, NULL), g, err, errlen) == 0) return 0;
	}
	return -1;
}

// tg_close destroys g's objects and context and releases the thread.
static void tg_close(tg_gl *g) {
	if (!g->ctx) return;
	if (g->fbo) g_DeleteFramebuffers(1, &g->fbo);
	if (g->rbo) g_DeleteRenderbuffers(1, &g->rbo);
	g->fbo = g->rbo = 0;
	tg_drop(tg_display, g);
	e_ReleaseThread();
}

// tg_size makes g's framebuffer w x h, 8-bit RGB with no alpha: read
// back as RGBA its alpha is opaque.
static int tg_size(tg_gl *g, int w, int h) {
	if (g->fbo && g->w == w && g->h == h) return 0;
	if (!g->fbo) {
		g_GenFramebuffers(1, &g->fbo);
		g_GenRenderbuffers(1, &g->rbo);
	}
	g_BindRenderbuffer(TG_RENDERBUFFER, g->rbo);
	g_RenderbufferStorage(TG_RENDERBUFFER, TG_RGB8, w, h);
	g_BindRenderbuffer(TG_RENDERBUFFER, 0);
	g_BindFramebuffer(TG_FRAMEBUFFER, g->fbo);
	g_FramebufferRenderbuffer(TG_FRAMEBUFFER, TG_COLOR_ATTACHMENT0, TG_RENDERBUFFER, g->rbo);
	int ok = g_CheckFramebufferStatus(TG_FRAMEBUFFER) == TG_FRAMEBUFFER_COMPLETE;
	g_BindFramebuffer(TG_FRAMEBUFFER, 0);
	g->w = ok ? w : 0;
	g->h = ok ? h : 0;
	return ok ? 0 : -1;
}

// tg_read copies g's framebuffer into pix as RGBA, first row the top,
// stride bytes a row; the pack state is left at its default.
static int tg_read(tg_gl *g, size_t stride, void *pix) {
	while (g_GetError()) {}
	g_BindFramebuffer(TG_READ_FRAMEBUFFER, g->fbo);
	g_PixelStorei(TG_PACK_ROW_LENGTH, (int)(stride / 4));
	g_ReadPixels(0, 0, g->w, g->h, TG_RGBA, TG_UNSIGNED_BYTE, pix);
	g_PixelStorei(TG_PACK_ROW_LENGTH, 0);
	g_BindFramebuffer(TG_READ_FRAMEBUFFER, 0);
	return g_GetError() ? -1 : 0;
}

static void *tg_getproc(void *ctx, const char *name) { return e_GetProcAddress(name); }
static void *tg_getproc_ptr(void) { return (void *)tg_getproc; }
*/
import "C"

import (
	"errors"
	"image"
	"sync"
	"sync/atomic"
	"unsafe"
)

// glMu orders glOpen: the first finds the display for all.
var glMu sync.Mutex

// glContext is a GL context with no window, current on the thread it
// was opened on, and the framebuffer mpv renders into. Its methods are
// for that thread only.
type glContext struct {
	g C.tg_gl

	failing atomic.Bool // tests: read fails
}

// glOpen makes a GL context on a GPU current on the calling thread, an
// OS thread the caller has locked. An error without libEGL, without a
// GPU's driver, or with a software rasteriser only.
var glOpen = func() (*glContext, error) {
	glMu.Lock()
	defer glMu.Unlock()
	c := &glContext{}
	var buf [256]C.char
	if C.tg_open(&c.g, &buf[0], C.int(len(buf))) < 0 {
		return nil, errors.New(C.GoString(&buf[0]))
	}
	return c, nil
}

// renderer is the GPU's name as GL reports it.
func (c *glContext) renderer() string { return C.GoString(&c.g.renderer[0]) }

// getProc is the address lookup libmpv is given.
func (c *glContext) getProc() unsafe.Pointer { return C.tg_getproc_ptr() }

// size makes the framebuffer size pixels and returns its name.
func (c *glContext) size(size image.Point) (int, bool) {
	if C.tg_size(&c.g, C.int(size.X), C.int(size.Y)) < 0 {
		return 0, false
	}
	return int(c.g.fbo), true
}

// read copies the framebuffer into img, the size it was made.
func (c *glContext) read(img *image.RGBA) bool {
	if c.failing.Load() {
		return false
	}
	return C.tg_read(&c.g, C.size_t(img.Stride), unsafe.Pointer(&img.Pix[0])) == 0
}

func (c *glContext) close() { C.tg_close(&c.g) }
