package main

import (
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestNextPlaysDownTheListThenStops(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	u.togglePlay() // nothing playing: starts at the top
	for range 4 {
		fp.finish(t)
	}
	want := []string{"Come Together", "Help", "Hey Jude", "Bohemian Rhapsody", "Zebra"}
	if got := titles(tracksFor(u, fp.played)); !slices.Equal(got, want) {
		t.Errorf("played %v, want %v", got, want)
	}
	fp.finish(t)
	if u.playing.Path != "" || len(fp.played) != 5 {
		t.Error("playback should stop after the last track")
	}
}

func TestRepeatAllWraps(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.cycleRepeat() // all

	u.jump(u.tracks[4])
	fp.finish(t)
	if fp.last() != "Come Together" {
		t.Errorf("after last track got %q, want wrap to first", fp.last())
	}
}

func TestRepeatOneLoopsButNextMovesOn(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.cycleRepeat()
	u.cycleRepeat() // one

	u.jump(u.tracks[1])
	fp.finish(t)
	if fp.last() != "Help" {
		t.Errorf("repeat one: track end played %q, want Help again", fp.last())
	}
	u.next()
	if fp.last() != "Hey Jude" {
		t.Errorf("repeat one: next played %q, want Hey Jude", fp.last())
	}
}

func TestPrevious(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	u.jump(u.tracks[2])
	fp.pos = 10 * time.Second
	u.prev()
	if fp.pos != 0 || fp.last() != "Hey Jude" || len(fp.played) != 1 {
		t.Error("prev well into a track should restart it")
	}
	u.prev()
	if fp.last() != "Help" {
		t.Errorf("prev at start played %q, want Help", fp.last())
	}

	u.jump(u.tracks[0])
	u.prev()
	if fp.last() != "Come Together" || len(fp.played) != 3 {
		t.Error("prev on first track should restart it")
	}
	u.cycleRepeat() // all
	u.prev()
	if fp.last() != "Zebra" {
		t.Errorf("prev on first track with repeat all played %q, want Zebra", fp.last())
	}
}

func TestShufflePlaysEachTrackOncePerRound(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.toggleShuffle()

	u.togglePlay()
	for range 4 {
		fp.finish(t)
	}
	if got := uniq(fp.played); got != 5 || len(fp.played) != 5 {
		t.Fatalf("played %v: want all 5 tracks once", fp.played)
	}
	fp.finish(t)
	if u.playing.Path != "" {
		t.Error("shuffle without repeat should stop after one round")
	}

	u.cycleRepeat() // all
	u.togglePlay()
	for range 14 {
		fp.finish(t)
	}
	rounds := [][]string{fp.played[5:10], fp.played[10:15], fp.played[15:20]}
	for i, r := range rounds {
		if uniq(r) != 5 {
			t.Errorf("repeat-all round %d = %v, want all 5 tracks", i+1, r)
		}
	}
	for i := 6; i < len(fp.played); i++ {
		if fp.played[i] == fp.played[i-1] {
			t.Errorf("%s played twice in a row across rounds", titleOf(fp.played[i]))
		}
	}
}

func TestShuffleSingleTrackRepeatAll(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()[0])
	u.toggleShuffle()
	u.cycleRepeat() // all

	u.togglePlay()
	fp.finish(t)
	if len(fp.played) != 2 || u.playing.Path == "" {
		t.Errorf("a single track with repeat all should keep playing (played %v)", fp.played)
	}
}

func TestShufflePreviousRetracesHistory(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.toggleShuffle()

	u.togglePlay()
	u.next()
	u.next()
	a, b, c := fp.played[0], fp.played[1], fp.played[2]

	u.prev()
	u.prev()
	if got := fp.played[3:]; !slices.Equal(got, []string{b, a}) {
		t.Errorf("prev went to %v, want %v", got, []string{b, a})
	}
	u.next()
	if fp.played[5] != b {
		t.Errorf("next after going back played %q, want %q", fp.played[5], b)
	}
	u.next()
	if fp.played[6] != c {
		t.Errorf("then %q, want %q", fp.played[6], c)
	}
}

func TestSearchLimitsWhatPlaysNext(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.search.SetText("beatles")

	u.jump(u.tracks[2]) // Hey Jude, last Beatles track
	fp.finish(t)
	if u.playing.Path != "" {
		t.Errorf("played %q; the filtered list had ended", fp.last())
	}
}

