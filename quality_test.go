package main

import (
	"context"
	"math"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// checkFile probes and verifies path.
func checkFile(t *testing.T, path string) (Info, qualityReport) {
	t.Helper()
	info, err := Probe(path)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := verify(context.Background(), path, info)
	if err != nil {
		t.Fatal(err)
	}
	return info, rep
}

func near(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

// A file's contents decide its format, not its name: an MP3 renamed .flac
// is reported as MP3, and still plays.
func TestMisnamedFile(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.mp3")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "song.flac")
	os.WriteFile(path, data, 0o644)
	info, err := Probe(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "MP3" || info.Lossless || info.Mismatch != ".flac" {
		t.Errorf("got %+v", info)
	}
	s, _, err := Decode(path)
	if err != nil {
		t.Fatalf("misnamed file doesn't play: %v", err)
	}
	s.Close()

	a := assess(info, nil)
	if a.level != qualityLossy || !strings.Contains(a.checks[0].text, ".flac") {
		t.Errorf("assessment %+v", a)
	}
}

func TestGenuineFLACPasses(t *testing.T) {
	const rate = 44100
	path := filepath.Join(t.TempDir(), "genuine.flac")
	l := audiotest.Quantize(audiotest.Noise(6*rate, rate, audiotest.Pink), 16)
	r := audiotest.Quantize(audiotest.Noise(6*rate, rate, audiotest.Pink), 16)
	audiotest.WriteFLAC(t, path, rate, 16, l, r)

	info, rep := checkFile(t, path)
	if info.SampleRate != rate || info.Bits != 16 || info.Channels != 2 || info.Duration.Seconds() != 6 || info.Kbps < 800 || info.Kbps > 1450 { // 16-bit stereo, compressed or not
		t.Errorf("info %+v", info)
	}
	if rep.Damaged != "" || rep.Checksum != checksumOK || rep.Missing != 0 || rep.UsedBits != 16 {
		t.Errorf("report %+v", rep)
	}
	if !rep.Spectrum.Analysed || rep.Spectrum.Cutoff != 0 {
		t.Errorf("spectrum %+v", rep.Spectrum)
	}
	if a := assess(info, &rep); a.level != qualityLossless {
		t.Errorf("assessed %+v", a)
	}
}

// Real recordings often fade out steeply at the top, which must not be
// mistaken for an encoder's cut-off.
func TestSteepNaturalRollOffPasses(t *testing.T) {
	const rate = 44100
	for _, slope := range []float64{6, 12, 20, 30} { // dB per kHz above 12 kHz
		gain := func(f float64) float64 {
			g := audiotest.Pink(f)
			if f > 12000 {
				g *= math.Pow(10, -slope*(f-12000)/1000/20)
			}
			return g
		}
		path := filepath.Join(t.TempDir(), "rolloff.flac")
		audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(audiotest.Noise(6*rate, rate, gain), 16))
		if _, rep := checkFile(t, path); rep.Spectrum.Cutoff != 0 {
			t.Errorf("%v dB/kHz: cut-off reported at %v Hz", slope, rep.Spectrum.Cutoff)
		}
	}
}

// Lossless files made from MP3s have nothing above the encoder's cut-off.
func TestLossySourceDetected(t *testing.T) {
	const rate = 44100
	for _, hz := range []float64{11000, 16000, 17000, 19000} {
		for _, width := range []float64{100, 300, 600} { // encoders' filters differ in steepness
			path := filepath.Join(t.TempDir(), "transcoded.flac")
			s := audiotest.Noise(6*rate, rate, audiotest.Lowpass(audiotest.Pink, hz, width))
			audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(s, 16), audiotest.Quantize(s, 16))

			info, rep := checkFile(t, path)
			if !near(rep.Spectrum.Cutoff, hz+width/2, 250) {
				t.Errorf("cut at %v Hz over %v Hz: found %v", hz, width, rep.Spectrum.Cutoff)
				continue
			}
			a := assess(info, &rep)
			if a.level != qualitySuspect || !hasCheck(a, checkWarn, "very likely converted from a lossy") {
				t.Errorf("cut at %v Hz over %v Hz: assessed %+v", hz, width, a)
			}
		}
	}
}

// A 96 kHz file with nothing above 22 kHz was upsampled from CD quality.
func TestUpsampledHiResDetected(t *testing.T) {
	const rate = 96000
	dir := t.TempDir()

	upsampled := filepath.Join(dir, "upsampled.flac")
	audiotest.WriteFLAC(t, upsampled, rate, 24, audiotest.Quantize(audiotest.Noise(4*rate, rate, audiotest.Lowpass(audiotest.Pink, 21000, 1050)), 24))
	info, rep := checkFile(t, upsampled)
	a := assess(info, &rep)
	if a.level != qualitySuspect || !hasCheck(a, checkWarn, "upsampled") {
		t.Errorf("upsampled: cut-off %v, assessed %+v", rep.Spectrum.Cutoff, a)
	}

	real := filepath.Join(dir, "hires.flac")
	audiotest.WriteFLAC(t, real, rate, 24, audiotest.Quantize(audiotest.Noise(4*rate, rate, audiotest.Pink), 24))
	info, rep = checkFile(t, real)
	if a := assess(info, &rep); a.level != qualityHiRes || rep.UsedBits != 24 {
		t.Errorf("true hi-res: report %+v, assessed %+v", rep, a)
	}
}

// A 24-bit file made from a 16-bit one never uses its low 8 bits.
func TestPaddedBitsDetected(t *testing.T) {
	const rate = 48000
	s := audiotest.Quantize(audiotest.Noise(3*rate, rate, audiotest.Pink), 16)
	padded := make([]int32, len(s))
	for i, v := range s {
		padded[i] = v << 8
	}
	dir := t.TempDir()
	for _, path := range []string{filepath.Join(dir, "padded.flac"), filepath.Join(dir, "padded.wav")} {
		if strings.HasSuffix(path, ".flac") {
			audiotest.WriteFLAC(t, path, rate, 24, padded)
		} else {
			audiotest.WriteWAV(t, path, rate, 24, 3, padded)
		}
		info, rep := checkFile(t, path)
		a := assess(info, &rep)
		if rep.UsedBits != 16 || a.level != qualitySuspect || !hasCheck(a, checkWarn, "16 of its 24 bits") {
			t.Errorf("%s: used %d bits, assessed %+v", filepath.Base(path), rep.UsedBits, a)
		}
	}
}

func TestWAVFormats(t *testing.T) {
	const rate = 44100
	s := audiotest.Noise(2*rate, rate, audiotest.Pink)
	dir := t.TempDir()
	for _, c := range []struct{ bits, bytes int }{{8, 1}, {16, 2}, {24, 3}, {24, 4}, {32, 4}} {
		path := filepath.Join(dir, "x.wav")
		audiotest.WriteWAV(t, path, rate, c.bits, c.bytes, audiotest.Quantize(s, c.bits), audiotest.Quantize(s, c.bits))
		info, rep := checkFile(t, path)
		if !info.Lossless || info.Bits != c.bits || info.Channels != 2 || rep.UsedBits != c.bits || rep.Missing != 0 || rep.Damaged != "" {
			t.Errorf("%d-bit in %d bytes: info %+v report %+v", c.bits, c.bytes, info, rep)
		}
	}
}

func TestDamageDetected(t *testing.T) {
	const rate = 44100
	dir := t.TempDir()
	s := audiotest.Quantize(audiotest.Noise(3*rate, rate, audiotest.Pink), 16)

	// A flipped bit in the audio: FLAC's frame and MD5 checksums catch it.
	path := filepath.Join(dir, "flipped.flac")
	audiotest.WriteFLAC(t, path, rate, 16, s)
	data, _ := os.ReadFile(path)
	data[len(data)/2] ^= 0x10
	os.WriteFile(path, data, 0o644)
	info, rep := checkFile(t, path)
	if a := assess(info, &rep); a.level != qualityDamaged || !hasCheck(a, checkBad, "Damaged") {
		t.Errorf("flipped bit: report %+v, assessed %+v", rep, a)
	}

	// A WAV cut short, e.g. by an interrupted copy.
	path = filepath.Join(dir, "short.wav")
	audiotest.WriteWAV(t, path, rate, 16, 2, s)
	data, _ = os.ReadFile(path)
	os.WriteFile(path, data[:len(data)-rate*2], 0o644) // lose the last second
	info, rep = checkFile(t, path)
	if a := assess(info, &rep); !near(rep.Missing.Seconds(), 1, 0.01) || a.level != qualityDamaged || !hasCheck(a, checkBad, "missing") {
		t.Errorf("short WAV: report %+v, assessed %+v", rep, a)
	}
}

func TestVerifyStopsWhenCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	audiotest.WriteFLAC(t, path, 44100, 16, audiotest.Quantize(audiotest.Noise(44100, 44100, audiotest.Pink), 16))
	info, _ := Probe(path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verify(ctx, path, info); err != context.Canceled {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestQuietAudioIsNotJudged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "silence.flac")
	audiotest.WriteFLAC(t, path, 44100, 16, make([]int32, 3*44100))
	info, rep := checkFile(t, path)
	a := assess(info, &rep)
	if rep.Spectrum.Analysed || a.level != qualityLossless || !hasCheck(a, checkInfo, "quiet") {
		t.Errorf("report %+v, assessed %+v", rep, a)
	}
}

func hasCheck(a assessment, kind checkKind, text string) bool {
	for _, c := range a.checks {
		if c.kind == kind && strings.Contains(c.text, text) {
			return true
		}
	}
	return false
}

// genuineFLAC writes a short, clean FLAC file and returns a track for it.
func genuineFLAC(t *testing.T, name string) Track {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".flac")
	audiotest.WriteFLAC(t, path, 44100, 16, audiotest.Quantize(audiotest.Noise(2*44100, 44100, audiotest.Pink), 16))
	return Track{Path: path, Title: name, Format: "FLAC"}
}

