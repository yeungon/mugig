package main

import (
	"image"
	"math"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func sine(freq, amp float64, n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = amp * math.Sin(2*math.Pi*freq*float64(i)/float64(testRate))
	}
	return s
}

func TestAnalyzerPutsToneInTheRightBand(t *testing.T) {
	a := newAnalyzer(float64(testRate), numBands)
	out := make([]float64, numBands)
	for _, freq := range []float64{100, 1000, 5000} {
		a.bands(sine(freq, 0.5, fftSize), out)
		peak := 0
		for i, v := range out {
			if v > out[peak] {
				peak = i
			}
		}
		lo, hi := a.bandFreq(peak, float64(testRate))
		if freq < lo-30 || freq > hi+30 { // allow for a tone near a band edge
			t.Errorf("%v Hz tone peaked in band %d (%.0f–%.0f Hz)", freq, peak, lo, hi)
		}
		// Half volume is -6 dB; the tilt then lifts treble and lowers bass.
		if want := min((-6+a.tilt[peak]-floorDB)/-floorDB, 1); math.Abs(out[peak]-want) > 0.05 {
			t.Errorf("%v Hz at half volume: level %.2f, want about %.2f", freq, out[peak], want)
		}
		for i, v := range out {
			if (i < peak-3 || i > peak+3) && v > 0.2 {
				t.Errorf("%v Hz tone leaked into band %d (level %.2f)", freq, i, v)
			}
		}
	}

	a.bands(make([]float64, fftSize), out)
	if !allBelow(out, 1e-9) {
		t.Errorf("silence gave %v", out)
	}
}

func TestTiltPivotsAt1kHz(t *testing.T) {
	a := newAnalyzer(float64(testRate), numBands)
	if b := bandFor(1000); math.Abs(a.tilt[b]) > 1 {
		t.Errorf("tilt near 1 kHz = %.1f dB, want about 0", a.tilt[b])
	}
	if a.tilt[0] >= 0 || a.tilt[numBands-1] <= 0 || !slices.IsSorted(a.tilt) {
		t.Errorf("tilt should rise from bass to treble: %v", a.tilt)
	}
}

func TestTriggerSteadiesTheWaveform(t *testing.T) {
	// Two windows of a tone taken at different moments should show the
	// same picture once triggered.
	long := sine(441, 0.5, fftSize+500)
	a, b := long[:fftSize], long[137:137+fftSize]
	ia, ib := trigger(a, fftSize-waveSize), trigger(b, fftSize-waveSize)
	for i := range waveSize {
		if math.Abs(a[ia+i]-b[ib+i]) > 1e-6 {
			t.Fatalf("triggered windows differ at %d", i)
		}
	}
	if got := trigger(make([]float64, fftSize), 100); got != 100 {
		t.Errorf("silence should use the latest window, got start %d", got)
	}
}

func TestBandsCoverTheRange(t *testing.T) {
	a := newAnalyzer(float64(testRate), numBands)
	for i := 1; i < len(a.edges); i++ {
		if a.edges[i] <= a.edges[i-1] {
			t.Fatalf("band edges not increasing: %v", a.edges)
		}
	}
	lo, _ := a.bandFreq(0, float64(testRate))
	_, hi := a.bandFreq(numBands-1, float64(testRate))
	if lo > minFreq+25 || hi < maxFreq*0.9 || hi > float64(testRate)/2 {
		t.Errorf("bands span %.0f–%.0f Hz", lo, hi)
	}
}

func TestSmoothRisesFastFallsSlowly(t *testing.T) {
	levels, peaks := []float64{0}, []float64{0}
	smooth(levels, peaks, []float64{0.8})
	if levels[0] != 0.8 || peaks[0] != 0.8 {
		t.Fatalf("rise: level %v peak %v", levels[0], peaks[0])
	}
	smooth(levels, peaks, []float64{0})
	if math.Abs(levels[0]-(0.8-fallPerFrame)) > 1e-9 || math.Abs(peaks[0]-(0.8-peakFallPerFrame)) > 1e-9 {
		t.Errorf("fall: level %v peak %v", levels[0], peaks[0])
	}
	for range 100 {
		smooth(levels, peaks, []float64{0})
	}
	if levels[0] != 0 || peaks[0] != 0 {
		t.Errorf("should settle at 0: level %v peak %v", levels[0], peaks[0])
	}
}

