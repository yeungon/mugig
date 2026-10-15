package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSpeedChoice(t *testing.T) {
	u, fp, a := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	if u.speed != 1 || fp.speed != 1 || u.speedBtn.Text != "1×" || u.speedBtn.Importance != widget.LowImportance {
		t.Fatalf("start: speed %v, player %v, button %q", u.speed, fp.speed, u.speedBtn.Text)
	}
	if got := checkedOf(u.speedItems); got != "1× (Normal)" {
		t.Errorf("checked %q", got)
	}

	u.togglePlay()
	u.speedItems[4].Action() // 1.5×
	if fp.speed != 1.5 || u.speedBtn.Text != "1.5×" || u.speedBtn.Importance != widget.HighImportance {
		t.Errorf("player %v, button %q", fp.speed, u.speedBtn.Text)
	}
	if got := checkedOf(u.speedItems); got != "1.5×" {
		t.Errorf("checked %q", got)
	}
	if want := "Playing on Speakers at 44.1 kHz, at 1.5× speed with the pitch kept"; u.npOutput.Text != want {
		t.Errorf("output line %q", u.npOutput.Text)
	}
	if m := lastMedia(u); m.Rate != 1.5 {
		t.Errorf("system told rate %v", m.Rate)
	}

	// Remembered for next time.
	u2 := newUI(test.NewTempWindow(t, nil), a.Preferences(), uiDeps{player: &fakePlayer{bad: map[string]bool{}}, cache: u.cache, media: &fakeMedia{}})
	if u2.speed != 1.5 || u2.player.(*fakePlayer).speed != 1.5 {
		t.Errorf("next session: %v", u2.speed)
	}
}

func TestFasterSlower(t *testing.T) {
	u, fp, _ := newTestUI(t)
	var got []string
	for range 5 {
		u.stepSpeed(1)
		got = append(got, u.speedBtn.Text)
	}
	if strings.Join(got, " ") != "1.25× 1.5× 1.75× 2× 2×" {
		t.Errorf("faster: %q", got)
	}
	for range 8 {
		u.stepSpeed(-1)
	}
	if fp.speed != 0.5 {
		t.Errorf("slowest %v", fp.speed)
	}
}

// The button opens the list of speeds.
func TestSpeedButtonOpensChoices(t *testing.T) {
	u, _, _ := newTestUI(t)
	u.win.Resize(fyne.NewSize(1000, 600))
	test.Tap(u.speedBtn)
	var menu *widget.PopUpMenu
	if top := u.win.Canvas().Overlays().Top(); top != nil {
		visit(top, func(o fyne.CanvasObject) {
			if m, ok := o.(*widget.PopUpMenu); ok {
				menu = m
			}
		})
	}
	if menu == nil || len(menu.Items) != len(Speeds) {
		t.Fatalf("menu %v", menu)
	}
}
