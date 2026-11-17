//go:build unix

package kitty

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCellSizeNotTerminal(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "plain"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err := CellSize(f); err == nil {
		t.Fatal("CellSize on a regular file: no error")
	}
}

func TestCellSizeStdout(t *testing.T) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		t.Skip("stdout not a tty")
	}
	w, h, err := CellSize(os.Stdout)
	if ws.Xpixel == 0 || ws.Ypixel == 0 || ws.Col == 0 || ws.Row == 0 {
		if err == nil {
			t.Fatalf("zero pixel winsize %+v: no error", ws)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if w != int(ws.Xpixel)/int(ws.Col) || h != int(ws.Ypixel)/int(ws.Row) || w <= 0 || h <= 0 {
		t.Fatalf("CellSize = %dx%d, winsize %+v", w, h, ws)
	}
}
