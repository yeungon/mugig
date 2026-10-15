package ui

// Opening music: a folder is added to the library and scanned; audio files
// are added to the list and the first one plays. Both can be chosen with
// the system's Open panel (see pick_darwin.go) or dropped on the window.

import (
	"errors"
	"fmt"
	"io/fs"
	"mugig/internal/library"
	"os"
	"path/filepath"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// pickOptions says what an Open panel may choose.
type pickOptions struct {
	title    string // the panel's message
	files    bool   // audio files
	folders  bool
	multiple bool
	start    string // folder to start in, or "" for the system's choice
}

// picker shows an Open panel and calls done on the UI thread with the
// chosen paths, or none if cancelled.
type picker func(w fyne.Window, opts pickOptions, done func(paths []string))

// saver shows a Save panel for a file named name, and calls done on the UI
// thread with the path chosen, or "" if cancelled.
type saver func(w fyne.Window, name string, done func(path string))

// openItems asks for audio files or a folder, and opens them.
func (u *ui) openItems() {
	start := ""
	if n := len(u.folders); n > 0 && u.folders[n-1].connected {
		start = u.folders[n-1].path
	}
	u.pick(u.win, pickOptions{
		title:    "Choose audio files to play, or a folder to add",
		files:    true,
		folders:  true,
		multiple: true,
		start:    start,
	}, u.openPaths)
}

// openPaths opens what was chosen or dropped: folders are added to the
// library, and audio files are played.
func (u *ui) openPaths(paths []string) {
	var files, folders []string
	for _, p := range paths {
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			u.showOpenError(p, err)
			return
		case fi.IsDir():
			folders = append(folders, p)
		case library.IsAudioFile(p) && fi.Size() > 0:
			files = append(files, p)
		}
	}
	for _, dir := range folders {
		u.addFolder(dir)
	}
	switch {
	case len(files) > 0:
		u.openFiles(files)
	case len(folders) == 0 && len(paths) > 0:
		dialog.ShowInformation("Not audio", "Mugig plays MP3, FLAC, WAV and Ogg Vorbis files.", u.win)
	}
}

// openFiles adds audio files to the list, if not there already, and plays
// the first. Those outside the library's folders stay listed until Mugig
// quits.
func (u *ui) openFiles(paths []string) {
	u.async(func() {
		tracks := make([]library.Track, len(paths))
		for i, p := range paths {
			tracks[i] = library.ScanFile(u.cache, p).Track
		}
		onUI(func() {
			for _, t := range tracks {
				if !slices.ContainsFunc(u.all, func(o library.Track) bool { return o.Path == t.Path }) {
					u.all = append(u.all, t)
				}
			}
			u.applyView()
			u.showVisualizer(false)
			u.jump(tracks[0])
		})
	})
}

// showOpenError explains why path couldn't be opened.
func (u *ui) showOpenError(path string, err error) {
	dialog.ShowError(fmt.Errorf("Can't open %s: %s", filepath.Base(path), friendlyError(err)), u.win)
}

// friendlyError explains the usual reasons a file or folder can't be read.
func friendlyError(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "macOS hasn't allowed Mugig to read it. " +
			"Open System Settings → Privacy & Security → Files and Folders, " +
			"and turn on access for Mugig (\"Removable Volumes\" for USB disks)."
	case errors.Is(err, fs.ErrNotExist):
		return "it isn't there. If it's on a removable disk, connect the disk and try again."
	}
	return err.Error()
}

// dropped opens files and folders dropped on the window.
func (u *ui) dropped(_ fyne.Position, uris []fyne.URI) {
	var paths []string
	for _, uri := range uris {
		if uri.Scheme() == "file" {
			paths = append(paths, uri.Path())
		}
	}
	u.openPaths(paths)
}

// fynePicker uses Fyne's own dialogs, for systems without a native panel.
func fynePicker(w fyne.Window, opts pickOptions, done func([]string)) {
	if opts.folders && !opts.files {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err == nil && uri != nil {
				done([]string{uri.Path()})
			}
		}, w)
		return
	}
	dialog.ShowFileOpen(func(r fyne.URIReadCloser, err error) {
		if err == nil && r != nil {
			r.Close()
			done([]string{r.URI().Path()})
		}
	}, w)
}

// fyneSaver is Fyne's Save dialog, for systems without a native one here.
func fyneSaver(w fyne.Window, name string, done func(string)) {
	d := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			done("")
			return
		}
		wc.Close()
		done(wc.URI().Path())
	}, w)
	d.SetFileName(name)
	d.Show()
}
