package main

// The visualizer screen, shown in place of the track list.

import (
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// graphicEqIcon is Material Design's "graphic_eq" (Apache 2.0).
var graphicEqIcon = svgIcon("graphic_eq.svg", "M7 18h2V6H7v12zm4 4h2V2h-2v20zm-8-8h2v-4H3v4zm12 4h2V6h-2v12zm4-8v4h2v-4h-2z")

func (u *ui) buildVisualizer() fyne.CanvasObject {
	u.vis = newVisualizer(u.visSamples, numBands)

	u.visTitle = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyleHeading})
	u.visTitle.Truncation = fyne.TextTruncateEllipsis
	u.visDetail = secondaryLabel("")
	u.bigArt = newArtTile(132)
	u.visModes = widget.NewRadioGroup(visModeNames, func(s string) {
		if i := slices.Index(visModeNames, s); i >= 0 {
			u.setVisMode(visMode(i))
		}
	})
	u.visModes.Horizontal = true
	u.visModes.Required = true

	mode := visMode(u.prefs.Int(prefVisMode))
	if int(mode) >= len(visModeNames) || mode < 0 {
		mode = visSpectrum
	}
	u.setVisMode(mode)

	format, checks := u.buildQualityDetails()
	header := container.NewBorder(nil, nil, u.bigArt.box, container.NewVBox(u.visModes),
		container.NewVBox(layout.NewSpacer(), sectionLabel("NOW PLAYING"), u.visTitle, u.visDetail, format, checks, layout.NewSpacer()))
	u.visView = container.NewPadded(container.NewBorder(header, nil, nil, nil, u.vis.raster))
	u.visView.Hide()
	return u.visView
}

// visSamples feeds the visualizers: the latest audio and its sample rate,
// or 0 when there is nothing to show.
func (u *ui) visSamples(dst []float64) int {
	if u.paused {
		return 0
	}
	return u.player.Samples(dst)
}

// buildMiniVisualizer creates the strip at the end of the player bar. It
// always shows the spectrum, as a waveform this small just looks like noise.
// Clicking it opens the full visualizer.
func (u *ui) buildMiniVisualizer() fyne.CanvasObject {
	u.miniVis = newVisualizer(u.visSamples, miniBands)
	u.miniView = container.NewGridWrap(fyne.NewSize(180, 72),
		newTapArea(u.miniVis.raster, func() { u.showVisualizer(true) }))
	return u.miniView
}

// showMiniVisualizer turns the player-bar strip on or off.
func (u *ui) showMiniVisualizer(show bool) {
	u.prefs.SetBool(prefMiniVis, show)
	u.miniItem.Checked = show
	u.mainMenu.Refresh()
	if show {
		u.miniView.Show()
		u.animateVisualizers()
	} else {
		u.miniVis.stopAnimating()
		u.miniView.Hide()
	}
}

// animateVisualizers starts whichever visualizers are on screen. They stop
// by themselves once the sound has died away, so this is called whenever
// sound may start: on play and on resume.
func (u *ui) animateVisualizers() {
	if u.visView.Visible() {
		u.vis.start()
	}
	if u.miniView.Visible() {
		u.miniVis.start()
	}
}

// toggleVisualizer switches the main area between the track list and the
// visualizer.
func (u *ui) toggleVisualizer() {
	u.showVisualizer(!u.visView.Visible())
}

func (u *ui) showVisualizer(show bool) {
	if show {
		// The hidden track list may have focus; drop it so Space and Esc
		// reach the window's key handler.
		u.win.Canvas().Unfocus()
		u.songsPage.Hide()
		u.visView.Show()
		u.vis.start() // animates only while on screen
	} else {
		u.vis.stopAnimating()
		u.visView.Hide()
		u.songsPage.Show()
	}
	setToggle(u.visBtn, graphicEqIcon, show)
	u.updateNav(show)
	u.visItem.Checked = show
	u.mainMenu.Refresh()
}

func (u *ui) setVisMode(m visMode) {
	u.vis.setMode(m)
	u.prefs.SetInt(prefVisMode, int(m))
	u.visModes.SetSelected(visModeNames[m]) // no-op if already selected
	for i, item := range u.visModeItems {
		item.Checked = visMode(i) == m
	}
	u.mainMenu.Refresh()
}

// updateVisHeader shows the playing track above the visualizer.
func (u *ui) updateVisHeader() {
	title := u.visTitle.Segments[0].(*widget.TextSegment)
	if u.playing.Path == "" {
		title.Text = "Nothing playing"
		u.visDetail.SetText("Choose a track to see its sound here.")
	} else {
		title.Text = u.playing.Title
		var detail []string
		for _, s := range []string{u.playing.Artist, u.playing.Album} {
			if s != "" {
				detail = append(detail, s)
			}
		}
		if u.playing.Year > 0 {
			detail = append(detail, strconv.Itoa(u.playing.Year))
		}
		u.visDetail.SetText(strings.Join(detail, " · "))
	}
	u.visTitle.Refresh()
}

// tapArea makes any content clickable, with a hand cursor on hover.
type tapArea struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTapArea(content fyne.CanvasObject, onTap func()) *tapArea {
	t := &tapArea{content: content, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}

func (t *tapArea) Tapped(*fyne.PointEvent) { t.onTap() }

func (t *tapArea) Cursor() desktop.Cursor { return desktop.PointerCursor }
