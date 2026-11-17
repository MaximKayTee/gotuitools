//go:build !unix

package kitty

import (
	"errors"
	"os"
)

// CellSize is the pixel size of one terminal cell from TIOCGWINSZ on f (xpixel/ypixel divided by cols/rows). Error when the terminal reports zero pixels.
//
// Not unix: always an error.
func CellSize(f *os.File) (w, h int, err error) {
	return 0, 0, errors.New("kitty: CellSize needs TIOCGWINSZ (unix only)")
}
