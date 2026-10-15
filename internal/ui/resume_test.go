package ui

import (
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// reopen starts Mugig again with u's saved settings and library, as after
// quitting, with the library already scanned.
func reopen(t *testing.T, u *ui, a fyne.App) (*ui, *fakePlayer) {
	t.Helper()
	fp := &fakePlayer{bad: map[string]bool{}}
	u2 := newUI(test.NewTempWindow(t, nil), a.Preferences(), uiDeps{
		player: fp,
		cache:  u.cache,
		media:  &fakeMedia{},
		async:  func(f func()) { f() },
	})
	setLibrary(u2, u.all...)
	return u2, fp
}

// Quitting mid-song and opening Mugig again shows that song paused where
// it was, with Up Next as it was; Play carries on from there.
func TestResumeWhereLeftOff(t *testing.T) {
	u, fp, a := newTestUI(t)
	songs := fiveTracks()
	for i := range songs {
		songs[i].Length = 4 * time.Minute
	}
	setLibrary(u, songs...)
	u.jump(u.tracks[2]) // Hey Jude
	fp.pos = 75 * time.Second
	u.playLater(u.tracks[4])
	u.playLater(u.tracks[0])
	u.saveSession() // quit

	u2, fp2 := reopen(t, u, a)
	if u2.cued != nil || len(u2.queued) != 0 {
		t.Fatal("nothing should be restored before the library is scanned")
	}
	u2.restoreSession()
	if u2.cued == nil || u2.nowTitle.Text != "Hey Jude" || u2.elapsed.Text != "1:15" || u2.total.Text != "4:00" {
		t.Fatalf("player bar shows %q at %s of %s", u2.nowTitle.Text, u2.elapsed.Text, u2.total.Text)
	}
	if len(fp2.played) != 0 || u2.playing.Path != "" {
		t.Error("nothing should play by itself")
	}
	if got := titles(u2.queued); !slices.Equal(got, []string{"Zebra", "Come Together"}) {
		t.Errorf("Up Next = %v", got)
	}
	u2.togglePlay()
	if fp2.last() != "Hey Jude" || fp2.pos != 75*time.Second {
		t.Errorf("Play started %q at %v, want Hey Jude at 1:15", fp2.last(), fp2.pos)
	}
	fp2.finish(t)
	if fp2.last() != "Zebra" {
		t.Errorf("then %q, want Zebra from Up Next", fp2.last())
	}
}

// Moving the seek bar before pressing Play changes where it starts.
func TestSeekWhileCued(t *testing.T) {
	u, _, _ := newTestUI(t)
	songs := fiveTracks()
	songs[1].Length = 3 * time.Minute
	setLibrary(u, songs...)
	u.cue(u.tracks[1], 30*time.Second)
	u.seek.OnChangeEnded(100)
	if u.cued.at != 100*time.Second || u.elapsed.Text != "1:40" {
		t.Errorf("cued at %v, showing %s", u.cued.at, u.elapsed.Text)
	}
	u.seekTo(10 * time.Minute)
	if u.cued.at != 3*time.Minute {
		t.Errorf("seeking past the end: cued at %v", u.cued.at)
	}
}

// Playing another song, or Stop, forgets the cued one.
func TestOtherSongReplacesCue(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.cue(u.tracks[3], time.Minute)
	u.jump(u.tracks[0])
	if u.cued != nil || fp.pos != 0 {
		t.Error("playing another song should drop the cued one")
	}
	u.stop()
	u.cue(u.tracks[3], time.Minute)
	u.stop()
	if u.cued != nil || u.nowTitle.Text != "Not playing" {
		t.Error("Stop should clear the cued song")
	}
	u.saveSession()
	if u.prefs.String(prefResumeSong) != "" {
		t.Error("quitting while stopped should remember no song")
	}
}

// Songs gone from the library aren't restored; a song that was nearly
// over isn't either, past its end.
func TestResumeSkipsMissingSongs(t *testing.T) {
	u, fp, a := newTestUI(t)
	songs := fiveTracks()
	songs[3].Length = 2 * time.Minute
	setLibrary(u, songs...)
	u.jump(u.tracks[1])
	u.playLater(u.tracks[3])
	u.playLater(u.tracks[4])
	u.saveSession()

	u2, _ := reopen(t, u, a)
	setLibrary(u2, u.all[2:]...) // Come Together and Help are gone
	u2.restoreSession()
	if u2.cued != nil || u2.nowTitle.Text != "Not playing" {
		t.Errorf("a song no longer in the library shouldn't be cued: %q", u2.nowTitle.Text)
	}
	if got := titles(u2.queued); !slices.Equal(got, []string{"Bohemian Rhapsody", "Zebra"}) {
		t.Errorf("Up Next = %v", got)
	}

	u.jump(u.tracks[3])
	fp.pos = 2*time.Minute + time.Second // the length was a little short
	u.saveSession()
	u3, _ := reopen(t, u, a)
	u3.restoreSession()
	if u3.cued == nil || u3.cued.at != 0 {
		t.Errorf("past the end should start from the top, got %+v", u3.cued)
	}
}

// If a song is started before the library is restored, it carries on, and
// the saved Up Next goes before songs queued since. Up Next isn't saved
// over before then.
func TestPlayBeforeRestore(t *testing.T) {
	u, _, a := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[0])
	u.playLater(u.tracks[1])
	u.saveSession()

	u2, fp2 := reopen(t, u, a)
	u2.jump(u2.tracks[3])
	u2.playLater(u2.tracks[4])
	if got := u2.prefs.StringList(prefUpNext); len(got) != 1 {
		t.Errorf("saved Up Next overwritten before restoring: %v", got)
	}
	u2.restoreSession()
	if u2.cued != nil || fp2.last() != "Bohemian Rhapsody" {
		t.Error("the song playing should carry on")
	}
	if got := titles(u2.queued); !slices.Equal(got, []string{"Help", "Zebra"}) {
		t.Errorf("Up Next = %v", got)
	}
}

// Pausing saves the position, in case Mugig doesn't get to quit normally.
func TestPauseSavesPosition(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[2])
	if u.prefs.String(prefResumeSong) != u.tracks[2].Path {
		t.Error("the song should be saved as soon as it starts")
	}
	fp.pos = 42 * time.Second
	u.togglePlay()
	if u.prefs.Float(prefResumeAt) != 42 {
		t.Errorf("saved position %v, want 42", u.prefs.Float(prefResumeAt))
	}
}
