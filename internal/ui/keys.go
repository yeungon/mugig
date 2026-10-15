package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// skipStep is how far the skip forward/back shortcuts move.
const skipStep = 10 * time.Second

// volumeStep is how much the volume shortcuts change the volume.
const volumeStep = 0.1

// trackTable is the track list. Space toggles playback rather than
// re-selecting the focused cell, since the list keeps keyboard focus after
// a row is clicked.
type trackTable struct {
	widget.Table
	onSpace, onEscape, onSelectAll func()
}

func newTrackTable(
	length func() (int, int),
	create func() fyne.CanvasObject,
	update func(widget.TableCellID, fyne.CanvasObject),
	onSpace func(),
) *trackTable {
	t := &trackTable{onSpace: onSpace}
	t.Length, t.CreateCell, t.UpdateCell = length, create, update
	t.ExtendBaseWidget(t)
	return t
}

func (t *trackTable) TypedKey(e *fyne.KeyEvent) {
	switch e.Name {
	case fyne.KeySpace:
		t.onSpace()
	case fyne.KeyEscape:
		t.onEscape()
	default:
		t.Table.TypedKey(e)
	}
}

// TypedShortcut selects every song on ⌘A. It isn't a menu shortcut, which
// would also fire in the search box, where ⌘A selects the text.
func (t *trackTable) TypedShortcut(s fyne.Shortcut) {
	if _, ok := s.(*fyne.ShortcutSelectAll); ok {
		t.onSelectAll()
	}
}

// buildMenu creates the main menu. Its shortcuts work wherever keyboard
// focus is, unlike keys typed into a widget. Space (play/pause) can't be a
// menu shortcut, as it would fire while typing a search, so it is handled by
// the track list and by the window when nothing has focus.
func (u *ui) buildMenu() *fyne.MainMenu {
	cmd := fyne.KeyModifierShortcutDefault // ⌘ on macOS, Ctrl elsewhere
	alt := fyne.KeyModifierAlt
	item := func(label string, key fyne.KeyName, mod fyne.KeyModifier, action func()) *fyne.MenuItem {
		i := fyne.NewMenuItem(label, action)
		if key != "" {
			i.Shortcut = &desktop.CustomShortcut{KeyName: key, Modifier: mod}
		}
		return i
	}
	whilePlaying := func(f func()) func() {
		return func() {
			if u.playing.Path != "" {
				f()
			}
		}
	}

	u.clearQueueItem = item("Clear Up Next", "", 0, u.clearQueue)
	u.clearQueueItem.Disabled = len(u.queued) == 0
	u.shuffleItem = item("Shuffle", "", 0, u.toggleShuffle)
	u.repeatAllItem = item("Repeat All", "", 0, func() { u.setRepeat(repeatAll) })
	u.repeatOneItem = item("Repeat One", "", 0, func() { u.setRepeat(repeatOne) })
	u.checkItem = item("Check Audio Quality…", fyne.KeyI, cmd, u.checkAudioQuality)
	u.checkAllItem = item("Check All Songs", fyne.KeyI, cmd|fyne.KeyModifierShift, u.checkAllSongs)
	outputItems := u.buildOutputMenu()
	u.checkItem.Disabled = true // until a track that can be checked plays

	file := fyne.NewMenu("File",
		item("Open…", fyne.KeyO, cmd, u.openItems),
		item("Add Music Folder…", fyne.KeyO, cmd|fyne.KeyModifierShift, u.chooseFolderToAdd),
		item("Rescan Folders", fyne.KeyR, cmd, u.scanLibrary),
		item(revealLabel, fyne.KeyR, cmd|fyne.KeyModifierShift, whilePlaying(func() { u.revealTrack(u.playing) })),
		fyne.NewMenuItemSeparator(),
		item("Find", fyne.KeyF, cmd, func() {
			u.showVisualizer(false) // the results are in the track list
			u.win.Canvas().Focus(u.search)
		}),
	)
	u.visItem = item("Visualizer", fyne.KeyL, cmd, u.toggleVisualizer)
	u.visModeItems = nil
	for i, name := range visModeNames {
		u.visModeItems = append(u.visModeItems, item(name, "", 0, func() { u.setVisMode(visMode(i)) }))
	}
	u.miniItem = item("Mini Visualizer", "", 0, func() { u.showMiniVisualizer(!u.miniView.Visible()) })
	view := fyne.NewMenu("View", append([]*fyne.MenuItem{u.visItem, u.miniItem, fyne.NewMenuItemSeparator()}, u.visModeItems...)...)
	view.Items = append(view.Items, fyne.NewMenuItemSeparator(), u.buildColumnItems(), u.buildAppearanceMenu())
	controls := fyne.NewMenu("Controls",
		item("Play / Pause (Space)", "", 0, u.togglePlay),
		item("Stop", fyne.KeyPeriod, cmd, u.stop),
		item("Next", fyne.KeyRight, cmd, whilePlaying(u.next)),
		item("Previous", fyne.KeyLeft, cmd, whilePlaying(u.prev)),
		u.clearQueueItem,
		fyne.NewMenuItemSeparator(),
		item("Skip Forward", fyne.KeyRight, cmd|alt, func() { u.skip(skipStep) }),
		item("Skip Back", fyne.KeyLeft, cmd|alt, func() { u.skip(-skipStep) }),
		fyne.NewMenuItemSeparator(),
		item("Volume Up", fyne.KeyUp, cmd, func() { u.volume.SetValue(min(u.volume.Value+volumeStep, 1)) }),
		item("Volume Down", fyne.KeyDown, cmd, func() { u.volume.SetValue(max(u.volume.Value-volumeStep, 0)) }),
		item("Mute", fyne.KeyDown, cmd|alt, u.toggleMute),
		u.buildFadeInItem(),
		u.buildLevellingItem(),
		fyne.NewMenuItemSeparator(),
	)
	controls.Items = append(controls.Items, u.buildSpeedMenu(item)...)
	u.loadEQ()
	controls.Items = append(controls.Items,
		item("Equalizer…", fyne.KeyE, cmd|alt, u.showEqualizer),
		u.buildCrossfeedMenu())
	controls.Items = append(controls.Items,
		fyne.NewMenuItemSeparator(),
		u.shuffleItem, u.repeatAllItem, u.repeatOneItem,
		fyne.NewMenuItemSeparator(),
		u.checkItem,
		u.checkAllItem,
		fyne.NewMenuItemSeparator(),
	)
	controls.Items = append(controls.Items, outputItems...)
	return fyne.NewMainMenu(file, view, controls)
}

// onTypedKey handles keys typed while no widget has focus.
func (u *ui) onTypedKey(e *fyne.KeyEvent) {
	switch {
	case e.Name == fyne.KeySpace:
		u.togglePlay()
	case e.Name == fyne.KeyEscape && u.visView.Visible():
		u.showVisualizer(false)
	case e.Name == fyne.KeyEscape:
		u.clearSelection()
	}
}

// skip moves the playing track forward or back by d.
func (u *ui) skip(d time.Duration) {
	if u.playing.Path != "" {
		u.seekTo(u.player.Position() + d)
	}
}
