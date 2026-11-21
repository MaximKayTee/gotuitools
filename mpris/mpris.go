// Package mpris puts a player on the session bus as an MPRIS media
// player: media keys, headset buttons, playerctl and desktop widgets
// reach it. The program keeps playing itself: Set tells the bus what
// plays, Commands is what the bus asks for.
package mpris

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	objPath     = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	rootIface   = "org.mpris.MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"
	propsIface  = "org.freedesktop.DBus.Properties"
	noTrack     = dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")
)

// Status is the PlaybackStatus.
type Status string

const (
	Stopped Status = "Stopped"
	Playing Status = "Playing"
	Paused  Status = "Paused"
)

// State is what the bus shows.
type State struct {
	// Track names what plays, a video ID say. "": nothing, and the
	// bus name is not held.
	Track  string
	Status Status // "" is Stopped
	Title  string
	Artist string
	URL    string
	ArtURL string
	// Length 0: unknown, no seeking.
	Length   time.Duration
	Position time.Duration
	Volume   float64 // 0 to 1
	Rate     float64 // 1 normal; 0 is 1
}

// Kind is what a Command asks for.
type Kind int

const (
	PlayPause Kind = iota
	Play
	Pause
	Stop
	Seek        // by Offset
	SetPosition // to Position
	SetVolume   // to Volume
	SetRate     // to Rate
)

// Command is a request from the bus.
type Command struct {
	Kind     Kind
	Offset   time.Duration
	Position time.Duration
	Volume   float64
	Rate     float64
}

// Options name the player.
type Options struct {
	// Name ends the bus name: org.mpris.MediaPlayer2.<Name>.instance<pid>.
	// Letters, digits and _.
	Name string
	// Identity is the name shown; "" Name.
	Identity string
	// DesktopEntry is the .desktop file's name without the suffix.
	DesktopEntry string
	// MinRate and MaxRate bound SetRate; 0 is 1.
	MinRate, MaxRate float64
	// Address is the bus; "" the session bus.
	Address string
}

// Server is the player on the bus.
type Server struct {
	o    Options
	conn *dbus.Conn
	name string
	ch   chan Command
	now  func() time.Time

	mu    sync.Mutex
	st    State
	at    time.Time // when st.Position was told
	track int       // tracks so far: the track id
}

// Open connects to the bus. The name is held while a track is Set.
func Open(o Options) (*Server, error) {
	if o.Name == "" || !dbus.ObjectPath("/"+o.Name).IsValid() {
		return nil, fmt.Errorf("mpris: name %q", o.Name)
	}
	if o.Identity == "" {
		o.Identity = o.Name
	}
	if o.MinRate <= 0 {
		o.MinRate = 1
	}
	if o.MaxRate <= 0 {
		o.MaxRate = 1
	}
	var conn *dbus.Conn
	var err error
	if o.Address == "" {
		conn, err = dbus.ConnectSessionBus()
	} else {
		conn, err = dbus.Connect(o.Address)
	}
	if err != nil {
		return nil, fmt.Errorf("mpris: %w", err)
	}
	s := &Server{o: o, conn: conn, ch: make(chan Command, 16), now: time.Now,
		name: fmt.Sprintf("%s.%s.instance%d", rootIface, o.Name, os.Getpid())}
	s.st = clean(State{})
	if err := s.export(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("mpris: %w", err)
	}
	return s, nil
}

// Name is the bus name.
func (s *Server) Name() string { return s.name }

// Commands receives the bus's requests. One not read while 16 wait is
// dropped. nil on a nil Server.
func (s *Server) Commands() <-chan Command {
	if s == nil {
		return nil
	}
	return s.ch
}

// Close leaves the bus.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.conn.Close()
}

func clean(st State) State {
	if st.Track == "" {
		return State{Status: Stopped, Volume: st.Volume, Rate: 1}
	}
	if st.Status == "" {
		st.Status = Stopped
	}
	if st.Rate <= 0 {
		st.Rate = 1
	}
	st.Length, st.Position = max(st.Length, 0), max(st.Position, 0)
	return st
}

// seekSlack is how far Position may be from where playback alone puts
// it before the bus is told of a seek.
const seekSlack = time.Second

