package kitty

import (
	"strconv"
	"strings"
)

// Place creates a visible placement of image id with placement pid at cell (col,row) (0-based, absolute), scaled to cols x rows cells, showing the source rectangle (sx,sy,sw,sh in image pixels; sw/sh 0 = whole image), z-index z, without moving the cursor (C=1), q=2. The returned string saves the cursor (ESC 7), moves it with CUP (1-based), emits the placement, restores (ESC 8).
//
// Keys in order: a=p,i,p,c,r,x,y,w,h,z,C=1,q=2. Negative col, row,
// cols, rows, sx, sy, sw, sh are sent as 0; z as is.
//
// Source rectangle: kitty shows its intersection with the image; sw or
// sh 0 runs to the image's right or bottom edge. Kitty scales the
// rectangle into the cols x rows box. Aspect is kept only when the box
// matches the rectangle's aspect: kitty 0.49 letterboxes a mismatch,
// older versions stretch. The caller computes the box.
//
// A placement with the same id and pid replaces the previous one.
func Place(id, pid uint32, col, row, cols, rows int, sx, sy, sw, sh int, z int) string {
	var b strings.Builder
	b.Grow(96)
	b.WriteString("\x1b7\x1b[")
	b.WriteString(strconv.Itoa(max(row, 0) + 1))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(max(col, 0) + 1))
	b.WriteByte('H')
	b.WriteString(apcStart)
	b.WriteString("a=p,i=")
	b.WriteString(strconv.FormatUint(uint64(id), 10))
	b.WriteString(",p=")
	b.WriteString(strconv.FormatUint(uint64(pid), 10))
	for _, kv := range [...]struct {
		k string
		v int
	}{{",c=", cols}, {",r=", rows}, {",x=", sx}, {",y=", sy}, {",w=", sw}, {",h=", sh}} {
		b.WriteString(kv.k)
		b.WriteString(strconv.Itoa(max(kv.v, 0)))
	}
	b.WriteString(",z=")
	b.WriteString(strconv.Itoa(z))
	b.WriteString(",C=1,q=2")
	b.WriteString(apcEnd)
	b.WriteString("\x1b8")
	return b.String()
}

// DeletePlacement deletes placement pid of image id, keeping the image data (a=d,d=i,i=,p=,q=2).
//
// The image can be placed again without a new transmission.
func DeletePlacement(id, pid uint32) string {
	return apcStart + "a=d,d=i,i=" + strconv.FormatUint(uint64(id), 10) +
		",p=" + strconv.FormatUint(uint64(pid), 10) + ",q=2" + apcEnd
}
