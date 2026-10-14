package main

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// Play Next goes before songs already waiting, Play Later after them, and
// then the list carries on from where it was.
func TestPlayNextAndPlayLater(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	u.jump(u.tracks[0]) // Come Together
	u.playLater(u.tracks[3])
	u.playNext(u.tracks[4])
	if !isToggledOn(u.queueBtn) || u.clearQueueItem.Disabled {
		t.Error("the queue button and Clear Up Next should show songs are waiting")
	}
	for range 3 {
		fp.finish(t)
	}
	want := []string{"Come Together", "Zebra", "Bohemian Rhapsody", "Help"}
	if got := titles(tracksFor(u, fp.played)); !slices.Equal(got, want) {
		t.Errorf("played %v, want %v", got, want)
	}
	if isToggledOn(u.queueBtn) || !u.clearQueueItem.Disabled {
		t.Error("with Up Next empty, the queue button and menu item should be off")
	}
}

// Next takes the first song from Up Next, as a song ending does.
func TestNextPlaysUpNext(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[1]) // Help
	u.playNext(u.tracks[4])
	u.next()
	u.next()
	if want := []string{"Help", "Zebra", "Hey Jude"}; !slices.Equal(titles(tracksFor(u, fp.played)), want) {
		t.Errorf("played %v, want %v", titles(tracksFor(u, fp.played)), want)
	}
}

// With nothing playing, Play starts Up Next before the list.
func TestPlayStartsUpNext(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.playNext(u.tracks[2])
	u.togglePlay()
	fp.finish(t)
	if want := []string{"Hey Jude", "Come Together"}; !slices.Equal(titles(tracksFor(u, fp.played)), want) {
		t.Errorf("played %v, want %v (the list from the top)", titles(tracksFor(u, fp.played)), want)
	}
}

// Previous, during a song from Up Next, goes back to the list's song.
func TestPreviousFromUpNext(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[1]) // Help
	u.playNext(u.tracks[4])
	u.next()
	u.prev()
	if fp.last() != "Help" {
		t.Errorf("previous played %q, want Help", fp.last())
	}
	fp.finish(t)
	if fp.last() != "Hey Jude" {
		t.Errorf("then %q, want Hey Jude", fp.last())
	}
}

// Repeat One repeats a song from Up Next, and the list still carries on
// from its own place afterwards.
func TestRepeatOneOnUpNextSong(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[0])
	u.playNext(u.tracks[4])
	u.setRepeat(repeatOne)
	u.next()               // Zebra
	fp.finish(t)           // Zebra again
	u.setRepeat(repeatOne) // off
	fp.finish(t)
	want := []string{"Come Together", "Zebra", "Zebra", "Help"}
	if got := titles(tracksFor(u, fp.played)); !slices.Equal(got, want) {
		t.Errorf("played %v, want %v", got, want)
	}
}

// Shuffle plays Up Next first too, and doesn't play those songs again in
// the same round.
func TestShuffleWithUpNext(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.toggleShuffle()
	u.togglePlay()
	first := u.playing
	var other Track
	for _, tr := range u.tracks {
		if tr.Path != first.Path {
			other = tr
			break
		}
	}
	u.playNext(other)
	fp.finish(t)
	if u.playing.Path != other.Path {
		t.Fatalf("played %q, want %q from Up Next", u.playing.Title, other.Title)
	}
	for range 3 {
		fp.finish(t)
	}
	if uniq(fp.played) != 5 || len(fp.played) != 5 {
		t.Errorf("played %v: want all 5 once", fp.played)
	}
}

// A song whose folder was removed from the library is skipped.
func TestUpNextSkipsSongsNoLongerInLibrary(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[0])
	gone := u.tracks[3]
	u.playNext(gone)
	u.playLater(u.tracks[4])
	setLibrary(u, slices.DeleteFunc(slices.Clone(u.all), func(tr Track) bool { return tr.Path == gone.Path })...)
	fp.finish(t)
	if fp.last() != "Zebra" {
		t.Errorf("played %q, want Zebra (Bohemian Rhapsody is gone)", fp.last())
	}
}

// The Up Next list shows the songs waiting; a click plays one now, the ×
// takes one out, and Clear empties it.
func TestUpNextList(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.win.Resize(fyne.NewSize(1100, 700))

	u.showQueue()
	if !u.queuePop.Visible() || findList(u.queuePop.Content) != nil {
		t.Fatal("an empty Up Next should show a hint, not a list")
	}
	u.queuePop.Hide()

	u.playLater(u.tracks[1])
	u.playLater(u.tracks[2])
	u.playLater(u.tracks[3])
	u.showQueue()
	list := findList(u.queuePop.Content)
	if list == nil || list.Length() != 3 {
		t.Fatal("Up Next should list 3 songs")
	}
	row := newQueueRow()
	list.UpdateItem(0, row)
	if row.title.Text != "Help" || row.detail.Text != "Beatles" {
		t.Errorf("row shows %q / %q", row.title.Text, row.detail.Text)
	}
	test.Tap(row.remove) // Help
	if got := titles(u.queued); !slices.Equal(got, []string{"Hey Jude", "Bohemian Rhapsody"}) {
		t.Errorf("after removing: %v", got)
	}

	list = findList(u.queuePop.Content) // rebuilt
	list.Select(1)
	if fp.last() != "Bohemian Rhapsody" || u.queuePop.Visible() {
		t.Errorf("clicking a song should play it now and close the list; played %q", fp.last())
	}
	if got := titles(u.queued); !slices.Equal(got, []string{"Hey Jude"}) {
		t.Errorf("left waiting: %v", got)
	}

	u.showQueue()
	u.clearQueue()
	if len(u.queued) != 0 || findList(u.queuePop.Content) != nil {
		t.Error("Clear should empty Up Next")
	}
}

// The song menu offers Play Next and Play Later.
func TestTrackMenuQueues(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	m := u.trackMenu(u.tracks[2])
	for _, label := range []string{"Play Next", "Play Later"} {
		i := slices.IndexFunc(m.Items, func(it *fyne.MenuItem) bool { return it.Label == label })
		if i < 0 {
			t.Fatalf("no %s in %v", label, m.Items)
		}
		m.Items[i].Action()
	}
	if got := titles(u.queued); !slices.Equal(got, []string{"Hey Jude", "Hey Jude"}) {
		t.Errorf("Up Next = %v", got)
	}
}

// findList returns the first list in o, or nil.
func findList(o fyne.CanvasObject) *widget.List {
	switch o := o.(type) {
	case *widget.List:
		return o
	case *fyne.Container:
		for _, c := range o.Objects {
			if l := findList(c); l != nil {
				return l
			}
		}
	}
	return nil
}