func checkTexts(u *ui) []string { return u.np.texts() }

func TestQualityShownWhilePlaying(t *testing.T) {
	u, _, _ := newTestUI(t)
	flacTrack := genuineFLAC(t, "song")
	mp3, _ := filepath.Abs("testdata/sample.mp3")
	setLibrary(u, flacTrack, Track{Path: mp3, Title: "mp3"})

	// The format shows at once; the audio isn't checked until asked.
	u.jump(flacTrack)
	if !u.barBadge.Visible() || u.barBadge.level != qualityLossless || u.np.badge.level != qualityLossless {
		t.Errorf("FLAC: badges %v / %v", u.barBadge.level, u.np.badge.level)
	}
	if !strings.HasPrefix(u.np.spec.Text, "FLAC · 16-bit · 44.1 kHz · Mono") {
		t.Errorf("FLAC spec %q", u.np.spec.Text)
	}
	if got := checkTexts(u); len(got) != 1 || !strings.HasPrefix(got[0], "Not yet checked") {
		t.Errorf("before checking: %q", got)
	}
	if !u.checkBtn.Visible() || u.checkBtn.Text != "Check Audio Quality" || u.checkItem.Disabled {
		t.Error("checking should be offered")
	}

	// Checking (in the dialog) updates the Now Playing page too.
	u.checkAudioQuality()
	if got := checkTexts(u); len(got) != 4 || !strings.HasPrefix(got[0], "Intact") ||
		!strings.HasPrefix(got[2], "Loudness") || got[3] != "No clipping" {
		t.Errorf("after checking: %q", got)
	}
	if u.checkBtn.Text != "Show Spectrogram" || u.checkItem.Disabled {
		t.Error("a checked track should offer its spectrogram")
	}

	// MP3s have nothing to verify, but do have a spectrogram.
	u.next()
	if u.barBadge.level != qualityLossy || !strings.HasPrefix(u.np.spec.Text, "MP3 ·") {
		t.Errorf("MP3: badge %v, spec %q", u.barBadge.level, u.np.spec.Text)
	}
	if !u.checkBtn.Visible() || u.checkBtn.Text != "Show Spectrogram" || u.checkItem.Disabled {
		t.Error("an MP3's spectrogram should be offered")
	}

	u.stop()
	if u.barBadge.Visible() || u.np.badge.Visible() || u.np.spec.Text != "" || len(u.np.checks.Objects) != 0 ||
		!u.checkItem.Disabled || u.checkBtn.Visible() {
		t.Error("quality still shown after stopping")
	}
}

