package main

// The spectrogram: a picture of a file's sound, with time across and
// frequency up, brighter where it is louder. A file made from an MP3 shows a
// flat "shelf" where the encoder cut everything off; a real recording fades
// out unevenly.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	specRows     = 256 // frequency rows, from 0 Hz to half the sample rate
	pictureWidth = 720 // time columns

	// A picture keeps each point's level, from 0 dBFS down to keptFloorDB,
	// so it can be drawn for any range of levels without analysing again.
	keptFloorDB = -140

	// The range of levels drawn, from black to pale yellow, unless changed.
	// -120 dBFS is about the level of 16-bit dither.
	defaultTopDB   = 0
	defaultFloorDB = -120
)

// picture returns the spectrogram's levels, or nil if no audio was
// analysed. Each pixel is a level: 0 for keptFloorDB or quieter, up to 255
// for full scale. paint turns it into colours.
func (s *spectrum) picture() *image.Gray {
	n := len(s.columns)
	if n == 0 {
		return nil
	}
	img := image.NewGray(image.Rect(0, 0, pictureWidth, specRows))
	fullScale := math.Pow(specSize/4, 2) // a full-scale tone's bin, after the Hann window
	binsPerRow := float64(specSize / 2 / specRows)
	for x := range pictureWidth {
		a := x * n / pictureWidth
		b := max((x+1)*n/pictureWidth, a+1)
		for r := range specRows {
			p := 0.0
			for _, col := range s.columns[a:b] {
				p += float64(col[r])
			}
			p /= float64(b-a) * binsPerRow
			db := 10 * math.Log10(p/fullScale+1e-30)
			v := math.Round((db - keptFloorDB) / -keptFloorDB * 255)
			img.Pix[(specRows-1-r)*img.Stride+x] = uint8(min(max(v, 0), 255))
		}
	}
	return img
}

// levelDB is the level a picture's pixel value stands for.
func levelDB(v uint8) float64 { return keptFloorDB * (1 - float64(v)/255) }

// paint colours a spectrogram's levels, from black at floor dBFS or below
// to pale yellow at top or above.
func paint(levels *image.Gray, top, floor float64) *image.RGBA {
	var lut [256]color.RGBA
	for v := range lut {
		c := heat((levelDB(uint8(v)) - floor) / (top - floor))
		lut[v] = color.RGBA{c.R, c.G, c.B, 0xFF}
	}
	b := levels.Bounds()
	img := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		src := levels.Pix[(y-b.Min.Y)*levels.Stride:]
		dst := img.Pix[(y-b.Min.Y)*img.Stride:]
		for x := range b.Dx() {
			c := lut[src[x]]
			dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = c.R, c.G, c.B, c.A
		}
	}
	return img
}

// heatStops is the colour scale, from silence to full scale: black through
// violet and red to pale yellow, like the "magma" scale, which stays
// readable in greyscale and to colour-blind eyes.
var heatStops = []color.NRGBA{
	{0x00, 0x00, 0x04, 0xFF},
	{0x3B, 0x0F, 0x70, 0xFF},
	{0x8C, 0x29, 0x81, 0xFF},
	{0xDE, 0x49, 0x68, 0xFF},
	{0xFE, 0x9F, 0x6D, 0xFF},
	{0xFC, 0xFD, 0xBF, 0xFF},
}

func heat(t float64) color.NRGBA {
	t = min(max(t, 0), 1) * float64(len(heatStops)-1)
	i := min(int(t), len(heatStops)-2)
	return blend(heatStops[i], heatStops[i+1], t-float64(i))
}

// heatScale is the colour scale as a picture, loudest at the top, for the
// legend.
func heatScale() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 256))
	for y := range 256 {
		img.SetNRGBA(0, y, heat(1-float64(y)/255))
	}
	return img
}

// Levels are saved losslessly, as a greyscale PNG.
func encodePicture(levels *image.Gray) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, levels)
	return buf.Bytes(), err
}

