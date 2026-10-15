package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Volume Levelling is off unless turned on, and remembered. On, a song's
// loudness is measured when it starts, only the first time, and the player
// levels it by the gain that brings it to -14 LUFS; Now Playing says by how
// much. Off, the player is told to stop.
func TestVolumeLevelling(t *testing.T) {
	u, fp, a := newTestUI(t)
	song := genuineFLAC(t, "Noise")
	setLibrary(u, song)

	item := menuItem(t, u, "Volume Levelling")
	if item.Checked || fp.levelling != nil {
		t.Fatal("levelling should be off unless turned on")
	}
	item.Action()
	if !item.Checked || fp.levelling == nil || !a.Preferences().Bool(prefLevelling) {
		t.Fatal("turning it on should level, and be remembered")
	}

	u.jump(song)
	l, ok := u.loudness(song.Path)
	if !ok {
		t.Fatal("playing should have measured the song's loudness")
	}
	if g := fp.levelling(song.Path); g != l.Gain() || g == 0 {
		t.Errorf("the player's gain %v, want %v", g, l.Gain())
	}
	if !slices.Equal(fp.relevelled, []string{song.Path}) {
		t.Errorf("relevelled %v: the player should hear of the measurement", fp.relevelled)
	}
	u.updateOutputInfo()
	if want := fmt.Sprintf(", levelled by %+.1f dB", l.Gain()); !strings.Contains(u.npOutput.Text, want) ||
		strings.Contains(u.npOutput.Text, "unconverted") {
		t.Errorf("Now Playing says %q, want %q and not unconverted", u.npOutput.Text, want)
	}

	u.jump(song) // again: already measured
	if len(fp.relevelled) != 1 {
		t.Errorf("measured %d times, want once", len(fp.relevelled))
	}

	item.Action()
	if item.Checked || fp.levelling != nil || a.Preferences().Bool(prefLevelling) {
		t.Error("turning it off should stop levelling, and be remembered")
	}
	u.updateOutputInfo()
	if strings.Contains(u.npOutput.Text, "levelled") {
		t.Errorf("off, Now Playing still says %q", u.npOutput.Text)
	}
}

// A song that can't be read isn't measured over and over, and levelling
// leaves it alone.
func TestLevellingUnreadableSong(t *testing.T) {
	u, fp, _ := newTestUI(t)
	menuItem(t, u, "Volume Levelling").Action()
	bad := track("Gone", "Nobody", "Nothing", 2000) // no such file
	setLibrary(u, bad)
	u.jump(bad)
	if _, ok := u.loudness(bad.Path); ok || len(u.measuring) != 0 || len(fp.relevelled) != 0 {
		t.Errorf("unreadable song: measuring %v, relevelled %v", u.measuring, fp.relevelled)
	}
	if g := fp.levelling(bad.Path); g != 0 {
		t.Errorf("gain %v for a song that can't be measured, want 0", g)
	}
}

// A song not measured yet starts at the last levelled song's gain, then
// takes its own once measured.
func TestLevellingGuessesFromTheLastSong(t *testing.T) {
	u, fp, _ := newTestUI(t)
	menuItem(t, u, "Volume Levelling").Action()
	first, second := genuineFLAC(t, "First"), genuineFLAC(t, "Second")
	setLibrary(u, first, second)
	u.jump(first)
	firstGain := fp.levelling(first.Path)
	if firstGain == 0 {
		t.Fatal("the first song should be levelled")
	}
	u.async = func(func()) {} // measurements don't finish for now
	if g := fp.levelling(second.Path); g != firstGain {
		t.Errorf("an unmeasured song starts at %v dB, want the last song's %v", g, firstGain)
	}
}