func TestCheckFromMenu(t *testing.T) {
	u, _, _ := newTestUI(t)
	tr := genuineFLAC(t, "song")
	setLibrary(u, tr)
	u.jump(tr)
	u.checkItem.Action()
	if u.quality.state != checked || u.quality.rep.Checksum != checksumOK {
		t.Errorf("state %v", u.quality.state)
	}
}

func TestUnreadableFileExplained(t *testing.T) {
	u, _, _ := newTestUI(t)
	path := filepath.Join(t.TempDir(), "junk.flac")
	os.WriteFile(path, []byte("this is not audio at all"), 0o644)
	u.jump(Track{Path: path, Title: "junk"})
	if u.barBadge.Visible() || !strings.Contains(strings.Join(checkTexts(u), ""), "Can't read") || !u.checkItem.Disabled {
		t.Errorf("badge visible %v, checks %q", u.barBadge.Visible(), checkTexts(u))
	}
	p, spec, _ := u.checkTrack(Track{Path: path, Title: "junk"})
	if !strings.Contains(strings.Join(p.texts(), ""), "Can't read") || spec.levels != nil || spec.note == "" {
		t.Errorf("dialog: %q, note %q", p.texts(), spec.note)
	}
}

// Header reading that finishes after the user has moved on must not
// overwrite the new track's details; a check of another track neither.
func TestStaleQualityResultIgnored(t *testing.T) {
	u, _, _ := newTestUI(t)
	var pending []func()
	u.async = func(f func()) { pending = append(pending, f) }
	a, b := genuineFLAC(t, "a"), genuineFLAC(t, "b")
	setLibrary(u, a, b)

	u.jump(a)
	u.jump(b)
	if len(pending) != 4 {
		t.Fatalf("%d jobs queued, want 4: a's format and art, then b's", len(pending))
	}
	pending[0]() // a's format arrives late
	if u.barBadge.Visible() {
		t.Fatal("the previous track's format was shown")
	}
	pending[2]() // b's
	if u.barBadge.level != qualityLossless {
		t.Fatalf("current track's format not shown: %v", u.barBadge.level)
	}

	// b's check, finishing after a has started, is saved for b only.
	u.checkAudioQuality()
	check := pending[len(pending)-1]
	u.jump(a)
	pending[len(pending)-2]() // a's format
	check()
	if u.quality.state != notChecked || !u.qualityStore.get(b.Path, fileStamp(b.Path), &qualityReport{}) {
		t.Errorf("state %v", u.quality.state)
	}
}