func TestDrawing(t *testing.T) {
	v := newVisualizer(func([]float64) int { return 0 }, numBands)
	for i := range v.levels {
		v.levels[i], v.peaks[i] = 0.5, 0.75
	}
	img := v.draw(400, 200).(*image.RGBA)
	slot := 400 / numBands
	mid := slot / 2
	if alpha(img, mid, 199) == 0 || alpha(img, mid, 120) == 0 {
		t.Error("bar should fill the bottom half")
	}
	if alpha(img, mid, 80) != 0 {
		t.Error("above the bar should be clear")
	}
	if alpha(img, mid, 200-150-1) == 0 {
		t.Error("peak marker missing")
	}

	v.setMode(visWaveform)
	copy(v.wave, sine(1000, 0.5, waveSize))
	img = v.draw(400, 200).(*image.RGBA)
	if drawn(img) < 400 {
		t.Errorf("waveform drew only %d pixels", drawn(img))
	}
	if alpha(img, 200, 5) != 0 {
		t.Error("a half-volume wave shouldn't reach the top edge")
	}
	v.draw(0, 0) // a collapsed window must not panic
}

func TestVisualizerScreen(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	menuItem(t, u, "Visualizer").Action()
	if u.songsPage.Visible() || !u.visView.Visible() || !menuItem(t, u, "Visualizer").Checked || !isToggledOn(u.visBtn) ||
		u.navNowPlaying.Importance != widget.MediumImportance || u.navSongs.Importance != widget.LowImportance {
		t.Fatal("⌘L should switch to the visualizer")
	}
	if text := u.visTitle.String(); text != "Nothing playing" {
		t.Errorf("idle header %q", text)
	}

	u.togglePlay()
	if text, detail := u.visTitle.String(), u.visDetail.Text; text != "Come Together" || detail != "Beatles · Abbey Road · 1969" {
		t.Errorf("header %q / %q", text, detail)
	}
	for range 3 {
		u.vis.frame()
	}
	if band := bandFor(1000); u.vis.levels[band] < 0.8 {
		t.Errorf("1 kHz band at %.2f while playing a 1 kHz tone", u.vis.levels[band])
	}
	if drawn(u.vis.draw(300, 150).(*image.RGBA)) == 0 {
		t.Error("nothing drawn while playing")
	}

	u.togglePlay() // pause: the display should die away, then stop redrawing
	for range 200 {
		u.vis.frame()
	}
	if !allBelow(u.vis.peaks, 1e-9) || !u.vis.idle {
		t.Error("paused visualizer should settle and go idle")
	}

	u.onTypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if !u.songsPage.Visible() || u.visView.Visible() || menuItem(t, u, "Visualizer").Checked || isToggledOn(u.visBtn) {
		t.Error("Esc should return to the track list")
	}

	u.toggleVisualizer()
	menuItem(t, u, "Find").Action()
	if u.visView.Visible() || u.win.Canvas().Focused() != u.search {
		t.Error("Find should leave the visualizer and focus search")
	}
}

func TestVisualizerModeIsRemembered(t *testing.T) {
	u, _, a := newTestUI(t)
	if u.vis.mode != visSpectrum || u.visModes.Selected != "Spectrum" {
		t.Fatal("spectrum should be the default")
	}
	menuItem(t, u, "Waveform").Action()
	if u.vis.mode != visWaveform || u.visModes.Selected != "Waveform" ||
		!menuItem(t, u, "Waveform").Checked || menuItem(t, u, "Spectrum").Checked {
		t.Error("menu should switch mode, radio and checkmarks")
	}
	u.visModes.SetSelected("Spectrum")
	if u.vis.mode != visSpectrum || !menuItem(t, u, "Spectrum").Checked {
		t.Error("radio should switch mode and checkmarks")
	}
	u.visModes.SetSelected("Waveform")

	u2 := newUI(u.win, a.Preferences(), uiDeps{player: &fakePlayer{}, cache: u.cache})
	if u2.vis.mode != visWaveform || u2.visModes.Selected != "Waveform" {
		t.Error("mode should be restored on the next launch")
	}
}

