// Package humanize words numbers for people: counts, sizes, times and
// frequencies, as Mugig shows them.
package humanize

import (
	"fmt"
	"strings"
	"time"
)

// TotalTime formats how long a list of songs plays for: "48 min",
// "9 h 12 min", "7.4 days".
func TotalTime(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%.1f days", d.Hours()/24)
	case d >= time.Hour:
		return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d min", int(d.Round(time.Minute).Minutes()))
}

// FileSize formats a size in bytes as Finder does, in powers of 1000:
// "912 KB", "38.2 MB", "1.24 GB".
func FileSize(n int64) string {
	units := []string{"bytes", "KB", "MB", "GB", "TB"}
	v, u := float64(n), 0
	for v >= 999.5 && u < len(units)-1 {
		v /= 1000
		u++
	}
	switch {
	case u == 0:
		return fmt.Sprintf("%d bytes", n)
	case u == 1 || v >= 100:
		return fmt.Sprintf("%.0f %s", v, units[u])
	case v >= 10:
		return fmt.Sprintf("%.1f %s", v, units[u])
	}
	return fmt.Sprintf("%.2f %s", v, units[u])
}

// Plural gives a count and its noun, e.g. "1 song", "1,204 songs".
func Plural(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return Thousands(n) + " " + noun
}

// Thousands formats n with comma separators.
func Thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Clock renders d as m:ss, or h:mm:ss for long tracks.
func Clock(d time.Duration) string {
	s := int(d / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// KHz formats a frequency, e.g. "44.1 kHz", "48 kHz".
func KHz(hz float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", hz/1000), ".0") + " kHz"
}
