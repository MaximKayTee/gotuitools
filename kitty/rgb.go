package kitty

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"strconv"
)

// rawChunk is the raw bytes of one full 4096-char base64 chunk.
const rawChunk = chunkSize / 4 * 3

// Formats, the f key.
const (
	FormatRGB  = 24
	FormatRGBA = 32
	FormatPNG  = 100
)

// TransmitData encodes image data as image id, direct transmission
// (t=d), q=2: PNG bytes (FormatPNG), or w x h raw pixels (FormatRGB,
// FormatRGBA, rows top down), or when compressed their zlib stream
// (o=z). Transmitted only, not displayed. Chunking as TransmitPNG.
//
// Keys in order: a=t,f[,s,v][,o=z],t=d,i,q=2; s and v for raw formats.
// No data, an unknown format, or a raw format with w or h <= 0: "".
func TransmitData(id uint32, format, w, h int, data []byte, compressed bool) string {
	raw := format == FormatRGB || format == FormatRGBA
	if len(data) == 0 || (!raw && format != FormatPNG) || (raw && (w <= 0 || h <= 0)) {
		return ""
	}
	keys := strconv.AppendInt([]byte("a=t,f="), int64(format), 10)
	if raw {
		keys = strconv.AppendInt(append(keys, ",s="...), int64(w), 10)
		keys = strconv.AppendInt(append(keys, ",v="...), int64(h), 10)
	}
	if compressed {
		keys = append(keys, ",o=z"...)
	}
	keys = strconv.AppendUint(append(keys, ",t=d,i="...), uint64(id), 10)
	keys = append(keys, ",q=2"...)
	return string(appendChunked(nil, keys, data))
}

// TransmitRGBA encodes w x h 32-bit RGBA pixels (rgba[:w*h*4], straight
// alpha) as image id: f=32, as TransmitRGB. w or h <= 0, or rgba
// shorter than w*h*4: "".
func TransmitRGBA(id uint32, w, h int, rgba []byte, compress bool) string {
	if w <= 0 || h <= 0 || len(rgba) < w*h*4 {
		return ""
	}
	data := rgba[:w*h*4]
	if compress {
		data = Deflate(data)
	}
	return TransmitData(id, FormatRGBA, w, h, data, compress)
}

// Deflate is b's zlib stream at BestSpeed, the o=z payload.
func Deflate(b []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(b) / 2)
	zw, _ := zlib.NewWriterLevel(&buf, zlib.BestSpeed)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}

// TransmitRGB encodes w x h 24-bit RGB pixels (rgb[:w*h*3], rows top
// down) as image id: f=24, s=w, v=h, zlib (o=z, BestSpeed) when
// compress, direct transmission (t=d), q=2. Transmitted only, not
// displayed. Chunking as TransmitPNG.
//
// Keys in order: a=t,f=24,s,v[,o=z],t=d,i,q=2. w or h <= 0, or rgb
// shorter than w*h*3: "".
func TransmitRGB(id uint32, w, h int, rgb []byte, compress bool) string {
	if w <= 0 || h <= 0 || len(rgb) < w*h*3 {
		return ""
	}
	data := rgb[:w*h*3]
	if compress {
		data = Deflate(data)
	}
	return TransmitData(id, FormatRGB, w, h, data, compress)
}

// AppendDisplayRGB appends a transmit-and-display (a=T) of a w x h
// RGB image as image id, placement pid, at 0-based cell (col, row)
// scaled to cols x rows cells: ESC 7, CUP, the chunked command, ESC 8,
// as Place. data is rgb[:w*h*3] or, when compressed, its zlib stream
// (o=z). The cursor position kitty uses is the one at the last chunk.
//
// Keys in order: a=T,f=24,s,v[,o=z],t=d,i,p,c,r,C=1,q=2. Negative col,
// row, cols, rows are sent as 0. w or h <= 0, or no data: dst
// unchanged.
//
// Same id as a shown image: the new data replaces the old and its
// placements; nothing shows until the last chunk.
func AppendDisplayRGB(dst []byte, id, pid uint32, col, row, cols, rows, w, h int, data []byte, compressed bool) []byte {
	if w <= 0 || h <= 0 || len(data) == 0 {
		return dst
	}
	dst = append(dst, "\x1b7\x1b["...)
	dst = strconv.AppendInt(dst, int64(max(row, 0)+1), 10)
	dst = append(dst, ';')
	dst = strconv.AppendInt(dst, int64(max(col, 0)+1), 10)
	dst = append(dst, 'H')
	var kb [128]byte
	keys := rgbKeys(kb[:0], 'T', w, h, compressed)
	keys = strconv.AppendUint(append(keys, ",i="...), uint64(id), 10)
	keys = strconv.AppendUint(append(keys, ",p="...), uint64(pid), 10)
	keys = strconv.AppendInt(append(keys, ",c="...), int64(max(cols, 0)), 10)
	keys = strconv.AppendInt(append(keys, ",r="...), int64(max(rows, 0)), 10)
	keys = append(keys, ",C=1,q=2"...)
	dst = appendChunked(dst, keys, data)
	return append(dst, "\x1b8"...)
}

