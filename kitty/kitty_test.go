package kitty

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func TestSupported(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"empty", nil, false},
		{"kitty window id", map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "tmux-256color"}, true},
		{"term xterm-kitty", map[string]string{"TERM": "xterm-kitty"}, true},
		{"term xterm-ghostty", map[string]string{"TERM": "xterm-ghostty"}, true},
		{"term wezterm", map[string]string{"TERM": "wezterm"}, true},
		{"term_program WezTerm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "WezTerm"}, true},
		{"term_program ghostty", map[string]string{"TERM_PROGRAM": "ghostty"}, true},
		{"xterm", map[string]string{"TERM": "xterm-256color"}, false},
		{"tmux without kitty", map[string]string{"TERM": "tmux-256color", "TERM_PROGRAM": "tmux"}, false},
		{"alacritty", map[string]string{"TERM": "alacritty", "TERM_PROGRAM": "Alacritty"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Supported(func(k string) string { return tt.env[k] })
			if got != tt.want {
				t.Fatalf("Supported = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransmitPNGSingle(t *testing.T) {
	tests := []struct {
		id   uint32
		png  []byte
		want string
	}{
		{7, []byte("abc"), "\x1b_Ga=t,f=100,t=d,i=7,q=2;YWJj\x1b\\"},
		{4294967295, []byte{0x89, 'P', 'N', 'G'}, "\x1b_Ga=t,f=100,t=d,i=4294967295,q=2;iVBORw==\x1b\\"},
		{1, nil, ""},
	}
	for _, tt := range tests {
		if got := TransmitPNG(tt.id, tt.png); got != tt.want {
			t.Errorf("TransmitPNG(%d, %q) = %q, want %q", tt.id, tt.png, got, tt.want)
		}
	}
}

type apcCmd struct{ keys, payload string }

// parseAPC splits s into graphics commands; fails on any byte outside them.
func parseAPC(t *testing.T, s string) []apcCmd {
	t.Helper()
	var out []apcCmd
	for s != "" {
		if !strings.HasPrefix(s, "\x1b_G") {
			t.Fatalf("stray bytes before APC: %q", s[:min(len(s), 16)])
		}
		end := strings.Index(s, "\x1b\\")
		if end < 0 {
			t.Fatal("unterminated APC")
		}
		body := s[3:end]
		keys, payload, _ := strings.Cut(body, ";")
		out = append(out, apcCmd{keys, payload})
		s = s[end+2:]
	}
	return out
}

func TestTransmitPNGChunks(t *testing.T) {
	const first = "a=t,f=100,t=d,i=9,q=2"
	tests := []struct {
		rawLen int
		keys   []string
		sizes  []int
	}{
		{3069, []string{first}, []int{4092}},
		{3072, []string{first}, []int{4096}},
		{3073, []string{first + ",m=1", "m=0"}, []int{4096, 4}},
		{3075, []string{first + ",m=1", "m=0"}, []int{4096, 4}},
		{6144, []string{first + ",m=1", "m=0"}, []int{4096, 4096}},
		{6145, []string{first + ",m=1", "m=1", "m=0"}, []int{4096, 4096, 4}},
		{9216, []string{first + ",m=1", "m=1", "m=0"}, []int{4096, 4096, 4096}},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.rawLen), func(t *testing.T) {
			png := make([]byte, tt.rawLen)
			for i := range png {
				png[i] = byte(i*7 + i>>8)
			}
			cmds := parseAPC(t, TransmitPNG(9, png))
			if len(cmds) != len(tt.keys) {
				t.Fatalf("%d commands, want %d", len(cmds), len(tt.keys))
			}
			var all strings.Builder
			for i, c := range cmds {
				if c.keys != tt.keys[i] {
					t.Errorf("cmd %d keys %q, want %q", i, c.keys, tt.keys[i])
				}
				if len(c.payload) != tt.sizes[i] {
					t.Errorf("cmd %d payload %d bytes, want %d", i, len(c.payload), tt.sizes[i])
				}
				if i < len(cmds)-1 && len(c.payload)%4 != 0 {
					t.Errorf("cmd %d payload not a multiple of 4", i)
				}
				all.WriteString(c.payload)
			}
			dec, err := base64.StdEncoding.DecodeString(all.String())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(dec, png) {
				t.Fatal("reassembled payload differs from input")
			}
		})
	}
}

func TestTransmitPNGChunkBytes(t *testing.T) {
	png := bytes.Repeat([]byte{0}, 3073)
	b64 := base64.StdEncoding.EncodeToString(png)
	want := "\x1b_Ga=t,f=100,t=d,i=5,q=2,m=1;" + b64[:4096] + "\x1b\\" +
		"\x1b_Gm=0;" + b64[4096:] + "\x1b\\"
	if got := TransmitPNG(5, png); got != want {
		t.Fatalf("got %q\nwant %q", got[len(got)-40:], want[len(want)-40:])
	}
}

func TestVirtualPlace(t *testing.T) {
	tests := []struct {
		id, pid    uint32
		cols, rows int
		want       string
	}{
		{42, 1, 10, 5, "\x1b_Ga=p,U=1,i=42,p=1,c=10,r=5,C=1,q=2\x1b\\"},
		{4294967295, 16777215, 1, 1, "\x1b_Ga=p,U=1,i=4294967295,p=16777215,c=1,r=1,C=1,q=2\x1b\\"},
		{3, 0, -2, 0, "\x1b_Ga=p,U=1,i=3,p=0,c=0,r=0,C=1,q=2\x1b\\"},
	}
	for _, tt := range tests {
		if got := VirtualPlace(tt.id, tt.pid, tt.cols, tt.rows); got != tt.want {
			t.Errorf("VirtualPlace(%d, %d, %d, %d) = %q, want %q", tt.id, tt.pid, tt.cols, tt.rows, got, tt.want)
		}
	}
}

func TestDelete(t *testing.T) {
	if got, want := Delete(42), "\x1b_Ga=d,d=I,i=42,q=2\x1b\\"; got != want {
		t.Errorf("Delete = %q, want %q", got, want)
	}
	want := "\x1b_Ga=d,d=A,q=2\x1b\\\x1b_Ga=d,d=R,x=1,y=4294967295,q=2\x1b\\"
	if got := DeleteAll(); got != want {
		t.Errorf("DeleteAll = %q, want %q", got, want)
	}
}

func TestPlaceholderRow(t *testing.T) {
	const (
		ph    = "\U0010EEEE"
		reset = "\x1b[39;59m"
	)
	tests := []struct {
		name      string
		id, pid   uint32
		row, cols int
		want      string
	}{
		{
			"kitty doc 2x2 row 0", 42, 0, 0, 2,
			"\x1b[38;2;0;0;42m\x1b[58;2;0;0;0m" + ph + "̅̅" + ph + "̅̍" + reset,
		},
		{
			"kitty doc 2x2 row 1", 42, 0, 1, 2,
			"\x1b[38;2;0;0;42m\x1b[58;2;0;0;0m" + ph + "̍̅" + ph + "̍̍" + reset,
		},
		{
			"kitty doc msb 2", 42 + 2<<24, 0, 1, 2,
			"\x1b[38;2;0;0;42m\x1b[58;2;0;0;0m" + ph + "̍̅̎" + ph + "̍̍̎" + reset,
		},
		{
			"rgb byte order", 0x01020304, 0x00ABCDEF, 3, 1,
			"\x1b[38;2;2;3;4m\x1b[58;2;171;205;239m" + ph + "̐̅̍" + reset,
		},
		{
			"pid high byte dropped", 0x00FF0080, 0x7F000102, 9, 1,
			"\x1b[38;2;255;0;128m\x1b[58;2;0;1;2m" + ph + "͊̅" + reset,
		},
		{
			"msb 255", 0xFF000001, 1, 0, 1,
			"\x1b[38;2;0;0;1m\x1b[58;2;0;0;1m" + ph + "̅̅" + string(diacritics[255]) + reset,
		},
		{"zero cols", 1, 1, 0, 0, ""},
		{"negative cols", 1, 1, 0, -3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlaceholderRow(tt.id, tt.pid, tt.row, tt.cols); got != tt.want {
				t.Fatalf("got  %+q\nwant %+q", got, tt.want)
			}
		})
	}
}

func TestPlaceholderRowColumns(t *testing.T) {
	s := PlaceholderRow(1, 1, 0, 12)
	body := strings.TrimSuffix(strings.TrimPrefix(s, "\x1b[38;2;0;0;1m\x1b[58;2;0;0;1m"), "\x1b[39;59m")
	cells := strings.Split(body, "\U0010EEEE")[1:]
	if len(cells) != 12 {
		t.Fatalf("%d cells, want 12", len(cells))
	}
	for c, cell := range cells {
		want := string([]rune{diacritics[0], diacritics[c]})
		if cell != want {
			t.Errorf("cell %d = %+q, want %+q", c, cell, want)
		}
	}
}

func TestDiacritic(t *testing.T) {
	head := []rune{0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A}
	for i, r := range head {
		if got := diacritic(i); got != r {
			t.Errorf("diacritic(%d) = %U, want %U", i, got, r)
		}
	}
	if len(diacritics) != 297 {
		t.Fatalf("table has %d entries, want 297", len(diacritics))
	}
	clamp := []struct {
		n    int
		want rune
	}{
		{-1, 0x0305},
		{-1000, 0x0305},
		{295, 0x1D243},
		{296, 0x1D244},
		{297, 0x1D244},
		{100000, 0x1D244},
	}
	for _, c := range clamp {
		if got := diacritic(c.n); got != c.want {
			t.Errorf("diacritic(%d) = %U, want %U", c.n, got, c.want)
		}
	}
}

func TestDiacriticsCombiningUnique(t *testing.T) {
	seen := map[rune]int{}
	for i, r := range diacritics {
		if !unicode.Is(unicode.Mn, r) {
			t.Errorf("index %d %U not Mn", i, r)
		}
		if j, ok := seen[r]; ok {
			t.Errorf("%U at %d and %d", r, j, i)
		}
		seen[r] = i
	}
}

// Kitty's own table, when installed.
func TestDiacriticsMatchKittyFile(t *testing.T) {
	paths, _ := filepath.Glob("/usr/share/doc/kitty/html/_downloads/*/rowcolumn-diacritics.txt")
	if len(paths) == 0 {
		t.Skip("kitty rowcolumn-diacritics.txt not installed")
	}
	f, err := os.Open(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []rune
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		code, _, _ := strings.Cut(line, ";")
		v, err := strconv.ParseUint(code, 16, 32)
		if err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		got = append(got, rune(v))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(diacritics) {
		t.Fatalf("file has %d entries, table %d", len(got), len(diacritics))
	}
	for i := range got {
		if got[i] != diacritics[i] {
			t.Errorf("index %d: file %U, table %U", i, got[i], diacritics[i])
		}
	}
}

// Bubble Tea's renderer measures with ansi.StringWidth and cuts with
// ansi.Truncate.
func TestPlaceholderRowWidth(t *testing.T) {
	ids := []uint32{1, 42, 0x01020304, 0xFFFFFFFF}
	rows := []int{0, 5, 296}
	colsList := []int{1, 2, 10, 297}
	for _, id := range ids {
		for _, row := range rows {
			for _, cols := range colsList {
				s := PlaceholderRow(id, 7, row, cols)
				if w := ansi.StringWidth(s); w != cols {
					t.Errorf("id %#x row %d cols %d: StringWidth %d", id, row, cols, w)
				}
				if tr := ansi.Truncate(s, cols, ""); tr != s {
					t.Errorf("id %#x row %d cols %d: Truncate to own width changed the row", id, row, cols)
				}
			}
		}
	}
}

func TestPlaceholderRowTruncateKeepsCells(t *testing.T) {
	s := PlaceholderRow(0x02000001, 1, 4, 8)
	tr := ansi.Truncate(s, 3, "")
	if w := ansi.StringWidth(tr); w != 3 {
		t.Fatalf("truncated width %d, want 3", w)
	}
	want := PlaceholderRow(0x02000001, 1, 4, 3)
	want = strings.TrimSuffix(want, "\x1b[39;59m")
	if !strings.HasPrefix(tr, want) {
		t.Fatalf("truncated %+q\nwant prefix %+q", tr, want)
	}
}

func TestPlaceholderCells(t *testing.T) {
	whole := PlaceholderRow(7, 1, 2, 5)
	if got := PlaceholderCells(7, 1, 2, 0, 5); got != whole {
		t.Errorf("from 0: %q, want %q", got, whole)
	}
	// cells 2..4 of the row: the same runes as the row's last three
	part := []rune(strings.TrimSuffix(PlaceholderCells(7, 1, 2, 2, 3), "\x1b[39;59m"))
	all := []rune(strings.TrimSuffix(whole, "\x1b[39;59m"))
	if n := len(part); n < 9 || string(part[n-9:]) != string(all[len(all)-9:]) {
		t.Errorf("cells from 2: %q, row %q", string(part), string(all))
	}
	if PlaceholderCells(7, 1, 2, 2, 0) != "" {
		t.Error("no cells: not empty")
	}
}
