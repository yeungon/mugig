package main

import (
	"mugig/internal/library"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// newPlaylistUI is a test UI that saves playlists to a file.
func newPlaylistUI(t *testing.T) (*ui, *fakePlayer, fyne.App, string) {
	t.Helper()
	u, fp, a := newTestUI(t)
	path := filepath.Join(t.TempDir(), "playlists.json")
	u.playlistsPath = path
	setLibrary(u, fiveTracks()...)
	return u, fp, a, path
}

// answerName types name in the dialog on top and presses confirm.
func answerName(t *testing.T, u *ui, name, confirm string) {
	t.Helper()
	top := u.win.Canvas().Overlays().Top()
	var entry *widget.Entry
	visit(top, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok && entry == nil {
			entry = e
		}
	})
	if entry == nil {
		t.Fatal("no name box")
	}
	entry.SetText(name)
	test.Tap(findButton(t, top, confirm))
}

// itemIn finds the item labelled label in m.
func itemIn(t *testing.T, m *fyne.Menu, label string) *fyne.MenuItem {
	t.Helper()
	for _, it := range m.Items {
		if it.Label == label {
			return it
		}
	}
	t.Fatalf("no %q in %v", label, labelsOf(m.Items))
	return nil
}

// Songs are added from their right-click menu, to a new playlist or one
// there is; the playlist lists them in its order and plays in it.
func TestAddSongsToPlaylists(t *testing.T) {
	u, fp, a, path := newPlaylistUI(t)
	if len(u.playlistList.Objects) != 1 || !strings.Contains(allText(u.playlistList), "Add to Playlist") {
		t.Error("with no playlists, the section should say how to make one")
	}

	// Two songs selected, into a new playlist.
	u.selectAll()
	sel := []library.Track{u.tracks[3], u.tracks[1]}
	itemIn(t, u.addToPlaylistItem(sel).ChildMenu, "New Playlist…").Action()
	answerName(t, u, "Road Trip", "Create")
	if len(u.playlists) != 1 || u.playlists[0].Name != "Road Trip" || u.playlist != nil {
		t.Fatalf("playlists %v, shown %v: made from songs, the list should stay put", u.playlists, u.playlist)
	}
	if u.status.Text != "Added 2 songs to Road Trip" {
		t.Errorf("status %q", u.status.Text)
	}
	p := u.playlists[0]
	if row := u.playlistRows[p]; row == nil || row.count.Text != "2" {
		t.Error("the sidebar should list it with its count")
	}

	// One more, and one already in it, to the playlist there is.
	itemIn(t, u.trackMenu(u.tracks[0]), "Add to Playlist").ChildMenu.Items[0].Action()
	itemIn(t, u.addToPlaylistItem([]library.Track{u.tracks[1]}).ChildMenu, "Road Trip").Action()
	if u.status.Text != "Added 0 songs to Road Trip (1 already in it)" {
		t.Errorf("status %q", u.status.Text)
	}

	test.Tap(u.playlistRows[p])
	if u.playlist != p || u.pageTitle.Text != "Road Trip" || u.navSongs.Importance != widget.LowImportance {
		t.Errorf("shown %v, title %q", u.playlist, u.pageTitle.Text)
	}
	if got := titles(u.tracks); !slices.Equal(got, []string{"Bohemian Rhapsody", "Help", "Come Together"}) {
		t.Errorf("listed %v, want the order added", got)
	}
	u.togglePlay()
	fp.finish(t)
	if fp.last() != "Help" {
		t.Errorf("played %q after the first, want the playlist's next", fp.last())
	}

	// Saved, and read back by the next start.
	u2 := newUI(u.win, a.Preferences(), uiDeps{player: &fakePlayer{}, cache: u.cache, playlists: path})
	if len(u2.playlists) != 1 || len(u2.playlists[0].Songs) != 3 {
		t.Errorf("read back %+v", u2.playlists)
	}
}

// In a playlist, songs can be taken out and moved, while it is in its own
// order; the songs stay in the library.
func TestRemoveAndMoveInPlaylist(t *testing.T) {
	u, _, _, _ := newPlaylistUI(t)
	p := &library.Playlist{Name: "Mix"}
	for _, tr := range u.all[:4] {
		p.Add(tr.Path)
	}
	u.playlists = []*library.Playlist{p}
	u.rebuildPlaylistList()
	u.sortCol = -1
	u.showPlaylist(p)

	m := u.rowMenu(u.tracks[0])
	if !itemIn(t, m, "Move Up").Disabled || itemIn(t, m, "Move Down").Disabled {
		t.Error("the first song can move down only")
	}
	itemIn(t, m, "Move Down").Action()
	if got := titles(u.tracks); !slices.Equal(got, []string{"Help", "Come Together", "Hey Jude", "Bohemian Rhapsody"}) {
		t.Errorf("after moving down: %v", got)
	}
	itemIn(t, u.rowMenu(u.tracks[3]), "Remove from Playlist").Action()
	if len(u.tracks) != 3 || len(u.all) != 5 {
		t.Errorf("%d listed, %d in the library", len(u.tracks), len(u.all))
	}

	u.selectAll()
	itemIn(t, u.rowMenu(u.tracks[0]), "Remove from Playlist").Action()
	if len(u.tracks) != 0 || !strings.HasPrefix(u.count.Text, "No songs yet") {
		t.Errorf("%d listed, %q", len(u.tracks), u.count.Text)
	}

	u.sortBy(titleCol)
	p.Add(u.all[0].Path, u.all[1].Path)
	u.applyView()
	if slices.ContainsFunc(u.rowMenu(u.tracks[0]).Items, func(it *fyne.MenuItem) bool { return it.Label == "Move Up" }) {
		t.Error("moving makes no sense while the list is sorted by a column")
	}
	u.setFilter("")
	if slices.ContainsFunc(u.rowMenu(u.tracks[0]).Items, func(it *fyne.MenuItem) bool { return it.Label == "Remove from Playlist" }) {
		t.Error("Remove from Playlist outside a playlist")
	}
}

