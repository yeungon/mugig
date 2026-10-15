package quality

import (
	"math"
	"testing"
)

// feed runs n frames of f(t) (seconds) through a fresh meter.
func feed(rate float64, channels int, seconds float64, f func(t float64) [2]float64) levelsResult {
	l := newLevels(rate, channels)
	for i := range int(rate * seconds) {
		l.add(f(float64(i) / rate))
	}
	return l.result()
}

func sineAt(dbfs, hz float64) func(float64) [2]float64 {
	a := math.Pow(10, dbfs/20)
	return func(t float64) [2]float64 {
		v := a * math.Sin(2*math.Pi*hz*t)
		return [2]float64{v, v}
	}
}

// EBU Tech 3341, case 1: a 1 kHz sine at -23 dBFS in both channels reads
// -23.0 LUFS, at any sample rate.
func TestLoudnessMatchesEBUReference(t *testing.T) {
	for _, rate := range []float64{44100, 48000, 96000} {
		r := feed(rate, 2, 10, sineAt(-23, 1000))
		if math.Abs(r.LUFS-(-23)) > 0.1 {
			t.Errorf("%v Hz: %.2f LUFS, want -23.0", rate, r.LUFS)
		}
	}
	// The same at -33 dBFS reads -33 LUFS: the scale is in dB.
	if r := feed(48000, 2, 10, sineAt(-33, 1000)); math.Abs(r.LUFS-(-33)) > 0.1 {
		t.Errorf("-33 dBFS: %.2f LUFS", r.LUFS)
	}
}

// The K-weighting coefficients derived for 48 kHz are the standard's.
func TestKWeightingMatchesStandard(t *testing.T) {
	k := newKWeighting(48000)
	for _, c := range []struct{ got, want float64 }{
		{k.shelf.b0, 1.53512485958697}, {k.shelf.b1, -2.69169618940638}, {k.shelf.b2, 1.19839281085285},
		{k.shelf.a1, -1.69065929318241}, {k.shelf.a2, 0.73248077421585},
		{k.highpass.a1, -1.99004745483398}, {k.highpass.a2, 0.99007225036621},
	} {
		if math.Abs(c.got-c.want) > 1e-6 {
			t.Errorf("coefficient %v, want %v", c.got, c.want)
		}
	}
}

// Gating: quiet passages don't drag the loudness down. 10 s at -23 dBFS
// then 10 s of near-silence (-80) reads as the loud part alone.
func TestLoudnessGating(t *testing.T) {
	loud, quiet := sineAt(-23, 1000), sineAt(-80, 1000)
	r := feed(48000, 2, 20, func(t float64) [2]float64 {
		if t < 10 {
			return loud(t)
		}
		return quiet(t)
	})
	if math.Abs(r.LUFS-(-23)) > 0.2 {
		t.Errorf("%.2f LUFS", r.LUFS)
	}
	if r := feed(48000, 2, 5, func(float64) [2]float64 { return [2]float64{} }); !math.IsInf(r.LUFS, -1) {
		t.Errorf("silence: %v LUFS", r.LUFS)
	}
}

// A full-scale sine at a quarter of the sample rate, sampled 45 degrees
// off its peaks, has samples at -3 dB but its wave reaches 0 dB between
// them: the true peak must find it.
func TestTruePeakBetweenSamples(t *testing.T) {
	const rate = 48000.0
	r := feed(rate, 1, 2, func(t float64) [2]float64 {
		return [2]float64{math.Sin(2*math.Pi*rate/4*t + math.Pi/4)}
	})
	if math.Abs(r.SamplePeak-(-3.01)) > 0.05 {
		t.Errorf("sample peak %.2f dBFS, want -3.01", r.SamplePeak)
	}
	if math.Abs(r.TruePeak) > 0.3 {
		t.Errorf("true peak %.2f dBTP, want about 0", r.TruePeak)
	}
}

func TestDynamicRange(t *testing.T) {
	// A steady sine: the DR meter's RMS equals its peak, so DR0.
	if r := feed(44100, 2, 30, sineAt(-6, 1000)); r.DR != 0 {
		t.Errorf("steady sine: DR%d", r.DR)
	}
	// Quiet verses at -26 dB with loud choruses at -6 dB peak: the loudest
	// 20% decide, so mostly-quiet music still rates by its loud parts
	// against the peak.
	loud, quiet := sineAt(-6, 1000), sineAt(-26, 1000)
	r := feed(44100, 2, 60, func(t float64) [2]float64 {
		if int(t)%15 < 3 {
			return loud(t)
		}
		return quiet(t)
	})
	if r.DR != 0 {
		t.Errorf("loud parts are a steady sine too: DR%d", r.DR)
	}
	// Spiky sound: a -20 dB sine with a full-scale click every 3 s.
	base := sineAt(-20, 1000)
	r = feed(44100, 2, 30, func(t float64) [2]float64 {
		v := base(t)
		if math.Mod(t, 3) < 1.0/44100 {
			v = [2]float64{1, 1}
		}
		return v
	})
	if r.DR != 20 {
		t.Errorf("spiky: DR%d, want 20", r.DR)
	}
}

func TestClippingCounted(t *testing.T) {
	// A sine driven 6 dB past full scale and flattened there, as an
	// over-loud master is.
	r := feed(44100, 2, 1, func(t float64) [2]float64 {
		v := max(-1, min(1, 2*math.Sin(2*math.Pi*100*t)))
		return [2]float64{v, v}
	})
	if r.ClipRuns != 2*2*100 || r.ClipSamples < 44100 {
		t.Errorf("%d runs, %d samples", r.ClipRuns, r.ClipSamples)
	}
	// Touching full scale once isn't clipping.
	if r := feed(44100, 1, 1, sineAt(0, 100)); r.ClipRuns != 0 {
		t.Errorf("clean full-scale sine: %d runs", r.ClipRuns)
	}
}
