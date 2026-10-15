package ui

import (
	"mugig/internal/audio"
	"testing"
	"time"
)

// following names the song trackEnded will play, in each mode.
func TestFollowingSong(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	follows := func() string {
		t.Helper()
		if f, ok := u.following(); ok {
			return f.Title
		}
		return ""
	}
	u.jump(u.tracks[1])
	if got := follows(); got != "Hey Jude" {
		t.Errorf("list order: %q", got)
	}
	u.playLater(u.tracks[4])
	if got := follows(); got != "Zebra" {
		t.Errorf("Up Next: %q", got)
	}
	u.clearQueue()
	u.setRepeatMode(repeatOne)
	if got := follows(); got != "Help" {
		t.Errorf("repeat one: %q", got)
	}
	u.setRepeatMode(repeatOff)
	u.jump(u.tracks[4])
	if got := follows(); got != "" {
		t.Errorf("end of the list: %q", got)
	}
	u.setRepeatMode(repeatAll)
	if got := follows(); got != "Come Together" {
		t.Errorf("repeat all: %q", got)
	}

	u.toggleShuffle()
	for range 12 { // it is what actually plays next, round after round
		want := follows()
		fp.finish(t)
		if fp.last() != want {
			t.Fatalf("following said %q, %q played", want, fp.last())
		}
	}
}

// The next song is readied only near the end, and again if what follows
// changes.
func TestPrepareNextNearTheEnd(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[0])
	u.prepareNext()
	if fp.next != "" {
		t.Errorf("readied %q three minutes early", fp.next)
	}
	fp.pos = 3*time.Minute - audio.GaplessLead + time.Second
	u.prepareNext()
	if titleOf(fp.next) != "Help" {
		t.Errorf("readied %q", fp.next)
	}
	u.playNext(u.tracks[3])
	u.prepareNext()
	if titleOf(fp.next) != "Bohemian Rhapsody" {
		t.Errorf("after Play Next, readied %q", fp.next)
	}
	u.clearQueue()
	u.jump(u.tracks[4])
	fp.pos = 3*time.Minute - time.Second
	u.prepareNext()
	if fp.next != "" {
		t.Errorf("the last song: readied %q", fp.next)
	}
}