func decodePicture(data []byte) (*image.Gray, bool) {
	img, err := png.Decode(bytes.NewReader(data))
	g, ok := img.(*image.Gray)
	return g, err == nil && ok && g.Bounds().Dx() > 0
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// spectrogramView shows a spectrogram with a frequency scale on the left, a
// time scale below, a colour legend in dB on the right, and the cut-off, if
// one was found, marked across it.
type spectrogramView struct {
	widget.BaseWidget
	levels     *image.Gray // nil until there is one
	rate       int         // sample rate, for the frequency scale
	length     time.Duration
	cutoff     float64 // Hz, or 0
	note       string  // shown in place of the picture
	top, floor float64 // dBFS drawn brightest and black
}

func newSpectrogramView() *spectrogramView {
	v := &spectrogramView{top: defaultTopDB, floor: defaultFloorDB}
	v.ExtendBaseWidget(v)
	return v
}

// set shows levels, or note in its place if levels is nil.
func (v *spectrogramView) set(levels *image.Gray, rate int, length time.Duration, cutoff float64, note string) {
	v.levels, v.rate, v.length, v.cutoff, v.note = levels, rate, length, cutoff, note
	v.Refresh()
}

// setRange changes the levels drawn: top dBFS and louder are brightest,
// floor and quieter black.
func (v *spectrogramView) setRange(top, floor float64) {
	v.top, v.floor = top, min(floor, top-1)
	v.Refresh()
}

func (v *spectrogramView) MinSize() fyne.Size { return fyne.NewSize(560, 240) }

func (v *spectrogramView) CreateRenderer() fyne.WidgetRenderer {
	r := &spectrogramRenderer{
		v:      v,
		frame:  canvas.NewRectangle(color.Transparent),
		pic:    canvas.NewImageFromImage(nil),
		note:   canvas.NewText("", nil),
		line:   canvas.NewLine(nil),
		marker: canvas.NewText("", nil),
		scale:  canvas.NewImageFromImage(heatScale()),
		unit:   axisText("dB", nil),
	}
	r.frame.CornerRadius = 4
	r.pic.FillMode = canvas.ImageFillStretch
	r.pic.ScaleMode = canvas.ImageScaleSmooth
	r.scale.FillMode = canvas.ImageFillStretch
	r.scale.ScaleMode = canvas.ImageScaleSmooth
	r.note.TextSize = theme.CaptionTextSize()
	r.line.StrokeWidth = 1.5
	r.marker.TextSize = axisTextSize
	r.marker.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

const (
	axisTextSize = 10
	legendGap    = 10 // between the picture and the legend
	legendWidth  = 10
)

type spectrogramRenderer struct {
	v      *spectrogramView
	frame  *canvas.Rectangle // behind the picture, and alone when there is none
	pic    *canvas.Image
	note   *canvas.Text
	freqs  []*canvas.Text // frequency labels, with their Hz
	hz     []float64
	times  []*canvas.Text // time labels, evenly spaced
	line   *canvas.Line   // the cut-off
	marker *canvas.Text   // its label

	scale *canvas.Image  // the legend: the colours…
	dbs   []*canvas.Text // …with levels beside them…
	db    []float64
	unit  *canvas.Text // …and "dB" below

	// The picture last painted, kept until the levels or range change.
	painted               *image.RGBA
	paintedOf             *image.Gray
	paintedTop, paintedFl float64
}

// plot returns the picture's area within size, leaving room for the scales.
func (r *spectrogramRenderer) plot(size fyne.Size) (pos fyne.Position, s fyne.Size) {
	left := fyne.MeasureText("20k", axisTextSize, fyne.TextStyle{}).Width + 8
	right := legendGap + legendWidth + 4 + fyne.MeasureText("-140", axisTextSize, fyne.TextStyle{}).Width + 2
	bottom := fyne.MeasureText("0:00", axisTextSize, fyne.TextStyle{}).Height + 4
	top := float32(6) // half a label, so the top one fits
	return fyne.NewPos(left, top), fyne.NewSize(size.Width-left-right, size.Height-top-bottom)
}

func (r *spectrogramRenderer) Layout(size fyne.Size) {
	at, ps := r.plot(size)
	r.frame.Move(at)
	r.frame.Resize(ps)
	r.pic.Move(at)
	r.pic.Resize(ps)
	ns := r.note.MinSize()
	r.note.Move(fyne.NewPos(at.X+(ps.Width-ns.Width)/2, at.Y+(ps.Height-ns.Height)/2))

	nyquist := float64(r.v.rate) / 2
	y := func(hz float64) float32 { return at.Y + ps.Height*float32(1-hz/nyquist) }
	for i, t := range r.freqs {
		s := t.MinSize()
		t.Move(fyne.NewPos(at.X-s.Width-6, y(r.hz[i])-s.Height/2))
	}
	for i, t := range r.times {
		s := t.MinSize()
		x := at.X + ps.Width*float32(i)/float32(len(r.times)-1) - s.Width/2
		x = min(max(x, at.X), at.X+ps.Width-s.Width) // keep the end labels inside
		t.Move(fyne.NewPos(x, at.Y+ps.Height+2))
	}
	if r.v.cutoff > 0 && nyquist > 0 {
		cy := y(r.v.cutoff)
		r.line.Position1 = fyne.NewPos(at.X, cy)
		r.line.Position2 = fyne.NewPos(at.X+ps.Width, cy)
		ms := r.marker.MinSize()
		r.marker.Move(fyne.NewPos(at.X+ps.Width-ms.Width-6, cy-ms.Height-1))
	}

	lx := at.X + ps.Width + legendGap
	r.scale.Move(fyne.NewPos(lx, at.Y))
	r.scale.Resize(fyne.NewSize(legendWidth, ps.Height))
	span := r.v.top - r.v.floor
	for i, t := range r.dbs {
		s := t.MinSize()
		ty := at.Y + ps.Height*float32((r.v.top-r.db[i])/span)
		t.Move(fyne.NewPos(lx+legendWidth+4, ty-s.Height/2))
	}
	r.unit.Move(fyne.NewPos(lx, at.Y+ps.Height+2))
}

func (r *spectrogramRenderer) MinSize() fyne.Size { return r.v.MinSize() }

func (r *spectrogramRenderer) Refresh() {
	v := r.v
	muted := themeColor(theme.ColorNamePlaceHolder)
	r.frame.FillColor = themeColor(theme.ColorNameInputBackground)
	r.pic.Image = r.paint()
	r.pic.Hidden = v.levels == nil
	r.note.Text, r.note.Color = v.note, muted
	r.note.Hidden = v.levels != nil

	// Frequency labels every 2, 5, 10 or 20 kHz, whichever gives about five.
	r.freqs, r.hz = nil, nil
	nyquist := float64(v.rate) / 2
	if nyquist > 0 {
		step := 20_000.0
		for _, s := range []float64{2_000, 5_000, 10_000} {
			if nyquist/s <= 6 {
				step = s
				break
			}
		}
		for hz := 0.0; hz <= nyquist; hz += step {
			label := "0"
			if hz > 0 {
				label = fmt.Sprintf("%gk", hz/1000)
			}
			r.freqs = append(r.freqs, axisText(label, muted))
			r.hz = append(r.hz, hz)
		}
	}
	r.times = nil
	if v.length > 0 && v.levels != nil {
		for i := range 5 {
			r.times = append(r.times, axisText(formatTime(v.length*time.Duration(i)/4), muted))
		}
	}

	// Legend labels every 10 or 20 dB, whichever gives no more than seven.
	r.dbs, r.db = nil, nil
	step := 10.0
	if (v.top-v.floor)/step > 6 {
		step = 20
	}
	for db := math.Floor(v.top/step) * step; db >= v.floor; db -= step {
		r.dbs = append(r.dbs, axisText(fmt.Sprintf("%g", db), muted))
		r.db = append(r.db, db)
	}
	r.unit.Color = muted

	warn := themeColor(theme.ColorNameWarning)
	r.line.StrokeColor = warn
	r.marker.Color = warn
	r.marker.Text = kHz(v.cutoff) + " cut-off"
	r.line.Hidden = v.cutoff <= 0 || v.levels == nil
	r.marker.Hidden = r.line.Hidden

	r.frame.Refresh()
	r.pic.Refresh()
	r.note.Refresh()
	r.line.Refresh()
	r.marker.Refresh()
	r.unit.Refresh()
	r.Layout(v.Size())
}

// paint returns the picture in colour for the current range, painting it
// only when the levels or the range have changed.
func (r *spectrogramRenderer) paint() image.Image {
	v := r.v
	if v.levels == nil {
		return nil
	}
	if r.paintedOf != v.levels || r.paintedTop != v.top || r.paintedFl != v.floor {
		r.painted = paint(v.levels, v.top, v.floor)
		r.paintedOf, r.paintedTop, r.paintedFl = v.levels, v.top, v.floor
	}
	return r.painted
}

func axisText(s string, c color.Color) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = axisTextSize
	return t
}

func (r *spectrogramRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.frame, r.pic, r.note, r.line, r.marker, r.scale, r.unit}
	for _, t := range r.freqs {
		objs = append(objs, t)
	}
	for _, t := range r.times {
		objs = append(objs, t)
	}
	for _, t := range r.dbs {
		objs = append(objs, t)
	}
	return objs
}

func (r *spectrogramRenderer) Destroy() {}
