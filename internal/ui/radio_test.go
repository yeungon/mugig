package ui

import (
	"errors"
	"mugig/internal/audio"
	"mugig/internal/radio"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// fakeRadio stands in for connecting to stations.
type fakeRadio struct {
	opened []string                        // links connected to, in order
	news   map[string]func(audio.LiveInfo) // each link's news, to send some
	fail   map[string]error
}

func newRadioUI(t *testing.T, names ...string) (*ui, *fakePlayer, *fakeRadio, string) {
	t.Helper()
	u, fp, _ := newTestUI(t)
	path := filepath.Join(t.TempDir(), "stations.json")
	u.stationsPath = path
	r := &fakeRadio{news: map[string]func(audio.LiveInfo){}, fail: map[string]error{}}
	u.openLive = func(link string, onInfo func(audio.LiveInfo)) (*audio.Live, error) {
		r.opened = append(r.opened, link)
		if err := r.fail[link]; err != nil {
			return nil, err
		}
		r.news[link] = onInfo
		return &audio.Live{Link: link, Codec: "AAC", Kbps: 96}, nil
	}
	for _, n := range names {
		u.stations = append(u.stations, &radio.Station{Name: n, Link: "https://radio.example/" + strings.ToLower(n)})
	}
	u.rebuildStationList()
	return u, fp, r, path
}

// rowText is a sidebar row's name and detail.
func rowText(r *folderRow) string { return r.name.Text + " · " + r.detail.Text }

// entries finds the text boxes in the dialog on top.
func entries(u *ui) []*widget.Entry {
	var es []*widget.Entry
	visit(u.win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok {
			es = append(es, e)
		}
	})
	return es
}

// A station is added from the + with its name and link, which must be a
// web link; it is saved, and plays, LIVE in the player bar.
func TestAddStation(t *testing.T) {
	u, fp, r, path := newRadioUI(t)
	if !strings.Contains(allText(u.stationList), "Add an internet radio station") {
		t.Error("with no stations, the section should say how to add one")
	}
	test.Tap(u.newStationBtn)
	es := entries(u)
	if len(es) != 2 {
		t.Fatalf("%d boxes in the dialog", len(es))
	}
	es[0].SetText("World Service")
	add := findButton(t, u.win.Canvas().Overlays().Top(), "Add")
	es[1].SetText("bbc.co.uk/worldservice")
	if !add.Disabled() {
		t.Error("a link without http:// could be added")
	}
	es[1].SetText("  https://radio.example/ws.m3u8 ")
	if add.Disabled() {
		t.Fatal("a good link couldn't be added")
	}
	test.Tap(add)

	if len(u.stations) != 1 || *u.stations[0] != (radio.Station{Name: "World Service", Link: "https://radio.example/ws.m3u8", Format: "AAC · 96 kbps"}) {
		t.Fatalf("stations %+v", u.stations)
	}
	if saved := radio.LoadStations(path); len(saved) != 1 || saved[0].Format != "AAC · 96 kbps" {
		t.Errorf("saved %+v", saved)
	}
	if !slices.Equal(r.opened, []string{"https://radio.example/ws.m3u8"}) || fp.last() != "ws" {
		t.Errorf("connected to %v, played %v", r.opened, fp.played)
	}
	if u.nowTitle.Text != "World Service" || u.nowArtist.Text != "Live radio" || u.total.Text != "LIVE" || !u.seek.Disabled() {
		t.Errorf("bar: %q / %q, %q, seek enabled %v", u.nowTitle.Text, u.nowArtist.Text, u.total.Text, !u.seek.Disabled())
	}
	if got := rowText(u.stationRows[u.stations[0]]); got != "World Service · Playing · AAC · 96 kbps" {
		t.Errorf("row %q", got)
	}
	if m := lastMedia(u); !m.Live || m.Title != "World Service" || m.State != statePlaying {
		t.Errorf("media %+v", m)
	}
	if text := u.visDetail.Text; text != "Live radio · AAC · 96 kbps" {
		t.Errorf("Now Playing says %q", text)
	}
}

