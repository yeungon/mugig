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
	"image/jpeg"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	specRows       = 256  // frequency rows, from 0 Hz to half the sample rate
	pictureWidth   = 720  // time columns
	pictureFloorDB = -120 // dBFS drawn as black, about 16-bit dither's level
)

// picture draws the spectrogram, or returns nil if no audio was analysed.
func (s *spectrum) picture() image.Image {
	n := len(s.columns)
	if n == 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, pictureWidth, specRows))
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
			img.Set(x, specRows-1-r, heat((db-pictureFloorDB)/-pictureFloorDB))
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

func encodePicture(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88})
	return buf.Bytes(), err
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// spectrogramView shows a spectrogram with a frequency scale on the left, a
// time scale below, and the cut-off, if one was found, marked across it.
type spectrogramView struct {
	widget.BaseWidget
	img    image.Image // nil until there is one
	rate   int         // sample rate, for the frequency scale
	length time.Duration
	cutoff float64 // Hz, or 0
	note   string  // shown in place of the picture
}

func newSpectrogramView() *spectrogramView {
	v := &spectrogramView{}
	v.ExtendBaseWidget(v)
	return v
}

// set shows img, or note in its place if img is nil.
func (v *spectrogramView) set(img image.Image, rate int, length time.Duration, cutoff float64, note string) {
	v.img, v.rate, v.length, v.cutoff, v.note = img, rate, length, cutoff, note
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
	}
	r.frame.CornerRadius = 4
	r.pic.FillMode = canvas.ImageFillStretch
	r.pic.ScaleMode = canvas.ImageScaleSmooth
	r.note.TextSize = theme.CaptionTextSize()
	r.line.StrokeWidth = 1.5
	r.marker.TextSize = axisTextSize
	r.marker.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

const axisTextSize = 10

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
}

// plot returns the picture's area within size, leaving room for the scales.
func (r *spectrogramRenderer) plot(size fyne.Size) (pos fyne.Position, s fyne.Size) {
	left := fyne.MeasureText("20k", axisTextSize, fyne.TextStyle{}).Width + 8
	bottom := fyne.MeasureText("0:00", axisTextSize, fyne.TextStyle{}).Height + 4
	top := float32(6) // half a label, so the top one fits
	return fyne.NewPos(left, top), fyne.NewSize(size.Width-left-4, size.Height-top-bottom)
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
}

func (r *spectrogramRenderer) MinSize() fyne.Size { return r.v.MinSize() }

func (r *spectrogramRenderer) Refresh() {
	v := r.v
	muted := themeColor(theme.ColorNamePlaceHolder)
	r.frame.FillColor = themeColor(theme.ColorNameInputBackground)
	r.pic.Image = v.img
	r.pic.Hidden = v.img == nil
	r.note.Text, r.note.Color = v.note, muted
	r.note.Hidden = v.img != nil

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
	if v.length > 0 && v.img != nil {
		for i := range 5 {
			r.times = append(r.times, axisText(formatTime(v.length*time.Duration(i)/4), muted))
		}
	}

	warn := themeColor(theme.ColorNameWarning)
	r.line.StrokeColor = warn
	r.marker.Color = warn
	r.marker.Text = kHz(v.cutoff) + " cut-off"
	r.line.Hidden = v.cutoff <= 0 || v.img == nil
	r.marker.Hidden = r.line.Hidden

	r.frame.Refresh()
	r.pic.Refresh()
	r.note.Refresh()
	r.line.Refresh()
	r.marker.Refresh()
	r.Layout(v.Size())
}

func axisText(s string, c color.Color) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = axisTextSize
	return t
}

func (r *spectrogramRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.frame, r.pic, r.note, r.line, r.marker}
	for _, t := range r.freqs {
		objs = append(objs, t)
	}
	for _, t := range r.times {
		objs = append(objs, t)
	}
	return objs
}

func (r *spectrogramRenderer) Destroy() {}
