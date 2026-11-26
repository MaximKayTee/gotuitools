package mpv

import (
	"image"
	"slices"
	"strings"
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

func TestStateApplyObserved(t *testing.T) {
	s := mpvState{extra: []string{"metadata/by-key/icy-title", "audio-codec-name"}}
	ev := func(i int, format int, str string) mpvEvent {
		return mpvEvent{id: evPropertyChange, prop: propExtra + propID(i), format: format, str: str}
	}
	if !s.apply(ev(0, fmtString, "A - B")) || !s.apply(ev(1, fmtString, "mp3")) {
		t.Fatal("no change reported")
	}
	first := s.status().Props
	if first["metadata/by-key/icy-title"] != "A - B" || first["audio-codec-name"] != "mp3" {
		t.Fatalf("props %v", first)
	}
	if s.apply(ev(0, fmtString, "A - B")) {
		t.Error("same value reported as a change")
	}
	if !s.apply(ev(0, fmtString, "C - D\xff")) {
		t.Fatal("new value not reported")
	}
	if got := s.status().Props["metadata/by-key/icy-title"]; got != "C - D" {
		t.Errorf("title %q", got)
	}
	if first["metadata/by-key/icy-title"] != "A - B" {
		t.Error("a Status handed out changed")
	}
	if !s.apply(ev(0, fmtNone, "")) {
		t.Fatal("unset not reported")
	}
	if _, ok := s.status().Props["metadata/by-key/icy-title"]; ok {
		t.Error("unset property kept")
	}
	if s.apply(ev(2, fmtString, "x")) || s.apply(ev(0, fmtNone, "")) {
		t.Error("unknown or unchanged property reported")
	}
}

func TestExtraOptionsLast(t *testing.T) {
	o := optionList(Source{URL: "http://x/f"}, Options{Extra: [][2]string{{"vid", "no"}}})
	if last := o[len(o)-1]; last != [2]string{"vid", "no"} {
		t.Errorf("last option %v", last)
	}
}

func TestStateSubs(t *testing.T) {
	var s mpvState
	list := func(str string) mpvEvent {
		return mpvEvent{id: evPropertyChange, prop: propTrackList, format: fmtString, str: str}
	}
	const tracks = `[{"id":1,"type":"video","selected":true},{"id":1,"type":"audio","lang":"en","selected":false},` +
		`{"id":2,"type":"audio","lang":"jpn","title":"Japanese","selected":true},` +
		`{"id":1,"type":"sub","title":"German","lang":"de","selected":false,"external":true},` +
		`{"id":2,"type":"sub","title":"auto","lang":"en","selected":true,"external":true},` +
		`{"id":3,"type":"sub","selected":false}]`
	if !s.apply(list(tracks)) {
		t.Fatal("no change")
	}
	want := []Sub{{ID: 1, Lang: "de", Title: "German"}, {ID: 2, Lang: "en", Auto: true}, {ID: 3}}
	if st := s.status(); !slices.Equal(st.Subs, want) || st.Sub != 2 {
		t.Fatalf("subs %+v sub %d", st.Subs, st.Sub)
	}
	wantAudio := []Audio{{ID: 1, Lang: "en"}, {ID: 2, Lang: "jpn", Title: "Japanese"}}
	if st := s.status(); !slices.Equal(st.Audios, wantAudio) || st.Audio != 2 {
		t.Fatalf("audios %+v audio %d", st.Audios, st.Audio)
	}
	if !s.apply(list(strings.Replace(tracks, `"lang":"jpn","title":"Japanese","selected":true`, `"lang":"jpn","title":"Japanese","selected":false`, 1))) ||
		s.status().Audio != 0 {
		t.Fatalf("audio off: %+v", s.status())
	}
	s.apply(list(tracks))
	if s.apply(list(tracks)) {
		t.Fatal("same list: change")
	}
	if !s.apply(list(`[{"id":1,"type":"video","selected":true}]`)) || len(s.status().Subs) != 0 || s.status().Sub != 0 ||
		len(s.status().Audios) != 0 || s.status().Audio != 0 {
		t.Fatalf("no subs: %+v", s.status())
	}
	if s.apply(list("not json")) {
		t.Fatal("bad list: change")
	}
}

func TestOptionListSubs(t *testing.T) {
	get := func(o Options) map[string]string {
		m := map[string]string{}
		for _, kv := range optionList(Source{URL: "https://x"}, o) {
			m[kv[0]] = kv[1]
		}
		return m
	}
	m := get(Options{YTDL: true, AutoCaptions: true, SubLang: "pt-BR"})
	if m["slang"] != "pt-BR,pt" {
		t.Errorf("slang %q", m["slang"])
	}
	wait, _ := ytdlWaitPath()
	subs, _ := ytdlSubsPath()
	if wait == "" || subs == "" || m["scripts"] != wait+":"+subs {
		t.Errorf("scripts %q", m["scripts"])
	}
	m = get(Options{YTDL: true, SubLang: "en"})
	if m["slang"] != "en" || m["scripts"] != wait {
		t.Errorf("slang %q scripts %q", m["slang"], m["scripts"])
	}
	if m = get(Options{AutoCaptions: true}); m["scripts"] != "" || m["slang"] != "" || m["alang"] != "" {
		t.Errorf("no ytdl: %v", m)
	}
	m = get(Options{AudioLang: " ja , en-US,EN", SubLang: "pt-BR,pt_PT, ,en"})
	if m["alang"] != "ja,en-US,en" || m["slang"] != "pt-BR,pt,pt_PT,en" {
		t.Errorf("lists: alang %q slang %q", m["alang"], m["slang"])
	}
}

func TestStateRetry(t *testing.T) {
	fail := mpvEvent{id: evEndFile, reason: endFileError, errMsg: "loading failed"}
	s := mpvState{retries: ytdlRetries}
	for i := range ytdlRetries {
		if !s.retry(fail) {
			t.Fatalf("failure %d not run again", i+1)
		}
	}
	if s.retry(fail) {
		t.Fatal("run again more than ytdlRetries times")
	}
	if !s.apply(fail) || s.status().Err == nil {
		t.Fatalf("the last failure: %+v", s.status())
	}
	// not an end that is no error, not after the file loaded, not without retries
	s = mpvState{retries: ytdlRetries}
	if s.retry(mpvEvent{id: evEndFile, reason: 0}) || s.retry(mpvEvent{id: evFileLoaded}) {
		t.Fatal("no failure run again")
	}
	s.apply(mpvEvent{id: evFileLoaded})
	if s.retry(fail) {
		t.Fatal("failure after the load run again")
	}
	if s = (mpvState{}); s.retry(fail) {
		t.Fatal("run again without retries")
	}
}

func TestSpans(t *testing.T) {
	sec := func(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }
	at := func(s *mpvState, pos float64) (float64, bool) {
		s.apply(mpvEvent{id: evPropertyChange, prop: propTimePos, format: fmtDouble, d: pos})
		return s.follow(mpvEvent{id: evPropertyChange, prop: propTimePos, format: fmtDouble, d: pos})
	}
	seek := func(s *mpvState, pos float64) {
		s.follow(mpvEvent{id: evSeek})
		at(s, pos)
		s.follow(mpvEvent{id: evRestart})
	}
	newState := func() *mpvState {
		return &mpvState{loaded: true, landed: -1, skipped: -1, spans: []Span{
			{Start: 0, End: sec(5)},        // intro
			{Start: sec(20), End: sec(30)}, // sponsor
			{Start: sec(28), End: sec(40)}, // overlaps it
			{Start: sec(50), End: sec(52), Mute: true},
		}}
	}

	// nothing before the file loaded
	s := newState()
	s.loaded = false
	if to, _ := at(s, 0); to != -1 {
		t.Fatalf("before load: to %v", to)
	}
	// from the start: the span at 0 skips
	s = newState()
	if to, _ := at(s, 0); to != 5 || s.skips != 1 || s.skipped != 0 || s.pos != 5 {
		t.Fatalf("start: to %v, %+v", to, s)
	}
	// the skip's own seek lands at the end: plays on
	seek(s, 5)
	if to, _ := at(s, 5.04); to != -1 {
		t.Fatalf("after skip: to %v", to)
	}
	// played into: skipped to the end of the overlapping one too
	at(s, 19.96)
	if to, _ := at(s, 20.01); to != 40 || s.skipped != 1 {
		t.Fatalf("into sponsor: to %v, skipped %d", to, s.skipped)
	}
	// a seek into a span plays it and the one overlapping it
	seek(s, 25)
	for _, pos := range []float64{25.04, 27.9, 28.1, 31, 39.9} {
		if to, _ := at(s, pos); to != -1 {
			t.Fatalf("seeked into, at %v: to %v", pos, to)
		}
	}
	// a seek before a span: playing into it skips
	seek(s, 18)
	if to, _ := at(s, 20); to != 40 || s.skips != 3 {
		t.Fatalf("seeked before: to %v, skips %d", to, s.skips)
	}
	// positions while a seek runs are not played ones
	s.follow(mpvEvent{id: evSeek})
	if to, _ := at(s, 21); to != -1 {
		t.Fatalf("seeking: to %v", to)
	}
	s.follow(mpvEvent{id: evRestart})
	at(s, 21.04)
	// the end of a skip span plays: a seek can land a frame short
	s = newState()
	seek(s, 10)
	if to, _ := at(s, 39.95); to != -1 {
		t.Fatalf("tail: to %v", to)
	}

	// a mute span mutes however it was reached, the set mute kept
	s = newState()
	seek(s, 49)
	if _, remute := at(s, 49.9); remute || s.muted() {
		t.Fatal("muted before the span")
	}
	if _, remute := at(s, 50); !remute || !s.muted() {
		t.Fatal("not muted in the span")
	}
	seek(s, 51)
	if _, remute := at(s, 51.1); remute || !s.muted() {
		t.Fatal("seek in the span unmuted")
	}
	if _, remute := at(s, 52); !remute || s.muted() {
		t.Fatal("muted after the span")
	}
	s.userMute = true
	if !s.muted() {
		t.Fatal("set mute lost")
	}
}

func TestStateCached(t *testing.T) {
	var s mpvState
	ev := func(d float64, format int) mpvEvent {
		return mpvEvent{id: evPropertyChange, prop: propCacheTime, format: format, d: d}
	}
	if !s.apply(ev(42.5, fmtDouble)) || s.status().Cached != 42500*time.Millisecond {
		t.Fatalf("cached %v", s.status().Cached)
	}
	if s.apply(ev(42.5, fmtDouble)) {
		t.Fatal("same: change")
	}
	if !s.apply(ev(0, fmtNone)) || s.status().Cached != 0 {
		t.Fatalf("none: %v", s.status().Cached)
	}
}

func TestStateChapters(t *testing.T) {
	s := mpvState{chapter: -1}
	list := mpvEvent{id: evPropertyChange, prop: propChapterList, format: fmtString,
		str: `[{"title":"Opening","time":0},{"title":"Part A","time":90.5},{"time":1300}]`}
	at := func(i int64, format int) mpvEvent {
		return mpvEvent{id: evPropertyChange, prop: propChapter, format: format, i: i}
	}
	if st := s.status(); st.Chapters != nil || st.Chapter != -1 {
		t.Fatalf("none: %+v", st)
	}
	if !s.apply(list) || s.apply(list) {
		t.Fatal("list: change once")
	}
	want := []Chapter{{Title: "Opening"}, {Title: "Part A", Start: 90500 * time.Millisecond}, {Start: 1300 * time.Second}}
	if st := s.status(); !slices.Equal(st.Chapters, want) || st.Chapter != -1 {
		t.Fatalf("chapters %+v at %d", st.Chapters, st.Chapter)
	}
	if !s.apply(at(1, fmtInt64)) || s.status().Chapter != 1 || s.apply(at(1, fmtInt64)) {
		t.Fatalf("at 1: %d", s.status().Chapter)
	}
	if !s.apply(at(0, fmtNone)) || s.status().Chapter != -1 {
		t.Fatalf("none: %d", s.status().Chapter)
	}
	if !s.apply(mpvEvent{id: evPropertyChange, prop: propChapterList, format: fmtString, str: "[]"}) || s.status().Chapters != nil {
		t.Fatalf("emptied: %+v", s.status())
	}
}

func TestOptionSubOff(t *testing.T) {
	has := func(o Options) bool {
		return slices.Contains(optionList(Source{URL: "https://x"}, o), [2]string{"sid", "no"})
	}
	if has(Options{SubLang: "en"}) || !has(Options{SubLang: "en", SubOff: true}) {
		t.Error("sid=no only with SubOff")
	}
}