// AppendDisplayShm appends a transmit-and-display (a=T) of a w x h
// RGBA image read from the POSIX shared memory object name (t=s), at
// the cell and scaled as AppendDisplayRGB. The object holds w*h*4
// bytes, rows top down, straight alpha; the terminal unlinks it after
// reading. Same machine as the terminal only.
//
// Keys in order: a=T,f=32,s,v,t=s,i,p,c,r,C=1,q=2; the payload is the
// name in base64. w or h <= 0, or no name: dst unchanged.
func AppendDisplayShm(dst []byte, id, pid uint32, col, row, cols, rows, w, h int, name string) []byte {
	if w <= 0 || h <= 0 || name == "" {
		return dst
	}
	dst = append(dst, "\x1b7\x1b["...)
	dst = strconv.AppendInt(dst, int64(max(row, 0)+1), 10)
	dst = append(dst, ';')
	dst = strconv.AppendInt(dst, int64(max(col, 0)+1), 10)
	dst = append(dst, 'H')
	dst = append(dst, apcStart...)
	dst = strconv.AppendInt(append(dst, "a=T,f=32,s="...), int64(w), 10)
	dst = strconv.AppendInt(append(dst, ",v="...), int64(h), 10)
	dst = strconv.AppendUint(append(dst, ",t=s,i="...), uint64(id), 10)
	dst = strconv.AppendUint(append(dst, ",p="...), uint64(pid), 10)
	dst = strconv.AppendInt(append(dst, ",c="...), int64(max(cols, 0)), 10)
	dst = strconv.AppendInt(append(dst, ",r="...), int64(max(rows, 0)), 10)
	dst = append(dst, ",C=1,q=2;"...)
	dst = base64.StdEncoding.AppendEncode(dst, []byte(name))
	dst = append(dst, apcEnd...)
	return append(dst, "\x1b8"...)
}

// rgbKeys appends a=<action>,f=24,s=w,v=h[,o=z],t=d.
func rgbKeys(b []byte, action byte, w, h int, compressed bool) []byte {
	b = append(b, 'a', '=', action)
	b = append(b, ",f=24,s="...)
	b = strconv.AppendInt(b, int64(w), 10)
	b = append(b, ",v="...)
	b = strconv.AppendInt(b, int64(h), 10)
	if compressed {
		b = append(b, ",o=z"...)
	}
	return append(b, ",t=d"...)
}

// appendChunked appends data base64 encoded as graphics commands:
// one command without m up to 4096 chars; else the first carries keys
// and m=1, later ones only m=1, the last m=0.
func appendChunked(dst, keys, data []byte) []byte {
	if len(data) <= rawChunk {
		dst = append(dst, apcStart...)
		dst = append(dst, keys...)
		dst = append(dst, ';')
		dst = base64.StdEncoding.AppendEncode(dst, data)
		return append(dst, apcEnd...)
	}
	for first := true; len(data) > 0; first = false {
		n := min(rawChunk, len(data))
		chunk := data[:n]
		data = data[n:]
		dst = append(dst, apcStart...)
		if first {
			dst = append(dst, keys...)
			dst = append(dst, ',')
		}
		if len(data) == 0 {
			dst = append(dst, "m=0;"...)
		} else {
			dst = append(dst, "m=1;"...)
		}
		dst = base64.StdEncoding.AppendEncode(dst, chunk)
		dst = append(dst, apcEnd...)
	}
	return dst
}
