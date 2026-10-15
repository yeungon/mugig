package quality

import (
	"math"
	"math/cmplx"
	"math/rand/v2"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"path/filepath"
	"testing"
)

// gappy synthesises full-band noise the way a lossy encoder leaves it:
// short frames (overlap-added, as MP3's are) in which random bands above
// 8 kHz are dropped, the choice changing every few frames. Nothing is cut
// off, so only the holes test can tell.
func gappy(n int, rate float64, dropShare float64) []float64 {
	const frame, hop, band = 1024, 512, 8 // band: bins, about 340 Hz at 44.1 kHz
	rng := rand.New(rand.NewPCG(5, 6))
	out := make([]float64, n+frame)
	win := make([]float64, frame)
	for i := range win {
		win[i] = math.Sin(math.Pi * (float64(i) + 0.5) / frame)
	}
	var dropped map[int]bool
	x := make([]complex128, frame)
	for f, start := 0, 0; start < n; f, start = f+1, start+hop {
		if f%16 == 0 { // encoders' choices persist for a while
			dropped = map[int]bool{}
			for b := int(8000 / rate * frame / band); b*band < frame/2; b++ {
				dropped[b] = rng.Float64() < dropShare
			}
		}
		clear(x)
		for k := 1; k < frame/2; k++ {
			if dropped[k/band] {
				continue
			}
			x[k] = cmplx.Rect(audiotest.Pink(float64(k)*rate/frame), rng.Float64()*2*math.Pi)
			x[frame-k] = cmplx.Conj(x[k])
		}
		audio.FFT(x) // inverse, up to scale and order, for a real signal
		for i := range frame {
			out[start+i] += real(x[i]) * win[i]
		}
	}
	out = out[:n]
	peak := 0.0
	for _, v := range out {
		peak = max(peak, math.Abs(v))
	}
	for i := range out {
		out[i] *= 0.5 / peak
	}
	return out
}

func TestHolesDetectLossySource(t *testing.T) {
	const rate = 44100
	path := filepath.Join(t.TempDir(), "gappy.flac")
	s := gappy(8*rate, rate, 0.3)
	audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(s, 16), audiotest.Quantize(s, 16))
	info, rep := checkFile(t, path)
	a := Assess(info, &rep)
	if rep.Spectrum.Cutoff != 0 {
		t.Errorf("cut-off %v: this test is about holes alone", rep.Spectrum.Cutoff)
	}
	if rep.Spectrum.Holes < strongHoles || a.Level != Suspect ||
		!hasCheck(a, CheckWarn, "gaps of the kind MP3 and AAC encoders leave: very likely") {
		t.Errorf("holes %.4f, assessed %+v", rep.Spectrum.Holes, a)
	}
}

// Genuine sound, whether bright, dark or hi-res, stays near the share noise
// alone produces.
func TestGenuineAudioHasNoHoles(t *testing.T) {
	for _, c := range []struct {
		name       string
		rate, bits int
		gain       func(float64) float64
	}{
		{"pink 16-bit", 44100, 16, audiotest.Pink},
		{"dark", 44100, 16, func(f float64) float64 { return audiotest.Pink(f) * math.Pow(10, -f/1000*1.5/20) }},
		{"hi-res", 96000, 24, audiotest.Pink},
	} {
		path := filepath.Join(t.TempDir(), "genuine.flac")
		audiotest.WriteFLAC(t, path, c.rate, c.bits, audiotest.Quantize(audiotest.Noise(6*c.rate, float64(c.rate), c.gain), c.bits))
		info, rep := checkFile(t, path)
		if rep.Spectrum.Holes >= holesThreshold || Assess(info, &rep).Level == Suspect {
			t.Errorf("%s: holes %.4f", c.name, rep.Spectrum.Holes)
		}
	}
}

// A cut-off's silence isn't counted as holes, or every MP3 with a cut-off
// would also be reported as having gaps.
func TestCutoffSilenceIsNotHoles(t *testing.T) {
	const rate = 44100
	path := filepath.Join(t.TempDir(), "cut.flac")
	audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(audiotest.Noise(6*rate, rate, audiotest.Lowpass(audiotest.Pink, 16000, 300)), 16))
	_, rep := checkFile(t, path)
	if rep.Spectrum.Holes >= holesThreshold {
		t.Errorf("holes %.4f", rep.Spectrum.Holes)
	}
}
