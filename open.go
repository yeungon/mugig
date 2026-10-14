package main

// Opening music: a folder becomes the library and is scanned; audio files
// are added to the list and the first one plays. Both can be chosen with
// the system's Open panel (see pick_darwin.go) or dropped on the window.

import (
	"errors"
	"fmt"
	"io/fs"
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

// chooseFolder asks for the music folder and scans it.
func (u *ui) chooseFolder() {
	if u.cancel != nil {
		return // already scanning
	}
	u.pick(u.win, pickOptions{
		title:   "Choose the folder or disk with your music",
		folders: true,
		start:   u.lastFolder(),
	}, func(paths []string) {
		if len(paths) > 0 {
			u.openFolder(paths[0], true)
		}
	})
}

// openItems asks for audio files or a folder, and opens them.
func (u *ui) openItems() {
	u.pick(u.win, pickOptions{
		title:    "Choose audio files to play, or a folder to scan",
		files:    true,
		folders:  true,
		multiple: true,
		start:    u.lastFolder(),
	}, u.openPaths)
}

// openPaths opens what was chosen or dropped: the first folder, if any,
// becomes the library; otherwise the audio files are played.
func (u *ui) openPaths(paths []string) {
	var files []string
	for _, p := range paths {
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			u.showOpenError(p, err)
			return
		case fi.IsDir():
			u.openFolder(p, true)
			return
		case isAudioFile(p) && fi.Size() > 0:
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		if len(paths) > 0 {
			dialog.ShowInformation("Not audio", "Mugig plays MP3, FLAC, WAV and Ogg Vorbis files.", u.win)
		}
		return
	}
	u.openFiles(files)
}

// openFolder makes dir the music folder and scans it, after checking it
// can be read. Problems are shown in a dialog if ask is set (the user just
// chose it), otherwise only in the status line.
func (u *ui) openFolder(dir string, ask bool) {
	if u.cancel != nil {
		return // already scanning
	}
	if _, err := os.ReadDir(dir); err != nil {
		u.status.SetText(fmt.Sprintf("Can't open %s: %s", filepath.Base(dir), friendlyError(err)))
		if ask {
			u.showOpenError(dir, err)
		}
		return
	}
	u.startScan(dir)
}

// openFiles adds audio files to the list, if not there already, and plays
// the first. They stay until the next scan.
func (u *ui) openFiles(paths []string) {
	u.async(func() {
		tracks := make([]Track, len(paths))
		for i, p := range paths {
			tracks[i] = scanFile(u.cache, p).track
		}
		onUI(func() {
			for _, t := range tracks {
				if !slices.ContainsFunc(u.all, func(o Track) bool { return o.Path == t.Path }) {
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
