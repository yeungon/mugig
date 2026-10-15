package main

// The visualizer animates the audio that is playing, either as a spectrum
// (loudness per frequency band) or as a waveform (an oscilloscope trace).
// Each frame it reads the latest samples from the player's tap, analyses
// them, and draws into a canvas.Raster so it scales to any window size.

import (
	"image"
	"image/color"
	"math"
	"math/cmplx"
	"time"

	"fyne.io/fyne/v2/canvas"
)

type visMode int

// cdRate is the analyser's sample rate until a track says otherwise.
const cdRate = 44100

const (
	visSpectrum visMode = iota
	visWaveform
)

var visModeNames = []string{"Spectrum", "Waveform"}

const (
	frameRate = 30

	fftSize   = 2048    // samples per analysis, about 46 ms at 44.1 kHz
	numBands  = 40      // on the full screen
	miniBands = 24      // in the player bar, where there is less room
	minFreq   = 50.0    // Hz, left edge of the first band
	maxFreq   = 16000.0 // Hz, right edge of the last band
	floorDB   = -60.0   // this quiet or quieter shows as an empty bar
	// Music has far less energy in the treble than the bass, so without a
	// tilt the right-hand bars would barely move. Hardware analyzers apply
	// the same kind of slope, pivoting at 1 kHz.
	tiltDBPerOctave = 3.0

	fallPerFrame     = 0.035 // how fast bars drop (fraction of full height)
	peakFallPerFrame = 0.008 // peak markers drop more slowly
	waveSize         = 1024  // samples shown across the waveform, about 23 ms
	waveFade         = 0.8   // per frame once playback stops
)

// Colours from the app icon: violet at the bottom/centre, pink at the top.
var (
	colorLow  = color.NRGBA{0x6D, 0x28, 0xD9, 0xFF}
	colorHigh = color.NRGBA{0xDB, 0x27, 0x77, 0xFF}
	colorPeak = color.NRGBA{0xF9, 0xA8, 0xD4, 0xFF}
)

type visualizer struct {
	raster *canvas.Raster
	mode   visMode
	source func(dst []float64) int // fills dst with the latest samples and returns their rate; 0 when silent
	anRate int                     // the sample rate an was made for

	an      *analyzer
	samples []float64 // latest fftSize samples
	target  []float64 // band levels from this frame's analysis
	levels  []float64 // band levels on screen, smoothed
	peaks   []float64
	wave    []float64 // samples on screen
	idle    bool      // nothing left to animate, so frames skip redrawing
	img     *image.RGBA
	tick    time.Duration // between frames; 0 means frames are only drawn by calling frame
	stop    chan struct{}
}

func newVisualizer(source func(dst []float64) int, bands int) *visualizer {
	v := &visualizer{
		source:  source,
		an:      newAnalyzer(cdRate, bands),
		anRate:  cdRate,
		samples: make([]float64, fftSize),
		target:  make([]float64, bands),
		levels:  make([]float64, bands),
		peaks:   make([]float64, bands),
		wave:    make([]float64, waveSize),
		tick:    time.Second / frameRate,
	}
	v.raster = canvas.NewRaster(v.draw)
	return v
}

// start animates until there is nothing left to show (or stopAnimating is
// called), so an idle visualizer costs nothing. Called on the UI thread.
func (v *visualizer) start() {
	if v.stop != nil || v.tick == 0 {
		return
	}
	v.stop = make(chan struct{})
	go func(stop <-chan struct{}) {
		t := time.NewTicker(v.tick)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				onUI(v.frame)
			}
		}
	}(v.stop)
}

func (v *visualizer) stopAnimating() {
	if v.stop != nil {
		close(v.stop)
		v.stop = nil
	}
}

