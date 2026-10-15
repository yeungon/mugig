package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// folderChanges is what changed in the music folder.
type Changes struct {
	RootGone bool
	Scanned  []ScanResult               // files added or changed
	Gone     []string                   // files and folders deleted
	Folders  map[string]map[string]bool // folders read again, with the audio files now in them
}

// readChanges looks at each changed path. It runs off the UI thread.
func ReadChanges(root string, paths []string, cache *Cache) Changes {
	ch := Changes{Folders: map[string]map[string]bool{}}
	if _, err := os.Stat(root); err != nil {
		ch.RootGone = true // don't mistake a disconnected disk for deleted files
		return ch
	}
	sort.Strings(paths) // folders come before what is in them
	var read []string   // folders read whole, which cover their contents
	for _, p := range paths {
		if !Within(p, root) || skippedWithin(p, root) {
			continue
		}
		// A CUE sheet, or a file beside one, changes the songs of the
		// folder's disc images: read the folder again, whole.
		if p != root && (isCueFile(p) || IsAudioFile(p) && hasCueBeside(p)) {
			p = filepath.Dir(p)
		}
		if slices.ContainsFunc(read, func(d string) bool { return Within(p, d) }) {
			continue
		}
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			ch.Gone = append(ch.Gone, p)
		case fi.IsDir():
			read = append(read, p)
			found := map[string]bool{}
			filepath.WalkDir(p, func(f string, d fs.DirEntry, err error) error {
				switch {
				case err != nil:
				case d.IsDir() && f != p && skipFolder(f):
					return filepath.SkipDir
				case !d.IsDir() && (IsAudioFile(f) || isCueFile(f)) && !isEmpty(d):
					r := ScanFile(cache, f)
					for _, t := range r.Songs() {
						found[t.Path] = true
					}
					ch.Scanned = append(ch.Scanned, r)
				}
				return nil
			})
			ch.Folders[p] = found
		case IsAudioFile(p) && fi.Size() > 0:
			ch.Scanned = append(ch.Scanned, ScanFile(cache, p))
		}
	}
	return ch
}

// within reports whether path is dir or inside it.
func Within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
