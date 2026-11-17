//go:build unix

package kitty

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// CellSize is the pixel size of one terminal cell from TIOCGWINSZ on f (xpixel/ypixel divided by cols/rows). Error when the terminal reports zero pixels.
func CellSize(f *os.File) (w, h int, err error) {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, fmt.Errorf("kitty: TIOCGWINSZ: %w", err)
	}
	if ws.Xpixel == 0 || ws.Ypixel == 0 || ws.Col == 0 || ws.Row == 0 {
		return 0, 0, errors.New("kitty: terminal reports zero pixel size")
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row), nil
}
