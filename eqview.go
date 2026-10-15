package main

// The Equalizer window, from Controls > Equalizer…: a switch, presets, a
// graph of the response, and for each band a slider for its gain and
// boxes for its frequency and width. It is a window of its own, so it can
// stay open while choosing music. Changes are heard at once and saved.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const prefEQ = "equalizer" // the setting, as JSON

// eqPresets are gains for the ten bands, at their usual frequencies.
var eqPresets = []struct {
	name  string
	gains [EQBands]float64
}{
	{"Flat", [EQBands]float64{}},
	{"Bass Boost", [EQBands]float64{6, 5, 4, 2, 0, 0, 0, 0, 0, 0}},
	{"Bass Cut", [EQBands]float64{-6, -5, -4, -2, 0, 0, 0, 0, 0, 0}},
	{"Treble Boost", [EQBands]float64{0, 0, 0, 0, 0, 0, 2, 4, 5, 6}},
	{"Treble Cut", [EQBands]float64{0, 0, 0, 0, 0, 0, -2, -4, -5, -6}},
	{"Vocal", [EQBands]float64{-2, -2, -1, 0, 2, 3, 3, 2, 0, -1}},
	{"Loudness", [EQBands]float64{5, 4, 2, 0, -1, 0, 0, 1, 3, 4}},
}

const eqCustom = "Custom"

// loadEQ reads the saved setting and hands it to the player.
func (u *ui) loadEQ() {
	u.eq = EQSettings{Bands: DefaultEQBands()}
	if saved := u.prefs.String(prefEQ); saved != "" {
		var s EQSettings
		if json.Unmarshal([]byte(saved), &s) == nil {
			u.eq = s
		}
	}
	u.player.SetEQ(u.eq)
}

// setEQ applies a setting: to the player, the window and the Now Playing
// page. save also writes it to the preferences, which is left until a
// slider is let go.
func (u *ui) setEQ(s EQSettings, save bool) {
	u.eq = s
	u.player.SetEQ(s)
	if save {
		if b, err := json.Marshal(s); err == nil {
			u.prefs.SetString(prefEQ, string(b))
		}
	}
	if u.eqView != nil {
		u.eqView.show(s)
	}
	u.updateOutputInfo()
}

// showEqualizer opens the Equalizer window, or brings it to the front.
func (u *ui) showEqualizer() {
	if u.eqView != nil {
		u.eqView.win.RequestFocus()
		return
	}
	v := u.newEQView()
	u.eqView = v
	v.win.SetOnClosed(func() { u.eqView = nil })
	v.show(u.eq)
	v.win.Show()
}

// eqView is the Equalizer window's widgets.
type eqView struct {
	win      fyne.Window
	on       *widget.Check
	preset   *widget.Select
	graph    *eqGraph
	gains    [EQBands]*widget.Slider
	values   [EQBands]*widget.Label
	freqs    [EQBands]*widget.Entry
	qs       [EQBands]*widget.Entry
	headroom *widget.Label
	showing  bool // filling in the widgets: their callbacks are ignored
}

func (u *ui) newEQView() *eqView {
	v := &eqView{win: fyne.CurrentApp().NewWindow("Equalizer")}
	change := func(f func(s *EQSettings), save bool) {
		if v.showing {
			return
		}
		s := u.eq
		f(&s)
		u.setEQ(s, save)
	}
	v.on = widget.NewCheck("On", func(on bool) { change(func(s *EQSettings) { s.On = on }, true) })
	names := []string{}
	for _, p := range eqPresets {
		names = append(names, p.name)
	}
	v.preset = widget.NewSelect(names, func(name string) {
		for _, p := range eqPresets {
			if p.name == name {
				change(func(s *EQSettings) {
					s.On, s.Bands = true, DefaultEQBands()
					for i, g := range p.gains {
						s.Bands[i].Gain = g
					}
				}, true)
			}
		}
	})
	v.preset.PlaceHolder = eqCustom
	v.graph = newEQGraph()
	v.headroom = secondaryLabel("")

	columns := make([]fyne.CanvasObject, EQBands)
	for i := range EQBands {
		v.values[i] = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
		v.values[i].SizeName = theme.SizeNameCaptionText
		g := widget.NewSlider(-EQMaxGain, EQMaxGain)
		g.Step = 0.5
		g.Orientation = widget.Vertical
		g.OnChanged = func(gain float64) {
			change(func(s *EQSettings) { s.On, s.Bands[i].Gain = true, gain }, false)
		}
		g.OnChangeEnded = func(float64) { change(func(*EQSettings) {}, true) }
		v.gains[i] = g
		v.freqs[i] = bandEntry(func(text string) {
			if f, ok := parseFreq(text); ok {
				change(func(s *EQSettings) { s.Bands[i].Freq = f }, true)
			}
		})
		v.qs[i] = bandEntry(func(text string) {
			if q, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil && q >= 0.1 && q <= 10 {
				change(func(s *EQSettings) { s.Bands[i].Q = q }, true)
			}
		})
		kind := "Peak"
		switch i {
		case 0:
			kind = "Low shelf"
		case EQBands - 1:
			kind = "High shelf"
		}
		tall := canvas.NewRectangle(color.Transparent)
		tall.SetMinSize(fyne.NewSize(0, 170))
		columns[i] = container.NewVBox(
			v.values[i],
			container.NewStack(tall, g),
			v.freqs[i],
			v.qs[i],
			caption(kind),
		)
	}
	top := container.NewHBox(v.on, layout.NewSpacer(), widget.NewLabel("Preset"),
		container.NewGridWrap(fyne.NewSize(170, v.preset.MinSize().Height), v.preset))
	help := secondaryLabel("Each band: its gain in dB, its frequency in Hz, and its width (Q): the higher, the narrower.")
	help.Wrapping = fyne.TextWrapWord
	v.win.SetContent(container.NewPadded(container.NewBorder(
		container.NewVBox(top, v.graph),
		container.NewVBox(help, v.headroom), nil, nil,
		container.NewGridWithColumns(EQBands, columns...),
	)))
	v.win.Resize(fyne.NewSize(780, 600))
	return v
}

