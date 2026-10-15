package library

// Folders the scan leaves out because the audio in them isn't music: the
// sounds inside apps and plug-ins, code's test files, the system's own
// folders. They matter when a big folder, such as a whole disk or the home
// folder, is added. A folder added on purpose is always scanned, even if
// it is inside one of these.

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// notMusicNames are folder names to skip, in lower case.
var notMusicNames = map[string]bool{
	"node_modules":              true, // JavaScript packages
	"$recycle.bin":              true, // Windows' Recycle Bin on each disk
	"system volume information": true, // Windows' restore points
}

// notMusicExts are the extensions of macOS bundles: folders that Finder
// shows as one file, such as an app, holding its sounds.
var notMusicExts = []string{".app", ".appex", ".bundle", ".framework", ".kext", ".plugin"}

// notMusicPaths are system folders to skip on macOS: the home folder's
// Library holds apps' sounds, caches and voice messages.
var notMusicPaths = func() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	paths := []string{"/Library", "/System"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, "Library"))
	}
	return paths
}()

// skipFolder reports whether the scan should leave out the folder at path.
func skipFolder(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return hidden(path) || notMusicNames[name] ||
		slices.Contains(notMusicExts, filepath.Ext(name)) ||
		slices.Contains(notMusicPaths, path)
}

// skippedWithin reports whether path is, or is in, a folder below root
// that the scan leaves out, as in root/.Trashes/501/song.mp3, or is a
// hidden file.
func skippedWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	dir := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		dir = filepath.Join(dir, part)
		if skipFolder(dir) { // a hidden file is left out too
			return true
		}
	}
	return false
}
