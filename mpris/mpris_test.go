package mpris

import (
	"bufio"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// privateBus starts a dbus-daemon of the test's own.
func privateBus(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("no dbus-daemon")
	}
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address", "--address=unix:dir="+t.TempDir())
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(addr)
}

type rig struct {
	t    *testing.T
	s    *Server
	c    *dbus.Conn
	obj  dbus.BusObject
	sigs chan *dbus.Signal
	now  atomic.Int64 // the server's clock, ns
}

func (r *rig) advance(d time.Duration) { r.now.Add(int64(d)) }

func newRig(t *testing.T) *rig {
	addr := privateBus(t)
	s, err := Open(Options{Name: "probe", Identity: "Probe", DesktopEntry: "probe", MinRate: 0.25, MaxRate: 3, Address: addr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	c, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	r := &rig{t: t, s: s, c: c, obj: c.Object(s.Name(), objPath), sigs: make(chan *dbus.Signal, 16)}
	r.now.Store(1700000000e9)
	s.mu.Lock()
	s.now = func() time.Time { return time.Unix(0, r.now.Load()) }
	s.mu.Unlock()
	if err := c.AddMatchSignal(dbus.WithMatchObjectPath(objPath)); err != nil {
		t.Fatal(err)
	}
	c.Signal(r.sigs)
	return r
}

func (r *rig) owned() bool {
	var has bool
	if err := r.c.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, r.s.Name()).Store(&has); err != nil {
		r.t.Fatal(err)
	}
	return has
}

func (r *rig) waitOwned(want bool) {
	r.t.Helper()
	for range 200 {
		if r.owned() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("name owned: want %v", want)
}

func (r *rig) prop(name string) any {
	r.t.Helper()
	v, err := r.obj.GetProperty(playerIface + "." + name)
	if err != nil {
		r.t.Fatal(name, err)
	}
	return v.Value()
}

func (r *rig) signal() *dbus.Signal {
	r.t.Helper()
	select {
	case s := <-r.sigs:
		return s
	case <-time.After(2 * time.Second):
		r.t.Fatal("no signal")
		return nil
	}
}

// quiet: no signal waits after a round trip to the server.
func (r *rig) quiet() {
	r.t.Helper()
	r.prop("Rate")
	select {
	case s := <-r.sigs:
		r.t.Fatalf("signal %s %v", s.Name, s.Body)
	default:
	}
}

func (r *rig) command() Command {
	r.t.Helper()
	select {
	case c := <-r.s.Commands():
		return c
	case <-time.After(2 * time.Second):
		r.t.Fatal("no command")
		return Command{}
	}
}

var track = State{Track: "abc", Status: Playing, Title: "A title", Artist: "A channel", URL: "https://example.org/abc",
	ArtURL: "file:///tmp/abc.jpg", Length: 10 * time.Minute, Position: 30 * time.Second, Volume: 0.8, Rate: 1.5}

func TestNameHeldWhileTrack(t *testing.T) {
	r := newRig(t)
	if r.owned() {
		t.Fatal("name held with no track")
	}
	r.s.Set(track)
	r.waitOwned(true)
	if !strings.HasPrefix(r.s.Name(), "org.mpris.MediaPlayer2.probe.instance") {
		t.Fatal(r.s.Name())
	}
	r.s.Set(State{})
	r.waitOwned(false)
}

func TestProperties(t *testing.T) {
	r := newRig(t)
	r.s.Set(track)
	r.waitOwned(true)
	var all map[string]dbus.Variant
	if err := r.obj.Call(propsIface+".GetAll", 0, playerIface).Store(&all); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"PlaybackStatus": "Playing", "Rate": 1.5, "Volume": 0.8, "Position": int64(30e6),
		"MinimumRate": 0.25, "MaximumRate": 3.0, "CanGoNext": false, "CanGoPrevious": false,
		"CanPlay": true, "CanPause": true, "CanSeek": true, "CanControl": true}
	for k, w := range want {
		if got := all[k].Value(); got != w {
			t.Errorf("%s = %v, want %v", k, got, w)
		}
	}
	md := all["Metadata"].Value().(map[string]dbus.Variant)
	wantMD := map[string]any{"mpris:trackid": dbus.ObjectPath("/probe/track/1"), "mpris:length": int64(600e6),
		"xesam:title": "A title", "xesam:url": "https://example.org/abc", "mpris:artUrl": "file:///tmp/abc.jpg"}
	for k, w := range wantMD {
		if got := md[k].Value(); got != w {
			t.Errorf("%s = %v, want %v", k, got, w)
		}
	}
	if a := md["xesam:artist"].Value().([]string); len(a) != 1 || a[0] != "A channel" {
		t.Errorf("artist %v", a)
	}
	if v, err := r.obj.GetProperty(rootIface + ".Identity"); err != nil || v.Value() != "Probe" {
		t.Errorf("Identity %v %v", v, err)
	}
	var xml string
	if err := r.obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml); err != nil || !strings.Contains(xml, playerIface) {
		t.Errorf("introspect: %v", err)
	}
}

