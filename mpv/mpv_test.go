//go:build cgo && linux

package mpv

import (
	"bytes"
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMissingLibrary(t *testing.T) {
	err := loadLib([]string{"libtaurus-no-such-mpv.so"}, false)
	if !errors.Is(err, ErrNoLibmpv) || err.Error() == ErrNoLibmpv.Error() {
		t.Fatalf("err %v", err)
	}
}

// testVideo writes a 64x48 2s mp4 with a tone.
func testVideo(t *testing.T) string {
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
		"-f", "lavfi", "-i", "testsrc=size=64x48:rate=10:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-pix_fmt", "yuv420p", "-shortest", "-y", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, b)
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func closeWait(t *testing.T, p *Player) {
	p.Close()
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("teardown")
	}
}

func litPixels(img *image.RGBA) int {
	n := 0
	for y := range img.Rect.Dy() {
		row := img.Pix[y*img.Stride:]
		for x := range img.Rect.Dx() {
			if row[x*4]|row[x*4+1]|row[x*4+2] != 0 {
				n++
			}
		}
	}
	return n
}

func TestRendersFrame(t *testing.T) {
	path := testVideo(t)
	var frames, changes atomic.Int32
	var half atomic.Bool
	p, err := New(Source{Path: path}, Options{AudioOutput: "null", Volume: 50,
		Size: func(n image.Point) image.Point {
			if half.Load() {
				return n.Div(2)
			}
			return n
		},
		OnFrame:  func() { frames.Add(1) },
		OnChange: func() { changes.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	defer closeWait(t, p)
	waitFor(t, "first frame", func() bool { _, _, ok := p.Frame(); return ok })
	if p.Status().Size != image.Pt(64, 48) {
		t.Fatalf("size %v", p.Status().Size)
	}
	f, seq, _ := p.Frame()
	if f.Rect.Size() != image.Pt(64, 48) || f.Stride%64 != 0 || seq == 0 {
		t.Fatalf("frame %v stride %d seq %d", f.Rect, f.Stride, seq)
	}
	if n := litPixels(f); n < 64*48/2 {
		t.Fatalf("only %d lit pixels", n)
	}
	if f.Pix[3] != 0xff {
		t.Fatal("alpha not opaque")
	}
	if frames.Load() == 0 || changes.Load() == 0 {
		t.Fatalf("callbacks: %d frames %d changes", frames.Load(), changes.Load())
	}
	if st := p.Status(); !st.Loaded || st.Volume != 50 {
		t.Fatalf("status %+v", st)
	}

	// pause, seek, loop, speed, mute round-trip through mpv properties
	p.Pause()
	waitFor(t, "pause", func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.st.paused })
	p.Seek(time.Second, true)
	waitFor(t, "seek", func() bool {
		d := p.Status().Pos - time.Second
		return d > -60*time.Millisecond && d < 60*time.Millisecond
	})
	p.SetLoop(true)
	p.SetSpeed(1.5)
	p.SetMute(true)
	p.SetVolume(200)
	waitFor(t, "properties", func() bool {
		s := p.Status()
		return s.Loop && s.Speed == 1.5 && s.Mute && !s.Playing() && s.Volume == MaxVolume
	})
	// a smaller size after Redraw, paused
	half.Store(true)
	p.Redraw()
	waitFor(t, "half-size frame", func() bool { f, _, _ := p.Frame(); return f != nil && f.Rect.Size() == image.Pt(32, 24) })
	if p.Status().Dur < 1900*time.Millisecond {
		t.Fatalf("duration %v", p.Status().Dur)
	}
	// a held frame is never rendered into
	held, hs, _ := p.Frame()
	before := bytes.Clone(held.Pix)
	n := p.Rendered()
	p.Redraw()
	p.Redraw()
	waitFor(t, "redraws", func() bool { return p.Rendered() >= n+1 })
	time.Sleep(50 * time.Millisecond)
	if !bytes.Equal(before, held.Pix) {
		t.Fatal("held frame changed")
	}
	if _, s2, _ := p.Frame(); s2 == hs {
		t.Fatal("no newer frame")
	}
}

func TestEndPausesWithoutLoop(t *testing.T) {
	path := testVideo(t)
	p, err := New(Source{Path: path}, Options{AudioOutput: "null", Volume: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer closeWait(t, p)
	waitFor(t, "loaded", func() bool { return p.Status().Loaded })
	p.SetSpeed(4)
	waitFor(t, "end", func() bool { s := p.Status(); return s.EOF && !s.Playing() })
	p.Play() // restarts from the start
	waitFor(t, "restart", func() bool { s := p.Status(); return s.Playing() && s.Pos < time.Second })
}

func TestStreamsWithHeader(t *testing.T) {
	path := testVideo(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var authed, denied atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Hydrus-Client-API-Access-Key") != "k3y" || r.Header.Get("X-List") != "a, b" {
			denied.Add(1)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		authed.Add(1)
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "clip.mp4", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	p, err := New(Source{URL: srv.URL + "/get_files/file?file_id=1", Headers: []string{"Hydrus-Client-API-Access-Key: k3y", "X-List: a, b"}},
		Options{AudioOutput: "null", Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeWait(t, p)
	waitFor(t, "streamed frame", func() bool { _, _, ok := p.Frame(); return ok })
	if authed.Load() == 0 || denied.Load() != 0 {
		t.Fatalf("authed %d denied %d", authed.Load(), denied.Load())
	}
	if p.Status().Playing() {
		t.Fatal("Paused option ignored")
	}
}

// A ytdl URL that fails before it loaded is loaded again, ytdlRetries
// times; any other source once.
func TestRetriesFailedLoad(t *testing.T) {
	path := testVideo(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var deny, denied atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if denied.Load() < deny.Load() {
			denied.Add(1)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "clip.mp4", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	// the hook's yt-dlp fails at once
	o := Options{AudioOutput: "null", Paused: true, YTDL: true, Extra: [][2]string{{"script-opts", "ytdl_hook-ytdl_path=/bin/false"}}}
	play := func(o Options, n int32) *Player {
		t.Helper()
		deny.Store(n)
		denied.Store(0)
		p, err := New(Source{URL: srv.URL + "/clip.mp4"}, o)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	p := play(o, ytdlRetries)
	waitFor(t, "load after the failures", func() bool { return p.Status().Loaded || p.Status().Err != nil })
	if st := p.Status(); st.Err != nil || denied.Load() != ytdlRetries {
		t.Fatalf("err %v, denied %d", st.Err, denied.Load())
	}
	closeWait(t, p)

	p = play(o, 100)
	waitFor(t, "error", func() bool { return p.Status().Err != nil })
	time.Sleep(300 * time.Millisecond)
	if got := denied.Load(); got != ytdlRetries+1 || p.Status().Loaded {
		t.Fatalf("always failing: %d loads, want %d", got, ytdlRetries+1)
	}
	closeWait(t, p)

	o.YTDL, o.Extra = false, nil
	p = play(o, 100)
	waitFor(t, "error without ytdl", func() bool { return p.Status().Err != nil })
	time.Sleep(300 * time.Millisecond)
	if got := denied.Load(); got != 1 {
		t.Fatalf("without ytdl: %d loads, want 1", got)
	}
	closeWait(t, p)
}

func TestBadFileReportsError(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.mp4")
	os.WriteFile(bad, []byte("not a video at all"), 0o644)
	p, err := New(Source{Path: bad}, Options{AudioOutput: "null"})
	if err != nil {
		t.Fatal(err)
	}
	defer closeWait(t, p)
	waitFor(t, "error", func() bool { return p.Status().Err != nil })
}

func TestPlaysAnimations(t *testing.T) {
	testVideo(t) // skips without libmpv or ffmpeg
	dir := t.TempDir()
	for _, c := range []struct{ name, codec, format string }{
		{"anim.png", "apng", "apng"},
		{"anim.webp", "libwebp_anim", "webp"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := filepath.Join(dir, c.name)
			cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
				"-f", "lavfi", "-i", "testsrc=size=40x30:rate=10:duration=1", "-c:v", c.codec, "-f", c.format, "-y", out)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("ffmpeg %s: %v %s", c.codec, err, b)
			}
			p, err := New(Source{Path: out}, Options{AudioOutput: "null", Loop: true})
			if err != nil {
				t.Fatal(err)
			}
			defer closeWait(t, p)
			waitFor(t, "frame", func() bool { _, _, ok := p.Frame(); return ok })
			f, _, _ := p.Frame()
			if p.Status().Size != image.Pt(40, 30) || litPixels(f) == 0 {
				t.Fatalf("size %v", p.Status().Size)
			}
			waitFor(t, "animation", func() bool { return p.Rendered() >= 5 })
		})
	}
}

func TestCloseAfterControls(t *testing.T) {
	path := testVideo(t)
	p, err := New(Source{Path: path}, Options{AudioOutput: "null"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "loaded", func() bool { return p.Status().Loaded })
	closeWait(t, p)
	// controls after Close: no crash, no effect
	p.SetVolume(10)
	p.SeekBy(time.Second)
	p.Redraw()
	p.Close()
}

// TestObservesTags plays a tagged tone, no video: the observed
// properties arrive as Status.Props.
func TestObservesTags(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	title := strings.Repeat("Long title ", 12) + "end"
	out := filepath.Join(t.TempDir(), "tone.ogg")
	cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:a", "flac",
		"-metadata", "title="+title, "-metadata", "artist=Tester", "-y", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, b)
	}
	p, err := New(Source{Path: out}, Options{AudioOutput: "null", Paused: true,
		Observe: []string{"metadata/by-key/title", "metadata/by-key/artist", "audio-codec-name", "audio-params/samplerate", "metadata/by-key/none"},
		Extra:   [][2]string{{"vid", "no"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeWait(t, p)
	waitFor(t, "tags", func() bool {
		pr := p.Status().Props
		return pr["metadata/by-key/title"] != "" && pr["audio-params/samplerate"] != ""
	})
	pr := p.Status().Props
	if pr["metadata/by-key/title"] != title || pr["metadata/by-key/artist"] != "Tester" {
		t.Errorf("tags %q", pr)
	}
	if pr["audio-codec-name"] != "flac" || pr["audio-params/samplerate"] != "44100" {
		t.Errorf("audio %q", pr)
	}
	if _, ok := pr["metadata/by-key/none"]; ok {
		t.Errorf("absent tag has a value: %q", pr)
	}
}
