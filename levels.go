package main

// Loudness and peaks, measured in the same pass that checks a file's
// quality: integrated loudness (LUFS), true peak, the DR dynamic range
// figure, and clipping. The idea of measuring these alongside the quality
// check comes from AudioAuditor (github.com/Angel2mp3/AudioAuditor, Apache
// 2.0); the code follows the published standards below.

import (
	"math"
	"slices"
)

// levelsResult is what was measured. Its fields are exported to be saved.
type levelsResult struct {
	Measured    bool
	LUFS        float64 // integrated loudness, ITU-R BS.1770-4; -Inf for silence
	TruePeak    float64 // dBTP, from 4x oversampling
	SamplePeak  float64 // dBFS
	DR          int     // dynamic range in DR units (the DR meter's), 0 if too short
	ClipRuns    int     // places where the waveform is flattened at full scale
	ClipSamples int64   // samples in those places
}

const (
	gateBlock    = 0.4   // s: BS.1770 loudness blocks…
	gateStep     = 0.1   // s: …taken every 100 ms
	absoluteGate = -70   // LUFS
	relativeGate = -10   // LU below the loudness of the blocks above the absolute gate
	drBlock      = 3.0   // s: the DR meter's blocks
	clipLevel    = 0.999 // clipped sound sits flat at this close to full scale…
	clipRun      = 3     // …for at least this many samples in a row
	tpTaps       = 16    // true peak: taps per phase of the interpolating filter
	tpPhases     = 4     // 4x oversampling, as BS.1770 asks
)

// levels measures loudness and peaks of 1 or 2 channels, a frame at a time.
type levels struct {
	channels int
	kw       [2]kWeighting

	// Loudness: K-weighted energy per 100 ms step, summed over channels.
	stepLen  int
	stepFill int
	stepSum  float64
	steps    []float64

	// True peak and sample peak.
	fir      [tpPhases][tpTaps]float64
	hist     [2][tpTaps]float64 // last samples per channel, newest last
	truePeak float64
	samplePk float64
	frames   int64

	// DR: per channel and 3 s block, the sum of squares and the peak.
	drLen    int
	drFill   int
	drSq     [2]float64
	drPeak   [2]float64
	drBlocks [2][]drStat

	// Clipping: current run of equal full-scale samples per channel.
	prev        [2]float64
	run         [2]int
	clipRuns    int
	clipSamples int64
}

type drStat struct{ rms, peak float64 }

