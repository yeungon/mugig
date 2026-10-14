package main

// Showing audio quality: a badge in the player bar and on the Now Playing
// page, plus the format, the check's findings and the button to run it on
// the Now Playing page.

import (
	"context"
	"crypto/sha256"
	hexenc "encoding/hex"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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
	info   audioInfo
	err    error          // the headers couldn't be read
	rep    *qualityReport // nil until checked
	state  checkState
	cancel context.CancelFunc // stops reading the headers when the track changes
}

// loadQuality reads t's format and any saved check result. A zero t clears
// the display.
func (u *ui) loadQuality(t Track) {
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
		info, err := probe(t.Path)
		var rep qualityReport
		saved := err == nil && canAnalyse(info) && u.qualityStore.get(t.Path, fileStamp(t.Path), &rep)
		fyne.Do(func() {
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
	return q.path != "" && q.err == nil && canAnalyse(q.info)
}

// needsCheck reports whether the playing track is lossless and unchecked.
func (u *ui) needsCheck() bool {
	return u.canOpenQuality() && canVerify(u.quality.info) && u.quality.state == notChecked
}

// checkAudioQuality opens the Audio Quality dialog for the playing track.
func (u *ui) checkAudioQuality() {
	if u.canOpenQuality() {
		u.checkTrack(u.playing)
	}
}

// qualityText sums up what is known about a file's quality, adding a line
// for a check that is running or could be run.
func qualityText(info audioInfo, err error, rep *qualityReport, state checkState, offerCheck bool) assessment {
	if err != nil {
		return assessment{checks: []check{{checkInfo, "Can't read the audio format: " + err.Error()}}}
	}
	a := assess(info, rep)
	switch {
	case state == checking:
		a.checks = append(a.checks, check{checkPending, "Checking the audio…"})
	case offerCheck:
		a.checks = append(a.checks, check{checkInfo, "Not yet checked for damage or a lossy source"})
	}
	return a
}

// renderQuality updates the badges, the Now Playing details, and whether
// the check can be run.
func (u *ui) renderQuality() {
	q := &u.quality
	var a assessment
	if q.path != "" || q.err != nil {
		a = qualityText(q.info, q.err, q.rep, q.state, u.needsCheck())
	}
	u.barBadge.set(a.level)
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
func (u *ui) qualityChecked(path string, rep qualityReport, level qualityLevel) {
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

func (p *qualityPanel) show(a assessment) {
	p.badge.set(a.level)
	p.spec.SetText(a.spec)
	p.format.Refresh() // lay out again, as the badge may have appeared or gone
	rows := make([]fyne.CanvasObject, len(a.checks))
	for i, c := range a.checks {
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

func checkRow(c check) fyne.CanvasObject {
	icon, colour := theme.InfoIcon(), theme.ColorNamePlaceHolder
	switch c.kind {
	case checkOK:
		icon, colour = theme.ConfirmIcon(), theme.ColorNameSuccess
	case checkWarn:
		icon, colour = theme.WarningIcon(), theme.ColorNameWarning
	case checkBad:
		icon, colour = theme.ErrorIcon(), theme.ColorNameError
	case checkPending:
		icon = theme.MoreHorizontalIcon()
	}
	l := widget.NewLabel(c.text)
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

var badgeStyles = map[qualityLevel]struct {
	text   string
	colour fyne.ThemeColorName
	filled bool
}{
	qualityLossy:    {"LOSSY", theme.ColorNameDisabled, false},
	qualityLossless: {"LOSSLESS", colorNameAccent, false},
	qualityHiRes:    {"HI-RES LOSSLESS", theme.ColorNamePrimary, true},
	qualitySuspect:  {"LOSSLESS?", theme.ColorNameWarning, false},
	qualityDamaged:  {"DAMAGED", theme.ColorNameError, false},
}

const badgeTextSize = 10

// qualityBadge is a small outlined label such as "LOSSLESS". It is hidden
// when the quality is unknown, and centres itself vertically in whatever
// height it is given.
type qualityBadge struct {
	widget.BaseWidget
	level qualityLevel
	onTap func()
}

func newQualityBadge(onTap func()) *qualityBadge {
	b := &qualityBadge{onTap: onTap}
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}

func (b *qualityBadge) set(l qualityLevel) {
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

// qualityStore keeps check results on disk, so each file is checked only
// once. Entries are tied to the file's size and modification time, so an
// edited file is checked afresh.
type qualityStore struct {
	mu       sync.Mutex
	path     string // where it is saved; "" keeps it in memory only
	entries  map[string]storedQuality
	pictures map[string][]byte // spectrograms, when kept in memory
}

type storedQuality struct {
	Stamp  string
	Report qualityReport
	Level  qualityLevel // the verdict, for the song list's Quality column
}

// qualityVersion is bumped whenever qualityReport changes shape or the
// analysis measures differently, so old results are checked again.
const qualityVersion = 5

type qualityFile struct {
	Version int
	Entries map[string]storedQuality
}

// loadQualityStore reads the results saved at path. A missing, corrupt or
// outdated file just gives an empty store.
func loadQualityStore(path string) *qualityStore {
	s := &qualityStore{path: path, entries: map[string]storedQuality{}, pictures: map[string][]byte{}}
	var f qualityFile
	if path != "" && loadGob(path, &f) == nil && f.Version == qualityVersion && f.Entries != nil {
		s.entries = f.Entries
	}
	s.dropOldPictures()
	return s
}

// lookup returns the saved entry for path, if the file is unchanged.
func (s *qualityStore) lookup(path, stamp string) (storedQuality, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[path]
	return e, ok && e.Stamp == stamp && stamp != ""
}

// get fills rep with the saved result for path, if the file is unchanged.
func (s *qualityStore) get(path, stamp string, rep *qualityReport) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[path]
	if !ok || e.Stamp != stamp || stamp == "" {
		return false
	}
	*rep = e.Report
	return true
}

// put records a result and saves the store. Saving errors are ignored: the
// result is still remembered for this session.
func (s *qualityStore) put(path, stamp string, rep qualityReport, level qualityLevel) {
	s.putQuietly(path, stamp, rep, level)
	s.save()
}

// putQuietly records a result without saving, for checking many files at
// once: saving after each would rewrite the whole store thousands of
// times. Call save now and then, and at the end.
func (s *qualityStore) putQuietly(path, stamp string, rep qualityReport, level qualityLevel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[path] = storedQuality{stamp, rep, level}
}

func (s *qualityStore) save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		saveGob(s.path, qualityFile{Version: qualityVersion, Entries: s.entries})
	}
}

// forget drops the result for a file that has changed or gone.
func (s *qualityStore) forget(paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range paths {
		delete(s.entries, p)
	}
}

// levels returns every saved verdict, by path. Whether each file is still
// unchanged isn't checked here: that would mean reading thousands of files
// at startup. The watcher drops results for files that change.
func (s *qualityStore) levels() map[string]qualityLevel {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]qualityLevel, len(s.entries))
	for p, e := range s.entries {
		out[p] = e.Level
	}
	return out
}

// Spectrograms are kept as PNG files of their levels beside the store,
// named after a hash of the track's path, and only read when shown.
func (s *qualityStore) picturePath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(s.pictureDir(), hexenc.EncodeToString(sum[:12])+".png")
}

func (s *qualityStore) pictureDir() string {
	return filepath.Join(filepath.Dir(s.path), "spectrograms")
}

// dropOldPictures deletes spectrograms saved as coloured JPEGs by earlier
// versions, which can't be drawn for another range of levels. Their tracks
// are analysed again when next shown.
func (s *qualityStore) dropOldPictures() {
	if s.path == "" {
		return
	}
	old, _ := filepath.Glob(filepath.Join(s.pictureDir(), "*.jpg"))
	for _, f := range old {
		os.Remove(f)
	}
}

// putPicture saves the spectrogram for path. Saving errors are ignored, as
// for put.
func (s *qualityStore) putPicture(path string, img *image.Gray) {
	if img == nil {
		return
	}
	data, err := encodePicture(img)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		s.pictures[path] = data
		return
	}
	file := s.picturePath(path)
	if os.MkdirAll(filepath.Dir(file), 0o755) == nil {
		os.WriteFile(file, data, 0o644)
	}
}

// picture returns the saved spectrogram for path.
func (s *qualityStore) picture(path string) (*image.Gray, bool) {
	s.mu.Lock()
	data, ok := s.pictures[path]
	s.mu.Unlock()
	if !ok && s.path != "" {
		var err error
		data, err = os.ReadFile(s.picturePath(path))
		ok = err == nil
	}
	if !ok {
		return nil, false
	}
	return decodePicture(data)
}

func fileStamp(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return st.ModTime().Format(time.RFC3339Nano) + "/" + strconv.FormatInt(st.Size(), 10)
}

// ---------------------------------------------------------------------------
// Checking any track
// ---------------------------------------------------------------------------

// trackMenu is the right-click menu for a track in the list.
func (u *ui) trackMenu(t Track) *fyne.Menu {
	return fyne.NewMenu("",
		fyne.NewMenuItem("Play", func() { u.jump(t) }),
		fyne.NewMenuItem("Check Audio Quality…", func() { u.checkTrack(t) }),
	)
}

// checkTrack opens the Audio Quality dialog for t: its findings and its
// spectrogram, from the saved result or by analysing it now. Closing the
// dialog stops the analysis. It returns the dialog's panel and spectrogram,
// for tests.
func (u *ui) checkTrack(t Track) (*qualityPanel, *spectrogramView, dialog.Dialog) {
	p := newQualityPanel()
	spec := newSpectrogramView()
	bar := widget.NewProgressBar()
	bar.Hide()
	title := widget.NewLabelWithStyle(t.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Truncation = fyne.TextTruncateEllipsis
	var detail []string
	for _, s := range []string{t.Artist, t.Album} {
		if s != "" {
			detail = append(detail, s)
		}
	}
	levels := u.levelsControl(spec)
	levels.Hide()
	content := container.NewVBox(title, secondaryLabel(strings.Join(detail, " · ")),
		p.format, p.findings, spec, levels, bar)
	d := dialog.NewCustom("Audio Quality", "Close", content, u.win)
	ctx, cancel := context.WithCancel(context.Background())
	d.SetOnClosed(cancel)
	relayout := func() {
		content.Refresh()
		d.Resize(fyne.NewSize(700, content.MinSize().Height)) // grows with the findings
	}
	p.show(assessment{checks: []check{{checkPending, "Reading the file…"}}})
	relayout()
	d.Show()

	u.async(func() {
		// post runs f on the UI thread unless the dialog has been closed.
		post := func(f func()) {
			fyne.Do(func() {
				if ctx.Err() == nil {
					f()
					relayout()
				}
			})
		}
		info, err := probe(t.Path)
		if err != nil || !canAnalyse(info) {
			post(func() {
				p.show(qualityText(info, err, nil, notChecked, false))
				spec.set(nil, 0, 0, 0, "No spectrogram: this file can't be decoded")
			})
			return
		}
		show := func(rep qualityReport, img *image.Gray) {
			p.show(qualityText(info, nil, &rep, checked, false))
			spec.set(img, info.SampleRate, info.Duration, rep.Spectrum.Cutoff, "No sound to draw")
			levels.Hidden = img == nil
		}

		stamp := fileStamp(t.Path)
		var rep qualityReport
		if u.qualityStore.get(t.Path, stamp, &rep) {
			if img, ok := u.qualityStore.picture(t.Path); ok {
				post(func() { show(rep, img) })
				return
			}
		}
		post(func() {
			p.show(qualityText(info, nil, nil, checking, false))
			spec.set(nil, info.SampleRate, 0, 0, "Analysing…")
			bar.Show()
		})
		rep, img, err := analyse(ctx, t.Path, info, true, func(done float64) {
			post(func() { bar.SetValue(done) })
		})
		if err != nil {
			return // closed
		}
		level := assess(info, &rep).level
		u.qualityStore.putPicture(t.Path, img)
		u.qualityStore.put(t.Path, stamp, rep, level)
		post(func() {
			bar.Hide()
			show(rep, img)
		})
		fyne.Do(func() { u.qualityChecked(t.Path, rep, level) }) // it may be playing by now
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
