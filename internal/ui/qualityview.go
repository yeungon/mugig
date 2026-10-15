package ui

// Showing audio quality: a badge in the player bar and on the Now Playing
// page, plus the format, the check's findings and the button to run it on
// the Now Playing page.

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"mugig/internal/audio"
	"mugig/internal/library"
	"mugig/internal/quality"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The format of the playing track is read from its headers as soon as it
// starts, which is instant. Checking the audio means decoding all of it
// (about a second for a typical FLAC), so it is done only when asked, in the
// Audio Quality dialog, and the result is saved so no file is checked twice.

type checkState int

const (
	notChecked checkState = iota
	checking
	checked
)

// trackQuality is what is known about the playing track's quality.
type trackQuality struct {
	path   string // "" while the headers are being read, or nothing plays
	info   audio.Info
	err    error           // the headers couldn't be read
	rep    *quality.Report // nil until checked
	state  checkState
	cancel context.CancelFunc // stops reading the headers when the track changes
}

// loadQuality reads t's format and any saved check result. A zero t clears
// the display.
func (u *ui) loadQuality(t library.Track) {
	if u.quality.cancel != nil {
		u.quality.cancel()
	}
	u.quality = trackQuality{}
	u.renderQuality()
	if t.Path == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.quality.cancel = cancel
	u.async(func() {
		file := audio.FileOf(t.Path)
		info, err := audio.Probe(file)
		var rep quality.Report
		saved := err == nil && quality.CanAnalyse(info) && u.qualityStore.Get(file, quality.FileStamp(file), &rep)
		onUI(func() {
			if ctx.Err() != nil {
				return // the track has changed
			}
			q := &u.quality
			q.path, q.info, q.err = t.Path, info, err
			if saved {
				q.rep, q.state = &rep, checked
			}
			u.renderQuality()
		})
	})
}

// canOpenQuality reports whether the playing track can be shown in the
// Audio Quality dialog.
func (u *ui) canOpenQuality() bool {
	q := &u.quality
	return q.path != "" && q.err == nil && quality.CanAnalyse(q.info)
}

// needsCheck reports whether the playing track is lossless and unchecked.
func (u *ui) needsCheck() bool {
	return u.canOpenQuality() && quality.CanVerify(u.quality.info) && u.quality.state == notChecked
}

// checkAudioQuality opens the Audio Quality dialog for the playing track.
func (u *ui) checkAudioQuality() {
	if u.canOpenQuality() {
		u.checkTrack(u.playing)
	}
}

// qualityText sums up what is known about a file's quality, adding a line
// for a check that is running or could be run.
func qualityText(info audio.Info, err error, rep *quality.Report, state checkState, offerCheck bool) quality.Assessment {
	if err != nil {
		return quality.Assessment{Checks: []quality.Check{{Kind: quality.CheckInfo, Text: "Can't read the audio format: " + err.Error()}}}
	}
	a := quality.Assess(info, rep)
	switch {
	case state == checking:
		a.Checks = append(a.Checks, quality.Check{Kind: quality.CheckPending, Text: "Checking the audio…"})
	case offerCheck:
		a.Checks = append(a.Checks, quality.Check{Kind: quality.CheckInfo, Text: "Not yet checked for damage or a lossy source"})
	}
	return a
}

// renderQuality updates the badges, the Now Playing details, and whether
// the check can be run.
func (u *ui) renderQuality() {
	q := &u.quality
	var a quality.Assessment
	if q.path != "" || q.err != nil {
		a = qualityText(q.info, q.err, q.rep, q.state, u.needsCheck())
	}
	u.barBadge.set(a.Level)
	u.barArtistRow.Refresh() // lay out again, as the badge may have appeared or gone
	u.np.show(a)

	can := u.canOpenQuality()
	u.checkBtn.Hidden = !can
	if u.needsCheck() {
		u.checkBtn.SetText("Check Audio Quality")
	} else {
		u.checkBtn.SetText("Show Spectrogram")
	}
	u.npQuality.Refresh() // lay out again, as rows and the button come and go
	u.checkItem.Disabled = !can
	u.mainMenu.Refresh()
}

// qualityChecked takes a result from the Audio Quality dialog: for the song
// list, and in case it is for the playing track.
func (u *ui) qualityChecked(path string, rep quality.Report, level quality.Level) {
	u.setLevel(path, level)
	if q := &u.quality; q.path == path && q.state == notChecked {
		q.rep, q.state = &rep, checked
		u.renderQuality()
	}
}

