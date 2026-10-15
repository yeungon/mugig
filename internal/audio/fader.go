package audio

// The volume, gentle on the ears. Turning the volume up glides there
// rather than jumping, so a slip of the slider or a held key can't blast
// the music, and playing or resuming fades in from near silence over a
// second, rather than starting at full level. Pausing fades out over a
// quarter of a second rather than cutting off. Turning the volume down is
// immediate: that is the safe way. At full volume, once there, the
// samples pass through untouched, so playback stays bit-perfect.

import (
	"math"

	"github.com/gopxl/beep/v2"
)

const (
	riseDBPerSecond = 40   // how fast the volume rises
	fadeFloor       = 0.01 // -40 dB: where a fade in starts, a second below full
	fadeOutTime     = 0.25 // seconds a fade out takes to fall 40 dB, to fadeFloor
)

// fader applies a gain, rising to it gradually. Stream runs on the audio
// thread; the rest is called with the output locked.
type fader struct {
	s      beep.Streamer
	target float64 // the gain wanted
	gain   float64 // the gain now
	rise   float64 // the gain's multiple per sample while rising

	// A fade out scales the sound by out, falling from 1 by fall each
	// sample; when it is quiet enough, faded is called and the rest is
	// silent. faded is nil when not fading out.
	out   float64
	fall  float64
	faded func()
}

// setRate sets how fast the gain rises, and a fade out falls, at the
// sample rate of the audio.
func (f *fader) setRate(rate int) {
	f.rise = math.Pow(10, riseDBPerSecond/20/float64(max(rate, 1)))
	f.fall = math.Pow(fadeFloor, 1/(fadeOutTime*float64(max(rate, 1))))
	f.out = 1
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

// fadeOut fades the sound to silence, then calls done, on the audio
// thread with the output locked.
func (f *fader) fadeOut(done func()) {
	f.faded = done
}

// fadingOut reports whether a fade out is under way.
func (f *fader) fadingOut() bool { return f.faded != nil }

// cancelFadeOut stops a fade out, the sound rising again from where it
// had fallen to.
func (f *fader) cancelFadeOut() {
	f.gain *= f.out
	f.out, f.faded = 1, nil
}

func (f *fader) Stream(samples [][2]float64) (int, bool) {
	n, ok := f.s.Stream(samples)
	if f.faded != nil {
		f.streamFadingOut(samples[:n])
		return n, ok
	}
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

// streamFadingOut applies the gain and the falling fade out, ending it
// once the sound is down to fadeFloor: the rest is silent, and the next
// start fades in from silence.
func (f *fader) streamFadingOut(samples [][2]float64) {
	for i := range samples {
		if f.faded == nil {
			samples[i] = [2]float64{}
			continue
		}
		f.out *= f.fall
		g := f.gain * f.out
		samples[i][0] *= g
		samples[i][1] *= g
		if f.out <= fadeFloor {
			done := f.faded
			f.out, f.faded, f.gain = 1, nil, 0
			done()
		}
	}
}

func (f *fader) Err() error { return f.s.Err() }
