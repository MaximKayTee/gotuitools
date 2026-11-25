//go:build cgo && linux

package mpv

import (
	"errors"
	"fmt"
	"image"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// sizedClip writes a w x h mp4 of ffmpeg's testsrc2 at 24 frames a
// second with a tone, secs long.
func sizedClip(t *testing.T, w, h, secs int) string {
	t.Helper()
	if err := Available(); err != nil {
		t.Skip(err)
	}
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	out := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=24:duration=%d", w, h, secs),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%d", secs),
		"-pix_fmt", "yuv420p", "-shortest", "-y", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, b)
	}
	return out
}

// onGPU opens path with o and skips the test without a GPU renderer.
func onGPU(t *testing.T, path string, o Options) *Player {
	t.Helper()
	p, err := New(Source{Path: path}, o)
	if err != nil {
		t.Fatal(err)
	}
	if p.Renderer() == "" {
		closeWait(t, p)
		t.Skip("no GPU renderer")
	}
	return p
}

// stillAt is path's frame at 1 s, paused, rendered at size; the
// renderer's name with it.
func stillAt(t *testing.T, path string, size image.Point, o Options) (*image.RGBA, string) {
	t.Helper()
	o.AudioOutput, o.Paused, o.Start = "null", true, time.Second
	o.Size = func(image.Point) image.Point { return size }
	p, err := New(Source{Path: path}, o)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWait(t, p)
	waitFor(t, "the frame at 1 s", func() bool {
		f, _, ok := p.Frame()
		return ok && f.Rect.Size() == size && p.Status().Loaded
	})
	time.Sleep(200 * time.Millisecond) // the seek's frame, not the first decoded
	f, _, _ := p.Frame()
	return f, p.Renderer()
}

// differ compares the colours of two frames of one size: the worst
// difference of a channel, the mean, and the mean against b upside
// down.
func differ(a, b *image.RGBA) (worst int, mean, flipped float64) {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	var sum, fsum int
	for y := range h {
		ra, rb, rf := a.Pix[y*a.Stride:], b.Pix[y*b.Stride:], b.Pix[(h-1-y)*b.Stride:]
		for x := 0; x < w*4; x++ {
			if x%4 == 3 {
				continue
			}
			d := int(ra[x]) - int(rb[x])
			if d < 0 {
				d = -d
			}
			f := int(ra[x]) - int(rf[x])
			if f < 0 {
				f = -f
			}
			worst = max(worst, d)
			sum += d
			fsum += f
		}
	}
	n := float64(w * h * 3)
	return worst, float64(sum) / n, float64(fsum) / n
}

func opaque(img *image.RGBA) bool {
	for y := range img.Rect.Dy() {
		row := img.Pix[y*img.Stride:]
		for x := range img.Rect.Dx() {
			if row[x*4+3] != 0xff {
				return false
			}
		}
	}
	return true
}

// The software renderer's frame is the reference: the GPU's is the same
// picture, the same way up, opaque. At the video's size, with the GPU's
// chroma scaling set to the software renderer's, the colours agree to a
// few steps; mpv's own scalers differ on edges.
func TestGPUFrameMatchesSoftware(t *testing.T) {
	path := sizedClip(t, 320, 240, 3)
	bilinear := [][2]string{{"cscale", "bilinear"}}
	for _, c := range []struct {
		name  string
		size  image.Point
		extra [][2]string
		worst int // 0: not bounded
	}{
		{"native size, chroma as software", image.Pt(320, 240), bilinear, 4},
		{"native size", image.Pt(320, 240), nil, 0},
		{"half size", image.Pt(160, 120), nil, 0},
		{"odd stride", image.Pt(250, 188), nil, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			ref, r := stillAt(t, path, c.size, Options{Software: true, Extra: c.extra})
			if r != "" {
				t.Fatalf("Software rendered on %q", r)
			}
			got, r := stillAt(t, path, c.size, Options{Extra: c.extra})
			if r == "" {
				t.Skip("no GPU renderer")
			}
			if got.Rect != ref.Rect || got.Stride != ref.Stride {
				t.Fatalf("frame %v stride %d, software %v stride %d", got.Rect, got.Stride, ref.Rect, ref.Stride)
			}
			if !opaque(got) {
				t.Error("alpha not opaque")
			}
			worst, mean, flipped := differ(ref, got)
			t.Logf("%s: worst difference %d/255, mean %.3f; upside down %.3f", r, worst, mean, flipped)
			if mean >= flipped/2 {
				t.Errorf("mean difference %.3f, upside down %.3f", mean, flipped)
			}
			if c.worst > 0 && worst > c.worst {
				t.Errorf("worst difference %d, over %d", worst, c.worst)
			}
		})
	}
}