// Results are saved, so a file is only ever checked once, until it changes.
func TestQualityResultsSaved(t *testing.T) {
	store := filepath.Join(t.TempDir(), "quality.gob")
	tr := genuineFLAC(t, "song")
	newUIWithStore := func() *ui {
		u, _, _ := newTestUI(t)
		u.qualityStore = loadQualityStore(store)
		setLibrary(u, tr)
		return u
	}

	u := newUIWithStore()
	u.jump(tr)
	u.checkAudioQuality()
	if _, err := os.Stat(u.qualityStore.picturePath(tr.Path)); err != nil {
		t.Errorf("spectrogram not saved: %v", err)
	}

	// Next session: the saved result shows straight away, and the dialog
	// shows the saved spectrogram without analysing again.
	u = newUIWithStore()
	var pending []func()
	u.jump(tr)
	if u.quality.state != checked || !strings.HasPrefix(checkTexts(u)[0], "Intact") {
		t.Errorf("saved result not used: state %v, checks %q", u.quality.state, checkTexts(u))
	}
	u.async = func(f func()) { pending = append(pending, f) }
	_, spec, _ := u.checkTrack(tr)
	start := time.Now()
	pending[0]()
	if spec.levels == nil || time.Since(start) > 200*time.Millisecond {
		t.Errorf("saved spectrogram not shown promptly (%v)", time.Since(start))
	}

	// An edited file needs checking again.
	later := time.Now().Add(time.Hour)
	os.Chtimes(tr.Path, later, later)
	u = newUIWithStore()
	u.jump(tr)
	if u.quality.state != notChecked {
		t.Errorf("edited file: state %v", u.quality.state)
	}
}

func TestQualityStoreIgnoresBadFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quality.gob")
	os.WriteFile(path, []byte("garbage"), 0o644)
	s := loadQualityStore(path)
	s.put("/a.flac", "stamp", qualityReport{Checksum: checksumOK}, qualityLossless)
	var rep qualityReport
	if !loadQualityStore(path).get("/a.flac", "stamp", &rep) || rep.Checksum != checksumOK {
		t.Error("store not rewritten after a corrupt file")
	}
}

func TestBadgeOpensNowPlaying(t *testing.T) {
	u, _, _ := newTestUI(t)
	tr := genuineFLAC(t, "song")
	setLibrary(u, tr)
	u.jump(tr)
	test.Tap(u.barBadge)
	if !u.visView.Visible() {
		t.Error("tapping the badge should open Now Playing")
	}
}

// Long files are sampled rather than analysed whole; a cut-off must still
// be found.
func TestLongFilesSampled(t *testing.T) {
	const rate = 44100
	s := audiotest.Noise(1<<18, rate, audiotest.Lowpass(audiotest.Pink, 16000, 300))
	spec := newSpectrum(rate, specSize*specMaxFrame*4) // as if 4 times the analysed length
	if spec.every != 4 {
		t.Fatalf("every = %d, want 4", spec.every)
	}
	for range 2 {
		spec.add(s)
	}
	if spec.frames < minAnalysed || spec.frames > 2*len(s)/specSize/4+1 {
		t.Errorf("analysed %d blocks", spec.frames)
	}
	if res := spec.result(); !near(res.Cutoff, 16150, 250) {
		t.Errorf("cut-off %v", res.Cutoff)
	}
}

// sample.flac came from the reference encoder, so this checks the MD5 is
// computed the same way.
func TestRealFLACChecksumMatches(t *testing.T) {
	if _, rep := checkFile(t, "testdata/sample.flac"); rep.Checksum != checksumOK || rep.Damaged != "" {
		t.Errorf("report %+v", rep)
	}
}

// A cut-off to a level only 35 dB lower, rather than to silence, is weaker
// evidence, and is worded so.
func TestWeakCutoffWordedCautiously(t *testing.T) {
	const rate = 44100
	gain := func(f float64) float64 {
		if f > 16000 {
			return audiotest.Pink(f) * math.Pow(10, -35.0/20)
		}
		return audiotest.Pink(f)
	}
	path := filepath.Join(t.TempDir(), "weak.flac")
	audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(audiotest.Noise(6*rate, rate, gain), 16))
	info, rep := checkFile(t, path)
	a := assess(info, &rep)
	if rep.Spectrum.Drop < cutoffDrop || rep.Spectrum.Drop >= strongDrop || !hasCheck(a, checkWarn, "probably converted") {
		t.Errorf("drop %v dB, assessed %+v", rep.Spectrum.Drop, a)
	}
}

