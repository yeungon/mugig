package main

// The volume, gentle on the ears. Turning the volume up glides there
// rather than jumping, so a slip of the slider or a held key can't blast
// the music, and playing or resuming fades in from near silence over a
// second, rather than starting at full level. Turning it down is
// immediate: that is the safe way. At full volume, once there, the
// samples pass through untouched, so playback stays bit-perfect.

import (
	"math"

	"github.com/gopxl/beep/v2"
)

const (
	riseDBPerSecond = 40   // how fast the volume rises
	fadeFloor       = 0.01 // -40 dB: where a fade in starts, a second below full
)

// fader applies a gain, rising to it gradually. Stream runs on the audio
// thread; the rest is called with the output locked.
type fader struct {
	s      beep.Streamer
	target float64 // the gain wanted
	gain   float64 // the gain now
	rise   float64 // the gain's multiple per sample while rising
}

// setRate sets how fast the gain rises at the sample rate of the audio.
func (f *fader) setRate(rate int) {
	f.rise = math.Pow(10, riseDBPerSecond/20/float64(max(rate, 1)))
}

// set changes the gain wanted: at once if lower, gradually if higher.
func (f *fader) set(target float64) {
	f.target = target
	if target < f.gain {
		f.gain = target
	}
}

// fadeIn starts from near silence and rises to the gain wanted.
func (f *fader) fadeIn() {
	f.gain = 0
}

// settle goes to the gain wanted at once, as when a song follows another.
func (f *fader) settle() {
	f.gain = f.target
}

func (f *fader) Stream(samples [][2]float64) (int, bool) {
	n, ok := f.s.Stream(samples)
	if f.gain == f.target {
		if f.gain != 1 {
			for i := range samples[:n] {
				samples[i][0] *= f.gain
				samples[i][1] *= f.gain
			}
		}
		return n, ok
	}
	for i := range samples[:n] {
		if f.gain < f.target {
			f.gain = min(max(f.gain, fadeFloor*f.target)*f.rise, f.target)
		}
		samples[i][0] *= f.gain
		samples[i][1] *= f.gain
	}
	return n, ok
}

func (f *fader) Err() error { return f.s.Err() }
