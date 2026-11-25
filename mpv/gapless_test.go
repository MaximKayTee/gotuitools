//go:build cgo && linux

package mpv

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// samples plays the tone, the same tone queued after it by Command when
// queued, into a file of 16-bit samples, and is them. Each tone is a
// second of 1 kHz at 48 kHz: a whole number of its waves, so two with
// nothing between them are one unbroken wave.
func samples(t *testing.T, queued bool, extra ...[2]string) []int16 {
	t.Helper()
	const tone = "av://lavfi:sine=frequency=1000:sample_rate=48000:duration=1"
	file := filepath.Join(t.TempDir(), "out.wav")
	o := append([][2]string{{"vid", "no"}, {"ao-pcm-file", file}, {"audio-format", "s16"}, {"gapless-audio", "yes"}, {"prefetch-playlist", "yes"}}, extra...)
	// paused until the second is queued: the first is a second long
	p, err := New(Source{URL: tone}, Options{AudioOutput: "pcm", Volume: 100, Paused: queued, Observe: []string{"playlist-pos"}, Extra: o})
	if err != nil {
		t.Fatal(err)
	}
	want := "0"
	if queued {
		if err := p.Command("loadfile", tone, "append"); err != nil {
			t.Fatal(err)
		}
		p.Play()
		want = "1"
	}
	for end := time.Now().Add(20 * time.Second); ; {
		st := p.Status()
		if st.Err != nil || time.Now().After(end) {
			t.Fatalf("not played to its end: %+v", st)
		}
		if st.EOF && st.Props["playlist-pos"] == want {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.Close()
	<-p.Done()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(b, []byte("data"))
	if at < 0 {
		t.Fatalf("no samples in %d bytes", len(b))
	}
	b = b[at+8:]
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return out
}

// A file queued by Command plays after the one playing with no gap in
// the sound: what comes out is the samples of the two and no more, and
// the wave goes on over the join as it does anywhere else.
func TestCommandQueuesWithoutGap(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}
	const second, wave = 48000, 48
	one := samples(t, false)
	two := samples(t, true)
	// the end of the last file is held back at its end, paused: less
	// than a tenth of a second
	held := second - len(one)
	if held < 0 || held > second/10 {
		t.Fatalf("one tone: %d samples, want %d or a little under", len(one), second)
	}
	if len(two) != second+len(one) {
		t.Fatalf("two tones: %d samples, want %d: one whole and one less the %d held back", len(two), second+len(one), held)
	}
	// the tone repeats every 48 samples; over the join too
	worst := func(s []int16) int {
		w := 0
		for i := range s {
			d := int(s[i]) - int(one[i%wave])
			w = max(w, max(d, -d))
		}
		return w
	}
	if w := worst(one); w > 1 {
		t.Fatalf("one tone is not one wave over and over: worst difference %d", w)
	}
	w := worst(two)
	t.Logf("one tone %d samples, two %d: %d more; worst difference from the unbroken wave %d of 32768", len(one), len(two), len(two)-len(one), w)
	if w > 1 {
		t.Errorf("the join is not the wave going on: worst difference %d", w)
	}
	if err := (&Player{}).Command(); err != nil {
		t.Errorf("no command: %v", err)
	}
}

// SetProperty changes an option while it plays: a gain set before the
// tone starts is in every sample.
func TestSetProperty(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}
	const tone = "av://lavfi:sine=frequency=1000:sample_rate=48000:duration=1"
	file := filepath.Join(t.TempDir(), "out.wav")
	p, err := New(Source{URL: tone}, Options{AudioOutput: "pcm", Volume: 100, Paused: true,
		Extra: [][2]string{{"vid", "no"}, {"ao-pcm-file", file}, {"audio-format", "s16"}}})
	if err != nil {
		t.Fatal(err)
	}
	p.SetProperty("volume-gain", "-6.0206")
	p.Play()
	for end := time.Now().Add(20 * time.Second); !p.Status().EOF; {
		if p.Status().Err != nil || time.Now().After(end) {
			t.Fatalf("not played to its end: %+v", p.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.Close()
	<-p.Done()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	peak := 0
	for b = b[bytes.Index(b, []byte("data"))+8:]; len(b) >= 2; b = b[2:] {
		v := int(int16(binary.LittleEndian.Uint16(b)))
		peak = max(peak, max(v, -v))
	}
	// the tone is an eighth of full scale: 4096; half of it at -6.02 dB
	if peak < 2040 || peak > 2056 {
		t.Errorf("peak %d, want 2048", peak)
	}
}