func newLevels(rate float64, channels int) *levels {
	l := &levels{
		channels: min(max(channels, 1), 2),
		stepLen:  max(1, int(rate*gateStep)),
		drLen:    max(1, int(rate*drBlock)),
	}
	for c := range l.kw {
		l.kw[c] = newKWeighting(rate)
	}
	// Windowed-sinc interpolation filter: phase p gives the value p/4 of
	// the way from one sample to the next.
	for p := range tpPhases {
		sum := 0.0
		for t := range tpTaps {
			x := float64(t-tpTaps/2+1) - float64(p)/tpPhases                                       // distance from the point
			w := 0.42 + 0.5*math.Cos(math.Pi*x/(tpTaps/2)) + 0.08*math.Cos(2*math.Pi*x/(tpTaps/2)) // Blackman
			v := sinc(x) * w
			l.fir[p][t] = v
			sum += v
		}
		for t := range tpTaps {
			l.fir[p][t] /= sum // unity gain at low frequencies
		}
	}
	return l
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

// add measures one frame: a sample per channel, in -1 to 1. For a mono file
// only s[0] is used.
func (l *levels) add(s [2]float64) {
	l.frames++
	energy := 0.0
	for c := range l.channels {
		v := s[c]
		y := l.kw[c].filter(v)
		energy += y * y
		l.peaks(c, v)
		l.drSq[c] += v * v
		l.drPeak[c] = max(l.drPeak[c], math.Abs(v))
		// A clean wave only touches full scale in passing; a clipped one
		// stays there, repeating the same value.
		switch {
		case math.Abs(v) >= clipLevel && v == l.prev[c] && l.run[c] > 0:
			l.run[c]++
		case math.Abs(v) >= clipLevel && v == l.prev[c]:
			l.run[c] = 2
		default:
			l.endRun(c)
		}
		l.prev[c] = v
	}
	l.stepSum += energy
	if l.stepFill++; l.stepFill == l.stepLen {
		l.steps = append(l.steps, l.stepSum)
		l.stepSum, l.stepFill = 0, 0
	}
	if l.drFill++; l.drFill == l.drLen {
		for c := range l.channels {
			// The DR meter's RMS is sqrt(2) times the usual, so a sine's
			// equals its peak.
			l.drBlocks[c] = append(l.drBlocks[c], drStat{math.Sqrt(2 * l.drSq[c] / float64(l.drLen)), l.drPeak[c]})
			l.drSq[c], l.drPeak[c] = 0, 0
		}
		l.drFill = 0
	}
}

func (l *levels) endRun(c int) {
	if l.run[c] >= clipRun {
		l.clipRuns++
		l.clipSamples += int64(l.run[c])
	}
	l.run[c] = 0
}

// peaks updates the sample peak and, between loud samples, the peak
// between samples, which can be higher. Interpolating only near the
// loudest so far keeps this cheap.
func (l *levels) peaks(c int, v float64) {
	h := &l.hist[c]
	copy(h[:], h[1:])
	h[tpTaps-1] = v
	l.samplePk = max(l.samplePk, math.Abs(v))
	l.truePeak = max(l.truePeak, math.Abs(v))
	// The point between h[mid] and h[mid+1] has enough samples either side.
	const mid = tpTaps/2 - 1
	if max(math.Abs(h[mid]), math.Abs(h[mid+1])) < l.truePeak*0.5 {
		return // too quiet to beat the peak so far
	}
	for p := 1; p < tpPhases; p++ {
		y := 0.0
		for t, coef := range l.fir[p] {
			y += coef * h[t]
		}
		l.truePeak = max(l.truePeak, math.Abs(y))
	}
}

func (l *levels) result() levelsResult {
	for c := range l.channels {
		l.endRun(c)
	}
	r := levelsResult{
		Measured:    l.frames > 0,
		LUFS:        l.loudness(),
		TruePeak:    db(l.truePeak),
		SamplePeak:  db(l.samplePk),
		ClipRuns:    l.clipRuns,
		ClipSamples: l.clipSamples,
	}
	r.DR = l.dynamicRange()
	return r
}

func db(amplitude float64) float64 { return 20 * math.Log10(amplitude) }

// loudness is BS.1770-4's gated integrated loudness: 400 ms blocks every
// 100 ms; blocks under -70 LUFS are ignored, then blocks 10 LU under the
// loudness of the rest.
func (l *levels) loudness() float64 {
	var blocks []float64 // mean K-weighted energy of each 400 ms block
	per := int(gateBlock / gateStep)
	for i := 0; i+per <= len(l.steps); i++ {
		sum := 0.0
		for _, s := range l.steps[i : i+per] {
			sum += s
		}
		blocks = append(blocks, sum/float64(per*l.stepLen))
	}
	lufs := func(e float64) float64 { return -0.691 + 10*math.Log10(e) }
	gated := func(gate float64) (float64, int) {
		sum, n := 0.0, 0
		for _, e := range blocks {
			if lufs(e) > gate {
				sum += e
				n++
			}
		}
		return sum, n
	}
	sum, n := gated(absoluteGate)
	if n == 0 {
		return math.Inf(-1)
	}
	sum, n = gated(lufs(sum/float64(n)) + relativeGate)
	if n == 0 {
		return math.Inf(-1)
	}
	return lufs(sum / float64(n))
}

// dynamicRange follows the DR meter: per channel, the RMS of the loudest
// 20% of 3 s blocks against the second-highest block peak, so one stray
// sample can't set it; then the channels' average, rounded.
func (l *levels) dynamicRange() int {
	total, n := 0.0, 0
	for c := range l.channels {
		b := l.drBlocks[c]
		if len(b) < 2 {
			continue
		}
		peaks := make([]float64, len(b))
		for i, s := range b {
			peaks[i] = s.peak
		}
		slices.Sort(peaks)
		second := peaks[len(peaks)-2]
		slices.SortFunc(b, func(x, y drStat) int { return cmpFloat(y.rms, x.rms) })
		top := max(1, len(b)/5)
		sq := 0.0
		for _, s := range b[:top] {
			sq += s.rms * s.rms
		}
		rms := math.Sqrt(sq / float64(top))
		if rms <= 0 || second <= 0 {
			continue
		}
		total += db(second / rms)
		n++
	}
	if n == 0 {
		return 0
	}
	return int(math.Round(total / float64(n)))
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// kWeighting is BS.1770's K-weighting: a high shelf modelling the head,
// then a high-pass. The coefficients are derived for any sample rate from
// the filters' analogue parameters, as libebur128 does; at 48 kHz they
// match the standard's table.
type kWeighting struct {
	shelf, highpass biquad
}

func newKWeighting(rate float64) kWeighting {
	var k kWeighting
	{
		f0, gain, q := 1681.974450955533, 3.999843853973347, 0.7071752369554196
		K := math.Tan(math.Pi * f0 / rate)
		vh := math.Pow(10, gain/20)
		vb := math.Pow(vh, 0.4996667741545416)
		a0 := 1 + K/q + K*K
		k.shelf = biquad{
			b0: (vh + vb*K/q + K*K) / a0,
			b1: 2 * (K*K - vh) / a0,
			b2: (vh - vb*K/q + K*K) / a0,
			a1: 2 * (K*K - 1) / a0,
			a2: (1 - K/q + K*K) / a0,
		}
	}
	{
		f0, q := 38.13547087602444, 0.5003270373238773
		K := math.Tan(math.Pi * f0 / rate)
		a0 := 1 + K/q + K*K
		k.highpass = biquad{b0: 1, b1: -2, b2: 1, a1: 2 * (K*K - 1) / a0, a2: (1 - K/q + K*K) / a0}
	}
	return k
}

func (k *kWeighting) filter(x float64) float64 {
	return k.highpass.filter(k.shelf.filter(x))
}

// biquad is a second-order filter (direct form I).
type biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

func (f *biquad) filter(x float64) float64 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, x
	f.y2, f.y1 = f.y1, y
	return y
}
