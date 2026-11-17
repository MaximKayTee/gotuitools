// Package kitty encodes kitty terminal graphics protocol escape sequences for
// images shown through unicode placeholders.
//
// Placeholders: an image is transmitted once and given a virtual placement of
// cols x rows cells. Each text cell holding U+10EEEE plus row and column
// diacritics then shows that cell of the image, the image id carried in the
// foreground colour and the placement id in the underline colour. The image
// moves with the text, so placeholder rows lay out like any other string.
//
// Ids travel as 24-bit SGR colours (38;2 and 58;2): the terminal must pass
// truecolor through unchanged.
package kitty

import (
	"encoding/base64"
	"strconv"
	"strings"
)

const (
	placeholder = '\U0010EEEE'
	chunkSize   = 4096 // base64 chars per transmission chunk
	apcStart    = "\x1b_G"
	apcEnd      = "\x1b\\"
)

// Supported reports kitty graphics support: KITTY_WINDOW_ID set, or TERM/TERM_PROGRAM naming kitty, ghostty or wezterm. No terminal query.
func Supported(getenv func(string) string) bool {
	if getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	for _, k := range []string{"TERM", "TERM_PROGRAM"} {
		v := strings.ToLower(getenv(k))
		for _, name := range []string{"kitty", "ghostty", "wezterm"} {
			if strings.Contains(v, name) {
				return true
			}
		}
	}
	return false
}

// TransmitPNG encodes PNG bytes as image id: f=100, direct transmission (t=d), base64 in chunks of 4096 with m=1/m=0, q=2 (no responses). Image is transmitted only, not displayed.
//
// Payload up to 4096 base64 chars: one command, no m key. Longer: first
// command carries all keys and m=1, later ones only m=1, the last m=0.
// Empty png: "".
func TransmitPNG(id uint32, png []byte) string {
	if len(png) == 0 {
		return ""
	}
	data := base64.StdEncoding.EncodeToString(png)
	keys := "a=t,f=100,t=d,i=" + strconv.FormatUint(uint64(id), 10) + ",q=2"
	var b strings.Builder
	b.Grow(len(data) + len(keys) + (len(data)/chunkSize+1)*(len(apcStart)+len(apcEnd)+5))
	if len(data) <= chunkSize {
		writeAPC(&b, keys, data)
		return b.String()
	}
	for first := true; len(data) > 0; first = false {
		n := min(chunkSize, len(data))
		chunk := data[:n]
		data = data[n:]
		m := "m=1"
		if len(data) == 0 {
			m = "m=0"
		}
		if first {
			m = keys + "," + m
		}
		writeAPC(&b, m, chunk)
	}
	return b.String()
}

// VirtualPlace creates a virtual placement (U=1) of image id with placement id pid scaled to cols x rows cells, q=2. Placeholders then show it.
//
// Also C=1 (cursor stays). cols, rows below 0 are sent as 0. pid above
// 1<<24-1 cannot be matched by PlaceholderRow.
func VirtualPlace(id, pid uint32, cols, rows int) string {
	var b strings.Builder
	b.WriteString(apcStart)
	b.WriteString("a=p,U=1,i=")
	b.WriteString(strconv.FormatUint(uint64(id), 10))
	b.WriteString(",p=")
	b.WriteString(strconv.FormatUint(uint64(pid), 10))
	b.WriteString(",c=")
	b.WriteString(strconv.Itoa(max(cols, 0)))
	b.WriteString(",r=")
	b.WriteString(strconv.Itoa(max(rows, 0)))
	b.WriteString(",C=1,q=2")
	b.WriteString(apcEnd)
	return b.String()
}

// PlaceholderRow is one text row of cols placeholder cells for image id / placement pid: each cell U+10EEEE followed by the row diacritic and the column diacritic (and the third diacritic for id bits 24..31 when nonzero), with the SGR foreground colour set to the low 24 bits of id as RGB (38;2;r;g;b) and the underline colour (58;2;r;g;b) set to pid. Returned string begins with the SGR and ends with a reset so it can be embedded in a lipgloss/bubbletea view. The caller lays out successive rows itself.
//
// Reset is ESC[39;59m (default foreground and underline colour), so an
// enclosing style's background survives; it shows through transparent
// pixels. pid uses its low 24 bits. row and col above 296 clamp to the last
// diacritic, below 0 to the first. cols <= 0: "". Width per cell under
// x/ansi StringWidth: 1.
func PlaceholderRow(id, pid uint32, row, cols int) string {
	return PlaceholderCells(id, pid, row, 0, cols)
}

// PlaceholderCells is PlaceholderRow from image column col on: cols
// cells of row, the first showing column col. For a row with other
// text over part of the image.
func PlaceholderCells(id, pid uint32, row, col, cols int) string {
	if cols <= 0 {
		return ""
	}
	rd := diacritic(row)
	msb := id >> 24
	var b strings.Builder
	b.Grow(48 + cols*(4+2*4+4))
	writeSGRColour(&b, "38", id)
	writeSGRColour(&b, "58", pid)
	for c := range cols {
		b.WriteRune(placeholder)
		b.WriteRune(rd)
		b.WriteRune(diacritic(col + c))
		if msb != 0 {
			b.WriteRune(diacritics[msb])
		}
	}
	b.WriteString("\x1b[39;59m")
	return b.String()
}

// Delete frees image id and all its placements (a=d, d=I).
//
// Also q=2. d=I reaches virtual placements.
func Delete(id uint32) string {
	return apcStart + "a=d,d=I,i=" + strconv.FormatUint(uint64(id), 10) + ",q=2" + apcEnd
}

// DeleteAll frees every image (a=d, d=A).
//
// Two commands, both q=2: a=d,d=A (visible placements), then
// a=d,d=R,x=1,y=4294967295 (every image id, virtual placements included;
// kitty >= 0.33). d=A alone never touches virtual placements.
func DeleteAll() string {
	return apcStart + "a=d,d=A,q=2" + apcEnd +
		apcStart + "a=d,d=R,x=1,y=4294967295,q=2" + apcEnd
}

// diacritic returns the mark for n, clamped to the table.
func diacritic(n int) rune {
	return diacritics[min(max(n, 0), len(diacritics)-1)]
}

func writeAPC(b *strings.Builder, keys, payload string) {
	b.WriteString(apcStart)
	b.WriteString(keys)
	b.WriteByte(';')
	b.WriteString(payload)
	b.WriteString(apcEnd)
}

// writeSGRColour writes ESC[<code>;2;r;g;bm from the low 24 bits of v.
func writeSGRColour(b *strings.Builder, code string, v uint32) {
	var buf [32]byte
	s := append(buf[:0], "\x1b["...)
	s = append(s, code...)
	s = append(s, ";2;"...)
	s = strconv.AppendUint(s, uint64(v>>16&0xff), 10)
	s = append(s, ';')
	s = strconv.AppendUint(s, uint64(v>>8&0xff), 10)
	s = append(s, ';')
	s = strconv.AppendUint(s, uint64(v&0xff), 10)
	s = append(s, 'm')
	b.Write(s)
}