// A playlist can be renamed, duplicated and deleted; deleting the one
// shown goes back to all songs.
func TestRenameDuplicateDeletePlaylist(t *testing.T) {
	u, _, _, path := newPlaylistUI(t)
	itemIn(t, fyne.NewMenu("", u.addToPlaylistItem(nil).ChildMenu.Items...), "New Playlist…").Action()
	answerName(t, u, "Mix", "Create")
	p := u.playlists[0]
	p.Add(u.all[0].Path)

	itemIn(t, u.playlistMenu(p), "Rename…").Action()
	answerName(t, u, "Favourites", "Rename")
	itemIn(t, u.playlistMenu(p), "Duplicate").Action()
	if len(u.playlists) != 2 || u.playlists[1].Name != "Favourites copy" || len(u.playlists[1].Songs) != 1 {
		t.Fatalf("playlists %+v", u.playlists)
	}

	u.showPlaylist(p)
	itemIn(t, u.playlistMenu(p), "Delete Playlist…").Action()
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Delete"))
	if len(u.playlists) != 1 || u.playlist != nil || u.pageTitle.Text != "Songs" {
		t.Errorf("after deleting: %d playlists, shown %v, title %q", len(u.playlists), u.playlist, u.pageTitle.Text)
	}
	if got := library.LoadPlaylists(path); len(got) != 1 || got[0].Name != "Favourites copy" {
		t.Errorf("saved %+v", got)
	}
}

// A smart playlist is made from rules, counts the songs matching as they
// are typed, and lists them; its rules can be changed later.
func TestSmartPlaylist(t *testing.T) {
	u, _, _, _ := newPlaylistUI(t)
	u.editSmartPlaylist(nil)
	e := u.rulesDialog
	var value *widget.Entry
	visit(e.rows, func(o fyne.CanvasObject) {
		if en, ok := o.(*widget.Entry); ok {
			value = en
		}
	})
	value.SetText("beatles")
	if e.count.Text != "3 songs match" {
		t.Errorf("count %q", e.count.Text)
	}
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Add Rule"))
	var selects []*widget.Select
	visit(e.rows, func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Select); ok {
			selects = append(selects, s)
		}
	})
	selects[2].SetSelected("Year") // the second rule's field
	if e.rules.Rules[1].Op != library.OpIs || e.rules.Rules[1].Value != "" {
		t.Errorf("a new field should reset its comparison: %+v", e.rules.Rules[1])
	}
	selects = selects[:0]
	visit(e.rows, func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Select); ok {
			selects = append(selects, s)
		}
	})
	selects[3].SetSelected(library.OpLess)
	var entries []*widget.Entry
	visit(e.rows, func(o fyne.CanvasObject) {
		if en, ok := o.(*widget.Entry); ok {
			entries = append(entries, en)
		}
	})
	entries[1].SetText("1968")
	if e.count.Text != "1 song matches" {
		t.Errorf("count %q", e.count.Text)
	}
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Create"))
	if len(u.playlists) != 1 || u.playlist != u.playlists[0] || u.playlists[0].Smart == nil {
		t.Fatal("not made and shown")
	}
	if got := titles(u.tracks); !slices.Equal(got, []string{"Help"}) {
		t.Errorf("listed %v", got)
	}
	if row := u.playlistRows[u.playlists[0]]; !strings.HasPrefix(row.detail.Text, "Smart") {
		t.Errorf("row says %q", row.detail.Text)
	}

	// Edit: any rule rather than all.
	p := u.playlists[0]
	itemIn(t, u.playlistMenu(p), "Edit Rules…").Action()
	u.rulesDialog.any.SetSelected("any")
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Save"))
	if got := titles(u.tracks); !slices.Equal(got, []string{"Come Together", "Help", "Hey Jude"}) {
		t.Errorf("after editing: %v", got)
	}
	if slices.ContainsFunc(u.addToPlaylistItem(u.all).ChildMenu.Items, func(it *fyne.MenuItem) bool { return it.Label == p.Name }) {
		t.Error("songs can't be added to a smart playlist")
	}
}

// Up Next can be saved as a playlist, and songs moved to the Trash leave
// the playlists.
func TestQueueToPlaylistAndTrash(t *testing.T) {
	u, _, _, _ := newPlaylistUI(t)
	u.playLater(u.all[4], u.all[2])
	u.saveQueueAsPlaylist()
	answerName(t, u, "Later", "Create")
	p := u.playlists[0]
	if !slices.Equal(p.Songs, []string{u.all[4].Path, u.all[2].Path}) {
		t.Errorf("songs %v", p.Songs)
	}
	u.trash = func(string) error { return nil }
	u.trashSongs([]library.Track{u.all[4]})
	if !slices.Equal(p.Songs, []string{"/music/Hey Jude.mp3"}) {
		t.Errorf("after the Trash: %v", p.Songs)
	}
}
