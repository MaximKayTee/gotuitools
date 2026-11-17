package kitty

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"io"
	"strconv"
	"strings"
	"testing"
)

func pixels(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*13 + i>>9)
	}
	return b
}

func inflate(t *testing.T, b []byte) []byte {
	t.Helper()
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTransmitRGBSingle(t *testing.T) {
	tests := []struct {
		id   uint32
		w, h int
		rgb  []byte
		want string
	}{
		{7, 1, 1, []byte{1, 2, 3}, "\x1b_Ga=t,f=24,s=1,v=1,t=d,i=7,q=2;AQID\x1b\\"},
		{4294967295, 2, 1, []byte{255, 0, 0, 0, 255, 0, 9}, "\x1b_Ga=t,f=24,s=2,v=1,t=d,i=4294967295,q=2;/wAAAP8A\x1b\\"},
		{1, 0, 1, []byte{1, 2, 3}, ""},
		{1, 1, -1, []byte{1, 2, 3}, ""},
		{1, 2, 2, make([]byte, 11), ""},
	}
	for _, tt := range tests {
		if got := TransmitRGB(tt.id, tt.w, tt.h, tt.rgb, false); got != tt.want {
			t.Errorf("TransmitRGB(%d, %d, %d) = %q, want %q", tt.id, tt.w, tt.h, got, tt.want)
		}
	}
}

func TestTransmitRGBCompressed(t *testing.T) {
	rgb := bytes.Repeat([]byte{10, 20, 30}, 4*3)
	var z bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&z, zlib.BestSpeed)
	zw.Write(rgb)
	zw.Close()
	want := "\x1b_Ga=t,f=24,s=4,v=3,o=z,t=d,i=12,q=2;" + base64.StdEncoding.EncodeToString(z.Bytes()) + "\x1b\\"
	got := TransmitRGB(12, 4, 3, rgb, true)
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	cmds := parseAPC(t, got)
	dec, _ := base64.StdEncoding.DecodeString(cmds[0].payload)
	if !bytes.Equal(inflate(t, dec), rgb) {
		t.Fatal("inflated payload differs")
	}
}

// Chunk boundaries as TransmitPNG: 3072 raw bytes per 4096-char chunk.
func TestTransmitRGBChunks(t *testing.T) {
	const keys = "a=t,f=24,s=%d,v=1,t=d,i=9,q=2"
	for _, tt := range []struct {
		px    int // pixels, one row
		cmds  []string
		sizes []int
	}{
		{1023, []string{""}, []int{4092}},
		{1024, []string{""}, []int{4096}},
		{1025, []string{",m=1", "m=0"}, []int{4096, 4}},
		{2048, []string{",m=1", "m=0"}, []int{4096, 4096}},
		{2049, []string{",m=1", "m=1", "m=0"}, []int{4096, 4096, 4}},
	} {
		t.Run(strconv.Itoa(tt.px), func(t *testing.T) {
			rgb := pixels(tt.px * 3)
			cmds := parseAPC(t, TransmitRGB(9, tt.px, 1, rgb, false))
			if len(cmds) != len(tt.cmds) {
				t.Fatalf("%d commands, want %d", len(cmds), len(tt.cmds))
			}
			var all strings.Builder
			for i, c := range cmds {
				want := tt.cmds[i]
				if i == 0 {
					want = strings.Replace(keys, "%d", strconv.Itoa(tt.px), 1) + want
				}
				if c.keys != want {
					t.Errorf("cmd %d keys %q, want %q", i, c.keys, want)
				}
				if len(c.payload) != tt.sizes[i] {
					t.Errorf("cmd %d payload %d, want %d", i, len(c.payload), tt.sizes[i])
				}
				all.WriteString(c.payload)
			}
			dec, err := base64.StdEncoding.DecodeString(all.String())
			if err != nil || !bytes.Equal(dec, rgb) {
				t.Fatalf("reassembled payload differs: %v", err)
			}
		})
	}
}

func TestTransmitRGBChunkBytes(t *testing.T) {
	rgb := make([]byte, 1025*3)
	b64 := base64.StdEncoding.EncodeToString(rgb)
	want := "\x1b_Ga=t,f=24,s=1025,v=1,t=d,i=5,q=2,m=1;" + b64[:4096] + "\x1b\\" +
		"\x1b_Gm=0;" + b64[4096:] + "\x1b\\"
	if got := TransmitRGB(5, 1025, 1, rgb, false); got != want {
		t.Fatalf("got %q\nwant %q", got[len(got)-40:], want[len(want)-40:])
	}
}