// Position runs on from the last Set while playing, a second at most,
// at Rate.
func TestPositionRunsOn(t *testing.T) {
	r := newRig(t)
	r.s.Set(track)
	r.waitOwned(true)
	r.advance(400 * time.Millisecond)
	if got := r.prop("Position"); got != int64(30.6e6) {
		t.Errorf("after 0.4 s: %v", got)
	}
	r.advance(time.Minute)
	if got := r.prop("Position"); got != int64(31.5e6) {
		t.Errorf("after a minute: %v", got)
	}
	p := track
	p.Status = Paused
	r.s.Set(p)
	r.advance(time.Minute)
	if got := r.prop("Position"); got != int64(30e6) {
		t.Errorf("paused: %v", got)
	}
}

func TestChangesAndSeeked(t *testing.T) {
	r := newRig(t)
	r.s.Set(track)
	r.waitOwned(true)

	// playback alone: nothing sent
	st := track
	r.advance(250 * time.Millisecond)
	st.Position += 375 * time.Millisecond
	r.s.Set(st)
	r.quiet()

	st.Status = Paused
	r.s.Set(st)
	sig := r.signal()
	if sig.Name != propsIface+".PropertiesChanged" {
		t.Fatal(sig.Name)
	}
	ch := sig.Body[1].(map[string]dbus.Variant)
	if len(ch) != 1 || ch["PlaybackStatus"].Value() != "Paused" {
		t.Fatalf("changed %v", ch)
	}

	st.Position += 20 * time.Second
	r.s.Set(st)
	sig = r.signal()
	if sig.Name != playerIface+".Seeked" || sig.Body[0] != st.Position.Microseconds() {
		t.Fatalf("%s %v", sig.Name, sig.Body)
	}
	st.Position -= 5 * time.Second
	r.s.Set(st)
	if sig = r.signal(); sig.Name != playerIface+".Seeked" {
		t.Fatal(sig.Name)
	}

	// a stall while playing is no seek
	st.Status = Playing
	r.s.Set(st)
	r.signal()
	r.advance(10 * time.Second)
	r.s.Set(st)
	r.quiet()

	st.Track, st.Title, st.Position, st.Volume = "def", "Next", 0, 0.5
	r.s.Set(st)
	ch = r.signal().Body[1].(map[string]dbus.Variant)
	md, _ := ch["Metadata"].Value().(map[string]dbus.Variant)
	if md["mpris:trackid"].Value() != dbus.ObjectPath("/probe/track/2") || md["xesam:title"].Value() != "Next" || ch["Volume"].Value() != 0.5 {
		t.Fatalf("changed %v", ch)
	}
	r.quiet()
}

