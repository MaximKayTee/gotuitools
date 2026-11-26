//go:build linux

package player

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var sizeRe = regexp.MustCompile(`a=T,f=24,s=(\d+),v=(\d+)`)

// frameLog counts frame writes and keeps their sizes and times.
type frameLog struct {
	mu    sync.Mutex
	at    []time.Time
	sizes []string
	bytes int
}

func (l *frameLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bytes += len(p)
	if m := sizeRe.FindSubmatch(p); m != nil {
		l.at = append(l.at, time.Now())
		l.sizes = append(l.sizes, string(m[1])+"x"+string(m[2]))
	}
	return len(p), nil
}

func (l *frameLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.at)
}

func (l *frameLog) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bytes
}

func (l *frameLog) last() (time.Time, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.at) == 0 {
		return time.Time{}, ""
	}
	return l.at[len(l.at)-1], l.sizes[len(l.sizes)-1]
}

// testVideo writes a 640x360 12 s mp4 with a tone; skips without
// libmpv or ffmpeg.
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
		"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=12",
		"-pix_fmt", "yuv420p", "-shortest", "-y", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, b)
	}
	return out
}

// mpvThreads are the threads not named as the process: libmpv's and
// ffmpeg's. Not the CUDA driver's: one stays for the process once a
// GPU has decoded.
func mpvThreads(t *testing.T) []string {
	t.Helper()
	self, err := os.ReadFile("/proc/self/comm")
	if err != nil {
		t.Skip(err)
	}
	dirs, err := filepath.Glob("/proc/self/task/*/comm")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range dirs {
		b, err := os.ReadFile(d)
		if err != nil {
			continue
		}
		if !bytes.Equal(b, self) && !bytes.HasPrefix(b, []byte("cuda")) {
			out = append(out, strings.TrimSpace(string(b)))
		}
	}
	return out
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// TestLiveMPV plays a generated clip through libmpv, no sound (ao
// null), into a counting buffer: frames, duration, seek and volume in
// State, a resize mid-play keeps the position, Stop leaves no
// goroutines or mpv threads.
func TestLiveMPV(t *testing.T) {
	path := testVideo(t)
	g0, th0 := runtime.NumGoroutine(), mpvThreads(t)
	out := &frameLog{}
	p := New(Options{Output: out, CellPx: image.Pt(10, 20), AudioOutput: "null", Volume: 100})
	cpu0 := cpuTime()
	start := time.Now()
	if err := p.Play(context.Background(), Source{Target: path}, 2, 3, 60, 20); err != nil {
		t.Fatal(err)
	}
	opened := time.Since(start)
	waitFor(t, "first frame", func() bool { return out.count() > 0 })
	first, size := out.last()
	t.Logf("open %v, first frame %v (%s)", opened.Round(time.Millisecond), first.Sub(start).Round(time.Millisecond), size)
	time.Sleep(2 * time.Second)
	cpu := cpuTime() - cpu0
	wall := time.Since(start)
	n := out.count()
	st := p.State()
	t.Logf("2 s: %d frames sent (%.1f/s), %d B out, CPU %v of %v (%.0f %% of one core), state %+v, mpv threads %q",
		n, float64(n)/wall.Seconds(), out.size(), cpu.Round(time.Millisecond), wall.Round(time.Millisecond),
		100*cpu.Seconds()/wall.Seconds(), st, mpvThreads(t))
	if size != "600x340" || n < 10 || st.Dur < 11*time.Second || st.Dur > 13*time.Second || !st.Playing || !st.Loaded || st.Pos < time.Second {
		t.Fatalf("frames %d at %s, state %+v", n, size, st)
	}
	// seek and volume reflected; a time-pos from before the seek may
	// land after it
	before := p.State().Pos
	if err := p.Seek(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := p.Volume(-10); err != nil {
		t.Fatal(err)
	}
	if st := p.State(); st.Volume != 90 {
		t.Fatalf("after volume: %+v", st)
	}
	time.Sleep(500 * time.Millisecond)
	if st := p.State(); st.Pos < before+5*time.Second || st.Volume != 90 {
		t.Fatalf("mpv's state after seek: %+v (before %v)", st, before)
	}
	// resize mid-play: same handle, the new size, position kept
	at, posBefore := time.Now(), p.State().Pos
	if err := p.Play(context.Background(), Source{Target: path}, 4, 2, 40, 14); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resized frame", func() bool { _, s := out.last(); return s == "400x220" })
	rt, _ := out.last()
	time.Sleep(300 * time.Millisecond)
	want := posBefore + time.Since(at)
	got := p.State().Pos
	diff := (got - want).Abs()
	t.Logf("resize to 40x14: first new-size frame after %v; position %v, expected %v, off %v",
		rt.Sub(at).Round(time.Millisecond), got.Round(time.Millisecond), want.Round(time.Millisecond), diff.Round(time.Millisecond))
	if diff > 500*time.Millisecond {
		t.Fatalf("position not kept: %v, want %v", got, want)
	}
	// stop: no goroutines, no mpv threads
	during := mpvThreads(t)
	stopAt := time.Now()
	p.Stop()
	stopCall := time.Since(stopAt)
	p.Wait()
	teardown := time.Since(stopAt)
	deadline := time.Now().Add(2 * time.Second)
	for (runtime.NumGoroutine() > g0 || len(mpvThreads(t)) > len(th0)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	g1, th1 := runtime.NumGoroutine(), mpvThreads(t)
	t.Logf("stop: call %v, handle destroyed after %v; goroutines %d before, %d after; mpv threads %d before, %d playing, %d after %q",
		stopCall.Round(time.Microsecond), teardown.Round(time.Millisecond), g0, g1, len(th0), len(during), len(th1), th1)
	if g1 > g0 || len(th1) > len(th0) {
		buf := make([]byte, 1<<16)
		t.Fatalf("left running: goroutines %d > %d or threads %q\n%s", g1, g0, th1, buf[:runtime.Stack(buf, true)])
	}
}

// TestLiveURL plays the clip over HTTP with a request header, starting
// at 4 s.
func TestLiveURL(t *testing.T) {
	path := testVideo(t)
	var mu sync.Mutex
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("X-Test"))
		mu.Unlock()
		http.ServeFile(w, r, path)
	}))
	defer srv.Close()
	out := &frameLog{}
	p := New(Options{Output: out, AudioOutput: "null"})
	defer func() { p.Stop(); p.Wait() }()
	closed := make(chan struct{})
	src := Source{Key: "clip", Target: srv.URL + "/clip.mp4", Headers: []string{"X-Test: yes"},
		Start: 4 * time.Second, Close: func() { close(closed) }}
	if err := p.Play(context.Background(), src, 0, 0, 40, 14); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "frames", func() bool { return out.count() >= 3 && p.State().Loaded })
	if st := p.State(); st.Target != "clip" || st.Pos < 4*time.Second || st.Err != nil {
		t.Fatalf("state %+v", st)
	}
	mu.Lock()
	got := append([]string(nil), auth...)
	mu.Unlock()
	if len(got) == 0 || got[0] != "yes" {
		t.Fatalf("X-Test %q", got)
	}
	p.Stop()
	p.Wait()
	select {
	case <-closed:
	default:
		t.Fatal("Close not run")
	}
}

