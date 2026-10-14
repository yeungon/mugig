package main

import (
	"context"
	"image"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
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
	writeFLAC(t, path, rate, 16, quantize(noise(4*rate, rate, lowpass(pink, 16000, 300)), 16))
	info, _ := probe(path)
	_, img, err := analyse(context.Background(), path, info, true, nil)
	if err != nil || img == nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != pictureWidth || b.Dy() != specRows {
		t.Fatalf("picture is %v", b)
	}
	row := func(hz float64) int { return specRows - 1 - int(hz/(rate/2)*specRows) }
	above := brightness(img, 0, row(17500))         // 17.5 kHz to the top
	below := brightness(img, row(15000), row(5000)) // 5 to 15 kHz
	if above > 15 || below < 4*above+30 {
		t.Errorf("brightness above the cut-off %.0f, below %.0f", above, below)
	}
}

func TestAnalyseReportsProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	writeFLAC(t, path, 44100, 16, quantize(noise(3*44100, 44100, pink), 16))
	info, _ := probe(path)
	var got []float64
	analyse(context.Background(), path, info, true, func(f float64) { got = append(got, f) })
	if len(got) < 10 || got[len(got)-1] != 1 {
		t.Fatalf("progress %v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("progress went backwards: %v", got)
		}
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

	v.set(image.NewRGBA(image.Rect(0, 0, 10, 10)), 44100, 3*time.Minute, 16000, "")
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
