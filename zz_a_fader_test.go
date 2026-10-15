package main

import (
	"math"
	"testing"

	"github.com/gopxl/beep/v2"
)

// gainAfter runs f over seconds of a constant 1 and returns the gain on
// the last sample.
func gainAfter(f *fader, rate int, seconds float64) float64 {
	buf := make([][2]float64, int(seconds*float64(rate)))
	f.s = beep.StreamerFunc(func(s [][2]float64) (int, bool) {
		for i := range s {
			s[i] = [2]float64{1, 1}
		}
		return len(s), true
	})
	f.Stream(buf)
	return buf[len(buf)-1][0]
}

// Fading in rises from 40 dB down to full in a second, at an even pace in
// decibels; raising the volume glides there at the same pace, and
// lowering it is immediate.
func TestFader(t *testing.T) {
	const rate = 48000
	f := &fader{}
	f.setRate(rate)
	f.set(1)
	f.fadeIn()
	if g := dB(gainAfter(f, rate, 0.5)); math.Abs(g+20) > 0.2 {
		t.Errorf("half a second into a fade in: %.1f dB, want -20", g)
	}
	if g := gainAfter(f, rate, 0.51); g != 1 {
		t.Errorf("a second in: %v, want exactly 1", g)
	}

	f.set(0.125) // down 18 dB: at once
	if g := gainAfter(f, rate, 0.001); g != 0.125 {
		t.Errorf("turned down: %v", g)
	}
	f.set(1) // up 18 dB: over 0.45 s
	if g := dB(gainAfter(f, rate, 0.2)); math.Abs(g-(-18.06+8)) > 0.2 {
		t.Errorf("0.2 s after turning up: %.1f dB", g)
	}
	if g := gainAfter(f, rate, 0.3); g != 1 {
		t.Errorf("0.5 s after turning up: %v", g)
	}
	f.set(0)
	if g := gainAfter(f, rate, 0.001); g != 0 {
		t.Errorf("muted: %v", g)
	}
}

// The player fades in when playing starts or resumes, not when one song
// follows another, and not at all with Fade In off.
func TestPlayerFadesIn(t *testing.T) {
	p := testPlayer(t)
	gain := func() float64 { // the audio thread may be changing it
		p.out.mu.Lock()
		defer p.out.mu.Unlock()
		return p.vol.gain
	}
	song := longFLAC(t, 44100, 16, 3)
	p.SetFadeIn(true)
	p.Play(song, func(error) {})
	if g := gain(); g > 0.5 {
		t.Errorf("starting from stopped should fade in: gain %v", g)
	}
	p.Play(song, func(error) {}) // another song while playing
	if g := gain(); g != 1 {
		t.Errorf("a song following another starts at %v, want full", g)
	}
	p.TogglePause()
	p.TogglePause()
	if g := gain(); g > 0.5 {
		t.Errorf("resuming should fade in: gain %v", g)
	}
	p.Stop()
	p.SetFadeIn(false)
	p.Play(song, func(error) {})
	if g := gain(); g != 1 {
		t.Error("with Fade In off, playing starts at full")
	}
}
