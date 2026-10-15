package main

import (
	"math"
	"testing"

	"github.com/gopxl/beep/v2"
)

// toneThrough passes a second of a sine at hz and amplitude amp through
// the equalizer at rate, and returns the output's RMS and peak after it
// has settled.
func toneThrough(s EQSettings, rate int, hz, amp float64) (rms, peak float64) {
	pos := 0
	src := beep.StreamerFunc(func(out [][2]float64) (int, bool) {
		for i := range out {
			v := amp * math.Sin(2*math.Pi*hz*float64(pos)/float64(rate))
			out[i] = [2]float64{v, v}
			pos++
		}
		return len(out), true
	})
	e := &equalizer{s: src}
	e.set(s, rate)
	buf := make([][2]float64, rate)
	e.Stream(buf)
	var sum float64
	for _, v := range buf[rate/2:] {
		sum += v[0] * v[0]
		peak = max(peak, math.Abs(v[0]))
	}
	return math.Sqrt(sum / float64(rate-rate/2)), peak
}

// The sound changes by what the filters' response says, less the headroom
// taken off so the largest boost can't clip.
func TestEQMatchesItsResponse(t *testing.T) {
	s := EQSettings{On: true, Bands: DefaultEQBands()}
	for i, g := range []float64{6, 3, -4, 0, 2, -6, 0, 4, -3, 5} {
		s.Bands[i].Gain = g
	}
	for _, rate := range []int{44100, 96000} {
		f := NewEQFilters(s, float64(rate))
		headroom := 20 * math.Log10(f.gain)
		if want := -f.Peak(); math.Abs(headroom-want) > 1e-9 {
			t.Errorf("headroom %.2f dB, want %.2f", headroom, want)
		}
		for _, hz := range []float64{40, 125, 440, 1000, 3000, 8000, 15000} {
			rms, _ := toneThrough(s, rate, hz, 0.5)
			got := dB(rms / (0.5 / math.Sqrt2))
			if want := f.ResponseDB(hz) + headroom; math.Abs(got-want) > 0.1 {
				t.Errorf("%d Hz rate, %g Hz: %.2f dB, want %.2f", rate, hz, got, want)
			}
		}
	}
}

// Each kind of band does what it says: a peak lifts its own frequency by
// its gain and leaves the rest; a shelf lifts all below (or above) it.
func TestEQBandShapes(t *testing.T) {
	const rate = 48000
	resp := func(band int, gain, hz float64) float64 {
		s := EQSettings{On: true, Bands: DefaultEQBands()}
		s.Bands[band].Gain = gain
		return NewEQFilters(s, rate).ResponseDB(hz)
	}
	if got := resp(5, 6, 1000); math.Abs(got-6) > 0.01 {
		t.Errorf("peak at its frequency: %.2f dB", got)
	}
	if got := resp(5, 6, 100); math.Abs(got) > 0.2 {
		t.Errorf("peak far below: %.2f dB", got)
	}
	if got := resp(0, -8, 10); math.Abs(got+8) > 0.3 {
		t.Errorf("low shelf far below: %.2f dB", got)
	}
	if got := resp(0, -8, 1000); math.Abs(got) > 0.1 {
		t.Errorf("low shelf far above: %.2f dB", got)
	}
	if got := resp(9, 5, 22000); math.Abs(got-5) > 0.5 {
		t.Errorf("high shelf above: %.2f dB", got)
	}
	if got := resp(9, 5, 500); math.Abs(got) > 0.1 {
		t.Errorf("high shelf far below: %.2f dB", got)
	}
}

// A full-scale tone at the most boosted frequency still doesn't clip.
func TestEQDoesntClip(t *testing.T) {
	s := EQSettings{On: true, Bands: DefaultEQBands()}
	s.Bands[1].Gain = EQMaxGain
	if _, peak := toneThrough(s, 44100, 62, 1); peak > 1.001 {
		t.Errorf("peak %.3f", peak)
	}
}

// Off, or flat, the samples pass through untouched.
func TestEQOffIsBitExact(t *testing.T) {
	boosted := EQSettings{Bands: DefaultEQBands()}
	boosted.Bands[3].Gain = 6
	for _, s := range []EQSettings{boosted, {On: true, Bands: DefaultEQBands()}} {
		in := [][2]float64{{0.25, -0.5}, {0.125, 0}, {-1, 1}}
		got := make([][2]float64, len(in))
		e := &equalizer{s: beep.StreamerFunc(func(o [][2]float64) (int, bool) { return copy(o, in), true })}
		e.set(s, 44100)
		e.Stream(got)
		for i := range in {
			if got[i] != in[i] {
				t.Fatalf("on %v: sample %d is %v, want %v", s.On, i, got[i], in[i])
			}
		}
	}
}

// The band at 16 kHz still works at 32 kHz, where it is past the top of
// the range: it moves down to just below it.
func TestEQBandAboveTheRange(t *testing.T) {
	s := EQSettings{On: true, Bands: DefaultEQBands()}
	s.Bands[9].Gain = 6
	f := NewEQFilters(s, 32000)
	if r := f.ResponseDB(15000); math.IsNaN(r) || r < 3 {
		t.Errorf("response %.2f", r)
	}
}

// The player applies the setting chosen before or during a song, at the
// song's rate.
func TestPlayerEQ(t *testing.T) {
	p := testPlayer(t)
	s := EQSettings{On: true, Bands: DefaultEQBands()}
	s.Bands[2].Gain = 3
	p.SetEQ(s)
	p.Play(longFLAC(t, 96000, 24, 2), func(error) {})
	if p.eq.filters == nil || p.eq.filters.rate != 96000 {
		t.Fatal("the equalizer isn't on")
	}
	s.On = false
	p.SetEQ(s)
	if p.eq.filters != nil {
		t.Error("not turned off")
	}
}

// Ten bands on two channels at 192 kHz, a second of sound at a time.
func BenchmarkEQ192k(b *testing.B) {
	s := EQSettings{On: true, Bands: DefaultEQBands()}
	for i := range s.Bands {
		s.Bands[i].Gain = 3
	}
	buf := make([][2]float64, 192000)
	e := &equalizer{s: beep.StreamerFunc(func(o [][2]float64) (int, bool) { return len(o), true })}
	e.set(s, 192000)
	for b.Loop() {
		e.Stream(buf)
	}
}