// TestLiveYouTube plays a YouTube video through mpv's ytdl_hook.
// GOTOOLS_NET=1 only.
func TestLiveYouTube(t *testing.T) {
	if os.Getenv("GOTOOLS_NET") == "" {
		t.Skip("GOTOOLS_NET unset")
	}
	if err := Available(); err != nil {
		t.Skip(err)
	}
	out := &frameLog{}
	p := New(Options{Output: out, AudioOutput: "null", MaxFPS: 10, YTDL: true,
		YTDLFormat: "bestvideo[height<=720]+bestaudio/best[height<=720]/best"})
	defer func() { p.Stop(); p.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := p.Play(ctx, Source{Target: "https://www.youtube.com/watch?v=3aaB8Iqwse8"}, 0, 0, 40, 10); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(50 * time.Second)
	for {
		st := p.State()
		if n := out.count(); n >= 5 && st.Loaded && st.Dur > 0 {
			t.Logf("frames %d pos %v dur %v", n, st.Pos, st.Dur)
			return
		}
		if st.Err != nil {
			t.Fatal(st.Err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("no frames: %d, state %+v", out.count(), st)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestLiveTracksHeld: libmpv, a file with two English subtitle tracks
// and two untagged audio tracks, the second of each picked: they play in
// the file after, where mpv alone takes the first.
func TestLiveTracksHeld(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	srt := filepath.Join(dir, "s.srt")
	if err := os.WriteFile(srt, []byte("1\n00:00:00,000 --> 00:00:05,000\nHello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clip := func(name string) string {
		out := filepath.Join(dir, name)
		cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "testsrc=size=64x48:rate=10:duration=6",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=6",
			"-f", "lavfi", "-i", "sine=frequency=660:duration=6",
			"-i", srt, "-i", srt, "-map", "0", "-map", "1", "-map", "2", "-map", "3", "-map", "4",
			"-pix_fmt", "yuv420p", "-c:a", "flac", "-c:s", "srt",
			"-metadata:s:a:0", "title=Stereo", "-metadata:s:a:1", "title=Commentary",
			"-metadata:s:s:0", "language=eng", "-metadata:s:s:0", "title=Signs & Songs",
			"-metadata:s:s:1", "language=eng", "-metadata:s:s:1", "title=Dialogue",
			"-disposition:a:0", "default", "-disposition:a:1", "0", "-disposition:s:0", "default", "-disposition:s:1", "0", "-y", out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("ffmpeg: %v %s", err, b)
		}
		return out
	}
	one, two := clip("1.mkv"), clip("2.mkv")
	p := New(Options{Output: &frameLog{}, AudioOutput: "null"})
	defer p.Wait()
	defer p.Stop()
	ctx := context.Background()
	if err := p.Play(ctx, Source{Target: one}, 0, 0, 20, 10); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "tracks", func() bool {
		st := p.State()
		return st.Loaded && len(p.Subs()) == 2 && len(p.Audios()) == 2 && st.Audio != 0
	})
	if st := p.State(); st.Sub != 1 || st.Audio != 1 || p.Subs()[1] != (Sub{ID: 2, Lang: "eng", Title: "Dialogue"}) {
		t.Fatalf("mpv's picks: sub %d, audio %d, subs %+v", st.Sub, st.Audio, p.Subs())
	}
	if err := p.SetSub(2); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAudio(2); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := p.Play(ctx, Source{Target: two}, 0, 0, 20, 10); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the picks", func() bool { st := p.State(); return st.Target == two && st.Sub == 2 && st.Audio == 2 })
	t.Logf("picks playing %v after Play, at %v of the file", time.Since(start).Round(time.Millisecond), p.State().Pos.Round(time.Millisecond))
}
