// Package termout serialises terminal output: one Write reaches the
// terminal whole, never interleaved with another producer's.
//
// Bubble Tea (tea.WithOutput), the grid, preview and viewer image
// queues and the video panes share one Writer.
//
// Read, Close and Fd make a Writer a term.File, so Bubble Tea still
// finds the terminal for size and raw mode: Fd and Read go to the
// underlying writer when it has them; Close is a no-op.
//
// Hold keeps Write and WriteString output back until the Renderer
// handle writes the alternate screen switch: kitty keeps images per
// screen, so images sent before it are not shown in the alternate one.
package termout

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// AltScreen is the alternate screen switch.
const AltScreen = "\x1b[?1049h"

// Writer is a mutex around an io.Writer. Use New.
type Writer struct {
	mu   sync.Mutex
	w    io.Writer
	hold bool
	held []byte
}

// New wraps w.
func New(w io.Writer) *Writer { return &Writer{w: w} }

// Write writes p under the lock; kept back while held.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hold {
		w.held = append(w.held, p...)
		return len(p), nil
	}
	return w.w.Write(p)
}

// WriteString writes s under the lock; kept back while held.
func (w *Writer) WriteString(s string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hold {
		w.held = append(w.held, s...)
		return len(s), nil
	}
	return io.WriteString(w.w, s)
}

// Hold keeps Write and WriteString output back until the Renderer
// writes AltScreen.
func (w *Writer) Hold() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hold = true
}

// Renderer is the handle for Bubble Tea: its writes pass while held; the
// first carrying AltScreen ends the hold, the kept output after it.
func (w *Writer) Renderer() *Renderer { return &Renderer{w} }

// Renderer is a Writer's Bubble Tea handle; a term.File like it.
type Renderer struct{ *Writer }

// Write writes p under the lock, then the kept output once p switches
// to the alternate screen.
func (r *Renderer) Write(p []byte) (int, error) {
	w := r.Writer
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.w.Write(p)
	if err != nil || !w.hold || !bytes.Contains(p, []byte(AltScreen)) {
		return n, err
	}
	w.hold = false
	held := w.held
	w.held = nil
	if len(held) > 0 {
		if _, err := w.w.Write(held); err != nil {
			return n, err
		}
	}
	return n, nil
}

// WriteString is Write.
func (r *Renderer) WriteString(s string) (int, error) { return r.Write([]byte(s)) }

// Lock holds the lock for several writes until the returned func runs.
// The holder writes with WriteLocked; Write blocks meanwhile, also in
// the holder.
func (w *Writer) Lock() func() {
	w.mu.Lock()
	var once sync.Once
	return func() { once.Do(w.mu.Unlock) }
}

// WriteLocked writes p without locking; the caller holds Lock.
func (w *Writer) WriteLocked(p []byte) (int, error) { return w.w.Write(p) }

// ErrNoRead: the underlying writer cannot be read.
var ErrNoRead = errors.New("termout: underlying writer has no Read")

// Read reads from the underlying writer when it is an io.Reader.
func (w *Writer) Read(p []byte) (int, error) {
	if r, ok := w.w.(io.Reader); ok {
		return r.Read(p)
	}
	return 0, ErrNoRead
}

// Close does nothing; the underlying writer stays open.
func (w *Writer) Close() error { return nil }

// Fd is the underlying writer's descriptor, ^uintptr(0) when it has
// none.
func (w *Writer) Fd() uintptr {
	if f, ok := w.w.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}
