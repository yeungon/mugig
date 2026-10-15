package ui

// The About window: what Mugig is, its version, who made it, and where to
// find it. A menu item labelled "About" is moved by Fyne to the app menu on
// macOS, in place of the system's About panel, which only knows the name;
// elsewhere it stays in the Help menu.

import (
	"fmt"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	projectURL = "https://github.com/yeungon/mugig"
	aboutTag   = "A music player that checks your files."
	aboutIntro = "Plays your music bit-perfect and gapless, and tells you the truth " +
		"about it: whether a “lossless” file really is lossless, whether it is " +
		"intact, and how loud it is."
	aboutWidth = 400
)

// showAbout opens the About window, and returns it for tests.
func (u *ui) showAbout() dialog.Dialog {
	icon := canvas.NewImageFromResource(appIcon)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSquareSize(96))
	name := canvas.NewText("Mugig", themeColor(theme.ColorNameForeground))
	name.TextSize = theme.Size(theme.SizeNameHeadingText)
	name.TextStyle = fyne.TextStyle{Bold: true}
	name.Alignment = fyne.TextAlignCenter

	centred := func(l *widget.Label) *widget.Label {
		l.Alignment = fyne.TextAlignCenter
		return l
	}
	intro := centred(widget.NewLabel(aboutIntro))
	intro.Wrapping = fyne.TextWrapWord
	version := centred(secondaryLabel(versionText(appMetadata())))
	version.Hidden = version.Text == ""
	link, _ := url.Parse(projectURL)
	content := container.NewVBox(
		container.NewCenter(icon),
		name,
		version,
		centred(widget.NewLabelWithStyle(aboutTag, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})),
		intro,
		centred(widget.NewLabelWithStyle("Crafted by Vuong", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})),
		container.NewCenter(widget.NewHyperlink("github.com/yeungon/mugig", link)),
		centred(secondaryLabel("© 2026 Vuong Nguyen · MIT License")),
	)
	d := dialog.NewCustom("About Mugig", "Close", content, u.win)
	d.Resize(fyne.NewSize(aboutWidth, content.MinSize().Height))
	d.Show()
	// The intro's height is known once it has its width.
	d.Resize(fyne.NewSize(aboutWidth, content.MinSize().Height))
	return d
}

// appMetadata is the app's name and version; tests replace it, as Fyne's
// test app has none.
var appMetadata = func() fyne.AppMetadata { return fyne.CurrentApp().Metadata() }

// versionText is the version line, or "" when there is no version to show:
// Fyne knows it from FyneApp.toml, built into packaged apps and read from
// the project folder by go run.
func versionText(m fyne.AppMetadata) string {
	if m.Name == "" {
		return ""
	}
	return fmt.Sprintf("Version %s (%d)", m.Version, m.Build)
}
