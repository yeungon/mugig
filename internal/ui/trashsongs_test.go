package ui

import (
	"errors"
	"mugig/internal/library"
	"mugig/internal/quality"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

// Moving songs to the Trash, once confirmed, takes them out of the
// library, Up Next, the selection and the quality results; a song playing
// stops first.
func TestMoveSongsToTrash(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	var moved []string
	u.trash = func(p string) error { moved = append(moved, p); return nil }
	u.jump(u.tracks[1]) // Help
	u.playLater(u.tracks[3], u.tracks[4])
	u.levels[u.tracks[3].Path] = quality.Suspect
	u.selectAll()

	ts := []library.Track{u.tracks[1], u.tracks[3]}
	u.confirmTrash(ts)
	if len(moved) != 0 {
		t.Fatal("nothing should move before it is confirmed")
	}
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), strings.TrimSuffix(trashLabel, "…")))

	if !slices.Equal(moved, []string{"/music/Help.mp3", "/music/Bohemian Rhapsody.mp3"}) {
		t.Errorf("moved %v", moved)
	}
	if got := titles(u.tracks); !slices.Equal(got, []string{"Come Together", "Hey Jude", "Zebra"}) {
		t.Errorf("listed %v", got)
	}
	if got := titles(u.queued); !slices.Equal(got, []string{"Zebra"}) {
		t.Errorf("Up Next %v", got)
	}
	if len(u.selected) != 3 || u.levels[ts[1].Path] != quality.Unknown {
		t.Errorf("%d selected, level %v", len(u.selected), u.levels[ts[1].Path])
	}
	if u.playing.Path != "" || fp.onEnd != nil {
		t.Error("the song playing should have stopped")
	}
	if u.status.Text != "Moved 2 songs to "+trashName() {
		t.Errorf("status %q", u.status.Text)
	}
}

// Cancel moves nothing; a file the Trash refuses stays in the library and
// the reason is shown.
func TestTrashCancelledOrRefused(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.trash = func(p string) error {
		if strings.Contains(p, "Zebra") {
			return errors.New("the disk is read-only")
		}
		return nil
	}
	u.confirmTrash([]library.Track{u.tracks[0]})
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Cancel"))
	if len(u.all) != 5 {
		t.Fatal("Cancel should move nothing")
	}

	u.trashSongs([]library.Track{u.tracks[0], u.tracks[4]})
	if got := titles(u.tracks); !slices.Equal(got, []string{"Help", "Hey Jude", "Bohemian Rhapsody", "Zebra"}) {
		t.Errorf("listed %v", got)
	}
	top := u.win.Canvas().Overlays().Top()
	if top == nil || !strings.Contains(allText(top), "the disk is read-only") {
		t.Errorf("the reason should be shown")
	}
}
