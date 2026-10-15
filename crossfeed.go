package main

// Crossfeed, for headphones. On speakers each ear hears both speakers, the
// far one a little later and duller, as the head is in the way. On
// headphones each ear hears one channel only, so a guitar panned hard left
// sounds as if it were inside the left ear, which tires over an album.
// Crossfeed mixes into each ear a little of the other channel, low-passed
// as the head would, and lifts the treble of the direct channel to make
// up. This is Boris Mikhaylov's design from bs2b (Bauer stereophonic-to-
// binaural), with its three presets.

import (
	"math"

	"github.com/gopxl/beep/v2"
)

// crossfeedLevel is how much of each channel reaches the other ear.
type crossfeedLevel int

const (
	crossfeedOff crossfeedLevel = iota
	crossfeedLight
	crossfeedMedium
	crossfeedStrong
)

var crossfeedNames = []string{"Off", "Light", "Medium", "Strong"}

// crossfeedPresets are bs2b's: the low-pass cut-off of the far channel, in
// Hz, and how far below the near channel it is fed in, in dB.
var crossfeedPresets = map[crossfeedLevel]struct{ cutoff, feed float64 }{
	crossfeedLight:  {650, 9.5}, // Jan Meier's
	crossfeedMedium: {700, 6},   // Chu Moy's
	crossfeedStrong: {700, 4.5}, // bs2b's default
}

// crossfeed filters stereo audio. Off, it passes the samples through
// untouched. Stream is called on the audio thread; set with the output
// locked.
type crossfeed struct {
	s     beep.Streamer
	level crossfeedLevel
	rate  int

	a0Lo, b1Lo, a0Hi, a1Hi, b1Hi, gain float64
	lo, hi, asis                       [2]float64 // the filters' state, per channel
}

// set chooses the level, at the sample rate of the audio. The filters
// start afresh.
func (c *crossfeed) set(level crossfeedLevel, rate int) {
	c.level, c.rate = level, rate
	c.lo, c.hi, c.asis = [2]float64{}, [2]float64{}, [2]float64{}
	p, ok := crossfeedPresets[level]
	if !ok || rate <= 0 {
		c.level = crossfeedOff
		return
	}
	gLoDB := -p.feed*5/6 - 3
	gHiDB := p.feed/6 - 3
	gLo := math.Pow(10, gLoDB/20)
	gHi := 1 - math.Pow(10, gHiDB/20)
	hiCutoff := p.cutoff * math.Pow(2, (gLoDB-20*math.Log10(gHi))/12)

	x := math.Exp(-2 * math.Pi * p.cutoff / float64(rate))
	c.b1Lo, c.a0Lo = x, gLo*(1-x)
	x = math.Exp(-2 * math.Pi * hiCutoff / float64(rate))
	c.b1Hi, c.a0Hi, c.a1Hi = x, 1-gHi*(1-x), -x
	c.gain = 1 / (1 - gHi + gLo)
}

func (c *crossfeed) Stream(samples [][2]float64) (int, bool) {
	n, ok := c.s.Stream(samples)
	if c.level == crossfeedOff {
		return n, ok
	}
	for i, in := range samples[:n] {
		for ch := range 2 {
			c.lo[ch] = c.a0Lo*in[ch] + c.b1Lo*c.lo[ch]
			c.hi[ch] = c.a0Hi*in[ch] + c.a1Hi*c.asis[ch] + c.b1Hi*c.hi[ch]
		}
		c.asis = in
		samples[i] = [2]float64{(c.hi[0] + c.lo[1]) * c.gain, (c.hi[1] + c.lo[0]) * c.gain}
	}
	return n, ok
}

func (c *crossfeed) Err() error { return c.s.Err() }

const prefCrossfeed = "crossfeed"