// qualityPanel shows an assessment: the badge and format on one line, then
// the findings. The Now Playing page has one, and so does the dialog for
// checking other tracks.
type qualityPanel struct {
	badge    *qualityBadge
	spec     *widget.Label
	format   *fyne.Container // badge and spec
	checks   *fyne.Container
	themed   *container.ThemeOverride
	findings fyne.CanvasObject // checks, compact and lined up with the text above
}

func newQualityPanel() *qualityPanel {
	p := &qualityPanel{badge: newQualityBadge(nil), spec: secondaryLabel(""), checks: container.NewVBox()}
	p.format = container.NewBorder(nil, nil, p.badge, nil, p.spec)
	p.themed = container.NewThemeOverride(p.checks, compactTheme{})
	inset := theme.InnerPadding()
	p.findings = container.New(layout.NewCustomPaddedLayout(0, 0, inset, 0), p.themed)
	return p
}

func (p *qualityPanel) show(a quality.Assessment) {
	p.badge.set(a.Level)
	p.spec.SetText(a.Format)
	p.format.Refresh() // lay out again, as the badge may have appeared or gone
	rows := make([]fyne.CanvasObject, len(a.Checks))
	for i, c := range a.Checks {
		rows[i] = checkRow(c)
	}
	p.checks.Objects = rows
	p.themed.Refresh() // applies the compact theme to the new rows
}

// texts returns the findings shown, for tests.
func (p *qualityPanel) texts() []string {
	var out []string
	for _, row := range p.checks.Objects {
		out = append(out, row.(*fyne.Container).Objects[0].(*widget.Label).Text)
	}
	return out
}

func checkRow(c quality.Check) fyne.CanvasObject {
	icon, colour := theme.InfoIcon(), theme.ColorNamePlaceHolder
	switch c.Kind {
	case quality.CheckOK:
		icon, colour = theme.ConfirmIcon(), theme.ColorNameSuccess
	case quality.CheckWarn:
		icon, colour = theme.WarningIcon(), theme.ColorNameWarning
	case quality.CheckBad:
		icon, colour = theme.ErrorIcon(), theme.ColorNameError
	case quality.CheckPending:
		icon = theme.MoreHorizontalIcon()
	}
	l := widget.NewLabel(c.Text)
	l.Wrapping = fyne.TextWrapWord
	// Keep the icon beside the first line when the text wraps.
	return container.NewBorder(nil, nil,
		container.NewVBox(widget.NewIcon(theme.NewColoredResource(icon, colour))), nil, l)
}

// buildQualityDetails creates the Now Playing page's format line (badge and
// spec) and, below it, the findings and the button to check.
func (u *ui) buildQualityDetails() (line, checks fyne.CanvasObject) {
	u.np = newQualityPanel()
	u.checkBtn = widget.NewButtonWithIcon("Check Audio Quality", theme.SearchIcon(), u.checkAudioQuality)
	u.checkBtn.Hide()
	u.npQuality = container.NewVBox(u.np.findings, container.NewHBox(u.checkBtn))
	u.npOutput = secondaryLabel("")
	u.npOutput.Hide()
	return container.NewVBox(u.np.format, u.npOutput), u.npQuality
}

// compactTheme tightens the spacing of the findings list, which would
// otherwise take up much of the visualizer's room.
type compactTheme struct{}

func (compactTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return fyne.CurrentApp().Settings().Theme().Color(n, v)
}

func (compactTheme) Font(s fyne.TextStyle) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Font(s)
}

func (compactTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Icon(n)
}

func (compactTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameInnerPadding:
		return 3
	case theme.SizeNamePadding:
		return 2
	case theme.SizeNameInlineIcon:
		return 16
	}
	return fyne.CurrentApp().Settings().Theme().Size(n)
}

// ---------------------------------------------------------------------------
// Badge
// ---------------------------------------------------------------------------

var badgeStyles = map[quality.Level]struct {
	text   string
	colour fyne.ThemeColorName
	filled bool
}{
	quality.Lossy:    {"LOSSY", theme.ColorNameDisabled, false},
	quality.Lossless: {"LOSSLESS", colorNameAccent, false},
	quality.HiRes:    {"HI-RES LOSSLESS", theme.ColorNamePrimary, true},
	quality.Suspect:  {"LOSSLESS?", theme.ColorNameWarning, false},
	quality.Damaged:  {"DAMAGED", theme.ColorNameError, false},
}

const badgeTextSize = 10

// qualityBadge is a small outlined label such as "LOSSLESS". It is hidden
// when the quality is unknown, and centres itself vertically in whatever
// height it is given.
type qualityBadge struct {
	widget.BaseWidget
	level quality.Level
	onTap func()
}

