package ui

// Saving a spectrogram as a PNG, the way it looks in the Audio Quality
// dialog, with the song and its format above it, so the picture explains
// itself when shared, as in Spek.

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"mugig/internal/library"
	"mugig/internal/quality"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	pictureWidth = 900 // points; the saved image has twice as many pixels
	pictureScale = 2
	pictureSpecH = 400 // the spectrogram's height
)

// saveSpectrogram asks where to save spec, with t and a above it, and
// saves it there as a PNG.
func (u *ui) saveSpectrogram(t library.Track, a quality.Assessment, spec *spectrogramView) {
	var parts []string
	for _, s := range []string{t.Artist, t.Title} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	name := "Spectrogram.png"
	if len(parts) > 0 {
		name = fileNameFrom(strings.Join(parts, " - ")) + " (spectrogram).png"
	}
	u.save(u.win, name, func(path string) {
		if path == "" {
			return // cancelled
		}
		img := spectrogramPicture(t, a, spec)
		u.async(func() {
			err := writePNG(path, img)
			onUI(func() {
				if err != nil {
					dialog.ShowError(fmt.Errorf("couldn't save the spectrogram: %w", err), u.win)
					return
				}
				u.status.SetText("Saved the spectrogram to " + filepath.Base(path) + ".")
			})
		})
	})
}

// spectrogramPicture draws the dialog's contents again, off screen: t's
// title and details, the assessment a, and spec as it is shown, with the
// same range of levels.
func spectrogramPicture(t library.Track, a quality.Assessment, spec *spectrogramView) image.Image {
	p := newQualityPanel()
	p.show(a)
	copied := newSpectrogramView()
	copied.min = fyne.NewSize(0, pictureSpecH)
	copied.set(spec.levels, spec.rate, spec.length, spec.cutoff, spec.note)
	copied.setRange(spec.top, spec.floor)
	title := widget.NewLabelWithStyle(t.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Truncation = fyne.TextTruncateEllipsis
	pad := theme.Padding() * 2
	content := container.New(layout.NewCustomPaddedLayout(pad, pad, pad, pad),
		container.NewVBox(title, secondaryLabel(trackDetail(t)), p.format, p.findings, copied))

	c := software.NewCanvas()
	c.SetPadded(false)
	c.SetScale(pictureScale)
	c.SetContent(content)
	// Wrapped findings know their height only once they have their width.
	c.Resize(fyne.NewSize(pictureWidth, content.MinSize().Height))
	c.Resize(fyne.NewSize(pictureWidth, content.MinSize().Height))
	return c.Capture()
}

func writePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// trackDetail is t's artist and album, as shown under its title.
func trackDetail(t library.Track) string {
	var parts []string
	for _, s := range []string{t.Artist, t.Album} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// fileNameFrom makes s safe to offer as a file name, replacing the
// characters that separate folders.
func fileNameFrom(s string) string {
	return strings.NewReplacer("/", "-", ":", "-", "\\", "-").Replace(s)
}