// What the station says is playing shows in the player bar and the
// system's controls, and a dropped connection shows while it is made again.
func TestStationNews(t *testing.T) {
	u, _, r, _ := newRadioUI(t, "Jazz")
	s := u.stations[0]
	u.playStation(s)
	news := r.news[s.Link]

	news(audio.LiveInfo{Title: "Miles Davis - So What"})
	if u.nowArtist.Text != "Miles Davis - So What" {
		t.Errorf("bar says %q", u.nowArtist.Text)
	}
	if m := lastMedia(u); m.Title != "Miles Davis - So What" || m.Artist != "Jazz" {
		t.Errorf("media %+v", m)
	}
	news(audio.LiveInfo{Title: "Miles Davis - So What", Reconnecting: true})
	if u.nowArtist.Text != "Reconnecting…" || !strings.Contains(rowText(u.stationRows[s]), "Reconnecting…") {
		t.Errorf("bar %q, row %q", u.nowArtist.Text, rowText(u.stationRows[s]))
	}
	news(audio.LiveInfo{Title: "Miles Davis - So What"})
	if u.nowArtist.Text != "Miles Davis - So What" || !strings.Contains(rowText(u.stationRows[s]), "Playing") {
		t.Errorf("back: bar %q, row %q", u.nowArtist.Text, rowText(u.stationRows[s]))
	}
}

// Pausing hangs up; playing again connects again, to the broadcast as it
// is now. Seeking does nothing.
func TestPauseStation(t *testing.T) {
	u, fp, r, _ := newRadioUI(t, "Jazz")
	s := u.stations[0]
	test.Tap(u.stationRows[s])
	u.seekTo(time.Minute)
	if fp.pos != 0 {
		t.Error("seeking moved the station")
	}
	u.togglePlay()
	if fp.onEnd != nil || !u.paused || u.station == nil || u.nowTitle.Text != "Jazz" {
		t.Errorf("after pausing: still playing %v, paused %v", fp.onEnd != nil, u.paused)
	}
	if got := rowText(u.stationRows[s]); got != "Jazz · Paused" {
		t.Errorf("row %q", got)
	}
	if m := lastMedia(u); m.State != statePaused {
		t.Errorf("media %+v", m)
	}
	test.Tap(u.stationRows[s]) // the paused station's row plays it too
	if len(r.opened) != 2 || fp.onEnd == nil || u.paused {
		t.Errorf("connected %d times; playing %v", len(r.opened), fp.onEnd != nil)
	}
	test.Tap(u.stationRows[s]) // the station playing carries on
	if len(r.opened) != 2 {
		t.Error("tapping the station playing started it again")
	}
}

// Pausing while still connecting gives up on the connection.
func TestPauseWhileConnecting(t *testing.T) {
	u, fp, r, _ := newRadioUI(t, "Jazz")
	var later []func()
	u.async = func(f func()) { later = append(later, f) }
	u.playStation(u.stations[0])
	if u.nowArtist.Text != "Connecting…" || !strings.Contains(rowText(u.stationRows[u.stations[0]]), "Connecting…") {
		t.Errorf("bar %q", u.nowArtist.Text)
	}
	u.togglePlay()
	for _, f := range later {
		f()
	}
	if len(r.opened) != 1 || len(fp.played) != 0 || u.station.state != stationPaused {
		t.Errorf("connected %d, played %v", len(r.opened), fp.played)
	}
}

// Next and Previous move through the stations, going round.
func TestStepStations(t *testing.T) {
	u, fp, _, _ := newRadioUI(t, "One", "Two", "Three")
	u.playStation(u.stations[0])
	u.next()
	u.prev()
	u.prev()
	if got := fp.played; !slices.Equal(got, []string{
		"https://radio.example/one", "https://radio.example/two", "https://radio.example/one", "https://radio.example/three",
	}) {
		t.Errorf("played %v", got)
	}
	u.remote(remoteNext, 0) // the media keys too
	if fp.last() != "one" {
		t.Errorf("media key played %v", fp.last())
	}
}

