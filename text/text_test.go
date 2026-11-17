package text

import (
	"image/color"
	"strings"
	"testing"
	"time"
)

func TestFit(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
	}{
		{"abc", 5, "abc  "},
		{"abcdef", 4, "abc…"},
		{"日本語", 4, "日… "},
		{"\x1b[1mab\x1b[m", 3, "\x1b[1mab\x1b[m "},
		{"x", 0, ""},
	} {
		if got := Fit(c.in, c.w); got != c.want {
			t.Errorf("Fit(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
		if c.w > 0 && Width(Fit(c.in, c.w)) != c.w {
			t.Errorf("Fit(%q, %d) width %d", c.in, c.w, Width(Fit(c.in, c.w)))
		}
	}
	if FitLeft("ab", 4) != "  ab" || Center("ab", 5) != " ab  " || Truncate("abc", 3) != "abc" || Cut("abcd", 2) != "ab" {
		t.Error("FitLeft/Center/Truncate/Cut")
	}
}

func TestRowAndBlock(t *testing.T) {
	if got := Row("left side", "R", 8); got != "left s… R" && Width(got) != 8 {
		t.Errorf("Row %q", got)
	}
	if got := Row("l", "right", 10); got != "l    right" {
		t.Errorf("Row %q", got)
	}
	if got := Row("l", "toolongright", 5); Width(got) != 5 {
		t.Errorf("Row overflow %q", got)
	}
	b := Block([]string{"a", "bcdef"}, 3, 3)
	if strings.Join(b, "|") != "a  |bc…|   " {
		t.Errorf("Block %q", b)
	}
	if got := Columns(2, "|", []string{"a", "b"}, []string{"c"}); strings.Join(got, ",") != "a|c,b|" {
		t.Errorf("Columns %q", got)
	}
}

func TestWrap(t *testing.T) {
	if got := Wrap("one two three", 7); strings.Join(got, "|") != "one two|three" {
		t.Errorf("Wrap %q", got)
	}
	got := WrapN("a b c\n\n\n\nd e f g h i j", 5, 3)
	if strings.Join(got, "|") != "a b c||d e…" {
		t.Errorf("WrapN %q", got)
	}
}

func TestOnReappliesBackground(t *testing.T) {
	got := On("a\x1b[mb", color.RGBA{1, 2, 3, 255})
	if strings.Count(got, "\x1b[48;2;1;2;3m") != 2 || On("x", nil) != "x" {
		t.Errorf("On %q", got)
	}
}

func TestClean(t *testing.T) {
	if got := Clean("a\tb\nc\x00\u009b"); got != `a b↵c\x00\x9b` {
		t.Errorf("Clean %q", got)
	}
	if OneLine("  a \n b ") != "a b" || FirstLine("a\r\nb") != "a" || CleanText("a\x01\nb") != "a\\x01\nb" {
		t.Error("OneLine/FirstLine/CleanText")
	}
	if Facts("a", "", "b") != "a · b" {
		t.Error("Facts")
	}
}

func TestBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KiB", 150 << 20: "150 MiB", -5: "0 B"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
	for n, want := range map[int64]string{0: "0B", 32 << 20: "32MiB", 500e6: "500MB", 1023: "1023B", 1024: "1KiB"} {
		if got := ExactBytes(n); got != want {
			t.Errorf("ExactBytes(%d) = %q, want %q", n, got, want)
		}
		if back, err := ParseBytes(ExactBytes(n)); err != nil || back != n {
			t.Errorf("ParseBytes(%q) = %d, %v", ExactBytes(n), back, err)
		}
	}
	for in, want := range map[string]int64{"2 GB": 2e9, "1.5g": 3 << 29, "512KiB": 512 << 10, "7": 7} {
		if got, err := ParseBytes(in); err != nil || got != want {
			t.Errorf("ParseBytes(%q) = %d, %v", in, got, err)
		}
	}
	if _, err := ParseBytes("lots"); err == nil {
		t.Error("ParseBytes(lots)")
	}
	if Rate(0) != Unknown || Rate(2048) != "2.0 KiB/s" {
		t.Error("Rate")
	}
}