// Set tells the bus what plays. Only what changed is sent; Position
// alone, moving as playback does, sends nothing. Never blocks on the
// bus's answer. No-op on a nil Server.
func (s *Server) Set(st State) {
	if s == nil {
		return
	}
	st = clean(st)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, oldAt, now := s.st, s.at, s.now()
	s.st, s.at = st, now
	if st.Track != old.Track {
		s.track++
	}
	if st.Track == "" {
		if old.Track != "" {
			s.bus("ReleaseName", s.name)
		}
		return
	}
	if old.Track == "" {
		// the properties are read once the name shows
		s.bus("RequestName", s.name, uint32(dbus.NameFlagDoNotQueue))
		return
	}
	ch := map[string]dbus.Variant{}
	if st.Status != old.Status {
		ch["PlaybackStatus"] = dbus.MakeVariant(string(st.Status))
	}
	if st.Track != old.Track || st.Title != old.Title || st.Artist != old.Artist || st.URL != old.URL ||
		st.ArtURL != old.ArtURL || st.Length != old.Length {
		ch["Metadata"] = dbus.MakeVariant(s.metadata())
	}
	if (st.Length > 0) != (old.Length > 0) {
		ch["CanSeek"] = dbus.MakeVariant(st.Length > 0)
	}
	if st.Volume != old.Volume {
		ch["Volume"] = dbus.MakeVariant(st.Volume)
	}
	if st.Rate != old.Rate {
		ch["Rate"] = dbus.MakeVariant(st.Rate)
	}
	if len(ch) > 0 {
		_ = s.conn.Emit(objPath, propsIface+".PropertiesChanged", playerIface, ch, []string{})
	}
	if st.Track != old.Track {
		return
	}
	played := time.Duration(0)
	if old.Status == Playing {
		played = time.Duration(float64(now.Sub(oldAt)) * old.Rate)
	}
	if st.Position < old.Position-seekSlack || st.Position > old.Position+played+seekSlack {
		_ = s.conn.Emit(objPath, playerIface+".Seeked", st.Position.Microseconds())
	}
}

// bus calls the bus daemon without waiting for the answer.
func (s *Server) bus(method string, args ...any) {
	s.conn.BusObject().Go("org.freedesktop.DBus."+method, dbus.FlagNoReplyExpected, nil, args...)
}

// extrapolate is how long Position runs on from the last Set while
// playing: Sets come several a second.
const extrapolate = time.Second

// position is where playback is now; under mu.
func (s *Server) position() time.Duration {
	p := s.st.Position
	if s.st.Status == Playing {
		p += time.Duration(float64(min(s.now().Sub(s.at), extrapolate)) * s.st.Rate)
	}
	if s.st.Length > 0 {
		p = min(p, s.st.Length)
	}
	return p
}

func (s *Server) trackID() dbus.ObjectPath {
	if s.st.Track == "" {
		return noTrack
	}
	return dbus.ObjectPath(fmt.Sprintf("/%s/track/%d", s.o.Name, s.track))
}

// metadata is the Metadata property; under mu.
func (s *Server) metadata() map[string]dbus.Variant {
	st := s.st
	m := map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(s.trackID())}
	if st.Track == "" {
		return m
	}
	if st.Length > 0 {
		m["mpris:length"] = dbus.MakeVariant(st.Length.Microseconds())
	}
	if st.Title != "" {
		m["xesam:title"] = dbus.MakeVariant(st.Title)
	}
	if st.Artist != "" {
		m["xesam:artist"] = dbus.MakeVariant([]string{st.Artist})
	}
	if st.URL != "" {
		m["xesam:url"] = dbus.MakeVariant(st.URL)
	}
	if st.ArtURL != "" {
		m["mpris:artUrl"] = dbus.MakeVariant(st.ArtURL)
	}
	return m
}

// props are iface's properties; under mu.
func (s *Server) props(iface string) map[string]dbus.Variant {
	v := dbus.MakeVariant
	switch iface {
	case rootIface:
		return map[string]dbus.Variant{
			"CanQuit": v(false), "CanRaise": v(false), "HasTrackList": v(false),
			"Identity": v(s.o.Identity), "DesktopEntry": v(s.o.DesktopEntry),
			"SupportedUriSchemes": v([]string{}), "SupportedMimeTypes": v([]string{}),
		}
	case playerIface:
		on := s.st.Track != ""
		return map[string]dbus.Variant{
			"PlaybackStatus": v(string(s.st.Status)), "Rate": v(s.st.Rate), "Metadata": v(s.metadata()),
			"Volume": v(s.st.Volume), "Position": v(s.position().Microseconds()),
			"MinimumRate": v(s.o.MinRate), "MaximumRate": v(s.o.MaxRate),
			"CanGoNext": v(false), "CanGoPrevious": v(false), "CanPlay": v(on), "CanPause": v(on),
			"CanSeek": v(on && s.st.Length > 0), "CanControl": v(true),
		}
	}
	return nil
}

// send queues c while a track is Set.
func (s *Server) send(c Command) {
	s.mu.Lock()
	on := s.st.Track != ""
	s.mu.Unlock()
	if !on {
		return
	}
	select {
	case s.ch <- c:
	default:
	}
}

var errArgs = dbus.MakeFailedError(errors.New("invalid arguments"))

