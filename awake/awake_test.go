package awake

import (
	"bufio"
	"os/exec"
	"strings"
	"sync"
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

// saver is a fake screensaver: the inhibits held, by cookie.
type saver struct {
	mu   sync.Mutex
	next uint32
	held map[uint32]string // cookie: app
	log  []string
}

func (s *saver) Inhibit(app, reason string) (uint32, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.held[s.next] = app
	s.log = append(s.log, "inhibit "+app+" "+reason)
	return s.next, nil
}

func (s *saver) UnInhibit(cookie uint32) *dbus.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.held, cookie)
	s.log = append(s.log, "uninhibit")
	return nil
}

func (s *saver) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.held)
}

func (s *saver) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

func newSaver(t *testing.T, addr string) *saver {
	c, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	s := &saver{held: map[uint32]string{}}
	if err := c.Export(s, path, service); err != nil {
		t.Fatal(err)
	}
	if r, err := c.RequestName(service, dbus.NameFlagDoNotQueue); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatal(r, err)
	}
	return s
}

func waitHeld(t *testing.T, s *saver, want int) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if s.count() == want {
			return
		}
	}
	t.Fatalf("held %d, want %d; calls %q", s.count(), want, s.calls())
}

func TestSetAndClose(t *testing.T) {
	addr := privateBus(t)
	s := newSaver(t, addr)
	in, err := Open(Options{App: "probe", Address: addr})
	if err != nil {
		t.Fatal(err)
	}
	in.Set(true)
	in.Set(true)
	waitHeld(t, s, 1)
	in.Set(false)
	waitHeld(t, s, 0)
	in.Set(true)
	waitHeld(t, s, 1)
	in.Close()
	waitHeld(t, s, 0)
	in.Set(true) // after Close: nothing
	in.Close()
	want := []string{"inhibit probe Playing video", "uninhibit", "inhibit probe Playing video", "uninhibit"}
	if got := s.calls(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %q, want %q", got, want)
	}
	if err := in.Err(); err != nil {
		t.Fatal(err)
	}
}

// Toggles faster than the bus end in the last state, without a call per
// toggle.
func TestLatestWins(t *testing.T) {
	addr := privateBus(t)
	s := newSaver(t, addr)
	in, err := Open(Options{Address: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 1000; i++ {
		in.Set(i%2 == 0)
	}
	in.Set(true)
	waitHeld(t, s, 1)
	if n := len(s.calls()); n > 100 {
		t.Fatalf("%d calls for 1001 sets", n)
	}
}

func TestNoService(t *testing.T) {
	in, err := Open(Options{Address: privateBus(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	in.Set(true)
	for end := time.Now().Add(2 * time.Second); in.Err() == nil; time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("no error without a screensaver")
		}
	}
	in.Set(false)
}

func TestNil(t *testing.T) {
	var in *Inhibitor
	in.Set(true)
	in.Close()
	if in.Err() != nil {
		t.Fatal("nil Err")
	}
}