func TestRightClickOpensTrackMenu(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	cell := u.table.CreateCell().(*trackCell)
	u.table.UpdateCell(widget.TableCellID{Row: 2, Col: 1}, cell)
	test.TapSecondary(cell)
	top := u.win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no menu shown")
	}
	var menu *widget.PopUpMenu
	visit(top, func(o fyne.CanvasObject) {
		if m, ok := o.(*widget.PopUpMenu); ok {
			menu = m
		}
	})
	if menu == nil {
		t.Fatal("the overlay holds no menu")
	}
	var labels []string
	for _, it := range u.trackMenu(u.tracks[2]).Items {
		labels = append(labels, it.Label)
	}
	if want := "Play|Play Next|Play Later||Add to Playlist||Get Info…|Check Audio Quality…||" + revealLabel + "|Copy File Path||" + trashLabel; strings.Join(labels, "|") != want {
		t.Errorf("menu %q", labels)
	}

	u.trackMenu(u.tracks[2]).Items[0].Action()
	if u.playing.Path != u.tracks[2].Path {
		t.Errorf("Play played %q", u.playing.Path)
	}
}

func TestCheckAnyTrack(t *testing.T) {
	u, _, _ := newTestUI(t)
	playing := genuineFLAC(t, "playing")
	other := filepath.Join(t.TempDir(), "other.flac")
	s := audiotest.Noise(4*44100, 44100, audiotest.Lowpass(audiotest.Pink, 16000, 300))
	audiotest.WriteFLAC(t, other, 44100, 16, audiotest.Quantize(s, 16))
	otherTrack := Track{Path: other, Title: "Other", Artist: "Someone"}
	setLibrary(u, playing, otherTrack)
	u.jump(playing)

	// Another track is checked in a dialog, and the result saved.
	p, spec, d := u.checkTrack(otherTrack)
	if p.badge.level != qualitySuspect || !strings.Contains(strings.Join(p.texts(), "|"), "lossy") {
		t.Errorf("dialog shows %v %q", p.badge.level, p.texts())
	}
	if spec.levels == nil || !near(spec.cutoff, 16150, 250) || spec.rate != 44100 {
		t.Errorf("spectrogram: img %v, cutoff %v, rate %v", spec.levels != nil, spec.cutoff, spec.rate)
	}
	if u.quality.state != notChecked {
		t.Error("the playing track's state changed")
	}
	d.Hide()
	u.jump(otherTrack)
	if u.quality.state != checked || u.barBadge.level != qualitySuspect {
		t.Errorf("saved result not used when played: state %v", u.quality.state)
	}
}

// An MP3's spectrogram shows its encoder's cut-off too.
func TestLossySpectrogram(t *testing.T) {
	u, _, _ := newTestUI(t)
	mp3, _ := filepath.Abs("testdata/sample.mp3")
	p, spec, _ := u.checkTrack(Track{Path: mp3, Title: "mp3"})
	if p.badge.level != qualityLossy || spec.levels == nil || spec.rate != 44100 {
		t.Errorf("badge %v, img %v", p.badge.level, spec.levels != nil)
	}
}

// Closing the dialog stops the check, and nothing is saved.
func TestClosingDialogStopsCheck(t *testing.T) {
	u, _, _ := newTestUI(t)
	var pending []func()
	u.async = func(f func()) { pending = append(pending, f) }
	tr := genuineFLAC(t, "song")
	_, _, d := u.checkTrack(tr)
	d.Hide()
	pending[0]()
	if u.qualityStore.get(tr.Path, fileStamp(tr.Path), &qualityReport{}) {
		t.Error("a closed dialog's check was saved")
	}
}

// A check finished in the dialog updates Now Playing if that track has
// started playing meanwhile.
func TestDialogResultReachesNowPlaying(t *testing.T) {
	u, _, _ := newTestUI(t)
	var pending []func()
	u.async = func(f func()) { pending = append(pending, f) }
	tr := genuineFLAC(t, "song")
	setLibrary(u, tr)
	u.checkTrack(tr)
	u.jump(tr)
	for len(pending) > 0 { // run everything, the dialog's check first
		f := pending[0]
		pending = pending[1:]
		f()
	}
	if u.quality.state != checked || !strings.HasPrefix(checkTexts(u)[0], "Intact") {
		t.Errorf("state %v, checks %q", u.quality.state, checkTexts(u))
	}
}
