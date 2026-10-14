package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestAppearance(t *testing.T) {
	u, _, a := newTestUI(t)
	if got := checkedOf(u.appearanceItems); got != "Automatic" {
		t.Errorf("checked %q at first", got)
	}
	// Light and Dark hold whatever the system uses, in the colours read by
	// the custom widgets and in the window itself.
	light := toNRGBA(appTheme{}.Color(theme.ColorNameBackground, theme.VariantLight))
	dark := toNRGBA(appTheme{}.Color(theme.ColorNameBackground, theme.VariantDark))
	u.win.Resize(fyne.NewSize(900, 600))
	background := func() any {
		return toNRGBA(u.win.Canvas().Capture().At(600, 590))
	}

	u.appearanceItems[1].Action() // Light
	if got := toNRGBA(themeColor(theme.ColorNameBackground)); got != light {
		t.Errorf("Light: background %v, want %v", got, light)
	}
	if checkedOf(u.appearanceItems) != "Light" || a.Preferences().String(prefAppearance) != "light" {
		t.Errorf("checked %q, saved %q", checkedOf(u.appearanceItems), a.Preferences().String(prefAppearance))
	}
	lightPixel := background()

	u.appearanceItems[2].Action() // Dark
	if got := toNRGBA(themeColor(theme.ColorNameBackground)); got != dark {
		t.Errorf("Dark: background %v, want %v", got, dark)
	}
	if background() == lightPixel {
		t.Error("the window didn't change")
	}

	// Remembered next time.
	a.Preferences().SetString(prefAppearance, "light")
	newUI(test.NewTempWindow(t, nil), a.Preferences(), uiDeps{player: &fakePlayer{bad: map[string]bool{}}, cache: u.cache, media: &fakeMedia{}})
	if got := toNRGBA(themeColor(theme.ColorNameBackground)); got != light {
		t.Errorf("next session: background %v, want light", got)
	}
}
