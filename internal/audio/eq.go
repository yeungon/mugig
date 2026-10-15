package audio

// The equalizer: ten bands, each with its own frequency, gain and width
// (Q). The lowest is a low shelf, which lifts or cuts everything below
// its frequency, the highest a high shelf, and the eight between are
// peaks. The filters are the standard ones from Robert Bristow-Johnson's
// Audio EQ Cookbook. Boosting would push loud music past full scale and
// clip, so the whole is turned down by as much as the largest boost.
// Flat, or off, the samples pass through untouched.

import (
	"math"
	"math/cmplx"

	"github.com/gopxl/beep/v2"
)

const (
	EQBands   = 10
	EQMaxGain = 12 // dB, either way
)

// EQBand is one band's setting.
type EQBand struct {
	Freq float64 // Hz
	Gain float64 // dB
	Q    float64
}

// EQSettings is the whole equalizer's setting.
type EQSettings struct {
	On    bool
	Bands [EQBands]EQBand
}

// DefaultEQBands are octave bands from 31 Hz to 16 kHz, flat. Peaks an
// octave wide have a Q of 1.41; shelves are at their usual 0.71.
func DefaultEQBands() [EQBands]EQBand {
	var b [EQBands]EQBand
	for i, f := range []float64{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000} {
		b[i] = EQBand{Freq: f, Q: 1.41}
	}
	b[0].Q, b[EQBands-1].Q = 0.71, 0.71
	return b
}

// Flat reports whether the setting changes nothing.
func (s EQSettings) Flat() bool {
	for _, b := range s.Bands {
		if b.Gain != 0 {
			return false
		}
	}
	return true
}

// Active reports whether the equalizer changes the sound.
func (s EQSettings) Active() bool { return s.On && !s.Flat() }

// eqFilter is one band's filter, a biquad: its coefficients, normalized
// so a0 is 1, and its state for each channel.
type eqFilter struct {
	b0, b1, b2, a1, a2 float64
	z1, z2             [2]float64 // transposed direct form II
}

// design sets the filter for band i of n at rate.
func (f *eqFilter) design(b EQBand, i, n int, rate float64) {
	freq := min(max(b.Freq, 10), 0.45*rate) // below the top of the range
	q := max(b.Q, 0.1)
	A := math.Pow(10, b.Gain/40)
	w := 2 * math.Pi * freq / rate
	cos, alpha := math.Cos(w), math.Sin(w)/(2*q)
	sq := 2 * math.Sqrt(A) * alpha
	var b0, b1, b2, a0, a1, a2 float64
	switch i {
	case 0: // low shelf
		b0 = A * ((A + 1) - (A-1)*cos + sq)
		b1 = 2 * A * ((A - 1) - (A+1)*cos)
		b2 = A * ((A + 1) - (A-1)*cos - sq)
		a0 = (A + 1) + (A-1)*cos + sq
		a1 = -2 * ((A - 1) + (A+1)*cos)
		a2 = (A + 1) + (A-1)*cos - sq
	case n - 1: // high shelf
		b0 = A * ((A + 1) + (A-1)*cos + sq)
		b1 = -2 * A * ((A - 1) + (A+1)*cos)
		b2 = A * ((A + 1) + (A-1)*cos - sq)
		a0 = (A + 1) - (A-1)*cos + sq
		a1 = 2 * ((A - 1) - (A+1)*cos)
		a2 = (A + 1) - (A-1)*cos - sq
	default: // peak
		b0, b1, b2 = 1+alpha*A, -2*cos, 1-alpha*A
		a0, a1, a2 = 1+alpha/A, -2*cos, 1-alpha/A
	}
	f.b0, f.b1, f.b2, f.a1, f.a2 = b0/a0, b1/a0, b2/a0, a1/a0, a2/a0
}

// response is the filter's gain at freq, as a multiple.
func (f *eqFilter) response(freq, rate float64) float64 {
	z := cmplx.Exp(complex(0, -2*math.Pi*freq/rate)) // z⁻¹
	num := complex(f.b0, 0) + complex(f.b1, 0)*z + complex(f.b2, 0)*z*z
	den := 1 + complex(f.a1, 0)*z + complex(f.a2, 0)*z*z
	return cmplx.Abs(num / den)
}

// EQFilters are the ten filters for a setting at a rate, and the gain
// that keeps their largest boost from clipping.
type EQFilters struct {
	f    [EQBands]eqFilter
	gain float64
	rate float64
}

func NewEQFilters(s EQSettings, rate float64) *EQFilters {
	e := &EQFilters{rate: rate}
	for i, b := range s.Bands {
		e.f[i].design(b, i, EQBands, rate)
	}
	e.gain = math.Pow(10, -max(e.Peak(), 0)/20)
	return e
}

// ResponseDB is the filters' combined gain at freq, in dB, not counting
// the gain that stops clipping.
func (e *EQFilters) ResponseDB(freq float64) float64 {
	g := 1.0
	for i := range e.f {
		g *= e.f[i].response(freq, e.rate)
	}
	return 20 * math.Log10(g)
}

// Peak is the largest gain from 20 Hz to 20 kHz (or the top of the
// range), in dB.
func (e *EQFilters) Peak() float64 {
	top := min(20000, 0.49*e.rate)
	peak := math.Inf(-1)
	for i := range 400 {
		f := 20 * math.Pow(top/20, float64(i)/399)
		peak = max(peak, e.ResponseDB(f))
	}
	return peak
}

// equalizer filters stereo audio. Stream is called on the audio thread;
// set with the output locked.
type equalizer struct {
	s       beep.Streamer
	setting EQSettings
	filters *EQFilters // nil while it changes nothing
}

// DesignEQ makes the filters for a setting at rate, or nil if it changes
// nothing.
func DesignEQ(s EQSettings, rate int) *EQFilters {
	if !s.Active() || rate <= 0 {
		return nil
	}
	return NewEQFilters(s, float64(rate))
}

// set applies a setting at the sample rate of the audio.
func (e *equalizer) set(s EQSettings, rate int) { e.use(s, DesignEQ(s, rate)) }

// use applies a setting, with its filters from DesignEQ. They take over
// the old filters' state, so moving a band while music plays doesn't
// click.
func (e *equalizer) use(s EQSettings, f *EQFilters) {
	e.setting = s
	if f != nil && e.filters != nil && e.filters.rate == f.rate {
		for i := range f.f {
			f.f[i].z1, f.f[i].z2 = e.filters.f[i].z1, e.filters.f[i].z2
		}
	}
	e.filters = f
}

func (e *equalizer) Stream(samples [][2]float64) (int, bool) {
	n, ok := e.s.Stream(samples)
	if e.filters == nil {
		return n, ok
	}
	fs := &e.filters.f
	g := e.filters.gain
	for i := range samples[:n] {
		for ch := range 2 {
			x := samples[i][ch] * g
			for k := range fs {
				f := &fs[k]
				y := f.b0*x + f.z1[ch]
				f.z1[ch] = f.b1*x - f.a1*y + f.z2[ch]
				f.z2[ch] = f.b2*x - f.a2*y
				x = y
			}
			samples[i][ch] = x
		}
	}
	return n, ok
}

func (e *equalizer) Err() error { return e.s.Err() }