// A song takes over from a station; the song before the station is what
// the next launch resumes.
func TestSongAfterStation(t *testing.T) {
	u, fp, _, _ := newRadioUI(t, "Jazz")
	setLibrary(u, fiveTracks()...)
	u.play(u.tracks[0])
	fp.pos = 30 * time.Second
	u.playStation(u.stations[0])
	u.saveSession()
	if song, at := u.prefs.String(prefResumeSong), u.prefs.Float(prefResumeAt); song != u.tracks[0].Path || at != 30 {
		t.Errorf("resumes %s at %v", song, at)
	}
	u.play(u.tracks[1])
	if u.station != nil || strings.Contains(rowText(u.stationRows[u.stations[0]]), "Playing") || u.total.Text == "LIVE" {
		t.Errorf("the station still shows: %q", rowText(u.stationRows[u.stations[0]]))
	}
	if u.nowTitle.Text != u.tracks[1].Title {
		t.Errorf("bar %q", u.nowTitle.Text)
	}
}

// A station that can't be reached, or stops, says why.
func TestStationProblems(t *testing.T) {
	u, fp, r, _ := newRadioUI(t, "Jazz", "Talk")
	r.fail[u.stations[0].Link] = errors.New("the station answered \"404 Not Found\"")
	u.playStation(u.stations[0])
	if u.station != nil || u.nowTitle.Text != "Not playing" || !strings.Contains(allText(u.win.Canvas().Overlays().Top()), "Couldn't play Jazz") {
		t.Errorf("after failing: %q", u.nowTitle.Text)
	}
	u.win.Canvas().Overlays().Remove(u.win.Canvas().Overlays().Top())

	u.playStation(u.stations[1])
	fp.fail(t, errors.New("lost the station"))
	if u.station != nil || !strings.Contains(allText(u.win.Canvas().Overlays().Top()), "Stopped playing Talk") {
		t.Error("losing the station wasn't said")
	}
	u.win.Canvas().Overlays().Remove(u.win.Canvas().Overlays().Top())
	u.playStation(u.stations[1])
	fp.finish(t)
	if u.station != nil || !strings.Contains(u.status.Text, "Talk's broadcast has ended") {
		t.Errorf("status %q", u.status.Text)
	}
}

// Editing a station's link while it plays plays the new one; removing it
// stops it.
func TestEditAndRemoveStation(t *testing.T) {
	u, fp, _, path := newRadioUI(t, "Jazz", "Talk")
	s := u.stations[0]
	u.playStation(s)
	widget.ShowPopUpMenuAtPosition(u.stationMenu(s), u.win.Canvas(), fyne.Position{})
	itemIn(t, u.stationMenu(s), "Edit…").Action()
	es := entries(u)
	es[0].SetText("Smooth Jazz")
	es[1].SetText("https://radio.example/smooth.pls")
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Save"))
	if s.Name != "Smooth Jazz" || fp.last() != "smooth" || u.nowTitle.Text != "Smooth Jazz" {
		t.Errorf("after editing: %+v, playing %v, bar %q", s, fp.played, u.nowTitle.Text)
	}
	if saved := radio.LoadStations(path); saved[0].Link != "https://radio.example/smooth.pls" {
		t.Errorf("saved %+v", saved[0])
	}

	itemIn(t, u.stationMenu(s), "Copy Link").Action()
	if got := fyne.CurrentApp().Clipboard().Content(); got != s.Link {
		t.Errorf("copied %q", got)
	}

	itemIn(t, u.stationMenu(s), "Remove Station…").Action()
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Yes"))
	if len(u.stations) != 1 || u.station != nil || fp.onEnd != nil || len(u.stationRows) != 1 {
		t.Errorf("after removing: %d stations, playing %v", len(u.stations), fp.onEnd != nil)
	}
	if saved := radio.LoadStations(path); len(saved) != 1 || saved[0].Name != "Talk" {
		t.Errorf("saved %+v", saved)
	}
}

// Radio plays at normal speed and isn't levelled, and Now Playing says so.
func TestStationOutputInfo(t *testing.T) {
	u, _, _, _ := newRadioUI(t, "Jazz")
	u.setSpeed(1.5)
	u.setLevelling(true)
	u.playStation(u.stations[0])
	text := u.npOutput.Text
	if strings.Contains(text, "speed") || !strings.Contains(text, "not levelled") {
		t.Errorf("Now Playing says %q", text)
	}
}