// show fills in the widgets from a setting.
func (v *eqView) show(s EQSettings) {
	v.showing = true
	defer func() { v.showing = false }()
	v.on.SetChecked(s.On)
	preset := ""
	for _, p := range eqPresets {
		match := true
		for i, b := range s.Bands {
			d := DefaultEQBands()[i]
			match = match && b.Gain == p.gains[i] && b.Freq == d.Freq && b.Q == d.Q
		}
		if match {
			preset = p.name
		}
	}
	if preset == "" {
		v.preset.ClearSelected() // shows Custom
	} else {
		v.preset.SetSelected(preset)
	}
	for i, b := range s.Bands {
		v.gains[i].SetValue(b.Gain)
		v.values[i].SetText(fmt.Sprintf("%+.1f", b.Gain))
		if !focused(v.win, v.freqs[i]) {
			v.freqs[i].SetText(formatFreq(b.Freq))
		}
		if !focused(v.win, v.qs[i]) {
			v.qs[i].SetText(strconv.FormatFloat(b.Q, 'f', -1, 64))
		}
	}
	v.graph.show(s)
	f := NewEQFilters(s, 48000)
	switch {
	case !s.On:
		v.headroom.SetText("Off: the sound passes through untouched.")
	case s.Flat():
		v.headroom.SetText("Flat: the sound passes through untouched.")
	case f.Peak() > 0.05:
		v.headroom.SetText(fmt.Sprintf("Turned down %.1f dB so the boost can't clip.", f.Peak()))
	default:
		v.headroom.SetText("")
	}
}

// focused reports whether e is being typed in, so it isn't rewritten
// under the cursor.
func focused(w fyne.Window, e *widget.Entry) bool {
	return w.Canvas().Focused() == e
}

// bandEntry is a small box for a band's number, applied as it is typed.
func bandEntry(onChanged func(string)) *widget.Entry {
	e := widget.NewEntry()
	e.OnChanged = onChanged
	return e
}

func caption(text string) *widget.Label {
	l := widget.NewLabelWithStyle(text, fyne.TextAlignCenter, fyne.TextStyle{})
	l.SizeName = theme.SizeNameCaptionText
	l.Importance = widget.LowImportance
	return l
}

