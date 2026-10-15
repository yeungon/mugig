package humanize

import (
	"testing"
	"time"
)

func TestFileSizeWording(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 bytes", 999: "999 bytes", 1000: "1 KB", 912_400: "912 KB", 999_600: "1.00 MB",
		38_213_456: "38.2 MB", 9_217: "9 KB", 1_240_000_000: "1.24 GB", 412_000_000_000: "412 GB",
	} {
		if got := FileSize(n); got != want {
			t.Errorf("FileSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestClock(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                                     "0:00",
		59*time.Second + 999*time.Millisecond: "0:59",
		65 * time.Second:                      "1:05",
		3725 * time.Second:                    "1:02:05",
	} {
		if got := Clock(d); got != want {
			t.Errorf("Clock(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		if got := Thousands(n); got != want {
			t.Errorf("Thousands(%d) = %q, want %q", n, got, want)
		}
	}
	if got := Plural(1, "song"); got != "1 song" {
		t.Errorf("Plural(1) = %q", got)
	}
	if got := Plural(1204, "song"); got != "1,204 songs" {
		t.Errorf("Plural(1204) = %q", got)
	}
}

func TestTotalTime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		40 * time.Second:             "1 min",
		48 * time.Minute:             "48 min",
		9*time.Hour + 12*time.Minute: "9 h 12 min",
		178 * time.Hour:              "7.4 days",
	} {
		if got := TotalTime(d); got != want {
			t.Errorf("TotalTime(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestKHz(t *testing.T) {
	for hz, want := range map[float64]string{44100: "44.1 kHz", 48000: "48 kHz", 192000: "192 kHz"} {
		if got := KHz(hz); got != want {
			t.Errorf("KHz(%v) = %q, want %q", hz, got, want)
		}
	}
}