func (s *Server) export() error {
	do := func(k Kind) func() *dbus.Error {
		return func() *dbus.Error { s.send(Command{Kind: k}); return nil }
	}
	none := func() *dbus.Error { return nil }
	player := map[string]any{
		"Next": none, "Previous": none,
		"PlayPause": do(PlayPause), "Play": do(Play), "Pause": do(Pause), "Stop": do(Stop),
		"Seek": func(us int64) *dbus.Error {
			s.send(Command{Kind: Seek, Offset: time.Duration(us) * time.Microsecond})
			return nil
		},
		"SetPosition": func(id dbus.ObjectPath, us int64) *dbus.Error {
			pos := time.Duration(us) * time.Microsecond
			s.mu.Lock()
			ok := id == s.trackID() && pos >= 0 && pos <= s.st.Length
			s.mu.Unlock()
			if ok {
				s.send(Command{Kind: SetPosition, Position: pos})
			}
			return nil
		},
		"OpenUri": func(string) *dbus.Error {
			return dbus.NewError("org.freedesktop.DBus.Error.NotSupported", []any{"OpenUri"})
		},
	}
	props := map[string]any{
		"Get": func(iface, name string) (dbus.Variant, *dbus.Error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if p, ok := s.props(iface)[name]; ok {
				return p, nil
			}
			return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{iface + "." + name})
		},
		"GetAll": func(iface string) (map[string]dbus.Variant, *dbus.Error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if p := s.props(iface); p != nil {
				return p, nil
			}
			return map[string]dbus.Variant{}, nil
		},
		"Set": func(iface, name string, value dbus.Variant) *dbus.Error {
			x, ok := value.Value().(float64)
			switch {
			case iface != playerIface || name != "Volume" && name != "Rate":
				return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{iface + "." + name})
			case !ok:
				return errArgs
			case name == "Volume":
				s.send(Command{Kind: SetVolume, Volume: max(x, 0)})
			case x > 0:
				s.send(Command{Kind: SetRate, Rate: min(max(x, s.o.MinRate), s.o.MaxRate)})
			}
			return nil
		},
	}
	for _, e := range []struct {
		iface string
		v     map[string]any
	}{
		{rootIface, map[string]any{"Raise": none, "Quit": none}}, {playerIface, player}, {propsIface, props},
	} {
		if err := s.conn.ExportMethodTable(e.v, objPath, e.iface); err != nil {
			return err
		}
	}
	return s.conn.Export(introspect.Introspectable(introspectXML), objPath, "org.freedesktop.DBus.Introspectable")
}

const introspectXML = `<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd">
<node>
 <interface name="org.freedesktop.DBus.Introspectable">
  <method name="Introspect"><arg name="xml" type="s" direction="out"/></method>
 </interface>
 <interface name="org.freedesktop.DBus.Properties">
  <method name="Get"><arg name="interface" type="s" direction="in"/><arg name="property" type="s" direction="in"/><arg name="value" type="v" direction="out"/></method>
  <method name="GetAll"><arg name="interface" type="s" direction="in"/><arg name="properties" type="a{sv}" direction="out"/></method>
  <method name="Set"><arg name="interface" type="s" direction="in"/><arg name="property" type="s" direction="in"/><arg name="value" type="v" direction="in"/></method>
  <signal name="PropertiesChanged"><arg name="interface" type="s"/><arg name="changed" type="a{sv}"/><arg name="invalidated" type="as"/></signal>
 </interface>
 <interface name="org.mpris.MediaPlayer2">
  <method name="Raise"/>
  <method name="Quit"/>
  <property name="CanQuit" type="b" access="read"/>
  <property name="CanRaise" type="b" access="read"/>
  <property name="HasTrackList" type="b" access="read"/>
  <property name="Identity" type="s" access="read"/>
  <property name="DesktopEntry" type="s" access="read"/>
  <property name="SupportedUriSchemes" type="as" access="read"/>
  <property name="SupportedMimeTypes" type="as" access="read"/>
 </interface>
 <interface name="org.mpris.MediaPlayer2.Player">
  <method name="Next"/>
  <method name="Previous"/>
  <method name="Pause"/>
  <method name="PlayPause"/>
  <method name="Stop"/>
  <method name="Play"/>
  <method name="Seek"><arg name="Offset" type="x" direction="in"/></method>
  <method name="SetPosition"><arg name="TrackId" type="o" direction="in"/><arg name="Position" type="x" direction="in"/></method>
  <method name="OpenUri"><arg name="Uri" type="s" direction="in"/></method>
  <signal name="Seeked"><arg name="Position" type="x"/></signal>
  <property name="PlaybackStatus" type="s" access="read"/>
  <property name="Rate" type="d" access="readwrite"/>
  <property name="Metadata" type="a{sv}" access="read"/>
  <property name="Volume" type="d" access="readwrite"/>
  <property name="Position" type="x" access="read"/>
  <property name="MinimumRate" type="d" access="read"/>
  <property name="MaximumRate" type="d" access="read"/>
  <property name="CanGoNext" type="b" access="read"/>
  <property name="CanGoPrevious" type="b" access="read"/>
  <property name="CanPlay" type="b" access="read"/>
  <property name="CanPause" type="b" access="read"/>
  <property name="CanSeek" type="b" access="read"/>
  <property name="CanControl" type="b" access="read"/>
 </interface>
</node>`