func TestCommands(t *testing.T) {
	r := newRig(t)
	call := func(method string, args ...any) {
		t.Helper()
		if err := r.obj.Call(playerIface+"."+method, 0, args...).Err; err != nil {
			t.Fatal(method, err)
		}
	}
	r.s.Set(track)
	r.waitOwned(true)
	for _, c := range []struct {
		method string
		kind   Kind
	}{{"PlayPause", PlayPause}, {"Play", Play}, {"Pause", Pause}, {"Stop", Stop}} {
		call(c.method)
		if got := r.command(); got.Kind != c.kind {
			t.Errorf("%s: %v", c.method, got)
		}
	}
	call("Seek", int64(-5e6))
	if got := r.command(); got.Kind != Seek || got.Offset != -5*time.Second {
		t.Errorf("Seek: %+v", got)
	}
	call("SetPosition", dbus.ObjectPath("/probe/track/1"), int64(90e6))
	if got := r.command(); got.Kind != SetPosition || got.Position != 90*time.Second {
		t.Errorf("SetPosition: %+v", got)
	}
	if err := r.obj.SetProperty(playerIface+".Volume", dbus.MakeVariant(0.25)); err != nil {
		t.Fatal(err)
	}
	if got := r.command(); got.Kind != SetVolume || got.Volume != 0.25 {
		t.Errorf("Volume: %+v", got)
	}
	if err := r.obj.SetProperty(playerIface+".Rate", dbus.MakeVariant(9.0)); err != nil {
		t.Fatal(err)
	}
	if got := r.command(); got.Kind != SetRate || got.Rate != 3 {
		t.Errorf("Rate: %+v", got)
	}

	// ignored: another track's position, one past the end, Next
	call("SetPosition", dbus.ObjectPath("/probe/track/7"), int64(90e6))
	call("SetPosition", dbus.ObjectPath("/probe/track/1"), int64(601e6))
	call("Next")
	call("Previous")
	if err := r.obj.SetProperty(playerIface+".PlaybackStatus", dbus.MakeVariant("Paused")); err == nil {
		t.Error("PlaybackStatus set")
	}
	if err := r.obj.Call(playerIface+".OpenUri", 0, "https://example.org").Err; err == nil {
		t.Error("OpenUri")
	}
	call("Pause")
	if got := r.command(); got.Kind != Pause {
		t.Errorf("after the ignored: %+v", got)
	}

	// no track: nothing, to the connection's own name too
	r.s.Set(State{})
	r.waitOwned(false)
	own := r.c.Object(r.s.conn.Names()[0], objPath)
	if err := own.Call(playerIface+".PlayPause", 0).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-r.s.Commands():
		t.Errorf("command with no track: %+v", c)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNil(t *testing.T) {
	var s *Server
	s.Set(track)
	s.Close()
	if s.Commands() != nil {
		t.Fatal("commands")
	}
	if _, err := Open(Options{Name: "a-b"}); err == nil {
		t.Fatal("name a-b")
	}
}

// playerctl, what desktop media key binds run, finds and drives it.
func TestPlayerctl(t *testing.T) {
	bin, err := exec.LookPath("playerctl")
	if err != nil {
		t.Skip("no playerctl")
	}
	r := newRig(t)
	r.s.Set(track)
	r.waitOwned(true)
	ctl := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(cmd.Environ(), "DBUS_SESSION_BUS_ADDRESS="+r.s.o.Address)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("playerctl %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := ctl("-l"); !strings.HasPrefix(got, "probe.instance") {
		t.Fatalf("list: %q", got)
	}
	if got := ctl("metadata", "--format", "{{status}}|{{title}}|{{artist}}|{{mpris:length}}"); got != "Playing|A title|A channel|600000000" {
		t.Errorf("metadata: %q", got)
	}
	ctl("play-pause")
	if got := r.command(); got.Kind != PlayPause {
		t.Errorf("play-pause: %+v", got)
	}
	ctl("position", "10-")
	if got := r.command(); got.Kind != Seek || got.Offset != -10*time.Second {
		t.Errorf("position 10-: %+v", got)
	}
	ctl("volume", "0.5")
	if got := r.command(); got.Kind != SetVolume || got.Volume != 0.5 {
		t.Errorf("volume: %+v", got)
	}
}
