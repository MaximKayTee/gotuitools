// Package awake keeps the screen from blanking or locking while a
// program plays video without a window of its own: an
// org.freedesktop.ScreenSaver inhibit on the session bus (godbus).
// The bus drops the inhibit when the connection closes, a crash too.
package awake

import (
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	service = "org.freedesktop.ScreenSaver"
	path    = dbus.ObjectPath("/org/freedesktop/ScreenSaver")
)

// Options name the inhibit.
type Options struct {
	App    string // the application name shown; "" is "gotuitools"
	Reason string // "" is "Playing video"
	// Address is the bus; "" the session bus.
	Address string
}

// Inhibitor holds the inhibit while On. Set never waits on the bus.
type Inhibitor struct {
	o    Options
	conn *dbus.Conn
	obj  dbus.BusObject
	kick chan struct{}
	done chan struct{}

	mu     sync.Mutex
	want   bool
	closed bool
	err    error
}

// Open connects to the bus. No screensaver service on it is not an
// error: Set then does nothing, Err says why.
func Open(o Options) (*Inhibitor, error) {
	if o.App == "" {
		o.App = "gotuitools"
	}
	if o.Reason == "" {
		o.Reason = "Playing video"
	}
	var conn *dbus.Conn
	var err error
	if o.Address == "" {
		conn, err = dbus.ConnectSessionBus()
	} else {
		conn, err = dbus.Connect(o.Address)
	}
	if err != nil {
		return nil, fmt.Errorf("awake: %w", err)
	}
	in := &Inhibitor{o: o, conn: conn, obj: conn.Object(service, path),
		kick: make(chan struct{}, 1), done: make(chan struct{})}
	go in.run()
	return in, nil
}

// Set holds the inhibit when on, drops it when not. Repeats are free.
// Nothing on a nil Inhibitor.
func (in *Inhibitor) Set(on bool) {
	if in == nil {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.want == on || in.closed {
		return
	}
	in.want = on
	select {
	case in.kick <- struct{}{}:
	default:
	}
}

// Err is the last failed bus call; nil after one succeeds.
func (in *Inhibitor) Err() error {
	if in == nil {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.err
}

// Close drops the inhibit and leaves the bus.
func (in *Inhibitor) Close() {
	if in == nil {
		return
	}
	in.mu.Lock()
	if in.closed {
		in.mu.Unlock()
		return
	}
	in.closed, in.want = true, false
	in.mu.Unlock()
	close(in.kick)
	<-in.done
	in.conn.Close()
}

// run applies the latest wanted state, one bus call at a time.
func (in *Inhibitor) run() {
	defer close(in.done)
	var cookie uint32
	held := false
	for range in.kick {
		in.mu.Lock()
		want := in.want
		in.mu.Unlock()
		var err error
		switch {
		case want && !held:
			if err = in.obj.Call(service+".Inhibit", 0, in.o.App, in.o.Reason).Store(&cookie); err == nil {
				held = true
			}
		case !want && held:
			err = in.obj.Call(service+".UnInhibit", 0, cookie).Err
			held = false // a failed UnInhibit is not retried; the bus drops it on Close
		}
		in.mu.Lock()
		in.err = err
		in.mu.Unlock()
	}
	if held {
		_ = in.obj.Call(service+".UnInhibit", 0, cookie).Err
	}
}
