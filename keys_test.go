package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

func TestSpaceTogglesPlayback(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	space := &fyne.KeyEvent{Name: fyne.KeySpace}

	u.table.TypedKey(space) // track list focused
	if fp.last() != "Come Together" {
		t.Fatal("space in the track list should start playback")
	}
	u.table.TypedKey(space)
	if !fp.paused {
		t.Error("space again should pause")
	}
	u.onTypedKey(space) // nothing focused
	if fp.paused {
		t.Error("space with nothing focused should resume")
	}
	u.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown}) // still reaches the table
	u.onTypedKey(&fyne.KeyEvent{Name: fyne.KeyA})
	if fp.paused || len(fp.played) != 1 {
		t.Error("other keys should not affect playback")
	}
}

func TestMenuShortcuts(t *testing.T) {
	u, _, _ := newTestUI(t)
	cmd, alt := fyne.KeyModifierShortcutDefault, fyne.KeyModifierAlt
	for _, c := range []struct {
		label string
		key   fyne.KeyName
		mod   fyne.KeyModifier
	}{
		{"Open…", fyne.KeyO, cmd},
		{"Add Music Folder…", fyne.KeyO, cmd | fyne.KeyModifierShift},
		{"Find", fyne.KeyF, cmd},
		{"Next", fyne.KeyRight, cmd},
		{"Previous", fyne.KeyLeft, cmd},
		{"Skip Forward", fyne.KeyRight, cmd | alt},
		{"Volume Up", fyne.KeyUp, cmd},
		{"Mute", fyne.KeyDown, cmd | alt},
	} {
		sc, ok := menuItem(t, u, c.label).Shortcut.(*desktop.CustomShortcut)
		if !ok || sc.KeyName != c.key || sc.Modifier != c.mod {
			t.Errorf("%s shortcut = %#v", c.label, menuItem(t, u, c.label).Shortcut)
		}
	}
	// Each shortcut must be unique, or one would shadow another.
	seen := map[string]string{}
	for _, m := range u.mainMenu.Items {
		for _, i := range m.Items {
			if i.Shortcut != nil {
				if other, dup := seen[i.Shortcut.ShortcutName()]; dup {
					t.Errorf("%s and %s share a shortcut", other, i.Label)
				}
				seen[i.Shortcut.ShortcutName()] = i.Label
			}
		}
	}
}

func TestMenuActions(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	run := func(label string) { menuItem(t, u, label).Action() }

	run("Next")
	if len(fp.played) != 0 {
		t.Error("Next with nothing playing should do nothing")
	}
	run("Play / Pause (Space)")
	run("Skip Forward")
	run("Skip Forward")
	if fp.pos != 20*time.Second {
		t.Errorf("skip forward twice: at %v", fp.pos)
	}
	run("Skip Back")
	run("Skip Back")
	run("Skip Back")
	if fp.pos != 0 {
		t.Errorf("skip back past start: at %v", fp.pos)
	}

	run("Volume Up")
	if fp.level < 0.89 || u.prefs.Float(prefVolume) != u.volume.Value {
		t.Errorf("volume up: %v (saved %v)", fp.level, u.prefs.Float(prefVolume))
	}
	for range 15 {
		run("Volume Down")
	}
	if fp.level != 0 {
		t.Errorf("volume down should stop at 0, got %v", fp.level)
	}

	run("Shuffle")
	if !u.shuffle || !menuItem(t, u, "Shuffle").Checked {
		t.Error("Shuffle menu item should turn shuffle on and be checked")
	}
	run("Repeat One")
	if u.repeat != repeatOne || !menuItem(t, u, "Repeat One").Checked || menuItem(t, u, "Repeat All").Checked {
		t.Error("Repeat One should be the only repeat item checked")
	}
	run("Repeat One")
	if u.repeat != repeatOff || menuItem(t, u, "Repeat One").Checked {
		t.Error("choosing Repeat One again should turn repeat off")
	}
	u.cycleRepeat() // the button keeps the menu in step
	if !menuItem(t, u, "Repeat All").Checked {
		t.Error("repeat button should update the menu")
	}

	run("Find")
	if u.win.Canvas().Focused() != u.search {
		t.Error("Find should focus the search box")
	}
	run("Stop")
	if u.playing.Path != "" {
		t.Error("Stop should stop")
	}
}

func menuItem(t *testing.T, u *ui, label string) *fyne.MenuItem {
	t.Helper()
	for _, m := range u.mainMenu.Items {
		for _, i := range m.Items {
			if i.Label == label {
				return i
			}
		}
	}
	t.Fatalf("no menu item %q", label)
	return nil
}

// clickAt clicks like the desktop driver does: the innermost object under
// pos that handles any kind of click (or focus) gets the click, and it only
// reacts if it is Tappable. Fyne's test.TapCanvas looks for Tappable
// objects alone, so it misses a widget that swallows clicks.
func clickAt(c fyne.Canvas, pos fyne.Position) {
	var hit fyne.CanvasObject
	var hitPos fyne.Position
	var walk func(o fyne.CanvasObject, origin fyne.Position)
	walk = func(o fyne.CanvasObject, origin fyne.Position) {
		if !o.Visible() {
			return
		}
		at := origin.Add(o.Position())
		rel := pos.Subtract(at)
		if rel.X < 0 || rel.Y < 0 || rel.X >= o.Size().Width || rel.Y >= o.Size().Height {
			return
		}
		switch o.(type) {
		case fyne.Tappable, fyne.SecondaryTappable, fyne.DoubleTappable, fyne.Focusable, desktop.Mouseable:
			hit, hitPos = o, rel
		}
		switch x := o.(type) {
		case *fyne.Container:
			for _, child := range x.Objects {
				walk(child, at)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(x).Objects() {
				walk(child, at)
			}
		}
	}
	walk(c.Content(), fyne.Position{})
	if tp, ok := hit.(fyne.Tappable); ok {
		tp.Tapped(&fyne.PointEvent{Position: hitPos, AbsolutePosition: pos})
	}
}

// Clicking a song in the list plays it, through the real click routing.
func TestClickingSongPlaysIt(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.win.Resize(fyne.NewSize(1000, 600))

	var cell *trackCell
	visit(u.table, func(o fyne.CanvasObject) {
		if c, ok := o.(*trackCell); ok && c.seg.Text == "Hey Jude" && c.Visible() {
			cell = c
		}
	})
	if cell == nil {
		t.Fatal("no cell shows Hey Jude")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(cell).Add(fyne.NewPos(10, 10))
	clickAt(u.win.Canvas(), at)
	if len(fp.played) != 1 || u.playing.Title != "Hey Jude" {
		t.Errorf("clicking Hey Jude played %q", fp.played)
	}
}

// The sidebar's buttons work through the real click routing too.
func TestClickingSidebar(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.win.Resize(fyne.NewSize(1000, 600))
	click := func(o fyne.CanvasObject) {
		clickAt(u.win.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(o).Add(fyne.NewPos(10, 10)))
	}
	click(u.navNowPlaying)
	if !u.visView.Visible() {
		t.Error("Now Playing didn't open")
	}
	click(u.navSongs)
	if u.visView.Visible() || !u.songsPage.Visible() {
		t.Error("Songs didn't open")
	}
}
