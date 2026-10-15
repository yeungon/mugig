package audio

import (
	"math"
	"testing"

	"github.com/gopxl/beep/v2"
)

// runCrossfeed passes one second of a tone at hz through crossfeed at
// level, the left channel at left and the right at right, and returns the
// RMS of each channel after it has settled.
func runCrossfeed(level CrossfeedLevel, hz, left, right float64) (l, r float64) {
	const rate = 44100
	in := make([][2]float64, rate)
	for i := range in {
		v := math.Sin(2 * math.Pi * hz * float64(i) / rate)
		in[i] = [2]float64{left * v, right * v}
	}
	pos := 0
	src := beep.StreamerFunc(func(s [][2]float64) (int, bool) {
		n := copy(s, in[pos:])
		pos += n
		return n, n > 0
	})
	c := &crossfeed{s: src}
	c.set(level, rate)
	out := make([][2]float64, rate)
	for n := 0; n < len(out); {
		got, _ := c.Stream(out[n:min(n+512, len(out))])
		n += got
	}
	for _, s := range out[rate/2:] {
		l += s[0] * s[0]
		r += s[1] * s[1]
	}
	k := float64(len(out) - rate/2)
	return math.Sqrt(l / k), math.Sqrt(r / k)
}

func dB(x float64) float64 { return 20 * math.Log10(x) }

// The far ear hears the bass of a channel panned hard to one side, the
// preset's number of decibels down, but hardly its treble; music in the
// middle keeps its level.
func TestCrossfeed(t *testing.T) {
	for _, tc := range []struct {
		level CrossfeedLevel
		feed  float64
	}{{CrossfeedLight, 9.5}, {CrossfeedMedium, 6}, {CrossfeedStrong, 4.5}} {
		l, r := runCrossfeed(tc.level, 60, 0.5, 0)
		if d := dB(l) - dB(r); math.Abs(d-tc.feed) > 0.5 {
			t.Errorf("%s: bass reaches the far ear %.1f dB down, want %.1f", CrossfeedNames[tc.level], d, tc.feed)
		}
		l, r = runCrossfeed(tc.level, 10000, 0.5, 0)
		if d := dB(l) - dB(r); d < 20 {
			t.Errorf("%s: treble reaches the far ear only %.1f dB down", CrossfeedNames[tc.level], d)
		}
		// bs2b keeps the bass in the middle at its level, and lets the
		// treble there drop a little, by up to 2 dB at the strongest.
		for _, c := range []struct{ hz, within float64 }{{60, 0.3}, {1000, 2}, {10000, 2}} {
			l, _ := runCrossfeed(tc.level, c.hz, 0.5, 0.5)
			if d := dB(l) - dB(0.5/math.Sqrt2); math.Abs(d) > c.within {
				t.Errorf("%s: %g Hz in the middle changes by %.1f dB", CrossfeedNames[tc.level], c.hz, d)
			}
		}
	}
}

// Off, the samples pass through untouched.
func TestCrossfeedOffIsBitExact(t *testing.T) {
	in := [][2]float64{{0.25, -0.5}, {0.125, 0}, {-1, 1}}
	got := make([][2]float64, len(in))
	c := &crossfeed{s: beep.StreamerFunc(func(s [][2]float64) (int, bool) { return copy(s, in), true })}
	c.set(CrossfeedOff, 44100)
	c.Stream(got)
	for i := range in {
		if got[i] != in[i] {
			t.Fatalf("sample %d: %v, want %v", i, got[i], in[i])
		}
	}
}

// The player applies the level chosen before or during a song, at the
// song's rate.
func TestPlayerCrossfeed(t *testing.T) {
	p := testPlayer(t)
	p.SetCrossfeed(CrossfeedStrong)
	p.Play(longFLAC(t, 96000, 24, 2), func(error) {})
	if p.cross.level != CrossfeedStrong || p.cross.rate != 96000 {
		t.Errorf("level %v at %d", p.cross.level, p.cross.rate)
	}
	p.SetCrossfeed(CrossfeedOff)
	if p.cross.level != CrossfeedOff {
		t.Error("not turned off")
	}
}
