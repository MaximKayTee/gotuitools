// Package mpv plays media through libmpv in process: libmpv is opened
// with dlopen at runtime (cgo, linux), frames come from the software
// render API into RGBA buffers. Without cgo, or off linux, Available
// reports ErrNoLibmpv and New fails.
//
// Threads: an event goroutine folds mpv property changes into a cached
// Status; a render goroutine waits for mpv's update callback, renders
// the current frame at Options.Size into one of three buffers and calls
// OnFrame. Frame hands out the newest buffer and holds it until the
// next call; rendering never writes into a held buffer.
//
// Controls are async mpv commands and change the cached Status at
// once. Close tears down on a goroutine: the render context first, as
// mpv requires, then mpv_terminate_destroy; Done is closed after it.
package mpv

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ErrNoLibmpv: libmpv could not be loaded.
var ErrNoLibmpv = errors.New("libmpv not found")

// LibmpvNames are tried in order.
var LibmpvNames = []string{"libmpv.so.2", "libmpv.so.1", "libmpv.so"}

// DefaultMaxSide caps the longer side of a rendered frame.
const DefaultMaxSide = 4096

// MaxVolume is mpv's default volume-max.
const MaxVolume = 130

// maxFrameSide caps Options.Size.
const maxFrameSide = 16384

// Source is what mpv opens: a local path, else a URL with request
// headers ("Name: value").
type Source struct {
	Path    string
	URL     string
	Headers []string
}

func (s Source) target() string {
	if s.Path != "" {
		return s.Path
	}
	return s.URL
}

// Options configure a Player.
type Options struct {
	Loop   bool
	Mute   bool
	Paused bool
	// Volume: start volume, 0 to MaxVolume.
	Volume float64
	// AudioOutput is mpv's ao; "" for mpv's default, "null" in tests.
	AudioOutput string
	// YTDL turns on mpv's ytdl_hook: URLs resolve through yt-dlp. A
	// URL that fails before it loaded is loaded again, twice at most.
	YTDL bool
	// YTDLFormat is ytdl-format; "" mpv's default.
	YTDLFormat string
	// AutoCaptions adds YouTube's auto-generated captions in the
	// video's language to the subtitle tracks; with YTDL.
	AutoCaptions bool
	// SubLang is the language of the subtitle track shown at the
	// start: slang; "" none.
	SubLang string
	// Start is the position playback starts at.
	Start time.Duration
	// Speed is the playback speed; 0 is 1.
	Speed float64
	// MaxSide caps the default frame size; 0 means DefaultMaxSide.
	MaxSide int
	// Size is the frame size to render for mpv's display size; nil:
	// RenderSize(native, 1, MaxSide). Zero renders nothing. Called on
	// the render goroutine.
	Size func(native image.Point) image.Point
	// OnFrame runs on the render goroutine after each rendered frame.
	OnFrame func()
	// OnChange runs on the event goroutine after a property change;
	// time-pos at most every 100 ms.
	OnChange func()
	// Observe names more mpv properties to follow, as strings:
	// "metadata/by-key/icy-title", "audio-codec-name". Their values are
	// Status.Props.
	Observe []string
	// Extra are more mpv options, set after the others: {"vid", "no"}.
	Extra [][2]string
}

// Status is the cached player state.
type Status struct {
	// Loaded: the file opened.
	Loaded bool
	Pos    time.Duration
	Dur    time.Duration
	Paused bool
	// Volume 0 to MaxVolume.
	Volume float64
	Mute   bool
	Speed  float64
	Loop   bool
	// EOF: at the end, keep-open.
	EOF bool
	// Size is mpv's display size; zero until known.
	Size image.Point
	// Subs are the subtitle tracks, Sub the ID of the one shown; 0
	// none. Not to be changed.
	Subs []Sub
	Sub  int
	// Err: the file failed to play.
	Err error
	// Props are the Options.Observe properties that have a value, by
	// name. Not to be changed.
	Props map[string]string
}

// Sub is a subtitle track.
type Sub struct {
	ID   int
	Lang string // "en", "pt-BR"; "" unknown
	// Auto: auto-generated captions, Options.AutoCaptions.
	Auto bool
}

// Playing reports a loaded file running: not paused, not at the end.
func (s Status) Playing() bool { return s.Loaded && !s.Paused && !s.EOF }

// RenderSize is the frame size to render native at scale: never above
// native, longer side at most maxSide, aspect kept.
func RenderSize(native image.Point, scale float32, maxSide int) image.Point {
	if native.X <= 0 || native.Y <= 0 {
		return image.Point{}
	}
	if maxSide <= 0 {
		maxSide = DefaultMaxSide
	}
	s := float64(scale)
	if s <= 0 || s > 1 {
		s = 1
	}
	if m := float64(max(native.X, native.Y)) * s; m > float64(maxSide) {
		s *= float64(maxSide) / m
	}
	w := int(math.Ceil(float64(native.X)*s - 1e-6))
	h := int(math.Ceil(float64(native.Y)*s - 1e-6))
	return image.Pt(max(w, 1), max(h, 1))
}

