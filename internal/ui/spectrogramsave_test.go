package ui

import (
	"image"
	"image/png"
	"mugig/internal/audiotest"
	"mugig/internal/library"
	"mugig/internal/quality"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// saveButton finds the Audio Quality dialog's Save as PNG button.
func saveButton(t *testing.T, u *ui) *widget.Button {
	t.Helper()
	var found *widget.Button
	visit(u.win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Save as PNG…" {
			found = b
		}
	})
	if found == nil {
		t.Fatal("no Save as PNG button")
	}
	return found
}

// shownIn reports whether o and every container above it in the dialog
// are visible.
func shownIn(u *ui, o fyne.CanvasObject) bool {
	var path []fyne.CanvasObject
	var walk func(fyne.CanvasObject, []fyne.CanvasObject) bool
	walk = func(c fyne.CanvasObject, above []fyne.CanvasObject) bool {
		if c == o {
			path = append(above, c)
			return true
		}
		if ct, ok := c.(*fyne.Container); ok {
			for _, child := range ct.Objects {
				if walk(child, append(above, c)) {
					return true
				}
			}
		}
		if w, ok := c.(fyne.Widget); ok {
			for _, child := range test.WidgetRenderer(w).Objects() {
				if walk(child, append(above, c)) {
					return true
				}
			}
		}
		return false
	}
	walk(u.win.Canvas().Overlays().Top(), nil)
	for _, c := range path {
		if !c.Visible() {
			return false
		}
	}
	return len(path) > 0
}

func TestSaveSpectrogram(t *testing.T) {
	u, _, _ := newTestUI(t)
	path := filepath.Join(t.TempDir(), "song.flac")
	s := audiotest.Noise(4*44100, 44100, audiotest.Lowpass(audiotest.Pink, 16000, 300))
	audiotest.WriteFLAC(t, path, 44100, 16, audiotest.Quantize(s, 16))
	tr := library.Track{Path: path, Title: "Mid/night", Artist: "AC/DC", Album: "Live"}

	// No button until there is a picture to save.
	var pending []func()
	u.async = func(f func()) { pending = append(pending, f) }
	u.checkTrack(tr)
	btn := saveButton(t, u)
	if shownIn(u, btn) {
		t.Error("Save shown before the spectrogram is drawn")
	}
	for len(pending) > 0 {
		f := pending[0]
		pending = pending[1:]
		f()
	}
	if !shownIn(u, btn) {
		t.Fatal("Save not shown with the spectrogram")
	}

	dir := t.TempDir()
	var offered string
	u.save = func(_ fyne.Window, name string, done func(string)) {
		offered = name
		done(filepath.Join(dir, name))
	}
	u.async = func(f func()) { f() }
	test.Tap(btn)
	if offered != "AC-DC - Mid-night (spectrogram).png" {
		t.Errorf("offered the name %q", offered)
	}
	f, err := os.Open(filepath.Join(dir, offered))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != pictureWidth*pictureScale || b.Dy() < pictureSpecH*pictureScale || b.Dy() > 2*pictureSpecH*pictureScale {
		t.Errorf("image is %v", b)
	}
	// The spectrogram itself is there: the noise's many shades fill it.
	if n := colours(img, image.Rect(b.Dx()/4, b.Dy()-pictureSpecH*pictureScale/2, b.Dx()/2, b.Dy()-pictureSpecH*pictureScale/4)); n < 50 {
		t.Errorf("only %d colours where the spectrogram should be", n)
	}
	if want := "Saved the spectrogram to " + offered + "."; u.status.Text != want {
		t.Errorf("status %q", u.status.Text)
	}

	// Cancelled: nothing more is written.
	u.status.SetText("")
	u.save = func(_ fyne.Window, _ string, done func(string)) { done("") }
	test.Tap(btn)
	if entries, _ := os.ReadDir(dir); len(entries) != 1 || u.status.Text != "" {
		t.Errorf("cancelled, but %d files, status %q", len(entries), u.status.Text)
	}

	// A folder that can't be written to: an error, and no status.
	u.save = func(_ fyne.Window, name string, done func(string)) { done(filepath.Join(dir, "missing", name)) }
	test.Tap(btn)
	if u.status.Text != "" || !strings.Contains(dialogText(u), "Couldn't save the spectrogram") {
		t.Errorf("status %q, dialog %q", u.status.Text, dialogText(u))
	}
}

func TestSpectrogramFileName(t *testing.T) {
	u, _, _ := newTestUI(t)
	var offered string
	u.save = func(_ fyne.Window, name string, done func(string)) { offered = name }
	u.saveSpectrogram(library.Track{Title: "Only a Title"}, quality.Assessment{}, newSpectrogramView())
	if offered != "Only a Title (spectrogram).png" {
		t.Errorf("offered %q", offered)
	}
	u.saveSpectrogram(library.Track{}, quality.Assessment{}, newSpectrogramView())
	if offered != "Spectrogram.png" {
		t.Errorf("offered %q", offered)
	}
}

// colours counts the different colours in r of img.
func colours(img image.Image, r image.Rectangle) int {
	seen := map[[4]uint32]bool{}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c0, c1, c2, c3 := img.At(x, y).RGBA()
			seen[[4]uint32{c0, c1, c2, c3}] = true
		}
	}
	return len(seen)
}

// dialogText joins the text of every label in the top overlay.
func dialogText(u *ui) string {
	var texts []string
	visit(u.win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if l, ok := o.(*widget.Label); ok {
			texts = append(texts, l.Text)
		}
	})
	return strings.Join(texts, "|")
}
