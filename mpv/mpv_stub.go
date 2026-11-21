//go:build !cgo || !linux

package mpv

import (
	"image"
	"time"
)

// Available reports whether libmpv loads; never in this build.
func Available() error { return ErrNoLibmpv }

// Player is unavailable in this build.
type Player struct{}

// New always fails in this build.
func New(Source, Options) (*Player, error) { return nil, ErrNoLibmpv }

func (*Player) Redraw()                            {}
func (*Player) Rendered() int                      { return 0 }
func (*Player) Frame() (*image.RGBA, uint64, bool) { return nil, 0, false }
func (*Player) Status() Status                     { return Status{} }
func (*Player) SetPause(bool)                      {}
func (*Player) Play()                              {}
func (*Player) Pause()                             {}
func (*Player) TogglePause()                       {}
func (*Player) Seek(time.Duration, bool)           {}
func (*Player) SeekBy(time.Duration)               {}
func (*Player) SeekPercent(float64)                {}
func (*Player) Step(int)                           {}
func (*Player) SetLoop(bool)                       {}
func (*Player) SetSpeed(float64)                   {}
func (*Player) SetVolume(float64)                  {}
func (*Player) SetMute(bool)                       {}
func (*Player) SetSub(int)                         {}
func (*Player) Close()                             {}

// Done is closed.
func (*Player) Done() <-chan struct{} { return closed }

var closed = func() chan struct{} { c := make(chan struct{}); close(c); return c }()
