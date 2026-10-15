package main

// The library is made of music folders, each scanned and watched on its
// own; their songs make one list. Choosing a folder in the sidebar narrows
// the list to it. A folder whose disk is unplugged stays in the sidebar,
// its songs hidden, and is scanned again when the disk is back.

import (
	"fmt"
	"mugig/internal/humanize"
	"mugig/internal/library"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// prefFolders lists the music folders. Earlier versions had a single
// folder, saved as prefFolder; it becomes the first entry.
const prefFolders = "musicFolders"

// musicFolder is one folder of the library. Its fields are used on the UI
// thread only.
type musicFolder struct {
	path      string
	connected bool         // false while its disk is unplugged, or it is gone
	missing   string       // why it isn't connected: "Not connected" or "Not found"
	watch     *folderWatch // follows changes while connected (see watch.go)
	row       *folderRow   // its entry in the sidebar
}

// folderState is what the connection poller needs to know of a folder.
type folderState struct {
	path      string
	connected bool
}

// loadFolders reads the saved folders, and whether each is there now.
func (u *ui) loadFolders() {
	paths := u.prefs.StringList(prefFolders)
	if old := u.prefs.String(prefFolder); old != "" {
		if len(paths) == 0 {
			paths = []string{old}
		}
		u.prefs.SetStringList(prefFolders, paths)
		u.prefs.RemoveValue(prefFolder)
	}
	u.folders = nil
	for _, p := range paths {
		f := &musicFolder{path: p, connected: true}
		if _, err := os.Stat(p); err != nil {
			f.connected, f.missing = false, missingText(p)
		}
		u.folders = append(u.folders, f)
	}
	u.publishFolders()
}

// saveFolders remembers the folders, in order.
func (u *ui) saveFolders() {
	paths := make([]string, len(u.folders))
	for i, f := range u.folders {
		paths[i] = f.path
	}
	u.prefs.SetStringList(prefFolders, paths)
	u.publishFolders()
}

// publishFolders hands the folders to the connection poller, which runs on
// another goroutine.
func (u *ui) publishFolders() {
	states := make([]folderState, len(u.folders))
	for i, f := range u.folders {
		states[i] = folderState{f.path, f.connected}
	}
	u.folderStates.Store(&states)
}

// folderAt returns the library folder with exactly this path, or nil.
func (u *ui) folderAt(path string) *musicFolder {
	for _, f := range u.folders {
		if f.path == path {
			return f
		}
	}
	return nil
}

// containing returns the library folder that path is in, or nil.
func (u *ui) containing(path string) *musicFolder {
	for _, f := range u.folders {
		if library.Within(path, f.path) {
			return f
		}
	}
	return nil
}

// scanLibrary scans every connected folder: at startup, and for Rescan.
// With no folders yet, it asks for one.
func (u *ui) scanLibrary() {
	if u.cancel != nil {
		return // already scanning
	}
	if len(u.folders) == 0 {
		u.chooseFolderToAdd()
		return
	}
	var gone, there []string
	for _, f := range u.folders {
		if _, err := os.Stat(f.path); err != nil {
			u.setConnected(f.path, false)
			gone = append(gone, filepath.Base(f.path))
			continue
		}
		f.connected, f.missing = true, ""
		there = append(there, f.path)
	}
	u.publishFolders()
	u.queueScan(there...)
	u.refreshFolders()
	if len(there) == 0 { // otherwise the scan's result shows, and the rows say what is missing
		u.status.SetText(fmt.Sprintf("Can't find %s. If it's on a removable disk, connect it: it is scanned as soon as it's back.",
			strings.Join(gone, ", ")))
	}
}

// chooseFolderToAdd asks for folders to add to the library.
func (u *ui) chooseFolderToAdd() {
	start := ""
	if n := len(u.folders); n > 0 {
		start = filepath.Dir(u.folders[n-1].path) // its neighbours are likely music too
		if _, err := os.Stat(start); err != nil {
			start = ""
		}
	}
	u.pick(u.win, pickOptions{
		title:    "Choose folders or disks with music to add",
		folders:  true,
		multiple: true,
		start:    start,
	}, func(paths []string) {
		for _, p := range paths {
			u.addFolder(p)
		}
	})
}

// addFolder adds dir to the library and scans it. A folder already in the
// library, or inside one, is not added twice; folders inside dir are
// replaced by it.
func (u *ui) addFolder(dir string) {
	dir = filepath.Clean(dir)
	if f := u.containing(dir); f != nil {
		msg := fmt.Sprintf("“%s” is already in your library.", filepath.Base(dir))
		if f.path != dir {
			msg = fmt.Sprintf("“%s” is already in your library, as part of “%s”.", filepath.Base(dir), filepath.Base(f.path))
		}
		dialog.ShowInformation("Already Added", msg, u.win)
		return
	}
	if _, err := os.ReadDir(dir); err != nil {
		u.showOpenError(dir, err)
		return
	}
	// Folders inside dir make way for it; their songs stay listed, and
	// are taken over by dir's scan.
	u.folders = slices.DeleteFunc(u.folders, func(f *musicFolder) bool {
		if !library.Within(f.path, dir) {
			return false
		}
		u.stopWatching(f)
		u.scanQueue = slices.DeleteFunc(u.scanQueue, func(p string) bool { return p == f.path })
		if u.filter == f.path {
			u.filter = ""
		}
		return true
	})
	u.folders = append(u.folders, &musicFolder{path: dir, connected: true})
	u.saveFolders()
	u.rebuildFolderList()
	u.queueScan(dir)
}

// confirmRemoveFolder asks before taking a folder out of the library.
func (u *ui) confirmRemoveFolder(path string) {
	n := 0
	for _, t := range u.all {
		if library.Within(t.Path, path) {
			n++
		}
	}
	msg := "Its songs are taken off the list. The files stay where they are."
	if n > 0 {
		msg = fmt.Sprintf("Its %s are taken off the list. The files stay where they are.", humanize.Plural(n, "song"))
	}
	d := dialog.NewConfirm(fmt.Sprintf("Remove “%s” from Mugig?", filepath.Base(path)), msg, func(ok bool) {
		if ok {
			u.removeFolder(path)
		}
	}, u.win)
	d.SetConfirmText("Remove")
	d.SetDismissText("Cancel")
	d.Show()
}

// removeFolder takes a folder out of the library, with its songs. The
// files themselves are left alone.
func (u *ui) removeFolder(path string) {
	f := u.folderAt(path)
	if f == nil {
		return
	}
	u.stopWatching(f)
	u.folders = slices.DeleteFunc(u.folders, func(o *musicFolder) bool { return o == f })
	u.scanQueue = slices.DeleteFunc(u.scanQueue, func(p string) bool { return p == path })
	if u.scanning == path {
		u.discardScan = true // its songs mustn't come back as the scan winds down
		u.cancel()
	}
	if u.filter == path {
		u.filter = ""
	}
	u.saveFolders()
	u.dropTracksIn(path)
	u.rebuildFolderList()
	cache := u.cache
	u.async(func() {
		cache.ForgetUnder(path)
		if err := cache.Save(); err != nil {
			fyne.LogError("saving library cache", err)
		}
	})
}

// dropTracksIn takes the songs in dir off the list.
func (u *ui) dropTracksIn(dir string) {
	n := len(u.all)
	u.all = slices.DeleteFunc(u.all, func(t library.Track) bool { return library.Within(t.Path, dir) })
	if len(u.all) != n {
		u.table.UnselectAll()
		u.applyView()
	}
}

// setConnected follows a folder's disk being unplugged, which hides its
// songs, or plugged back in, which scans it again.
func (u *ui) setConnected(path string, connected bool) {
	f := u.folderAt(path)
	if f == nil || f.connected == connected {
		return
	}
	f.connected = connected
	u.publishFolders()
	if connected {
		f.missing = ""
		u.queueScan(path)
		u.refreshFolders()
		return
	}
	f.missing = missingText(path)
	u.stopWatching(f)
	if u.scanning == path {
		u.discardScan = true
		u.cancel()
	}
	u.scanQueue = slices.DeleteFunc(u.scanQueue, func(p string) bool { return p == path })
	u.dropTracksIn(path)
	u.refreshFolders()
	u.status.SetText(fmt.Sprintf("%s is disconnected. Its songs will be back when it is connected again.",
		filepath.Base(path)))
}

// setFilter narrows the list to one folder's songs, or "" for all.
func (u *ui) setFilter(path string) {
	u.filter, u.playlist = path, nil
	u.table.UnselectAll()
	u.table.ScrollToTop()
	u.applyView()
	u.showVisualizer(false) // also marks the folder in the sidebar
}

// inFilter reports whether the list's folder, if any, holds t.
func (u *ui) inFilter(t library.Track) bool {
	return u.filter == "" || library.Within(t.Path, u.filter)
}

// missingText explains why a folder isn't there: its disk is unplugged if
// the nearest folder above it that still exists is where disks appear
// (/Volumes on macOS, /media or /mnt on Linux), or even the drive is gone
// (Windows); otherwise the folder itself was moved or deleted.
func missingText(path string) string {
	for d := filepath.Dir(path); ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			if mountParents[d] || userMountParents[filepath.Dir(d)] {
				return "Not connected"
			}
			return "Not found"
		}
		if filepath.Dir(d) == d {
			return "Not connected"
		}
	}
}

// mountParents are the folders where disks appear, directly or, on Linux,
// in a folder per user.
var (
	mountParents     = map[string]bool{"/Volumes": true, "/media": true, "/mnt": true, "/run/media": true}
	userMountParents = map[string]bool{"/media": true, "/run/media": true}
)

// onRemovableDisk reports whether path is on a disk other than the
// system's, as USB disks are, for the sidebar's icon.
func onRemovableDisk(path string) bool {
	switch runtime.GOOS {
	case "windows":
		system := os.Getenv("SystemDrive")
		return system != "" && !strings.EqualFold(filepath.VolumeName(path), system)
	case "darwin":
		return strings.HasPrefix(path, "/Volumes/")
	}
	for p := range mountParents {
		if strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// displayPath abbreviates the home folder to ~.
func displayPath(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home+string(filepath.Separator)) {
		return "~" + dir[len(home):]
	}
	return dir
}
