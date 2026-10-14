package main

// Choosing where sound goes, and showing whether it reaches the device
// unconverted.

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
)

const (
	prefOutputDevice = "outputDevice"    // chosen device's ID, "" for the system default
	prefMatchRate    = "matchSampleRate" // switch the device to each track's rate
	prefExclusive    = "exclusiveOutput" // take the device for Mugig alone while playing
)

// buildOutputMenu creates the Output Device submenu, and the Match Output
// Sample Rate and Exclusive Access items, from the saved choices.
// Exclusive access is offered only where the system has it.
func (u *ui) buildOutputMenu() []*fyne.MenuItem {
	u.outputItem = fyne.NewMenuItem("Output Device", nil)
	u.outputItem.ChildMenu = fyne.NewMenu("")
	u.matchRateItem = fyne.NewMenuItem("Match Output Sample Rate", func() {
		u.setMatchRate(!u.matchRateItem.Checked)
	})
	u.matchRateItem.Checked = u.prefs.BoolWithFallback(prefMatchRate, true)
	u.exclusiveItem = fyne.NewMenuItem("Exclusive Access", func() {
		u.setExclusive(!u.exclusiveItem.Checked)
	})
	u.exclusiveItem.Checked = u.prefs.Bool(prefExclusive)
	items := []*fyne.MenuItem{u.outputItem, u.matchRateItem}
	if u.canHog {
		items = append(items, u.exclusiveItem)
	}
	return items
}

// applyOutputPrefs hands the saved choices to the player.
func (u *ui) applyOutputPrefs() {
	u.player.SetExclusive(u.canHog && u.exclusiveItem.Checked)
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

// setExclusive turns exclusive access on or off: while a song plays or is
// paused, no other app's sound can mix in or change the device's rate.
func (u *ui) setExclusive(on bool) {
	u.prefs.SetBool(prefExclusive, on)
	u.exclusiveItem.Checked = on
	u.player.SetExclusive(on)
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
	case device == track && u.speed == 1 && u.crossfeed == crossfeedOff && !u.eq.active():
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
	if u.eq.active() {
		text += ", with the equalizer"
	}
	if u.crossfeed != crossfeedOff {
		text += fmt.Sprintf(", with %s crossfeed", strings.ToLower(crossfeedNames[u.crossfeed]))
	}
	switch {
	case u.player.Exclusive():
		text += ". Exclusive access: no other sound is mixed in"
	case u.exclusiveItem.Checked && u.canHog:
		text += ". Exclusive access isn't available: another app may have the device"
	}
	u.npOutput.SetText(text)
	u.npOutput.Show()
}
