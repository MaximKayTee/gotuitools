package mpv

import (
	"image"
	"slices"
	"testing"
	"time"
)

func TestRenderSize(t *testing.T) {
	cases := []struct {
		native image.Point
		scale  float32
		max    int
		want   image.Point
	}{
		{image.Pt(1920, 1080), 1, 0, image.Pt(1920, 1080)},
		{image.Pt(1920, 1080), 3, 0, image.Pt(1920, 1080)},
		{image.Pt(1920, 1080), 0.5, 0, image.Pt(960, 540)},
		{image.Pt(1920, 1080), 0, 0, image.Pt(1920, 1080)},
		{image.Pt(7680, 4320), 1, 0, image.Pt(4096, 2304)},
		{image.Pt(1000, 10), 0.001, 0, image.Pt(1, 1)},
		{image.Pt(0, 0), 1, 0, image.Pt(0, 0)},
	}
	for _, c := range cases {
		if got := RenderSize(c.native, c.scale, c.max); got != c.want {
			t.Errorf("%v at %v: %v, want %v", c.native, c.scale, got, c.want)
		}
	}
}

func TestStateApply(t *testing.T) {
	var s mpvState
	ev := func(p propID, format int, set func(*mpvEvent)) mpvEvent {
		e := mpvEvent{id: evPropertyChange, prop: p, format: format}
		set(&e)
		return e
	}
	s.apply(mpvEvent{id: evFileLoaded})
	s.apply(ev(propTimePos, fmtDouble, func(e *mpvEvent) { e.d = 1.5 }))
	s.apply(ev(propDuration, fmtDouble, func(e *mpvEvent) { e.d = 10 }))
	s.apply(ev(propVolume, fmtDouble, func(e *mpvEvent) { e.d = 70 }))
	s.apply(ev(propSpeed, fmtDouble, func(e *mpvEvent) { e.d = 1 }))
	s.apply(ev(propLoopFile, fmtString, func(e *mpvEvent) { e.str = "inf" }))
	s.apply(ev(propDWidth, fmtInt64, func(e *mpvEvent) { e.i = 640 }))
	s.apply(ev(propDHeight, fmtInt64, func(e *mpvEvent) { e.i = 360 }))
	st := s.status()
	if !st.Loaded || !st.Playing() || st.Pos != 1500*time.Millisecond || st.Dur != 10*time.Second || st.Volume != 70 || !st.Loop || st.Size != image.Pt(640, 360) {
		t.Fatalf("status %+v", st)
	}
	s.apply(ev(propPause, fmtFlag, func(e *mpvEvent) { e.flag = true }))
	s.apply(ev(propLoopFile, fmtString, func(e *mpvEvent) { e.str = "no" }))
	if st := s.status(); st.Playing() || st.Loop {
		t.Fatalf("paused %+v", st)
	}
	// a property going unavailable keeps the last volume
	s.apply(ev(propVolume, fmtNone, func(*mpvEvent) {}))
	s.apply(ev(propEOF, fmtFlag, func(e *mpvEvent) { e.flag = true }))
	if st := s.status(); st.Volume != 70 || !st.EOF {
		t.Fatalf("%+v", st)
	}
	if s.apply(mpvEvent{id: evEndFile, reason: endFileError, errMsg: "unrecognized file format"}); s.err == nil || s.err.Error() != "mpv: unrecognized file format" {
		t.Fatalf("err %v", s.err)
	}
}

func TestOptionList(t *testing.T) {
	get := func(opts [][2]string, k string) (string, bool) {
		i := slices.IndexFunc(opts, func(kv [2]string) bool { return kv[0] == k })
		if i < 0 {
			return "", false
		}
		return opts[i][1], true
	}
	o := optionList(Source{URL: "http://x/f", Headers: []string{"A: 1", "B: 2"}}, Options{Loop: true, Volume: 150, Mute: true, AudioOutput: "null"})
	for k, want := range map[string]string{"vo": "libmpv", "loop-file": "inf", "volume": "130", "mute": "yes", "ao": "null",
		"keep-open": "yes", "config": "no", "osd-level": "0"} {
		if got, _ := get(o, k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if got, _ := get(o, "http-header-fields"); got != "A: 1,B: 2" {
		t.Errorf("headers %q", got)
	}
	o = optionList(Source{URL: "http://x/f", Headers: []string{`A: x,y\z`}}, Options{})
	if got, _ := get(o, "http-header-fields"); got != `A: x\,y\\z` {
		t.Errorf("escaped headers %q", got)
	}
	o = optionList(Source{Path: "/f.mp4", Headers: []string{"A: 1"}}, Options{Volume: 55})
	if _, ok := get(o, "http-header-fields"); ok {
		t.Fatal("headers sent for a local path")
	}
	if _, ok := get(o, "ao"); ok {
		t.Fatal("ao set by default")
	}
	if v, _ := get(o, "volume"); v != "55" {
		t.Fatalf("volume %q", v)
	}
}