// frame reads new audio, updates what is shown and redraws. Called on the UI
// thread.
func (v *visualizer) frame() {
	rate := v.source(v.samples)
	playing := rate > 0
	if playing && rate != v.anRate { // each track plays at its own rate
		v.an, v.anRate = newAnalyzer(float64(rate), len(v.target)), rate
	}
	if playing {
		v.an.bands(v.samples, v.target)
		start := trigger(v.samples, len(v.samples)-waveSize)
		copy(v.wave, v.samples[start:start+waveSize])
	} else {
		clear(v.target)
		for i := range v.wave {
			v.wave[i] *= waveFade
		}
	}
	smooth(v.levels, v.peaks, v.target)

	// Once everything has settled to zero, redraw one last time and then
	// skip redrawing. If nothing is playing, stop the timer too, until start
	// is called again. A playing track can be silent for a while (an intro,
	// a gap between movements), so it keeps the timer running.
	quiet := allBelow(v.peaks, 0.001) && allBelow(v.wave, 0.001)
	if quiet && v.idle {
		if !playing {
			v.stopAnimating()
		}
		return
	}
	v.idle = quiet
	v.raster.Refresh()
}

func (v *visualizer) setMode(m visMode) {
	v.mode = m
	v.idle = false
	v.raster.Refresh()
}

// smooth moves the displayed levels toward target like a meter: straight up
// on a louder sound, gradually down after it. Peak markers fall slower.
func smooth(levels, peaks, target []float64) {
	for i, t := range target {
		if t > levels[i] {
			levels[i] = t
		} else {
			levels[i] = max(t, levels[i]-fallPerFrame)
		}
		if levels[i] > peaks[i] {
			peaks[i] = levels[i]
		} else {
			peaks[i] = max(levels[i], peaks[i]-peakFallPerFrame)
		}
	}
}

// trigger finds where to start the waveform, as an oscilloscope does: at
// the first rising zero crossing, so a steady tone stands still on screen
// instead of jittering. Without one it uses limit, the latest window.
func trigger(s []float64, limit int) int {
	for i := 1; i <= limit; i++ {
		if s[i-1] < 0 && s[i] >= 0 {
			return i
		}
	}
	return limit
}

func allBelow(s []float64, limit float64) bool {
	for _, x := range s {
		if math.Abs(x) >= limit {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Analysis
// ---------------------------------------------------------------------------

// analyzer turns fftSize samples into loudness levels for a set of bands
// spaced evenly in pitch (logarithmically in frequency), as hearing is.
type analyzer struct {
	window []float64 // Hann window, which keeps one tone from smearing across bands
	buf    []complex128
	edges  []int     // first FFT bin of each band, plus one past the last
	tilt   []float64 // dB added to each band (see tiltDBPerOctave)
}

func newAnalyzer(rate float64, bands int) *analyzer {
	a := &analyzer{
		window: make([]float64, fftSize),
		buf:    make([]complex128, fftSize),
		edges:  make([]int, bands+1),
		tilt:   make([]float64, bands),
	}
	for i := range a.window {
		a.window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftSize-1))
	}
	binHz := rate / fftSize
	for i := range a.edges {
		f := minFreq * math.Pow(maxFreq/minFreq, float64(i)/float64(bands))
		a.edges[i] = int(math.Round(f / binHz))
		if i > 0 && a.edges[i] <= a.edges[i-1] {
			a.edges[i] = a.edges[i-1] + 1 // low bands are narrower than a bin
		}
	}
	for b := range a.tilt {
		lo, hi := a.bandFreq(b, rate)
		a.tilt[b] = tiltDBPerOctave * math.Log2(math.Sqrt(lo*hi)/1000)
	}
	return a
}

// bandFreq returns the frequency range of band i, for tests and labels.
func (a *analyzer) bandFreq(i int, rate float64) (lo, hi float64) {
	binHz := rate / fftSize
	return float64(a.edges[i]) * binHz, float64(a.edges[i+1]) * binHz
}

// bands writes each band's loudness to out, from 0 (at or below floorDB) to
// 1 (a full-scale tone near 1 kHz), after the tilt.
func (a *analyzer) bands(samples, out []float64) {
	for i, s := range samples {
		a.buf[i] = complex(s*a.window[i], 0)
	}
	FFT(a.buf)
	for b := range out {
		peak := 0.0
		for k := a.edges[b]; k < a.edges[b+1] && k < fftSize/2; k++ {
			peak = max(peak, cmplx.Abs(a.buf[k]))
		}
		// 4/N turns a Hann-windowed bin back into the tone's amplitude.
		db := 20*math.Log10(peak*4/fftSize+1e-12) + a.tilt[b]
		out[b] = min(max((db-floorDB)/-floorDB, 0), 1)
	}
}

// ---------------------------------------------------------------------------
// Drawing
// ---------------------------------------------------------------------------

// draw renders the current frame at w×h pixels. The background is left
// transparent so the theme shows through.
func (v *visualizer) draw(w, h int) image.Image {
	if v.img == nil || v.img.Rect.Dx() != w || v.img.Rect.Dy() != h {
		v.img = image.NewRGBA(image.Rect(0, 0, w, h))
	} else {
		clear(v.img.Pix)
	}
	if w > 0 && h > 0 {
		if v.mode == visWaveform {
			drawWave(v.img, v.wave)
		} else {
			drawBars(v.img, v.levels, v.peaks)
		}
	}
	return v.img
}

// drawBars draws one bar per band, shaded from colorLow to colorHigh by
// height, with a thin marker at each band's recent peak.
func drawBars(img *image.RGBA, levels, peaks []float64) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	slot := float64(w) / float64(len(levels))
	barW := max(1, int(slot*0.7))
	capH := max(2, h/150)
	for i, level := range levels {
		x0 := int(float64(i)*slot + (slot-float64(barW))/2)
		top := h - int(level*float64(h))
		for y := top; y < h; y++ {
			fillRow(img, x0, x0+barW, y, blend(colorLow, colorHigh, 1-float64(y)/float64(h)))
		}
		if peaks[i] > 0.01 {
			py := h - int(peaks[i]*float64(h)) - capH
			for y := max(py, 0); y < min(py+capH, h); y++ {
				fillRow(img, x0, x0+barW, y, colorPeak)
			}
		}
	}
}

