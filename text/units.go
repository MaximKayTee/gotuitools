package text

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Unknown is the value of a figure not known yet.
const Unknown = "–"

// Bytes is n in 1024 steps, one decimal under 100: "1.4 MiB".
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(max(n, 0), 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	prec := 1
	if v >= 100 {
		prec = 0
	}
	return strconv.FormatFloat(v, 'f', prec, 64) + " " + string("KMGTP"[exp]) + "iB"
}

// Rate is bytes per second, "840 KiB/s"; Unknown at 0.
func Rate(n int64) string {
	if n <= 0 {
		return Unknown
	}
	return Bytes(n) + "/s"
}

var byteUnits = []struct {
	name string
	mult int64
}{
	{"TiB", 1 << 40}, {"TB", 1e12}, {"GiB", 1 << 30}, {"GB", 1e9},
	{"MiB", 1 << 20}, {"MB", 1e6}, {"KiB", 1 << 10}, {"kB", 1e3},
}

// ExactBytes is n exactly, in the unit with the fewest digits, binary
// first on a tie: 32MiB, 500MB, 1023B. ParseBytes reads it back.
func ExactBytes(n int64) string {
	num, unit := strconv.FormatInt(n, 10), "B"
	if n == 0 {
		return num + unit
	}
	for _, u := range byteUnits {
		if n%u.mult != 0 {
			continue
		}
		if s := strconv.FormatInt(n/u.mult, 10); len(s) < len(num) {
			num, unit = s, u.name
		}
	}
	return num + unit
}

var parseUnits = []struct {
	suffix string
	mult   int64
}{
	{"kib", 1 << 10}, {"mib", 1 << 20}, {"gib", 1 << 30}, {"tib", 1 << 40},
	{"kb", 1000}, {"mb", 1000 * 1000}, {"gb", 1000 * 1000 * 1000}, {"tb", 1000 * 1000 * 1000 * 1000},
	{"k", 1 << 10}, {"m", 1 << 20}, {"g", 1 << 30}, {"t", 1 << 40},
	{"b", 1},
}

// ParseBytes reads "1024", "512KiB", "2 GB", "1.5g".
func ParseBytes(s string) (int64, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range parseUnits {
		if n, ok := strings.CutSuffix(t, u.suffix); ok {
			t, mult = strings.TrimSpace(n), u.mult
			break
		}
	}
	if i, err := strconv.ParseInt(t, 10, 64); err == nil {
		if i != 0 && (i*mult)/mult != i {
			return 0, fmt.Errorf("%q out of range", s)
		}
		return i * mult, nil
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f*float64(mult) > math.MaxInt64 {
		return 0, fmt.Errorf("%q is not a byte size", s)
	}
	return int64(f * float64(mult)), nil
}

// Duration is d as a Go duration without zero tails: 24h, 1h30m, 90s,
// 500ms. ParseDuration reads it back.
func Duration(d time.Duration) string {
	s := d.String()
	if t, ok := strings.CutSuffix(s, "m0s"); ok {
		s = t + "m"
	}
	if t, ok := strings.CutSuffix(s, "h0m"); ok {
		s = t + "h"
	}
	return s
}

// ParseDuration reads Go durations, a "d" (24h) unit, and bare numbers
// as seconds.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	secs := func(f float64) (time.Duration, error) {
		if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > float64(math.MaxInt64/int64(time.Second)) {
			return 0, fmt.Errorf("%v seconds out of range", f)
		}
		return time.Duration(f * float64(time.Second)), nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return secs(f)
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return secs(f * 86400)
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration", s)
	}
	return d, nil
}

// Rounded is d rounded to what matters at its size: 1.234s, 12.3s, 3m4s.
func Rounded(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

// Clock is d as m:ss or h:mm:ss, rounded to the second.
func Clock(d time.Duration) string {
	s := int(max(d, 0).Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// ClockUp is Clock with seconds rounded up: a countdown.
func ClockUp(d time.Duration) string {
	return Clock((max(d, 0) + time.Second - 1) / time.Second * time.Second)
}

// ParseClock reads "36:47" or "1:02:03"; false when it is not one.
func ParseClock(clock string) (time.Duration, bool) {
	n := 0
	for _, part := range strings.Split(clock, ":") {
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 {
			return 0, false
		}
		n = n*60 + v
	}
	return time.Duration(n) * time.Second, true
}

// Ago is a short relative time: now, 5 m, 3 h, 2 d, 3 w, 4 mo, 2 y.
func Ago(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%d m", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d d", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d w", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d mo", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%d y", int(d.Hours()/24/365))
}

// Since is a relative time in words: just now, 5m ago, 3h ago,
// yesterday, then the date "Jan 2".
func Since(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	case d < 48*time.Hour:
		return "yesterday"
	}
	return t.Local().Format("Jan 2")
}

// Until is a time ahead: now, in 30s, in 5m, 15:04, then "Jan 2 15:04".
func Until(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := t.Sub(now)
	switch {
	case d < 0:
		return "now"
	case d < time.Minute:
		return "in " + strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return "in " + strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return t.Local().Format("15:04")
	}
	return t.Local().Format("Jan 2 15:04")
}

// Compact is a count as 999, 1.2K, 34K, 1.5M, 12M; "" for n <= 0.
func Compact(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 10_000:
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	case n < 1_000_000:
		return fmt.Sprintf("%dK", n/1000)
	case n < 10_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	return fmt.Sprintf("%dM", n/1_000_000)
}

// Thousands is n with comma separators: 1,234,567.
func Thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if len(s) > 3 {
		var b strings.Builder
		pre := len(s) % 3
		if pre > 0 {
			b.WriteString(s[:pre])
		}
		for i := pre; i < len(s); i += 3 {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(s[i : i+3])
		}
		s = b.String()
	}
	if neg {
		return "-" + s
	}
	return s
}

// Count is n with thousands separators and the word for its number:
// "1 file", "1,204 files".
func Count(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return Thousands(n) + " " + many
}

// Plural is Count with many = word+"s".
func Plural(n int64, word string) string { return Count(n, word, word+"s") }

// Percent is frac as "62%", clamped to 0..100.
func Percent(frac float64) string {
	return strconv.Itoa(int(min(max(frac, 0), 1)*100)) + "%"
}
