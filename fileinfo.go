package main

// File details and the file's place on disk: the song list's right-click
// menu offers Get Info, showing the file in Finder (Explorer on Windows,
// the file manager on Linux), and copying its path.

import (
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// revealTrack shows t's file in the file manager, or says it can't be
// found, as when its disk is unplugged.
func (u *ui) revealTrack(t Track) {
	if _, err := os.Stat(t.Path); err != nil {
		u.missingFile(t)
		return
	}
	u.async(func() {
		if err := u.reveal(t.Path); err != nil {
			onUI(func() { dialog.ShowError(err, u.win) })
		}
	})
}

func (u *ui) missingFile(t Track) {
	dialog.ShowInformation("File Not Found",
		fmt.Sprintf("Can't find %s.\nIf it's on a removable disk, connect it and try again.", filepath.Base(t.Path)), u.win)
}

// showInfo shows t's file details. The format is read from the file, which
// can take a moment on a slow disk, so it fills in when ready.
func (u *ui) showInfo(t Track) dialog.Dialog {
	st, err := os.Stat(t.Path)
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
	if l, ok := u.levels[t.Path]; ok && l != qualityUnknown {
		quality = levelNames[l]
	}
	year := ""
	if t.Year > 0 {
		year = fmt.Sprint(t.Year)
	}
	form := widget.NewForm(
		widget.NewFormItem("Name", value(filepath.Base(t.Path))),
		widget.NewFormItem("Where", value(filepath.Dir(t.Path))),
		widget.NewFormItem("Size", value(fmt.Sprintf("%s (%s bytes)", fileSize(st.Size()), thousands(int(st.Size()))))),
		widget.NewFormItem("Modified", value(st.ModTime().Format("2 Jan 2006 at 15:04"))),
		widget.NewFormItem("Format", format),
		widget.NewFormItem("Length", length),
		widget.NewFormItem("Title", value(t.Title)),
		widget.NewFormItem("Artist", value(t.Artist)),
		widget.NewFormItem("Album", value(t.Album)),
		widget.NewFormItem("Year", value(year)),
		widget.NewFormItem("Quality", value(quality)),
	)
	show := widget.NewButtonWithIcon(revealLabel, theme.FolderOpenIcon(), func() { u.revealTrack(t) })
	content := container.NewVBox(form, container.NewHBox(show))
	d := dialog.NewCustom("Get Info", "Close", content, u.win)
	d.Resize(fyne.NewSize(560, content.MinSize().Height))
	d.Show()

	u.async(func() {
		info, err := probe(t.Path)
		onUI(func() {
			if err != nil {
				format.SetText("Can't read the format: " + err.Error())
				length.SetText("")
				return
			}
			format.SetText(describe(info))
			length.SetText(formatTime(info.Duration))
			content.Refresh()
		})
	})
	return d
}