func newQualityBadge(onTap func()) *qualityBadge {
	b := &qualityBadge{onTap: onTap}
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}

func (b *qualityBadge) set(l quality.Level) {
	b.level = l
	if _, ok := badgeStyles[l]; ok {
		b.Show()
	} else {
		b.Hide()
	}
	b.Refresh()
}

func (b *qualityBadge) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *qualityBadge) Cursor() desktop.Cursor {
	if b.onTap != nil {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (b *qualityBadge) CreateRenderer() fyne.WidgetRenderer {
	r := &badgeRenderer{b: b, box: canvas.NewRectangle(color.Transparent), text: canvas.NewText("", nil)}
	r.box.CornerRadius = 4
	r.box.StrokeWidth = 1
	r.text.TextSize = badgeTextSize
	r.text.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

type badgeRenderer struct {
	b    *qualityBadge
	box  *canvas.Rectangle
	text *canvas.Text
}

func (r *badgeRenderer) pill() fyne.Size {
	t := r.text.MinSize()
	return fyne.NewSize(t.Width+12, t.Height+2)
}

// The badge is inset like a label's text, so it lines up with the labels
// above it.
func (r *badgeRenderer) MinSize() fyne.Size {
	p := r.pill()
	return fyne.NewSize(p.Width+theme.InnerPadding(), p.Height)
}

func (r *badgeRenderer) Layout(s fyne.Size) {
	p, inset := r.pill(), theme.InnerPadding()
	r.box.Resize(p)
	r.box.Move(fyne.NewPos(inset, (s.Height-p.Height)/2))
	t := r.text.MinSize()
	r.text.Move(fyne.NewPos(inset+(p.Width-t.Width)/2, (s.Height-t.Height)/2))
	r.text.Resize(t)
}

func (r *badgeRenderer) Refresh() {
	style := badgeStyles[r.b.level]
	c := themeColor(style.colour)
	r.text.Text = style.text
	r.box.StrokeColor = c
	r.box.FillColor = color.Transparent
	r.text.Color = c
	if style.filled {
		r.box.FillColor = c
		r.text.Color = themeColor(theme.ColorNameForegroundOnPrimary)
	}
	r.box.Refresh()
	r.text.Refresh()
	r.Layout(r.b.Size())
}

func (r *badgeRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.box, r.text} }

func (r *badgeRenderer) Destroy() {}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Checking any track
// ---------------------------------------------------------------------------

// trackMenu is the right-click menu for a track in the list.
func (u *ui) trackMenu(t library.Track) *fyne.Menu {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Play", func() { u.jump(t) }),
		fyne.NewMenuItem("Play Next", func() { u.playNext(t) }),
		fyne.NewMenuItem("Play Later", func() { u.playLater(t) }),
		fyne.NewMenuItemSeparator(),
		u.addToPlaylistItem([]library.Track{t}),
	}
	items = append(items, u.playlistSongItems([]library.Track{t})...)
	return fyne.NewMenu("", append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Get Info…", func() { u.showInfo(t) }),
		fyne.NewMenuItem("Check Audio Quality…", func() { u.checkTrack(t) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(revealLabel, func() { u.revealTrack(t) }),
		fyne.NewMenuItem("Copy File Path", func() { fyne.CurrentApp().Clipboard().SetContent(audio.FileOf(t.Path)) }),
		fyne.NewMenuItemSeparator(),
		u.trashItem([]library.Track{t}),
	)...)
}

// checkTrack opens the Audio Quality dialog for t: its findings and its
// spectrogram, from the saved result or by analysing it now. Closing the
// dialog stops the analysis. It returns the dialog's panel and spectrogram,
// for tests.
func (u *ui) checkTrack(t library.Track) (*qualityPanel, *spectrogramView, dialog.Dialog) {
	p := newQualityPanel()
	spec := newSpectrogramView()
	bar := widget.NewProgressBar()
	bar.Hide()
	title := widget.NewLabelWithStyle(t.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Truncation = fyne.TextTruncateEllipsis
	var shown quality.Assessment // what the picture is saved with
	saveBtn := widget.NewButtonWithIcon("Save as PNG…", theme.DocumentSaveIcon(), func() {
		u.saveSpectrogram(t, shown, spec)
	})
	tools := container.NewBorder(nil, nil, nil, saveBtn, u.levelsControl(spec))
	tools.Hide()
	content := container.NewVBox(title, secondaryLabel(trackDetail(t)),
		p.format, p.findings, spec, tools, bar)
	d := dialog.NewCustom("Audio Quality", "Close", content, u.win)
	ctx, cancel := context.WithCancel(context.Background())
	d.SetOnClosed(cancel)
	relayout := func() {
		content.Refresh()
		d.Resize(fyne.NewSize(700, content.MinSize().Height)) // grows with the findings
	}
	p.show(quality.Assessment{Checks: []quality.Check{{Kind: quality.CheckPending, Text: "Reading the file…"}}})
	relayout()
	d.Show()

	u.async(func() {
		// post runs f on the UI thread unless the dialog has been closed.
		post := func(f func()) {
			onUI(func() {
				if ctx.Err() == nil {
					f()
					relayout()
				}
			})
		}
		file := audio.FileOf(t.Path) // a song from a CUE sheet: its whole disc image
		info, err := audio.Probe(file)
		if err != nil || !quality.CanAnalyse(info) {
			post(func() {
				p.show(qualityText(info, err, nil, notChecked, false))
				spec.set(nil, 0, 0, 0, "No spectrogram: this file can't be decoded")
			})
			return
		}
		show := func(rep quality.Report, img *image.Gray) {
			shown = qualityText(info, nil, &rep, checked, false)
			p.show(shown)
			spec.set(img, info.SampleRate, info.Duration, rep.Spectrum.Cutoff, "No sound to draw")
			tools.Hidden = img == nil
		}

		stamp := quality.FileStamp(file)
		var rep quality.Report
		if u.qualityStore.Get(file, stamp, &rep) {
			if img, ok := u.qualityStore.Picture(file); ok {
				post(func() { show(rep, img) })
				return
			}
		}
		post(func() {
			p.show(qualityText(info, nil, nil, checking, false))
			spec.set(nil, info.SampleRate, 0, 0, "Analysing…")
			bar.Show()
		})
		rep, img, err := quality.Analyse(ctx, file, info, true, func(done float64) {
			post(func() { bar.SetValue(done) })
		})
		if err != nil {
			return // closed
		}
		level := quality.Assess(info, &rep).Level
		u.qualityStore.PutPicture(file, img)
		u.qualityStore.Put(file, stamp, rep, level)
		post(func() {
			bar.Hide()
			show(rep, img)
		})
		onUI(func() { u.qualityChecked(t.Path, rep, level) }) // it may be playing by now
	})
	return p, spec, d
}

// The range of levels the spectrogram shows can be changed, as in Spek:
// lowering the floor brings out quiet detail such as noise and dither;
// raising it, or lowering the top, separates loud sounds.
const (
	prefSpecTop   = "spectrogramTop"
	prefSpecFloor = "spectrogramFloor"
)

var (
	topChoices   = []float64{0, -10, -20, -30, -40}
	floorChoices = []float64{-60, -70, -80, -90, -100, -110, -120, -130, -140}
)

// levelsControl creates the dialog's choice of levels shown in spec, set to
// the range last chosen. A new choice is remembered for next time.
func (u *ui) levelsControl(spec *spectrogramView) *fyne.Container {
	top := nearest(topChoices, u.prefs.FloatWithFallback(prefSpecTop, defaultTopDB))
	floor := nearest(floorChoices, u.prefs.FloatWithFallback(prefSpecFloor, defaultFloorDB))
	spec.setRange(top, floor)
	choice := func(choices []float64, chosen float64, pref string, set func(float64)) *widget.Select {
		labels := make([]string, len(choices))
		for i, db := range choices {
			labels[i] = fmt.Sprintf("%g dB", db)
		}
		sel := widget.NewSelect(labels, nil)
		sel.SetSelectedIndex(slices.Index(choices, chosen))
		sel.OnChanged = func(string) {
			db := choices[sel.SelectedIndex()]
			u.prefs.SetFloat(pref, db)
			set(db)
			spec.setRange(top, floor)
		}
		return sel
	}
	label := func(text string) *widget.Label {
		l := widget.NewLabel(text)
		l.Importance = widget.LowImportance
		return l
	}
	return container.NewHBox(
		label("Levels from"),
		choice(topChoices, top, prefSpecTop, func(db float64) { top = db }),
		label("to"),
		choice(floorChoices, floor, prefSpecFloor, func(db float64) { floor = db }),
	)
}

// nearest returns the choice closest to v.
func nearest(choices []float64, v float64) float64 {
	best := choices[0]
	for _, c := range choices {
		if math.Abs(c-v) < math.Abs(best-v) {
			best = c
		}
	}
	return best
}
