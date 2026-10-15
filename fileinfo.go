package main

// File details and the file's place on disk: the song list's right-click
// menu offers Get Info, showing the file in Finder (Explorer on Windows,
// the file manager on Linux), and copying its path.

import (
	"fmt"
	"mugig/internal/audio"
	"mugig/internal/humanize"
	"os"
	"path/filepath"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// revealTrack shows t's file in the file manager, or says it can't be
// found, as when its disk is unplugged.
func (u *ui) revealTrack(t Track) {
	file := audio.FileOf(t.Path) // a song from a CUE sheet: its disc image
	if _, err := os.Stat(file); err != nil {
		u.missingFile(t)
		return
	}
	u.async(func() {
		if err := u.reveal(file); err != nil {
			onUI(func() { dialog.ShowError(err, u.win) })
		}
	})
}

func (u *ui) missingFile(t Track) {
	dialog.ShowInformation("File Not Found",
		fmt.Sprintf("Can't find %s.\nIf it's on a removable disk, connect it and try again.", filepath.Base(audio.FileOf(t.Path))), u.win)
}

// showInfo shows t's file details. The format is read from the file, which
// can take a moment on a slow disk, so it fills in when ready.
//
// For a song from a CUE sheet, the file is its disc image, and the dialog
// says where in it the song is.
func (u *ui) showInfo(t Track) dialog.Dialog {
	file := audio.FileOf(t.Path)
	st, err := os.Stat(file)
	if err != nil {
		u.missingFile(t)
		return nil
	}
	value := func(s string) *widget.Label {
		l := widget.NewLabel(s)
		l.Wrapping = fyne.TextWrapBreak
		l.Selectable = true
		return l
	}
	format, length := value("Reading…"), value("Reading…")
	quality := "Not checked yet"
	if l := u.levelOf(t); l != qualityUnknown {
		quality = levelNames[l]
	}
	year := ""
	if t.Year > 0 {
		year = fmt.Sprint(t.Year)
	}
	form := widget.NewForm(
		widget.NewFormItem("Name", value(filepath.Base(file))),
		widget.NewFormItem("Where", value(filepath.Dir(file))),
		widget.NewFormItem("Size", value(fmt.Sprintf("%s (%s bytes)", humanize.FileSize(st.Size()), humanize.Thousands(int(st.Size()))))),
		widget.NewFormItem("Modified", value(st.ModTime().Format("2 Jan 2006 at 15:04"))),
		widget.NewFormItem("Format", format),
		widget.NewFormItem("Length", length),
		widget.NewFormItem("Title", value(t.Title)),
		widget.NewFormItem("Artist", value(t.Artist)),
		widget.NewFormItem("Album", value(t.Album)),
		widget.NewFormItem("Year", value(year)),
		widget.NewFormItem("Quality", value(quality)),
	)
	cue := audio.FromCue(t.Path)
	if cue {
		at := fmt.Sprintf("A disc image, with a CUE sheet: this song runs from %s to %s", humanize.Clock(t.Start), humanize.Clock(t.Start+t.Length))
		form.Items = slices.Insert(form.Items, 2, widget.NewFormItem("Part", value(at)))
	}
	show := widget.NewButtonWithIcon(revealLabel, theme.FolderOpenIcon(), func() { u.revealTrack(t) })
	content := container.NewVBox(form, container.NewHBox(show))
	d := dialog.NewCustom("Get Info", "Close", content, u.win)
	d.Resize(fyne.NewSize(560, content.MinSize().Height))
	d.Show()

	u.async(func() {
		info, err := audio.Probe(file)
		onUI(func() {
			if err != nil {
				format.SetText("Can't read the format: " + err.Error())
				length.SetText("")
				return
			}
			format.SetText(describe(info))
			length.SetText(humanize.Clock(info.Duration))
			if cue {
				length.SetText(fmt.Sprintf("%s, of %s for the whole image", humanize.Clock(t.Length), humanize.Clock(info.Duration)))
			}
			content.Refresh()
		})
	})
	return d
}
