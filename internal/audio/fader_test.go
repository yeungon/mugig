package audio

import (
	"math"
	"testing"
	"time"

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
	p.SetFade(true)
	p.Play(song, func(error) {})
	if g := gain(); g > 0.5 {
		t.Errorf("starting from stopped should fade in: gain %v", g)
	}
	p.Play(song, func(error) {}) // another song while playing
	if g := gain(); g != 1 {
		t.Errorf("a song following another starts at %v, want full", g)
	}
	p.TogglePause()
	waitFor(t, "the pause's fade out", func() bool { return paused(p) })
	p.TogglePause()
	if g := gain(); g > 0.5 {
		t.Errorf("resuming should fade in: gain %v", g)
	}
	p.Stop()
	p.SetFade(false)
	p.Play(song, func(error) {})
	if g := gain(); g != 1 {
		t.Error("with Fade In off, playing starts at full")
	}
}

// paused reports whether the player's output is paused, rather than
// fading out to pause.
func paused(p *Player) bool {
	p.out.mu.Lock()
	defer p.out.mu.Unlock()
	return p.ctrl != nil && p.ctrl.Paused
}

// A fade out falls 40 dB in a quarter of a second, at an even pace in
// decibels, then says so once and leaves silence; the next start fades in.
func TestFaderFadesOut(t *testing.T) {
	const rate = 48000
	f := &fader{}
	f.setRate(rate)
	f.set(0.5)
	f.settle()
	calls := 0
	f.fadeOut(func() { calls++ })
	if g := dB(gainAfter(f, rate, 0.125) / 0.5); math.Abs(g+20) > 0.2 {
		t.Errorf("halfway through the fade out: %.1f dB, want -20", g)
	}
	if calls != 0 || !f.fadingOut() {
		t.Fatal("the fade out ended too soon")
	}
	if g := gainAfter(f, rate, 0.2); g != 0 {
		t.Errorf("after the fade out: %v, want silence", g)
	}
	if calls != 1 || f.fadingOut() || f.gain != 0 {
		t.Errorf("fade out ended %d times, still fading %v, gain %v", calls, f.fadingOut(), f.gain)
	}
	// Starting again rises from silence to the volume set.
	if g := gainAfter(f, rate, 1.01); g != 0.5 {
		t.Errorf("a second after starting again: %v, want 0.5", g)
	}
}

// Cancelling a fade out rises again from where it had fallen to, at the
// usual pace, rather than jumping back.
func TestFaderCancelFadeOut(t *testing.T) {
	const rate = 48000
	f := &fader{}
	f.setRate(rate)
	f.set(1)
	f.settle()
	f.fadeOut(func() { t.Error("a cancelled fade out shouldn't end") })
	gainAfter(f, rate, 0.125) // down 20 dB
	f.cancelFadeOut()
	if g := dB(gainAfter(f, rate, 0.25)); math.Abs(g+10) > 0.3 {
		t.Errorf("a quarter second after cancelling: %.1f dB, want -10", g)
	}
	if g := gainAfter(f, rate, 0.3); g != 1 {
		t.Errorf("then back to full: %v", g)
	}
}

// Pausing fades out before pausing, and says paused at once; pressing
// again while it fades carries on playing. With fades off it pauses at
// once.
func TestPlayerFadesOutOnPause(t *testing.T) {
	p := testPlayer(t)
	p.SetFade(true)
	if err := p.Play(longFLAC(t, 44100, 16, 4), func(error) {}); err != nil {
		t.Fatal(err)
	}
	if !p.TogglePause() {
		t.Error("TogglePause should say paused")
	}
	if paused(p) {
		t.Error("the output should fade out before pausing")
	}
	waitFor(t, "the fade out to pause", func() bool { return paused(p) })
	at := p.Position()
	time.Sleep(100 * time.Millisecond)
	if p.Position() != at {
		t.Error("the song should stand still once paused")
	}

	p.TogglePause()      // resume
	p.TogglePause()      // pause: fading out
	if p.TogglePause() { // pressed again while fading
		t.Error("pressing again while fading out should carry on playing")
	}
	time.Sleep(400 * time.Millisecond) // longer than a fade out
	if paused(p) {
		t.Error("a cancelled fade out shouldn't pause")
	}

	p.SetFade(false)
	if !p.TogglePause() || !paused(p) {
		t.Error("with fades off, pausing should be immediate")
	}
}