func TestSoftwareWithoutGL(t *testing.T) {
	path := testVideo(t)
	open := glOpen
	glOpen = func() (*glContext, error) { return nil, errors.New("no libEGL") }
	defer func() { glOpen = open }()
	p, err := New(Source{Path: path}, Options{AudioOutput: "null"})
	if err != nil {
		t.Fatal(err)
	}
	defer closeWait(t, p)
	if r := p.Renderer(); r != "" {
		t.Fatalf("renderer %q", r)
	}
	waitFor(t, "first frame", func() bool { _, _, ok := p.Frame(); return ok })
	f, _, _ := p.Frame()
	if n := litPixels(f); n < 64*48/2 || !opaque(f) {
		t.Fatalf("%d lit pixels, opaque %v", n, opaque(f))
	}
}

// A GPU that stops giving frames mid-play: the software renderer takes
// over and the video goes on.
func TestSoftwareAfterGPUFails(t *testing.T) {
	path := testClip(t, 20)
	var ctx atomic.Pointer[glContext]
	open := glOpen
	glOpen = func() (*glContext, error) {
		c, err := open()
		if err == nil {
			ctx.Store(c)
		}
		return c, err
	}
	defer func() { glOpen = open }()
	p := onGPU(t, path, Options{AudioOutput: "null"})
	defer closeWait(t, p)
	waitFor(t, "first frame", func() bool { _, _, ok := p.Frame(); return ok })
	ctx.Load().failing.Store(true)
	waitFor(t, "the software renderer", func() bool { return p.Renderer() == "" })
	_, seq, _ := p.Frame()
	pos := p.Status().Pos
	waitFor(t, "frames after it", func() bool {
		f, s, _ := p.Frame()
		return s > seq+5 && litPixels(f) > 64*48/2 && p.Status().Pos > pos+300*time.Millisecond
	})
}

func TestTwoPlayersOnGPU(t *testing.T) {
	path := testVideo(t)
	a := onGPU(t, path, Options{AudioOutput: "null", Loop: true})
	defer closeWait(t, a)
	b := onGPU(t, path, Options{AudioOutput: "null", Loop: true})
	waitFor(t, "frames from both", func() bool {
		fa, _, oka := a.Frame()
		fb, _, okb := b.Frame()
		return oka && okb && litPixels(fa) > 64*48/2 && litPixels(fb) > 64*48/2
	})
	closeWait(t, b)
	n := a.Rendered()
	waitFor(t, "frames from the first after the second closed", func() bool { return a.Rendered() > n+3 })
}

// cpuTime is the process's CPU time so far, user and system.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// 1280x720 at 24 frames a second: the process's CPU time a frame, on
// the GPU and in software, and the time to the first frame.
func TestRenderCost(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	path := sizedClip(t, 1280, 720, 6)
	for _, software := range []bool{false, true} {
		start := time.Now()
		var frames atomic.Int64
		p, err := New(Source{Path: path}, Options{AudioOutput: "null", Software: software, OnFrame: func() { frames.Add(1) }})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "first frame", func() bool { return frames.Load() > 0 })
		first := time.Since(start)
		time.Sleep(500 * time.Millisecond)
		c0, f0 := cpuTime(), frames.Load()
		time.Sleep(3 * time.Second)
		c1, f1 := cpuTime(), frames.Load()
		r := p.Renderer()
		closeWait(t, p)
		if f1 == f0 {
			t.Fatal("no frames")
		}
		if r == "" {
			r = "software"
		}
		t.Logf("%s: CPU per frame %v over %d frames, first frame after %v", r, ((c1 - c0) / time.Duration(f1-f0)).Round(10*time.Microsecond), f1-f0, first.Round(time.Millisecond))
	}
}
