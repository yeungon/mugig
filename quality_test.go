package main

import (
	"mugig/internal/audiotest"
	"mugig/internal/library"
	"mugig/internal/quality"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// genuineFLAC writes a short, clean FLAC file and returns a track for it.
func genuineFLAC(t *testing.T, name string) library.Track {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".flac")
	audiotest.WriteFLAC(t, path, 44100, 16, audiotest.Quantize(audiotest.Noise(2*44100, 44100, audiotest.Pink), 16))
	return library.Track{Path: path, Title: name, Format: "FLAC"}
}

func checkTexts(u *ui) []string { return u.np.texts() }

func TestQualityShownWhilePlaying(t *testing.T) {
	u, _, _ := newTestUI(t)
	flacTrack := genuineFLAC(t, "song")
	mp3, _ := filepath.Abs("testdata/sample.mp3")
	setLibrary(u, flacTrack, library.Track{Path: mp3, Title: "mp3"})

	// The format shows at once; the audio isn't checked until asked.
	u.jump(flacTrack)
	if !u.barBadge.Visible() || u.barBadge.level != quality.Lossless || u.np.badge.level != quality.Lossless {
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
	if u.barBadge.level != quality.Lossy || !strings.HasPrefix(u.np.spec.Text, "MP3 ·") {
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
	if u.quality.state != checked || u.quality.rep.Checksum != quality.ChecksumOK {
		t.Errorf("state %v", u.quality.state)
	}
}

func TestUnreadableFileExplained(t *testing.T) {
	u, _, _ := newTestUI(t)
	path := filepath.Join(t.TempDir(), "junk.flac")
	os.WriteFile(path, []byte("this is not audio at all"), 0o644)
	u.jump(library.Track{Path: path, Title: "junk"})
	if u.barBadge.Visible() || !strings.Contains(strings.Join(checkTexts(u), ""), "Can't read") || !u.checkItem.Disabled {
		t.Errorf("badge visible %v, checks %q", u.barBadge.Visible(), checkTexts(u))
	}
	p, spec, _ := u.checkTrack(library.Track{Path: path, Title: "junk"})
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
	if u.barBadge.level != quality.Lossless {
		t.Fatalf("current track's format not shown: %v", u.barBadge.level)
	}

	// b's check, finishing after a has started, is saved for b only.
	u.checkAudioQuality()
	check := pending[len(pending)-1]
	u.jump(a)
	pending[len(pending)-2]() // a's format
	check()
	if u.quality.state != notChecked || !u.qualityStore.Get(b.Path, quality.FileStamp(b.Path), &quality.Report{}) {
		t.Errorf("state %v", u.quality.state)
	}
}

// Results are saved, so a file is only ever checked once, until it changes.
func TestQualityResultsSaved(t *testing.T) {
	store := filepath.Join(t.TempDir(), "quality.gob")
	tr := genuineFLAC(t, "song")
	newUIWithStore := func() *ui {
		u, _, _ := newTestUI(t)
		u.qualityStore = quality.LoadStore(store)
		setLibrary(u, tr)
		return u
	}

	u := newUIWithStore()
	u.jump(tr)
	u.checkAudioQuality()
	if _, err := os.Stat(u.qualityStore.PicturePath(tr.Path)); err != nil {
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
	otherTrack := library.Track{Path: other, Title: "Other", Artist: "Someone"}
	setLibrary(u, playing, otherTrack)
	u.jump(playing)

	// Another track is checked in a dialog, and the result saved.
	p, spec, d := u.checkTrack(otherTrack)
	if p.badge.level != quality.Suspect || !strings.Contains(strings.Join(p.texts(), "|"), "lossy") {
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
	if u.quality.state != checked || u.barBadge.level != quality.Suspect {
		t.Errorf("saved result not used when played: state %v", u.quality.state)
	}
}

// An MP3's spectrogram shows its encoder's cut-off too.
func TestLossySpectrogram(t *testing.T) {
	u, _, _ := newTestUI(t)
	mp3, _ := filepath.Abs("testdata/sample.mp3")
	p, spec, _ := u.checkTrack(library.Track{Path: mp3, Title: "mp3"})
	if p.badge.level != quality.Lossy || spec.levels == nil || spec.rate != 44100 {
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
	if u.qualityStore.Get(tr.Path, quality.FileStamp(tr.Path), &quality.Report{}) {
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