// mpv event ids and formats used here.
const (
	evNone           = 0
	evShutdown       = 1
	evEndFile        = 7
	evFileLoaded     = 8
	evPropertyChange = 22

	fmtNone   = 0
	fmtString = 1
	fmtFlag   = 3
	fmtInt64  = 4
	fmtDouble = 5

	endFileError = 4
)

// Observed properties; the id is the mpv reply_userdata.
type propID uint64

const (
	propTimePos propID = iota + 1
	propDuration
	propPause
	propVolume
	propMute
	propSpeed
	propLoopFile
	propDWidth
	propDHeight
	propEOF
	propTrackList
	// propExtra and up: Options.Observe, in order.
	propExtra propID = 1000
)

var observed = []struct {
	id     propID
	name   string
	format int
}{
	{propTimePos, "time-pos", fmtDouble},
	{propDuration, "duration", fmtDouble},
	{propPause, "pause", fmtFlag},
	{propVolume, "volume", fmtDouble},
	{propMute, "mute", fmtFlag},
	{propSpeed, "speed", fmtDouble},
	{propLoopFile, "loop-file", fmtString},
	{propDWidth, "dwidth", fmtInt64},
	{propDHeight, "dheight", fmtInt64},
	{propEOF, "eof-reached", fmtFlag},
	{propTrackList, "track-list", fmtString},
}

// mpvEvent is a flattened mpv_event.
type mpvEvent struct {
	id     int
	err    int
	prop   propID
	format int
	d      float64
	i      int64
	flag   bool
	str    string
	reason int
	errMsg string
}

// mpvState mirrors mpv properties.
type mpvState struct {
	loaded bool
	pos    float64
	dur    float64
	paused bool
	volume float64
	mute   bool
	speed  float64
	loop   bool
	w, h   int
	eof    bool
	subs   []Sub
	sub    int
	err    error
	// retries left for a load that fails before the file loaded
	retries int
	// extra names the Options.Observe properties; props holds their
	// values, replaced whole on a change.
	extra []string
	props map[string]string
}

// ytdlRetries is how often a ytdl URL that failed before it loaded is
// loaded again: YouTube now and then hands out stream URLs that answer
// 403 for good, and only a new extraction gives working ones.
const ytdlRetries = 2

// retry reports a failed load to run again, and counts it: an end
// with an error before the file loaded, retries left.
func (s *mpvState) retry(e mpvEvent) bool {
	if e.id != evEndFile || e.reason != endFileError || s.loaded || s.retries <= 0 {
		return false
	}
	s.retries--
	return true
}

// apply folds an event in; it reports a visible change.
func (s *mpvState) apply(e mpvEvent) bool {
	switch e.id {
	case evFileLoaded:
		s.loaded, s.err = true, nil
		return true
	case evEndFile:
		if e.reason == endFileError {
			s.err = fmt.Errorf("mpv: %s", e.errMsg)
			return true
		}
		return false
	case evPropertyChange:
	default:
		return false
	}
	none := e.format == fmtNone
	switch e.prop {
	case propTimePos:
		s.pos = e.d
	case propDuration:
		s.dur = e.d
	case propPause:
		s.paused = e.flag
	case propVolume:
		if !none {
			s.volume = e.d
		}
	case propMute:
		s.mute = e.flag
	case propSpeed:
		if !none {
			s.speed = e.d
		}
	case propLoopFile:
		if !none {
			s.loop = e.str != "no" && e.str != ""
		}
	case propDWidth:
		s.w = int(e.i)
	case propDHeight:
		s.h = int(e.i)
	case propEOF:
		s.eof = e.flag
	case propTrackList:
		subs, sub := parseSubs(e.str)
		if sub == s.sub && slices.Equal(subs, s.subs) {
			return false
		}
		s.subs, s.sub = subs, sub
	default:
		return s.applyExtra(e)
	}
	return true
}

// applyExtra folds an Options.Observe property in: a copy of props with
// the value set, or without it when the property has none.
func (s *mpvState) applyExtra(e mpvEvent) bool {
	i := int(e.prop) - int(propExtra)
	if e.prop < propExtra || i >= len(s.extra) {
		return false
	}
	name, val := s.extra[i], strings.ToValidUTF8(e.str, "")
	old, had := s.props[name]
	has := e.format == fmtString
	if has == had && val == old {
		return false
	}
	props := make(map[string]string, len(s.props)+1)
	for k, v := range s.props {
		props[k] = v
	}
	if has {
		props[name] = val
	} else {
		delete(props, name)
	}
	s.props = props
	return true
}

