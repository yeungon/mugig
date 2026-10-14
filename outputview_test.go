package main

import (
	"fyne.io/fyne/v2"
	"strings"
	"testing"
)

func TestOutputDeviceMenu(t *testing.T) {
	u, fp, a := newTestUI(t)
	items := u.outputItem.ChildMenu.Items
	if len(items) != 4 || items[0].Label != "System Default" || !items[0].Checked ||
		items[2].Label != "Speakers" || items[3].Label != "USB DAC" {
		t.Fatalf("menu %v", labelsOf(items))
	}

	items[3].Action() // USB DAC
	items = u.outputItem.ChildMenu.Items
	if fp.device != "bb" || !items[3].Checked || items[0].Checked {
		t.Errorf("after choosing the DAC: device %q, menu %v", fp.device, checkedOf(items))
	}

	// The choice is remembered.
	u2 := newUI(u.win, a.Preferences(), uiDeps{player: &fakePlayer{}, cache: u.cache})
	if !u2.outputItem.ChildMenu.Items[3].Checked || u2.player.(*fakePlayer).device != "bb" {
		t.Error("output device not restored")
	}
}

func TestMatchSampleRate(t *testing.T) {
	u, fp, a := newTestUI(t)
	if !u.matchRateItem.Checked || !fp.matchRate {
		t.Fatal("matching the track's rate should be on by default")
	}
	u.matchRateItem.Action()
	if u.matchRateItem.Checked || fp.matchRate {
		t.Error("toggling didn't turn it off")
	}
	fp2 := &fakePlayer{}
	newUI(u.win, a.Preferences(), uiDeps{player: fp2, cache: u.cache})
	if fp2.matchRate {
		t.Error("setting not remembered")
	}
}

func TestOutputInfoOnNowPlaying(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	if u.npOutput.Visible() {
		t.Error("shown with nothing playing")
	}
	u.togglePlay()
	if !u.npOutput.Visible() || u.npOutput.Text != "Playing on Speakers at 44.1 kHz, unconverted" {
		t.Errorf("%q", u.npOutput.Text)
	}
	u.stop()
	if u.npOutput.Visible() {
		t.Error("still shown after stopping")
	}
}

func labelsOf(items []*fyne.MenuItem) []string {
	var out []string
	for _, i := range items {
		out = append(out, i.Label)
	}
	return out
}

func checkedOf(items []*fyne.MenuItem) string {
	var out []string
	for _, i := range items {
		if i.Checked {
			out = append(out, i.Label)
		}
	}
	return strings.Join(out, ",")
}

// Exclusive access is off until turned on, is remembered, and the Now
// Playing page says whether Mugig has the device to itself.
func TestExclusiveAccess(t *testing.T) {
	u, fp, a := newTestUI(t)
	if u.exclusiveItem.Checked || fp.exclusive {
		t.Fatal("exclusive access should be off by default")
	}
	controls := u.mainMenu.Items[2].Items
	if got := controls[len(controls)-1]; u.canHog != (got == u.exclusiveItem) {
		t.Errorf("the item should be offered only where the system has it: last item %q", got.Label)
	}
	setLibrary(u, fiveTracks()...)
	u.togglePlay()
	u.exclusiveItem.Action()
	if !fp.exclusive || !u.exclusiveItem.Checked {
		t.Fatal("not turned on")
	}
	if !strings.HasSuffix(u.npOutput.Text, "unconverted. Exclusive access: no other sound is mixed in") {
		t.Errorf("%q", u.npOutput.Text)
	}
	fp.hogFails = true
	u.updateOutputInfo()
	if !strings.Contains(u.npOutput.Text, "Exclusive access isn't available") {
		t.Errorf("%q", u.npOutput.Text)
	}

	fp2 := &fakePlayer{}
	newUI(u.win, a.Preferences(), uiDeps{player: fp2, cache: u.cache})
	if fp2.exclusive != u.canHog {
		t.Error("setting not remembered")
	}
	u.exclusiveItem.Action()
	if fp.exclusive {
		t.Error("not turned off")
	}
}