func TestClickingRowsAndHighlight(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	u.table.Select(widget.TableCellID{Row: 3, Col: 2})
	if fp.last() != "Bohemian Rhapsody" {
		t.Fatalf("clicking a row played %q", fp.last())
	}
	u.table.Select(widget.TableCellID{Row: 3, Col: 0}) // another cell, same row
	if len(fp.played) != 1 {
		t.Error("clicking the playing row again should not restart it")
	}

	u.sortBy(0) // title: moves the playing track to row 0
	u.next()
	if fp.last() != "Come Together" {
		t.Errorf("next after re-sort played %q, want the row below in the new order", fp.last())
	}

	u.stop()
	u.table.Select(widget.TableCellID{Row: 1, Col: 0})
	if fp.last() != "Come Together" || len(fp.played) != 3 {
		t.Errorf("after stop, clicking the same row should play it again (played %v)", fp.played)
	}
}

func TestPauseAndStopUpdateControls(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	u.togglePlay()
	if u.nextBtn.Disabled() || u.seek.Disabled() || u.total.Text != "3:00" ||
		u.nowTitle.Text != "Come Together" || u.nowArtist.Text != "Beatles" || u.playBtn.icon != theme.MediaPauseIcon() {
		t.Error("controls should be live while playing")
	}
	u.togglePlay()
	if !fp.paused || u.playBtn.icon != theme.MediaPlayIcon() || u.nowTitle.Text != "Come Together" {
		t.Errorf("pause: paused=%v title=%q", fp.paused, u.nowTitle.Text)
	}
	u.stop()
	if u.playing.Path != "" || !u.nextBtn.Disabled() || !u.seek.Disabled() || u.nowTitle.Text != "Not playing" || u.nowArtist.Text != "" {
		t.Error("stop should reset the controls")
	}
}

func TestUnplayableFileStops(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	fp.bad[u.tracks[1].Path] = true

	u.jump(u.tracks[0])
	fp.finish(t)
	if u.playing.Path != "" || !u.nextBtn.Disabled() {
		t.Error("a file that fails to play should leave the player stopped")
	}
}

func TestSettingsArePersisted(t *testing.T) {
	u, _, a := newTestUI(t)
	u.toggleShuffle()
	u.cycleRepeat()
	u.sortBy(1)
	u.sortBy(1)
	u.volume.SetValue(0)
	u.volume.OnChangeEnded(0)

	fp2 := &fakePlayer{}
	u2 := newUI(u.win, a.Preferences(), uiDeps{player: fp2, cache: u.cache})
	if !u2.shuffle || u2.repeat != repeatAll || u2.sortCol != 1 || !u2.sortDesc {
		t.Errorf("restored shuffle=%v repeat=%v sort=%d/%v",
			u2.shuffle, u2.repeat, u2.sortCol, u2.sortDesc)
	}
	if u2.volume.Value != 0 || fp2.level != 0 {
		t.Errorf("restored volume = %v (player %v), want muted", u2.volume.Value, fp2.level)
	}
}

func TestMuteRestoresVolume(t *testing.T) {
	u, fp, _ := newTestUI(t)
	if fp.level != 0.8 {
		t.Fatalf("default volume = %v, want 0.8", fp.level)
	}
	u.volume.SetValue(0.5)
	u.toggleMute()
	if fp.level != 0 {
		t.Error("mute should silence")
	}
	u.toggleMute()
	if fp.level != 0.5 {
		t.Errorf("unmute restored %v, want 0.5", fp.level)
	}
}

func tracksFor(u *ui, paths []string) []Track {
	var ts []Track
	for _, p := range paths {
		ts = append(ts, u.all[slices.IndexFunc(u.all, func(t Track) bool { return t.Path == p })])
	}
	return ts
}

func uniq(s []string) int {
	m := map[string]bool{}
	for _, v := range s {
		m[v] = true
	}
	return len(m)
}

// The shuffle and repeat icons are hand-written SVG; make sure Fyne's
// renderer actually draws them rather than showing a blank button.
func TestCustomIconsRender(t *testing.T) {
	newTestApp(t)
	for name, res := range map[string]fyne.Resource{
		"shuffle": shuffleIcon, "repeat": repeatIcon, "repeat one": repeatOneIcon,
	} {
		img := canvas.NewImageFromResource(res)
		w := test.NewTempWindow(t, img)
		w.Resize(fyne.NewSize(48, 48))
		c := w.Canvas().Capture()
		bg, drawn := c.At(0, 0), 0
		for y := c.Bounds().Min.Y; y < c.Bounds().Max.Y; y++ {
			for x := c.Bounds().Min.X; x < c.Bounds().Max.X; x++ {
				if c.At(x, y) != bg {
					drawn++
				}
			}
		}
		if drawn < 100 {
			t.Errorf("%s icon drew only %d pixels", name, drawn)
		}
	}
}
