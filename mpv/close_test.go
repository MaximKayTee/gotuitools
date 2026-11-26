//go:build cgo && linux

package mpv

import (
	"testing"
	"time"
)

// Close while a file plays, and at once after a pause, when mpv has a
// frame out that is not taken yet: the time to Done. The second is not
// held by that frame.
func TestCloseTime(t *testing.T) {
	path := testClip(t, 20)
	closed := func(pause bool) time.Duration {
		p, err := New(Source{Path: path}, Options{AudioOutput: "null"})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "playing", func() bool { s := p.Status(); return s.Loaded && s.Pos > 1500*time.Millisecond })
		if pause {
			if err := p.command(false, "set", "pause", "yes"); err != nil {
				t.Fatal(err)
			}
		}
		start := time.Now()
		closeWait(t, p)
		return time.Since(start)
	}
	playing, paused := closed(false), closed(true)
	t.Logf("Close to Done: playing %v, just paused %v", playing.Round(time.Millisecond), paused.Round(time.Millisecond))
	if paused > playing+150*time.Millisecond {
		t.Errorf("just paused: %v, playing %v", paused, playing)
	}
}
