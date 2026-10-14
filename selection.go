package main

// Selecting several songs: ⌘-click (Ctrl-click on Windows and Linux) adds
// or removes a song, ⇧-click selects the songs between the last one clicked
// and this one, and ⌘A selects the whole list. A plain click still plays
// the song, and clears the selection. Right-clicking a selected song acts
// on all of them: play them, queue them, check them, or copy their paths.
// The line under the title totals the selection.

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// keyModifiers returns the modifier keys held down now.
func keyModifiers() fyne.KeyModifier {
	if d, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		return d.CurrentKeyModifiers()
	}
	return 0
}

// clickTrack handles a click on a song's row.
func (u *ui) clickTrack(t Track, at fyne.Position) {
	switch mods := u.modifiers(); {
	case mods&fyne.KeyModifierShortcutDefault != 0:
		u.toggleSelected(t)
	case mods&fyne.KeyModifierShift != 0:
		u.selectRange(t)
	default:
		u.clearSelection()
		u.tapTable(at) // plays it
	}
}

// toggleSelected adds t to the selection, or takes it out.
func (u *ui) toggleSelected(t Track) {
	if u.selected[t.Path] {
		delete(u.selected, t.Path)
	} else {
		u.selected[t.Path] = true
	}
	u.anchor = t.Path
	u.selectionChanged()
}

// selectRange selects the songs from the last one clicked to t, as listed.
// With none clicked yet, it starts at the playing song, or the top.
func (u *ui) selectRange(t Track) {
	from := u.indexOf(u.anchor)
	if from < 0 {
		from = max(u.playingIndex(), 0)
	}
	to := u.indexOf(t.Path)
	if to < 0 {
		return
	}
	clear(u.selected)
	for i := min(from, to); i <= max(from, to); i++ {
		u.selected[u.tracks[i].Path] = true
	}
	u.selectionChanged()
}

// selectAll selects every song listed.
func (u *ui) selectAll() {
	for _, t := range u.tracks {
		u.selected[t.Path] = true
	}
	u.selectionChanged()
}

func (u *ui) clearSelection() {
	if len(u.selected) == 0 {
		return
	}
	clear(u.selected)
	u.anchor = ""
	u.selectionChanged()
}

// pruneSelection drops songs that are no longer listed, after a search or
// a change of folder, so actions only touch songs that can be seen.
func (u *ui) pruneSelection() {
	if len(u.selected) == 0 {
		return
	}
	listed := make(map[string]bool, len(u.tracks))
	for _, t := range u.tracks {
		listed[t.Path] = true
	}
	for p := range u.selected {
		if !listed[p] {
			delete(u.selected, p)
		}
	}
}

func (u *ui) selectionChanged() {
	u.table.Refresh()
	u.updateCount()
}

// selectedTracks returns the selected songs in list order.
func (u *ui) selectedTracks() []Track {
	var ts []Track
	for _, t := range u.tracks {
		if u.selected[t.Path] {
			ts = append(ts, t)
		}
	}
	return ts
}

// rowMenu is the right-click menu for t's row: for all the selected songs
// if t is one of several selected, otherwise for t alone, which then
// becomes the only song selected, as in Finder.
func (u *ui) rowMenu(t Track) *fyne.Menu {
	if u.selected[t.Path] && len(u.selected) > 1 {
		return u.songsMenu(u.selectedTracks())
	}
	u.clearSelection()
	return u.trackMenu(t)
}

// songsMenu is the right-click menu for several songs.
func (u *ui) songsMenu(ts []Track) *fyne.Menu {
	return fyne.NewMenu("",
		fyne.NewMenuItem("Play", func() {
			u.playNext(ts[1:]...)
			u.jump(ts[0])
		}),
		fyne.NewMenuItem("Play Next", func() { u.playNext(ts...) }),
		fyne.NewMenuItem("Play Later", func() { u.playLater(ts...) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Check Audio Quality", func() { u.checkSongs(ts) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(revealLabel, func() { u.revealTrack(ts[0]) }),
		fyne.NewMenuItem("Copy File Paths", func() {
			paths := make([]string, len(ts))
			for i, t := range ts {
				paths[i] = t.Path
			}
			fyne.CurrentApp().Clipboard().SetContent(strings.Join(paths, "\n"))
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(trashLabel, func() { u.confirmTrash(ts) }),
	)
}
