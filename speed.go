package main

// The playback speed control: a button in the player bar showing the speed,
// which opens the choice of speeds, and the same choices, with Faster and
// Slower, in the Controls menu. The speed stays as chosen, across songs and
// sessions, until changed. The pitch is kept (see stretch.go).

import (
	"fmt"
	"mugig/internal/audio"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

const prefSpeed = "playbackSpeed"

func speedLabel(s float64) string { return fmt.Sprintf("%g×", s) }

// buildSpeedMenu creates the Controls menu's speed items: Faster, Slower,
// and the Speed submenu.
func (u *ui) buildSpeedMenu(item func(string, fyne.KeyName, fyne.KeyModifier, func()) *fyne.MenuItem) []*fyne.MenuItem {
	cmd := fyne.KeyModifierShortcutDefault
	u.speedItems = nil
	for _, s := range audio.Speeds {
		label := speedLabel(s)
		if s == 1 {
			label += " (Normal)"
		}
		u.speedItems = append(u.speedItems, fyne.NewMenuItem(label, func() { u.setSpeed(s) }))
	}
	sub := fyne.NewMenuItem("Speed", nil)
	sub.ChildMenu = fyne.NewMenu("", u.speedItems...)
	return []*fyne.MenuItem{
		item("Faster", fyne.KeyRightBracket, cmd, func() { u.stepSpeed(1) }),
		item("Slower", fyne.KeyLeftBracket, cmd, func() { u.stepSpeed(-1) }),
		sub,
	}
}

// buildSpeedButton creates the player bar's speed button, and applies the
// speed last chosen.
func (u *ui) buildSpeedButton() *widget.Button {
	u.speedBtn = widget.NewButton("", u.showSpeedChoices)
	u.speed = nearest(audio.Speeds, u.prefs.FloatWithFallback(prefSpeed, 1))
	u.showSpeed()
	return u.speedBtn
}

// showSpeedChoices opens the list of speeds over the button.
func (u *ui) showSpeedChoices() {
	menu := fyne.NewMenu("", u.speedItems...)
	c := fyne.CurrentApp().Driver().CanvasForObject(u.speedBtn)
	if c == nil {
		return
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(u.speedBtn)
	pop := widget.NewPopUpMenu(menu, c)
	// Above the button: the player bar is at the bottom of the window.
	pop.ShowAtPosition(at.SubtractXY(0, pop.MinSize().Height))
}

// setSpeed changes the playback speed and remembers it.
func (u *ui) setSpeed(s float64) {
	u.speed = s
	u.prefs.SetFloat(prefSpeed, s)
	u.showSpeed()
	u.updateOutputInfo()
	u.publishNowPlaying()
}

// stepSpeed moves to the next speed up (by 1) or down (by -1).
func (u *ui) stepSpeed(by int) {
	i := slices.Index(audio.Speeds, u.speed) + by
	if i >= 0 && i < len(audio.Speeds) {
		u.setSpeed(audio.Speeds[i])
	}
}

// showSpeed applies the speed to the player and shows it on the button and
// in the menu. The button stands out while the speed isn't normal.
func (u *ui) showSpeed() {
	u.player.SetSpeed(u.speed)
	u.speedBtn.SetText(speedLabel(u.speed))
	u.speedBtn.Importance = widget.LowImportance
	if u.speed != 1 {
		u.speedBtn.Importance = widget.HighImportance
	}
	u.speedBtn.Refresh()
	for i, it := range u.speedItems {
		it.Checked = audio.Speeds[i] == u.speed
	}
	if u.mainMenu != nil {
		u.mainMenu.Refresh()
	}
}