func TestDurations(t *testing.T) {
	for d, want := range map[time.Duration]string{24 * time.Hour: "24h0m0s", 90 * time.Minute: "1h30m", 90 * time.Second: "1m30s", time.Minute: "1m", 500 * time.Millisecond: "500ms"} {
		if want == "24h0m0s" {
			want = "24h"
		}
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
		if back, err := ParseDuration(Duration(d)); err != nil || back != d {
			t.Errorf("ParseDuration(%q) = %v, %v", Duration(d), back, err)
		}
	}
	for in, want := range map[string]time.Duration{"2d": 48 * time.Hour, "30": 30 * time.Second, "1.5": 1500 * time.Millisecond} {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v", in, got, err)
		}
	}
	if Clock(3723*time.Second) != "1:02:03" || Clock(-time.Second) != "0:00" || Clock(1499*time.Millisecond) != "0:01" {
		t.Error("Clock")
	}
	if ClockUp(1001*time.Millisecond) != "0:02" || ClockUp(time.Second) != "0:01" {
		t.Errorf("ClockUp %q %q", ClockUp(1001*time.Millisecond), ClockUp(time.Second))
	}
	if d, ok := ParseClock("1:02:03"); !ok || d != 3723*time.Second {
		t.Error("ParseClock")
	}
	if _, ok := ParseClock("1:x"); ok {
		t.Error("ParseClock bad")
	}
	if Rounded(1234567*time.Microsecond) != "1.2s" {
		t.Errorf("Rounded %q", Rounded(1234567*time.Microsecond))
	}
}

func TestRelative(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.Local)
	for d, want := range map[time.Duration]string{10 * time.Second: "now", 5 * time.Minute: "5 m", 3 * time.Hour: "3 h", 50 * time.Hour: "2 d", 15 * 24 * time.Hour: "2 w", 100 * 24 * time.Hour: "3 mo", 800 * 24 * time.Hour: "2 y"} {
		if got := Ago(now.Add(-d), now); got != want {
			t.Errorf("Ago(%v) = %q, want %q", d, got, want)
		}
	}
	if Since(now.Add(-30*time.Hour), now) != "yesterday" || Since(now.Add(-5*time.Minute), now) != "5m ago" || Since(time.Time{}, now) != "" {
		t.Error("Since")
	}
	if Until(now.Add(90*time.Second), now) != "in 1m" || Until(now.Add(-time.Second), now) != "now" {
		t.Error("Until")
	}
}

func TestCounts(t *testing.T) {
	for n, want := range map[int64]string{0: "", 999: "999", 1234: "1.2K", 34567: "34K", 1500000: "1.5M", 12345678: "12M"} {
		if got := Compact(n); got != want {
			t.Errorf("Compact(%d) = %q, want %q", n, got, want)
		}
	}
	if Thousands(-1234567) != "-1,234,567" || Thousands(999) != "999" {
		t.Error("Thousands")
	}
	if Plural(1, "file") != "1 file" || Plural(1204, "file") != "1,204 files" || Count(2, "mouse", "mice") != "2 mice" {
		t.Error("Plural/Count")
	}
	if Percent(0.625) != "62%" || Percent(2) != "100%" {
		t.Error("Percent")
	}
}

func TestTruncateStart(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
	}{{"/a/b/config.toml", 9, "…fig.toml"}, {"short", 9, "short"}, {"abc", 0, ""}, {"日本語です", 5, "…です"}} {
		if got := TruncateStart(c.in, c.w); got != c.want || Width(got) > c.w {
			t.Errorf("TruncateStart(%q, %d) = %q", c.in, c.w, got)
		}
	}
}