func TestAppendDisplayRGB(t *testing.T) {
	tests := []struct {
		name                 string
		id, pid              uint32
		col, row, cols, rows int
		w, h                 int
		data                 []byte
		compressed           bool
		want                 string
	}{
		{
			"origin raw", 7, 1, 0, 0, 1, 1, 1, 1, []byte{1, 2, 3}, false,
			"\x1b7\x1b[1;1H\x1b_Ga=T,f=24,s=1,v=1,t=d,i=7,p=1,c=1,r=1,C=1,q=2;AQID\x1b\\\x1b8",
		},
		{
			"offset compressed flag", 0x02000001, 1, 12, 3, 40, 12, 400, 240, []byte{0x78, 0x01}, true,
			"\x1b7\x1b[4;13H\x1b_Ga=T,f=24,s=400,v=240,o=z,t=d,i=33554433,p=1,c=40,r=12,C=1,q=2;eAE=\x1b\\\x1b8",
		},
		{
			"negatives clamp", 3, 2, -4, -1, -2, -3, 1, 1, []byte{0, 0, 0}, false,
			"\x1b7\x1b[1;1H\x1b_Ga=T,f=24,s=1,v=1,t=d,i=3,p=2,c=0,r=0,C=1,q=2;AAAA\x1b\\\x1b8",
		},
		{"no data", 3, 2, 0, 0, 1, 1, 1, 1, nil, false, "pre"},
		{"no size", 3, 2, 0, 0, 1, 1, 0, 1, []byte{1, 2, 3}, false, "pre"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pre := []byte("pre")
			got := AppendDisplayRGB(pre, tt.id, tt.pid, tt.col, tt.row, tt.cols, tt.rows, tt.w, tt.h, tt.data, tt.compressed)
			want := tt.want
			if want != "pre" {
				want = "pre" + want
			}
			if string(got) != want {
				t.Fatalf("got  %q\nwant %q", got, want)
			}
		})
	}
}

func TestAppendDisplayRGBChunks(t *testing.T) {
	rgb := pixels(2049 * 3)
	got := string(AppendDisplayRGB(nil, 33554433, 1, 9, 4, 40, 12, 2049, 1, rgb, false))
	head := "\x1b7\x1b[5;10H"
	if !strings.HasPrefix(got, head) || !strings.HasSuffix(got, "\x1b8") {
		t.Fatalf("wrap %q ... %q", got[:20], got[len(got)-10:])
	}
	cmds := parseAPC(t, got[len(head):len(got)-2])
	want := []string{"a=T,f=24,s=2049,v=1,t=d,i=33554433,p=1,c=40,r=12,C=1,q=2,m=1", "m=1", "m=0"}
	if len(cmds) != len(want) {
		t.Fatalf("%d commands", len(cmds))
	}
	var all strings.Builder
	for i, c := range cmds {
		if c.keys != want[i] {
			t.Errorf("cmd %d keys %q, want %q", i, c.keys, want[i])
		}
		all.WriteString(c.payload)
	}
	dec, _ := base64.StdEncoding.DecodeString(all.String())
	if !bytes.Equal(dec, rgb) {
		t.Fatal("payload differs")
	}
	if got != head+displayFromTransmit(rgb) {
		t.Fatal("chunks differ from TransmitRGB's")
	}
}

// displayFromTransmit is the a=T chunk run built from TransmitRGB's
// a=t output with the display keys spliced in.
func displayFromTransmit(rgb []byte) string {
	s := TransmitRGB(33554433, 2049, 1, rgb, false)
	s = strings.Replace(s, "a=t,", "a=T,", 1)
	s = strings.Replace(s, ",q=2,m=1;", ",p=1,c=40,r=12,C=1,q=2,m=1;", 1)
	return s + "\x1b8"
}

func TestTransmitData(t *testing.T) {
	for _, c := range []struct {
		format, w, h int
		data         []byte
		z            bool
		want         string
	}{
		{FormatPNG, 0, 0, []byte("png"), false, "\x1b_Ga=t,f=100,t=d,i=7,q=2;cG5n\x1b\\"},
		{FormatRGB, 1, 1, []byte{1, 2, 3}, false, "\x1b_Ga=t,f=24,s=1,v=1,t=d,i=7,q=2;AQID\x1b\\"},
		{FormatRGBA, 1, 1, []byte{1, 2, 3}, true, "\x1b_Ga=t,f=32,s=1,v=1,o=z,t=d,i=7,q=2;AQID\x1b\\"},
		{FormatRGB, 0, 1, []byte{1}, false, ""},
		{FormatRGB, 1, 1, nil, false, ""},
		{12, 1, 1, []byte{1}, false, ""},
	} {
		if got := TransmitData(7, c.format, c.w, c.h, c.data, c.z); got != c.want {
			t.Errorf("f=%d %dx%d: %q, want %q", c.format, c.w, c.h, got, c.want)
		}
	}
	// PNG through TransmitData equals TransmitPNG
	big := bytes.Repeat([]byte{9}, 10000)
	if TransmitData(5, FormatPNG, 0, 0, big, false) != TransmitPNG(5, big) {
		t.Fatal("png differs")
	}
}

func TestTransmitRGBA(t *testing.T) {
	px := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	got := TransmitRGBA(3, 2, 1, px, true)
	if !strings.HasPrefix(got, "\x1b_Ga=t,f=32,s=2,v=1,o=z,t=d,i=3,q=2;") {
		t.Fatalf("%q", got)
	}
	payload := strings.TrimSuffix(strings.SplitN(got, ";", 2)[1], "\x1b\\")
	z, _ := base64.StdEncoding.DecodeString(payload)
	r, err := zlib.NewReader(bytes.NewReader(z))
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := io.ReadAll(r); !bytes.Equal(out, px) {
		t.Fatalf("payload %v", out)
	}
	if TransmitRGBA(3, 2, 2, px, false) != "" {
		t.Fatal("short pixels")
	}
}
