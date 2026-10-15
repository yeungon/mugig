package ui

import (
	"errors"
	"math"
	"mugig/internal/audio"
	"mugig/internal/library"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// The tests' app has the real icon, as Run gives it.
func init() {
	icon, err := os.ReadFile(filepath.Join("..", "..", "IconRounded.png"))
	if err != nil {
		panic(err)
	}
	setIcon(icon)
}

// fakePlayer stands in for real audio so tests make no sound.
type fakePlayer struct {
	played []string // every path Play was called with, in order
	onEnd  func(error)
	paused bool
	pos    time.Duration
	level  float64
	speed  float64
	fade   bool
	feed   audio.CrossfeedLevel
	eq     audio.EQSettings
	bad    map[string]bool // paths that fail to play
	next   string          // the path readied to follow, by Prepare

	device     string
	matchRate  bool
	exclusive  bool
	hogFails   bool                      // another app has the device to itself
	levelling  func(path string) float64 // nil when off
	relevelled []string
}

func (f *fakePlayer) Play(path string, onEnd func(error)) error {
	if f.bad[path] {
		return errors.New("cannot play " + filepath.Base(path))
	}
	f.played = append(f.played, path)
	f.onEnd, f.paused, f.pos, f.next = onEnd, false, 0, ""
	return nil
}

func (f *fakePlayer) Prepare(path string) { f.next = path }

func (f *fakePlayer) TogglePause() bool                   { f.paused = !f.paused; return f.paused }
func (f *fakePlayer) Stop()                               { f.onEnd, f.next = nil, "" }
func (f *fakePlayer) Position() time.Duration             { return f.pos }
func (f *fakePlayer) Duration() time.Duration             { return 3 * time.Minute }
func (f *fakePlayer) Seek(d time.Duration) error          { f.pos = d; return nil }
func (f *fakePlayer) SetVolume(level float64)             { f.level = level }
func (f *fakePlayer) SetFade(on bool)                     { f.fade = on }
func (f *fakePlayer) SetLevelling(g func(string) float64) { f.levelling = g }
func (f *fakePlayer) Relevel(path string)                 { f.relevelled = append(f.relevelled, path) }
func (f *fakePlayer) SetSpeed(speed float64)              { f.speed = speed }
func (f *fakePlayer) SetCrossfeed(l audio.CrossfeedLevel) { f.feed = l }
func (f *fakePlayer) SetEQ(s audio.EQSettings)            { f.eq = s }

// testRate is the sample rate of the fake player's audio.
const testRate = 44100

// Samples gives a 1 kHz sine at half volume while something is playing.
func (f *fakePlayer) Samples(dst []float64) int {
	if f.onEnd == nil {
		return 0
	}
	for i := range dst {
		dst[i] = 0.5 * math.Sin(2*math.Pi*1000*float64(i)/float64(testRate))
	}
	return testRate
}

func (f *fakePlayer) Output() (int, int) {
	if f.onEnd == nil {
		return 0, 0
	}
	return testRate, testRate
}

func (f *fakePlayer) Devices() []audio.Device {
	return []audio.Device{{ID: "aa", Name: "Speakers", IsDefault: true}, {ID: "bb", Name: "USB DAC"}}
}
func (f *fakePlayer) SetDevice(id string)  { f.device = id }
func (f *fakePlayer) SetMatchRate(on bool) { f.matchRate = on }
func (f *fakePlayer) DevicesChanged()      {}
func (f *fakePlayer) SetExclusive(on bool) { f.exclusive = on }
func (f *fakePlayer) Exclusive() bool      { return f.exclusive && !f.hogFails && f.onEnd != nil }

// finish simulates the current track playing to its end.
func (f *fakePlayer) finish(t *testing.T) {
	t.Helper()
	if f.onEnd == nil {
		t.Fatal("finish called with nothing playing")
	}
	end := f.onEnd
	f.onEnd = nil
	end(nil)
}

// fail simulates the current track stopping early with err.
func (f *fakePlayer) fail(t *testing.T, err error) {
	t.Helper()
	if f.onEnd == nil {
		t.Fatal("fail called with nothing playing")
	}
	end := f.onEnd
	f.onEnd = nil
	end(err)
}

// last returns the title of the most recently played track.
func (f *fakePlayer) last() string {
	if len(f.played) == 0 {
		return ""
	}
	return titleOf(f.played[len(f.played)-1])
}

// newTestUI builds the full UI against a fake player and a fresh in-memory
// app, so preferences start empty.
func newTestUI(t *testing.T) (*ui, *fakePlayer, fyne.App) {
	t.Helper()
	a := newTestApp(t)
	fp := &fakePlayer{bad: map[string]bool{}}
	u := newUI(test.NewTempWindow(t, nil), a.Preferences(), uiDeps{
		player: fp,
		cache:  library.LoadCache(filepath.Join(t.TempDir(), "library.gob")),
		media:  &fakeMedia{},
		async:  func(f func()) { f() }, // the test driver runs fyne.Do inline anyway
		pick: func(fyne.Window, pickOptions, func([]string)) {
			t.Error("unexpected Open panel") // tests that expect one set u.pick
		},
	})
	u.save = func(fyne.Window, string, func(string)) {
		t.Error("unexpected Save panel") // tests that expect one set u.save
	}
	u.vis.tick, u.miniVis.tick = 0, 0 // tests draw frames by calling frame, not on a timer
	u.reveal = func(string) error {
		t.Error("unexpected Finder window") // tests that expect one set u.reveal
		return nil
	}
	u.trash = func(string) error {
		t.Error("unexpected move to Trash") // tests that expect one set u.trash
		return nil
	}
	// No real folder watching: tests that want it use a fakeWatcher.
	u.watchFolder = func(string, func([]string)) (func(), error) { return func() {}, nil }
	u.reconnectEvery = 0
	return u, fp, a
}

// newTestApp is an in-memory app using the real theme, which defines the
// app's own colour names.
func newTestApp(t *testing.T) fyne.App {
	a := test.NewTempApp(t)
	a.Settings().SetTheme(appTheme{})
	return a
}

// fakeMedia records what would be shown in the OS media controls.
type fakeMedia struct{ updates []nowPlayingInfo }

func (m *fakeMedia) Update(info nowPlayingInfo) { m.updates = append(m.updates, info) }

func lastMedia(u *ui) nowPlayingInfo {
	m := u.media.(*fakeMedia)
	return m.updates[len(m.updates)-1]
}

// track makes a Track whose path is derived from its title.
func track(title, artist, album string, year int) library.Track {
	t := library.Track{
		Path:   "/music/" + title + ".mp3",
		Title:  title,
		Artist: artist,
		Album:  album,
		Year:   year,
		Format: "MP3",
	}
	return t
}

func titleOf(path string) string {
	b := filepath.Base(path)
	return b[:len(b)-len(filepath.Ext(b))]
}

// setLibrary loads tracks as if a scan had just finished.
func setLibrary(u *ui, tracks ...library.Track) {
	u.all = tracks
	u.applyView()
}

func titles(ts []library.Track) []string {
	var s []string
	for _, t := range ts {
		s = append(s, t.Title)
	}
	return s
}

// fiveTracks is a small library already in library (artist) order.
func fiveTracks() []library.Track {
	return []library.Track{
		track("Come Together", "Beatles", "Abbey Road", 1969),
		track("Help", "Beatles", "Help!", 1965),
		track("Hey Jude", "Beatles", "Hey Jude", 1968),
		track("Bohemian Rhapsody", "Queen", "A Night at the Opera", 1975),
		track("Zebra", "", "", 0),
	}
}

// allText joins the text of every label in o.
func allText(o fyne.CanvasObject) string {
	var out []string
	visit(o, func(o fyne.CanvasObject) {
		if l, ok := o.(*widget.Label); ok {
			out = append(out, l.Text)
		}
	})
	return strings.Join(out, " ")
}

func near(got, want, tol float64) bool { return math.Abs(got-want) <= tol }
