package ui

// View > Appearance: follow the system's light or dark mode (the default),
// or keep Mugig light or dark whatever the system uses.

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const prefAppearance = "appearance" // "auto", "light" or "dark"

var appearances = []struct{ key, label string }{
	{"auto", "Automatic"},
	{"light", "Light"},
	{"dark", "Dark"},
}

// pinnedTheme is the app's theme in one variant, whatever the system's.
type pinnedTheme struct {
	appTheme
	variant fyne.ThemeVariant
}

func (t pinnedTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.appTheme.Color(n, t.variant)
}

func themeFor(appearance string) fyne.Theme {
	switch appearance {
	case "light":
		return pinnedTheme{variant: theme.VariantLight}
	case "dark":
		return pinnedTheme{variant: theme.VariantDark}
	}
	return appTheme{}
}

// buildAppearanceMenu creates the View menu's Appearance submenu.
func (u *ui) buildAppearanceMenu() *fyne.MenuItem {
	u.appearanceItems = nil
	for _, a := range appearances {
		u.appearanceItems = append(u.appearanceItems, fyne.NewMenuItem(a.label, func() { u.setAppearance(a.key) }))
	}
	item := fyne.NewMenuItem("Appearance", nil)
	item.ChildMenu = fyne.NewMenu("", u.appearanceItems...)
	return item
}

// setAppearance applies and remembers an appearance.
func (u *ui) setAppearance(key string) {
	u.prefs.SetString(prefAppearance, key)
	u.applyAppearance()
}

// applyAppearance sets the theme for the saved appearance and ticks it in
// the menu.
func (u *ui) applyAppearance() {
	key := u.prefs.StringWithFallback(prefAppearance, "auto")
	fyne.CurrentApp().Settings().SetTheme(themeFor(key))
	for i, a := range appearances {
		u.appearanceItems[i].Checked = a.key == key
	}
	if u.mainMenu != nil {
		u.mainMenu.Refresh()
	}
}