func (s *mpvState) status() Status {
	st := Status{
		Loaded: s.loaded,
		Pos:    secs(s.pos),
		Dur:    secs(s.dur),
		Paused: s.paused,
		Volume: s.volume,
		Mute:   s.mute,
		Speed:  s.speed,
		Loop:   s.loop,
		EOF:    s.eof,
		Subs:   s.subs,
		Sub:    s.sub,
		Err:    s.err,
		Props:  s.props,
	}
	if s.w > 0 && s.h > 0 {
		st.Size = image.Pt(s.w, s.h)
	}
	return st
}

// slang is lang, then its primary subtag: "pt-BR,pt".
func slang(lang string) string {
	if i := strings.IndexAny(lang, "-_"); i > 0 {
		return lang + "," + lang[:i]
	}
	return lang
}

// autoTitle is the title ytdl-subs.lua gives the tracks it adds.
const autoTitle = "auto"

// parseSubs reads the subtitle tracks and the one selected from mpv's
// track-list, as JSON.
func parseSubs(list string) ([]Sub, int) {
	var tracks []struct {
		ID       int    `json:"id"`
		Type     string `json:"type"`
		Lang     string `json:"lang"`
		Title    string `json:"title"`
		Selected bool   `json:"selected"`
	}
	if json.Unmarshal([]byte(list), &tracks) != nil {
		return nil, 0
	}
	var subs []Sub
	sel := 0
	for _, t := range tracks {
		if t.Type != "sub" || t.ID <= 0 {
			continue
		}
		subs = append(subs, Sub{ID: t.ID, Lang: t.Lang, Auto: t.Title == autoTitle})
		if t.Selected {
			sel = t.ID
		}
	}
	return subs, sel
}

func secs(f float64) time.Duration {
	if math.IsNaN(f) || math.Abs(f) > float64(math.MaxInt64/int64(time.Second)) {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func loopValue(on bool) string {
	if on {
		return "inf"
	}
	return "no"
}

func clampVolume(v float64) float64 { return min(max(v, 0), MaxVolume) }

// optional are set before mpv_initialize, errors ignored: mpv's
// built-in scripts, options of newer mpv versions.
var optional = [][2]string{
	{"load-stats-overlay", "no"},
	{"load-console", "no"},
	{"load-select", "no"},
	{"load-positioning", "no"},
	{"load-commands", "no"},
	{"load-context-menu", "no"},
	{"load-auto-profiles", "no"},
}

// listEscape escapes a string list item: mpv splits on unescaped commas.
var listEscape = strings.NewReplacer(`\`, `\\`, ",", `\,`)

// optionList is the option set before mpv_initialize.
func optionList(src Source, o Options) [][2]string {
	opts := [][2]string{
		{"config", "no"},
		{"terminal", "no"},
		{"load-scripts", "no"},
		{"ytdl", yesNo(o.YTDL)},
		{"osc", "no"},
		{"osd-level", "0"},
		{"input-default-bindings", "no"},
		{"input-vo-keyboard", "no"},
		{"vo", "libmpv"},
		{"hwdec", "no"},
		{"idle", "yes"},
		{"keep-open", "yes"},
		{"audio-display", "no"},
		{"loop-file", loopValue(o.Loop)},
		{"volume", fmt.Sprintf("%g", clampVolume(o.Volume))},
		{"mute", yesNo(o.Mute)},
		{"pause", yesNo(o.Paused)},
	}
	if o.AudioOutput != "" {
		opts = append(opts, [2]string{"ao", o.AudioOutput})
	}
	if o.YTDL && o.YTDLFormat != "" {
		opts = append(opts, [2]string{"ytdl-format", o.YTDLFormat})
	}
	if o.Speed > 0 {
		opts = append(opts, [2]string{"speed", fmt.Sprintf("%g", min(max(o.Speed, 0.1), 8))})
	}
	if o.Start > 0 {
		opts = append(opts, [2]string{"start", fmt.Sprintf("%.3f", o.Start.Seconds())})
	}
	if o.YTDL {
		var scripts []string
		if path, err := ytdlWaitPath(); err == nil {
			scripts = append(scripts, path)
		}
		if path, err := ytdlSubsPath(); err == nil && o.AutoCaptions {
			scripts = append(scripts, path)
		}
		if len(scripts) > 0 {
			opts = append(opts, [2]string{"scripts", strings.Join(scripts, string(filepath.ListSeparator))})
		}
	}
	if o.SubLang != "" {
		opts = append(opts, [2]string{"slang", slang(o.SubLang)})
	}
	if src.Path == "" {
		var fields []string
		for _, h := range src.Headers {
			if name, val, ok := strings.Cut(h, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "User-Agent") {
				opts = append(opts, [2]string{"user-agent", strings.TrimSpace(val)})
				continue
			}
			fields = append(fields, listEscape.Replace(h))
		}
		if len(fields) > 0 {
			opts = append(opts, [2]string{"http-header-fields", strings.Join(fields, ",")})
		}
	}
	return append(opts, o.Extra...)
}