func bandFor(freq float64) int {
	a := newAnalyzer(float64(testRate), numBands)
	for i := range numBands {
		if lo, hi := a.bandFreq(i, float64(testRate)); freq >= lo && freq < hi {
			return i
		}
	}
	return -1
}

func alpha(img *image.RGBA, x, y int) uint8 { return img.RGBAAt(x, y).A }

func drawn(img *image.RGBA) int {
	n := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			n++
		}
	}
	return n
}

func TestMiniVisualizer(t *testing.T) {
	u, _, a := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	if !u.miniView.Visible() || !menuItem(t, u, "Mini Visualizer").Checked {
		t.Fatal("the strip should be on by default")
	}
	if len(u.miniVis.levels) != miniBands || u.miniVis.mode != visSpectrum {
		t.Errorf("strip: %d bands, mode %v", len(u.miniVis.levels), u.miniVis.mode)
	}

	menuItem(t, u, "Waveform").Action() // only the full screen changes style
	u.togglePlay()
	u.miniVis.frame()
	if u.miniVis.mode != visSpectrum || u.miniVis.levels[miniBandFor(1000)] < 0.8 {
		t.Error("strip should show the playing tone as a spectrum")
	}
	if drawn(u.miniVis.draw(180, 72).(*image.RGBA)) == 0 {
		t.Error("strip drew nothing while playing")
	}

	test.Tap(u.miniView.Objects[0].(*tapArea))
	if !u.visView.Visible() {
		t.Error("clicking the strip should open the full visualizer")
	}

	menuItem(t, u, "Mini Visualizer").Action()
	if u.miniView.Visible() || menuItem(t, u, "Mini Visualizer").Checked {
		t.Error("menu should hide the strip")
	}
	u2 := newUI(u.win, a.Preferences(), uiDeps{player: &fakePlayer{}, cache: u.cache})
	if u2.miniView.Visible() || menuItem(t, u2, "Mini Visualizer").Checked {
		t.Error("a hidden strip should stay hidden on the next launch")
	}
}

// A track that starts with silence must not put the visualizer to sleep:
// it has to come alive when the music starts.
func TestVisualizerSurvivesSilentIntro(t *testing.T) {
	loud := false
	v := newVisualizer(func(dst []float64) int {
		if loud {
			copy(dst, sine(1000, 0.5, len(dst)))
		} else {
			clear(dst)
		}
		return testRate // playing throughout
	}, miniBands)
	v.stop = make(chan struct{}) // as if start had been called

	for range 2 * frameRate { // two seconds of silent intro
		v.frame()
	}
	if v.stop == nil {
		t.Fatal("timer stopped during a silent intro")
	}
	loud = true
	v.frame()
	if v.levels[miniBandFor(1000)] < 0.8 {
		t.Error("bars should rise when the music starts")
	}
}

// Visualizers stop their timer once there is nothing left to show, so an
// idle app doesn't redraw 30 times a second.
func TestVisualizerStopsWhenQuiet(t *testing.T) {
	sound := true
	v := newVisualizer(func(dst []float64) int {
		if sound {
			copy(dst, sine(1000, 0.5, len(dst)))
			return testRate
		}
		return 0
	}, miniBands)
	v.stop = make(chan struct{}) // as if start had been called

	v.frame()
	sound = false
	for i := 0; v.stop != nil; i++ {
		if i > 200 {
			t.Fatal("still animating long after the sound stopped")
		}
		v.frame()
	}
	if !allBelow(v.levels, 1e-9) {
		t.Error("stopped before the bars had fallen")
	}
}

func miniBandFor(freq float64) int {
	a := newAnalyzer(float64(testRate), miniBands)
	for i := range miniBands {
		if lo, hi := a.bandFreq(i, float64(testRate)); freq >= lo && freq < hi {
			return i
		}
	}
	return -1
}
