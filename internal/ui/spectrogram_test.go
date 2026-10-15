package ui

import (
	"context"
	"image"
	"image/color"
	"math"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"mugig/internal/quality"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// brightness is the mean luminance (0-255) of img's rows from y0 to y1.
func brightness(img image.Image, y0, y1 int) float64 {
	sum, n := 0.0, 0
	for y := y0; y < y1; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			sum += (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 257
			n++
		}
	}
	return sum / float64(n)
}

// The picture of a file made from an MP3 is dark above the cut-off: the
// shelf the eye looks for.
func TestSpectrogramShowsShelf(t *testing.T) {
	const rate = 44100
	path := filepath.Join(t.TempDir(), "x.flac")
	audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(audiotest.Noise(4*rate, rate, audiotest.Lowpass(audiotest.Pink, 16000, 300)), 16))
	info, _ := audio.Probe(path)
	_, img, err := quality.Analyse(context.Background(), path, info, true, nil)
	if err != nil || img == nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != quality.PictureWidth || b.Dy() != quality.SpecRows {
		t.Fatalf("picture is %v", b)
	}
	row := func(hz float64) int { return quality.SpecRows - 1 - int(hz/(rate/2)*quality.SpecRows) }
	pic := paint(img, defaultTopDB, defaultFloorDB)
	above := brightness(pic, 0, row(17500))         // 17.5 kHz to the top
	below := brightness(pic, row(15000), row(5000)) // 5 to 15 kHz
	if above > 15 || below < 4*above+30 {
		t.Errorf("brightness above the cut-off %.0f, below %.0f", above, below)
	}
}

func TestSpectrogramViewMarksCutoff(t *testing.T) {
	newTestApp(t)
	v := newSpectrogramView()
	w := test.NewTempWindow(t, v)
	w.Resize(fyne.NewSize(600, 260))
	r := test.WidgetRenderer(v).(*spectrogramRenderer)

	v.set(nil, 44100, 0, 0, "Analysing…")
	if !r.pic.Hidden || r.note.Hidden || !r.line.Hidden {
		t.Error("with no picture, only the note should show")
	}

	v.set(image.NewGray(image.Rect(0, 0, 10, 10)), 44100, 3*time.Minute, 16000, "")
	if r.pic.Hidden || !r.note.Hidden || r.line.Hidden || r.marker.Text != "16 kHz cut-off" {
		t.Errorf("picture hidden %v, line hidden %v, marker %q", r.pic.Hidden, r.line.Hidden, r.marker.Text)
	}
	if len(r.freqs) != 5 || r.freqs[4].Text != "20k" || len(r.times) != 5 || r.times[4].Text != "3:00" {
		t.Errorf("scales: %d frequency labels, %d time labels", len(r.freqs), len(r.times))
	}
	// The line sits at 16 kHz on the frequency scale: between the 15k and
	// 20k labels, nearer 15k.
	ly := r.line.Position1.Y
	y15 := r.freqs[3].Position().Y + r.freqs[3].MinSize().Height/2
	y20 := r.freqs[4].Position().Y + r.freqs[4].MinSize().Height/2
	if !(ly < y15 && ly > y20 && y15-ly < ly-y20) {
		t.Errorf("cut-off at y=%v, 15k at %v, 20k at %v", ly, y15, y20)
	}
}

// Changing the range repaints the same levels: quiet sound that is black
// with the floor at -60 dB shows with the floor at -140 dB.
func TestSpectrogramRange(t *testing.T) {
	levels := image.NewGray(image.Rect(0, 0, 2, 1))
	quiet := uint8(math.Round((-100.0 - quality.KeptFloorDB) / -quality.KeptFloorDB * 255)) // -100 dBFS
	levels.Pix[0], levels.Pix[1] = quiet, 255
	narrow, wide := paint(levels, 0, -60), paint(levels, 0, -140)
	if b := brightness(narrow, 0, 1); b > 130 {
		t.Errorf("full scale and silence average %.0f", b)
	}
	if narrow.RGBAAt(0, 0) != (color.RGBA{0, 0, 4, 255}) {
		t.Errorf("-100 dB with the floor at -60 drawn %v, want black", narrow.RGBAAt(0, 0))
	}
	if brightness(wide.SubImage(image.Rect(0, 0, 1, 1)), 0, 1) < 20 {
		t.Errorf("-100 dB with the floor at -140 drawn %v, want visible", wide.RGBAAt(0, 0))
	}
	if narrow.RGBAAt(1, 0) != wide.RGBAAt(1, 0) {
		t.Error("full scale should be brightest either way")
	}
}

func TestSpectrogramLegend(t *testing.T) {
	newTestApp(t)
	v := newSpectrogramView()
	w := test.NewTempWindow(t, v)
	w.Resize(fyne.NewSize(600, 260))
	r := test.WidgetRenderer(v).(*spectrogramRenderer)
	v.set(image.NewGray(image.Rect(0, 0, 10, 10)), 44100, time.Minute, 0, "")

	labels := func() (out []string) {
		for _, t := range r.dbs {
			out = append(out, t.Text)
		}
		return out
	}
	if got := labels(); !slices.Equal(got, []string{"0", "-20", "-40", "-60", "-80", "-100", "-120"}) {
		t.Errorf("legend %q", got)
	}
	// The top and bottom labels sit at the ends of the colour scale.
	mid := func(i int) float32 { return r.dbs[i].Position().Y + r.dbs[i].MinSize().Height/2 }
	top, bottom := r.scale.Position().Y, r.scale.Position().Y+r.scale.Size().Height
	if math.Abs(float64(mid(0)-top)) > 1 || math.Abs(float64(mid(len(r.dbs)-1)-bottom)) > 1 {
		t.Errorf("labels from %v to %v, scale from %v to %v", mid(0), mid(len(r.dbs)-1), top, bottom)
	}
	if r.scale.Position().X < r.pic.Position().X+r.pic.Size().Width {
		t.Error("legend overlaps the picture")
	}

	v.setRange(-20, -80)
	if got := labels(); !slices.Equal(got, []string{"-20", "-30", "-40", "-50", "-60", "-70", "-80"}) {
		t.Errorf("legend %q", got)
	}
}

// The range chosen in the dialog is applied and remembered.
func TestLevelsControl(t *testing.T) {
	u, _, _ := newTestUI(t)
	spec := newSpectrogramView()
	row := u.levelsControl(spec)
	if spec.top != 0 || spec.floor != -120 {
		t.Errorf("default range %v to %v", spec.top, spec.floor)
	}
	floor := row.Objects[3].(*widget.Select)
	floor.SetSelected("-140 dB")
	if spec.floor != -140 {
		t.Errorf("floor %v after choosing -140 dB", spec.floor)
	}
	row.Objects[1].(*widget.Select).SetSelected("-20 dB")

	again := newSpectrogramView()
	u.levelsControl(again)
	if again.top != -20 || again.floor != -140 {
		t.Errorf("next dialog's range %v to %v", again.top, again.floor)
	}
}