// drawWave draws the samples as an oscilloscope trace: a bright line, with
// a soft fill between it and the centre line. Each pixel column spans the
// lowest to highest sample that falls in it, joined to its neighbour so
// steep edges stay unbroken.
func drawWave(img *image.RGBA, wave []float64) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	mid := float64(h) / 2
	thick := max(2, h/120)
	line := blend(colorHigh, colorPeak, 0.35)
	fillRow(img, 0, w, h/2, color.NRGBA{colorLow.R, colorLow.G, colorLow.B, 0x80}) // centre line

	prevTop, prevBot := h/2, h/2
	for x := range w {
		from := x * len(wave) / w
		to := min(max((x+1)*len(wave)/w, from+1), len(wave))
		lo, hi := wave[from], wave[from]
		for _, s := range wave[from:to] {
			lo, hi = min(lo, s), max(hi, s)
		}
		top, bot := int(mid-hi*mid), int(mid-lo*mid)
		joinTop, joinBot := min(top, prevBot), max(bot, prevTop)
		prevTop, prevBot = top, bot

		for y := max(min(top, h/2), 0); y <= min(max(bot, h/2), h-1); y++ {
			dist := math.Abs(float64(y)-mid) / mid
			c := blend(colorLow, colorHigh, dist)
			c.A = 0x50
			img.SetRGBA(x, y, toRGBA(c))
		}
		for y := max(joinTop-thick/2, 0); y <= min(joinBot+thick/2, h-1); y++ {
			img.SetRGBA(x, y, toRGBA(line))
		}
	}
}

func fillRow(img *image.RGBA, x0, x1, y int, c color.NRGBA) {
	rgba := toRGBA(c)
	x0, x1 = max(x0, 0), min(x1, img.Rect.Dx())
	off := img.PixOffset(x0, y)
	for x := x0; x < x1; x++ {
		img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3] = rgba.R, rgba.G, rgba.B, rgba.A
		off += 4
	}
}

// blend mixes a and b, t from 0 (all a) to 1 (all b).
func blend(a, b color.NRGBA, t float64) color.NRGBA {
	t = min(max(t, 0), 1)
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), mix(a.A, b.A)}
}

// toRGBA converts to the premultiplied colour image.RGBA stores.
func toRGBA(c color.NRGBA) color.RGBA {
	return color.RGBAModel.Convert(c).(color.RGBA)
}
