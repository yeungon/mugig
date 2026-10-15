package main

// Move to Trash, from a song's right-click menu: for files that failed the
// quality check, or that aren't wanted. The files go to the system's
// Trash (on a USB disk, that disk's own), from where they can be put back,
// and leave the library.

import (
	"fmt"
	"mugig/internal/audio"
	"mugig/internal/humanize"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// trashLabel is the menu item: Windows calls its Trash the Recycle Bin.
var trashLabel = map[bool]string{true: "Move to Recycle Bin…", false: "Move to Trash…"}[runtime.GOOS == "windows"]

// trashName is the system's name for its Trash.
func trashName() string {
	if runtime.GOOS == "windows" {
		return "the Recycle Bin"
	}
	return "the Trash"
}

// trashItem is Move to Trash for songs. It is off for songs from a CUE
// sheet: their file is the whole disc image, with the album's other songs.
func (u *ui) trashItem(ts []Track) *fyne.MenuItem {
	item := fyne.NewMenuItem(trashLabel, func() { u.confirmTrash(ts) })
	item.Disabled = slices.ContainsFunc(ts, func(t Track) bool { return audio.FromCue(t.Path) })
	return item
}

// confirmTrash asks before moving songs to the Trash.
func (u *ui) confirmTrash(ts []Track) {
	title := fmt.Sprintf("Move %s to %s?", humanize.Plural(len(ts), "song"), trashName())
	if len(ts) == 1 {
		title = fmt.Sprintf("Move “%s” to %s?", ts[0].Title, trashName())
	}
	text := widget.NewLabel(fmt.Sprintf("The files will leave the library. You can put them back from %s.", trashName()))
	if len(ts) == 1 {
		text.SetText(fmt.Sprintf("The file will leave the library. You can put it back from %s.", trashName()))
	}
	text.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm(title, strings.TrimSuffix(trashLabel, "…"), "Cancel", text, func(ok bool) {
		if ok {
			u.trashSongs(ts)
		}
	}, u.win)
	d.SetConfirmImportance(widget.DangerImportance)
	d.Resize(fyne.NewSize(420, d.MinSize().Height))
	d.Show()
}

// trashSongs moves the songs' files to the Trash, then takes those that
// went out of the library. A song playing stops first, so its file isn't
// held open.
func (u *ui) trashSongs(ts []Track) {
	if slices.ContainsFunc(ts, func(t Track) bool { return t.Path == u.playing.Path }) {
		u.stop()
	}
	if u.cued != nil && slices.ContainsFunc(ts, func(t Track) bool { return t.Path == u.cued.track.Path }) {
		u.setStopped()
	}
	move := u.trash
	u.async(func() {
		var gone []string
		var failed []string
		for _, t := range ts {
			if err := move(t.Path); err != nil {
				failed = append(failed, fmt.Sprintf("%s: %s", filepath.Base(t.Path), err))
				continue
			}
			gone = append(gone, t.Path)
		}
		onUI(func() {
			u.dropSongs(gone)
			if len(failed) > 0 {
				dialog.ShowError(fmt.Errorf("Couldn't move %s to %s:\n%s",
					humanize.Plural(len(failed), "song"), trashName(), strings.Join(failed, "\n")), u.win)
			}
		})
	})
}

// dropSongs takes songs whose files are gone out of the library, Up Next,
// the selection, the cache and the saved quality results.
func (u *ui) dropSongs(paths []string) {
	if len(paths) == 0 {
		return
	}
	gone := make(map[string]bool, len(paths))
	for _, p := range paths {
		gone[p] = true
		delete(u.selected, p)
		delete(u.levels, p)
	}
	u.all = slices.DeleteFunc(u.all, func(t Track) bool { return gone[t.Path] })
	u.queued = slices.DeleteFunc(u.queued, func(t Track) bool { return gone[t.Path] })
	u.queueChanged()
	u.forgetInPlaylists(paths)
	u.applyView()
	u.status.SetText(fmt.Sprintf("Moved %s to %s", humanize.Plural(len(paths), "song"), trashName()))
	cache, store := u.cache, u.qualityStore
	u.async(func() {
		cache.forget(paths)
		if err := cache.save(); err != nil {
			fyne.LogError("saving library cache", err)
		}
		store.Forget(paths)
		store.Save()
	})
}