// formatFreq shows a frequency as 62 or 1.5k.
func formatFreq(f float64) string {
	if f >= 1000 {
		return strconv.FormatFloat(f/1000, 'f', -1, 64) + "k"
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseFreq reads a frequency typed as 62, 1500, 1.5k or 1.5 kHz, from
// 20 Hz to 20 kHz.
func parseFreq(text string) (float64, bool) {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.TrimSpace(strings.TrimSuffix(t, "hz"))
	mult := 1.0
	if strings.HasSuffix(t, "k") {
		t, mult = strings.TrimSuffix(t, "k"), 1000
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	f *= mult
	if err != nil || f < 20 || f > 20000 {
		return 0, false
	}
	return f, true
}

// eqGraph draws the equalizer's response from 20 Hz to 20 kHz, ±12 dB.
type eqGraph struct {
	widget.BaseWidget
	setting EQSettings
}

func newEQGraph() *eqGraph {
	g := &eqGraph{}
	g.ExtendBaseWidget(g)
	return g
}

func (g *eqGraph) show(s EQSettings) {
	g.setting = s
	g.Refresh()
}

func (g *eqGraph) CreateRenderer() fyne.WidgetRenderer {
	r := &eqGraphRenderer{g: g, bg: canvas.NewRectangle(color.Transparent)}
	r.bg.CornerRadius = theme.InputRadiusSize()
	for _, db := range []float64{12, 6, 0, -6, -12} {
		r.dbLines = append(r.dbLines, canvas.NewLine(color.Transparent))
		r.dbLabels = append(r.dbLabels, gridLabel(fmt.Sprintf("%+g", db)))
	}
	for _, f := range []float64{100, 1000, 10000} {
		r.fLines = append(r.fLines, canvas.NewLine(color.Transparent))
		r.fLabels = append(r.fLabels, gridLabel(formatFreq(f)))
	}
	r.curve = canvas.NewRaster(r.drawCurve)
	r.Refresh()
	return r
}

func gridLabel(text string) *canvas.Text {
	t := canvas.NewText(text, color.Transparent)
	t.TextSize = theme.CaptionTextSize() * 0.9
	return t
}

type eqGraphRenderer struct {
	g                 *eqGraph
	bg                *canvas.Rectangle
	dbLines, fLines   []*canvas.Line
	dbLabels, fLabels []*canvas.Text
	curve             *canvas.Raster
	size              fyne.Size
	filters           *EQFilters // nil when flat or off
	line              color.Color
}

func (r *eqGraphRenderer) MinSize() fyne.Size { return fyne.NewSize(300, 140) }

// x places a frequency across the graph, on a log scale; y a gain in dB.
func (r *eqGraphRenderer) x(f float64) float32 {
	return float32(math.Log(f/20)/math.Log(1000)) * r.size.Width
}

func (r *eqGraphRenderer) y(db float64) float32 {
	pad := float32(8)
	return pad + float32((EQMaxGain-db)/(2*EQMaxGain))*(r.size.Height-2*pad)
}

func (r *eqGraphRenderer) Layout(size fyne.Size) {
	r.size = size
	r.bg.Resize(size)
	for i, db := range []float64{12, 6, 0, -6, -12} {
		y := r.y(db)
		r.dbLines[i].Position1, r.dbLines[i].Position2 = fyne.NewPos(0, y), fyne.NewPos(size.Width, y)
		r.dbLabels[i].Move(fyne.NewPos(4, y-r.dbLabels[i].MinSize().Height))
	}
	for i, f := range []float64{100, 1000, 10000} {
		x := r.x(f)
		r.fLines[i].Position1, r.fLines[i].Position2 = fyne.NewPos(x, 0), fyne.NewPos(x, size.Height)
		r.fLabels[i].Move(fyne.NewPos(x+3, size.Height-r.fLabels[i].MinSize().Height))
	}
	r.curve.Resize(size)
}

// drawCurve draws the response into a w×h pixel image, flat while the
// equalizer is off. The curve is a function of x, so each column of
// pixels is filled between where the curve enters and leaves it, which
// joins up without gaps at any slope.
func (r *eqGraphRenderer) drawCurve(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w < 2 || r.size.Width == 0 || r.line == nil {
		return img
	}
	scale := float64(h) / float64(r.size.Height)
	yAt := func(px float64) float64 { // the curve's height at pixel column px
		freq := 20 * math.Pow(1000, px/float64(w))
		db := 0.0
		if r.filters != nil {
			db = min(max(r.filters.ResponseDB(freq), -EQMaxGain-1), EQMaxGain+1)
		}
		return float64(r.y(db)) * scale
	}
	c := color.NRGBAModel.Convert(r.line).(color.NRGBA)
	half := scale // the line is 2 points thick
	prev := yAt(0)
	for x := range w {
		next := yAt(float64(x) + 1)
		top, bottom := min(prev, next)-half, max(prev, next)+half
		for y := max(int(top), 0); y <= min(int(bottom), h-1); y++ {
			// Cover of this pixel by [top, bottom], for smooth edges.
			cover := min(float64(y+1), bottom) - max(float64(y), top)
			if cover <= 0 {
				continue
			}
			a := c
			a.A = uint8(float64(c.A) * min(cover, 1))
			img.SetNRGBA(x, y, a)
		}
		prev = next
	}
	return img
}

func (r *eqGraphRenderer) Refresh() {
	v := fyne.CurrentApp().Settings().ThemeVariant()
	th := r.g.Theme()
	r.bg.FillColor = th.Color(theme.ColorNameInputBackground, v)
	grid := color.NRGBAModel.Convert(th.Color(theme.ColorNameForeground, v)).(color.NRGBA)
	grid.A = 0x28 // faint, on either background
	for _, l := range append(r.dbLines, r.fLines...) {
		l.StrokeColor, l.StrokeWidth = grid, 1
	}
	for _, t := range append(r.dbLabels, r.fLabels...) {
		t.Color = th.Color(theme.ColorNamePlaceHolder, v)
	}
	r.line = th.Color(theme.ColorNamePrimary, v)
	if !r.g.setting.On {
		r.line = th.Color(theme.ColorNameDisabled, v)
	}
	r.filters = DesignEQ(r.g.setting, 48000)
	r.curve.Refresh()
	canvas.Refresh(r.g)
}

func (r *eqGraphRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.bg}
	for _, l := range append(r.dbLines, r.fLines...) {
		objs = append(objs, l)
	}
	for _, t := range append(r.dbLabels, r.fLabels...) {
		objs = append(objs, t)
	}
	return append(objs, r.curve)
}

func (r *eqGraphRenderer) Destroy() {}
