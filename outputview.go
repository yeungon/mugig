package main

// Choosing where sound goes, and showing whether it reaches the device
// unconverted.

import (
	"fmt"

	"fyne.io/fyne/v2"
)

const (
	prefOutputDevice = "outputDevice"    // chosen device's ID, "" for the system default
	prefMatchRate    = "matchSampleRate" // switch the device to each track's rate
)

// buildOutputMenu creates the Output Device submenu and the Match Output
// Sample Rate item, from the saved choices.
func (u *ui) buildOutputMenu() (devices, match *fyne.MenuItem) {
	u.outputItem = fyne.NewMenuItem("Output Device", nil)
	u.outputItem.ChildMenu = fyne.NewMenu("")
	u.matchRateItem = fyne.NewMenuItem("Match Output Sample Rate", func() {
		u.setMatchRate(!u.matchRateItem.Checked)
	})
	u.matchRateItem.Checked = u.prefs.BoolWithFallback(prefMatchRate, true)
	return u.outputItem, u.matchRateItem
}

// applyOutputPrefs hands the saved choices to the player.
func (u *ui) applyOutputPrefs() {
	u.player.SetMatchRate(u.matchRateItem.Checked)
	u.player.SetDevice(u.prefs.String(prefOutputDevice))
	u.refreshDevices()
}

// refreshDevices lists the output devices in the menu, ticking the one in
// use: the chosen one, or System Default if none is chosen or it is
// unplugged.
func (u *ui) refreshDevices() {
	chosen := u.prefs.String(prefOutputDevice)
	devices := u.player.Devices()
	present := false
	for _, d := range devices {
		present = present || d.ID == chosen
	}
	def := fyne.NewMenuItem("System Default", func() { u.setOutputDevice("") })
	def.Checked = chosen == "" || !present
	items := []*fyne.MenuItem{def, fyne.NewMenuItemSeparator()}
	for _, d := range devices {
		item := fyne.NewMenuItem(d.Name, func() { u.setOutputDevice(d.ID) })
		item.Checked = d.ID == chosen
		items = append(items, item)
	}
	u.outputItem.ChildMenu.Items = items
	u.mainMenu.Refresh()
	u.updateOutputInfo()
}

func (u *ui) setOutputDevice(id string) {
	u.prefs.SetString(prefOutputDevice, id)
	u.player.SetDevice(id)
	u.refreshDevices()
}

func (u *ui) setMatchRate(on bool) {
	u.prefs.SetBool(prefMatchRate, on)
	u.matchRateItem.Checked = on
	u.player.SetMatchRate(on)
	u.mainMenu.Refresh()
	u.updateOutputInfo()
}

// devicesChanged follows devices being plugged in or out, or the system's
// default output changing.
func (u *ui) devicesChanged() {
	u.player.DevicesChanged()
	u.refreshDevices()
}

// updateOutputInfo says on the Now Playing page where the sound goes, and
// whether it is converted on the way.
func (u *ui) updateOutputInfo() {
	defer u.visView.Refresh() // lay the page out again, as the line comes and goes
	device, track := u.player.Output()
	if device == 0 {
		u.npOutput.Hide()
		return
	}
	name := "System Default"
	chosen := u.prefs.String(prefOutputDevice)
	for _, d := range u.player.Devices() {
		if d.ID == chosen || (chosen == "" && d.IsDefault) {
			name = d.Name
		}
	}
	var text string
	switch {
	case device == track && u.speed == 1:
		text = fmt.Sprintf("Playing on %s at %s, unconverted", name, kHz(float64(track)))
	case device == track:
		text = fmt.Sprintf("Playing on %s at %s", name, kHz(float64(track)))
	default:
		text = fmt.Sprintf("Playing on %s, converted from %s to %s", name,
			kHz(float64(track)), kHz(float64(device)))
	}
	if u.speed != 1 {
		text += fmt.Sprintf(", at %s speed with the pitch kept", speedLabel(u.speed))
	}
	u.npOutput.SetText(text)
	u.npOutput.Show()
}
