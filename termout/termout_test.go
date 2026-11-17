package termout

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// trickle writes one byte at a time with a yield between, so unguarded
// concurrent Writes interleave.
type trickle struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (t *trickle) Write(p []byte) (int, error) {
	for _, b := range p {
		t.mu.Lock()
		t.buf.WriteByte(b)
		t.mu.Unlock()
		runtime.Gosched()
	}
	return len(p), nil
}

// block is "<g.i:" + filler + ">", filler one letter for the writer.
func block(g, i, n int) string {
	return fmt.Sprintf("<%d.%d:%s>", g, i, strings.Repeat(string(rune('a'+g)), n))
}

// checkBlocks fails unless s is exactly whole blocks, each writer's in
// order.
func checkBlocks(t *testing.T, s string, writers, per int) {
	t.Helper()
	next := make([]int, writers)
	for len(s) > 0 {
		var g, i int
		if _, err := fmt.Sscanf(s, "<%d.%d:", &g, &i); err != nil {
			t.Fatalf("no block start at %q", s[:min(len(s), 40)])
		}
		if g < 0 || g >= writers || i != next[g] {
			t.Fatalf("block %d.%d out of order, want %d.%d", g, i, g, next[g])
		}
		end := strings.IndexByte(s, '>')
		head := strings.IndexByte(s, ':')
		body := s[head+1 : end]
		if strings.Trim(body, string(rune('a'+g))) != "" {
			t.Fatalf("block %d.%d interleaved: %q", g, i, body[:min(len(body), 60)])
		}
		next[g]++
		s = s[end+1:]
	}
	for g, n := range next {
		if n != per {
			t.Fatalf("writer %d: %d blocks, want %d", g, n, per)
		}
	}
}

func TestConcurrentWritesWhole(t *testing.T) {
	const writers, per = 8, 40
	tr := &trickle{}
	w := New(tr)
	var wg sync.WaitGroup
	for g := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				b := block(g, i, 1+(g*31+i*17)%300)
				if i%2 == 0 {
					_, _ = w.Write([]byte(b))
				} else {
					_, _ = w.WriteString(b)
				}
			}
		}()
	}
	wg.Wait()
	checkBlocks(t, tr.buf.String(), writers, per)
}

// The trickle writer interleaves without the lock: the test above can
// fail.
func TestTrickleInterleavesUnguarded(t *testing.T) {
	const writers, per = 4, 20
	tr := &trickle{}
	var wg sync.WaitGroup
	for g := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				_, _ = tr.Write([]byte(block(g, i, 200)))
			}
		}()
	}
	wg.Wait()
	s := tr.buf.String()
	interleaved := false
	for i := 0; i+1 < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' && s[i+1] >= 'a' && s[i+1] <= 'z' && s[i] != s[i+1] {
			interleaved = true
			break
		}
	}
	if !interleaved {
		t.Skip("scheduler kept unguarded writes whole this run")
	}
}

func TestLockHoldsSeveralWrites(t *testing.T) {
	const writers, per = 6, 30
	tr := &trickle{}
	w := New(tr)
	var wg sync.WaitGroup
	for g := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				b := block(g, i, 50+g)
				if g%2 == 0 {
					// one block in three locked writes
					unlock := w.Lock()
					_, _ = w.WriteLocked([]byte(b[:3]))
					_, _ = w.WriteLocked([]byte(b[3 : len(b)-1]))
					_, _ = w.WriteLocked([]byte(b[len(b)-1:]))
					unlock()
					unlock() // second call does nothing
				} else {
					_, _ = w.Write([]byte(b))
				}
			}
		}()
	}
	wg.Wait()
	checkBlocks(t, tr.buf.String(), writers, per)
}

// term.File, the interface Bubble Tea checks output against.
var _ interface {
	io.ReadWriteCloser
	Fd() uintptr
} = (*Writer)(nil)

func TestFileDelegation(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := New(f)
	if w.Fd() != f.Fd() {
		t.Fatalf("Fd = %d, want %d", w.Fd(), f.Fd())
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatalf("write after Close: %v", err)
	}
	var b bytes.Buffer
	nb := New(&b)
	if nb.Fd() != ^uintptr(0) {
		t.Fatalf("Fd without file = %d", nb.Fd())
	}
	if _, err := New(writeOnly{}).Read(make([]byte, 1)); err == nil {
		t.Fatal("Read: no error")
	}
}

type writeOnly struct{}

func (writeOnly) Write(p []byte) (int, error) { return len(p), nil }

func TestHoldUntilAltScreen(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf)
	w.WriteString("a")
	w.Hold()
	w.WriteString("img1")
	w.Write([]byte("img2"))
	r := w.Renderer()
	r.Write([]byte("frame0"))
	if got := buf.String(); got != "aframe0" {
		t.Fatalf("held output written: %q", got)
	}
	r.Write([]byte(AltScreen + "\x1b[2Jframe1"))
	w.WriteString("img3")
	r.Write([]byte("frame2"))
	if got, want := buf.String(), "aframe0"+AltScreen+"\x1b[2Jframe1img1img2img3frame2"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	var _ interface {
		io.ReadWriteCloser
		Fd() uintptr
	} = r
}
