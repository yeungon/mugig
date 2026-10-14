package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// clickRow clicks a cell of row with the given modifier keys held.
func clickRow(u *ui, row int, mods fyne.KeyModifier) *trackCell {
	u.modifiers = func() fyne.KeyModifier { return mods }
	defer func() { u.modifiers = func() fyne.KeyModifier { return 0 } }()
	cell := u.table.CreateCell().(*trackCell)
	u.table.UpdateCell(widget.TableCellID{Row: row, Col: albumCol}, cell)
	test.Tap(cell)
	return cell
}

// cellAt returns row's cell as it is drawn now.
func cellAt(u *ui, row int) *trackCell {
	cell := u.table.CreateCell().(*trackCell)
	u.table.UpdateCell(widget.TableCellID{Row: row, Col: titleCol}, cell)
	return cell
}

const cmdKey = fyne.KeyModifierShortcutDefault

// ⌘-click adds and removes songs without playing them; the rows are
// highlighted and the line under the title totals them.
func TestCommandClickSelects(t *testing.T) {
	u, fp, _ := newTestUI(t)
	songs := fiveTracks()
	for i := range songs {
		songs[i].Length, songs[i].Size = time.Minute, 2_000_000
	}
	setLibrary(u, songs...)
	clickRow(u, 1, cmdKey)
	clickRow(u, 3, cmdKey)
	if len(fp.played) != 0 {
		t.Error("⌘-click shouldn't play")
	}
	if got := titles(u.selectedTracks()); !slices.Equal(got, []string{"Help", "Bohemian Rhapsody"}) {
		t.Errorf("selected %v", got)
	}
	if !cellAt(u, 1).bg.Visible() || cellAt(u, 2).bg.Visible() {
		t.Error("only selected rows should be highlighted")
	}
	if u.count.Text != "2 songs selected · 2 min · 4.00 MB" {
		t.Errorf("stats %q", u.count.Text)
	}
	clickRow(u, 1, cmdKey)
	if got := titles(u.selectedTracks()); !slices.Equal(got, []string{"Bohemian Rhapsody"}) {
		t.Errorf("⌘-click again should deselect: %v", got)
	}
}

// ⇧-click selects from the last song clicked, either way up the list.
func TestShiftClickSelectsRange(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	clickRow(u, 3, cmdKey)
	clickRow(u, 1, fyne.KeyModifierShift)
	if got := titles(u.selectedTracks()); !slices.Equal(got, []string{"Help", "Hey Jude", "Bohemian Rhapsody"}) {
		t.Errorf("selected %v", got)
	}
	clickRow(u, 4, fyne.KeyModifierShift) // from the same anchor
	if got := titles(u.selectedTracks()); !slices.Equal(got, []string{"Bohemian Rhapsody", "Zebra"}) {
		t.Errorf("selected %v", got)
	}
	if len(fp.played) != 0 {
		t.Error("⇧-click shouldn't play")
	}

	u.clearSelection()
	clickRow(u, 2, fyne.KeyModifierShift) // no anchor: from the top
	if got := titles(u.selectedTracks()); !slices.Equal(got, []string{"Come Together", "Help", "Hey Jude"}) {
		t.Errorf("selected %v", got)
	}
}

// A plain click plays the song and clears the selection.
func TestPlainClickPlaysAndDeselects(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	clickRow(u, 1, cmdKey)
	clickRow(u, 2, cmdKey)
	u.win.Resize(fyne.NewSize(1100, 700))
	var zebra *trackCell // as drawn in the window, so the click lands on its row
	visit(u.win.Canvas().Content(), func(o fyne.CanvasObject) {
		if c, ok := o.(*trackCell); ok && c.seg.Text == "Zebra" {
			zebra = c
		}
	})
	if zebra == nil {
		t.Fatal("Zebra's row isn't drawn")
	}
	test.Tap(zebra)
	if fp.last() != "Zebra" || len(u.selected) != 0 {
		t.Errorf("played %q, %d still selected", fp.last(), len(u.selected))
	}
	if !strings.HasPrefix(u.count.Text, "5 tracks") {
		t.Errorf("stats %q should be back to the library's", u.count.Text)
	}
}

// Right-clicking one of several selected songs offers actions on all of
// them; right-clicking another song selects just that one.
func TestMenuForSelectedSongs(t *testing.T) {
	u, fp, a := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	clickRow(u, 4, cmdKey)
	clickRow(u, 0, cmdKey)
	clickRow(u, 2, cmdKey)

	m := u.rowMenu(u.tracks[2])
	var labels []string
	for _, it := range m.Items {
		labels = append(labels, it.Label)
	}
	if want := "Play|Play Next|Play Later||Check Audio Quality||" + revealLabel + "|Copy File Paths||" + trashLabel; strings.Join(labels, "|") != want {
		t.Fatalf("menu %q", labels)
	}
	action := func(m *fyne.Menu, label string) {
		t.Helper()
		i := slices.IndexFunc(m.Items, func(it *fyne.MenuItem) bool { return it.Label == label })
		if i < 0 {
			t.Fatalf("no %q", label)
		}
		m.Items[i].Action()
	}

	action(m, "Play Later")
	if got := titles(u.queued); !slices.Equal(got, []string{"Come Together", "Hey Jude", "Zebra"}) {
		t.Errorf("Play Later queued %v, want list order", got)
	}
	u.clearQueue()
	action(m, "Play")
	if fp.last() != "Come Together" || !slices.Equal(titles(u.queued), []string{"Hey Jude", "Zebra"}) {
		t.Errorf("Play: playing %q, Up Next %v", fp.last(), titles(u.queued))
	}
	action(m, "Copy File Paths")
	if got := a.Clipboard().Content(); got != "/music/Come Together.mp3\n/music/Hey Jude.mp3\n/music/Zebra.mp3" {
		t.Errorf("clipboard %q", got)
	}
	var shown string
	u.reveal = func(p string) error { shown = p; return nil }
	action(m, revealLabel)
	if shown != "" {
		t.Errorf("revealed %q for songs that aren't on disk", shown)
	}

	m = u.rowMenu(u.tracks[1]) // not selected
	if m.Items[0].Label != "Play" || len(u.selected) != 0 || slices.ContainsFunc(m.Items, func(it *fyne.MenuItem) bool { return it.Label == "Copy File Paths" }) {
		t.Error("right-clicking an unselected song should be about that song alone")
	}
}

// ⌘A selects every song listed, Esc clears; a search leaves only the
// songs still listed selected.
func TestSelectAllAndSearch(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.table.TypedShortcut(&fyne.ShortcutSelectAll{})
	if len(u.selected) != 5 {
		t.Fatalf("⌘A selected %d", len(u.selected))
	}
	u.search.SetText("queen")
	if got := titles(u.selectedTracks()); len(u.selected) != 1 || !slices.Equal(got, []string{"Bohemian Rhapsody"}) {
		t.Errorf("after search: %v", got)
	}
	u.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if len(u.selected) != 0 {
		t.Error("Esc should clear the selection")
	}
	u.search.SetText("")
	u.onTypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape}) // nothing focused: harmless
}

// Check Audio Quality on a selection checks its lossless songs.
func TestCheckSelectedSongs(t *testing.T) {
	u, _, _ := newTestUI(t)
	songs := fiveTracks()
	songs[0].Format, songs[3].Format = "FLAC", "WAV"
	setLibrary(u, songs...)
	u.selectAll()
	u.songsMenu(u.selectedTracks()).Items[4].Action()
	if u.batch == nil || u.batch.total != 2 {
		t.Fatalf("batch %+v, want 2 lossless songs", u.batch)
	}
}
