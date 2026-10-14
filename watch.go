package main

// Keeping the library in step with the music folder: when files are added,
// changed or deleted, only those are read again, so the list updates in
// place without a rescan. If the folder's disk is disconnected, the list is
// kept, and the folder is scanned again when the disk comes back.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
)

const (
	settleTime    = 1500 * time.Millisecond // wait for changes to stop, e.g. while copying an album
	reconnectPoll = 3 * time.Second         // how often to look for a disconnected folder
)

// watchFunc starts watching a folder (see watchFolder).
type watchFunc func(root string, changed func(paths []string)) (stop func(), err error)

// folderWatch follows one music folder. Its fields are guarded by mu, as
// changes arrive on other goroutines.
type folderWatch struct {
	root    string
	stop    func() // stops the watcher; nil while disconnected
	mu      sync.Mutex
	pending map[string]bool // changed paths not yet applied
	timer   *time.Timer
	closed  bool
}

// startWatching follows root for changes, replacing any previous watch.
func (u *ui) startWatching(root string) {
	u.stopWatching()
	w := &folderWatch{root: root, pending: map[string]bool{}}
	u.watch = w
	stop, err := u.watchFolder(root, func(paths []string) { u.folderChanged(w, paths) })
	if err != nil {
		fyne.LogError("watching "+root, err)
		return
	}
	w.stop = stop
	if u.reconnectEvery > 0 {
		go u.watchConnection(w)
	}
}

func (u *ui) stopWatching() {
	w := u.watch
	if w == nil {
		return
	}
	u.watch = nil
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	if w.stop != nil {
		w.stop()
	}
}

// folderChanged collects changed paths, and applies them once they have
// settled. With no settle time (in tests) they are applied at once, on the
// caller's goroutine.
func (u *ui) folderChanged(w *folderWatch, paths []string) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	for _, p := range paths {
		w.pending[p] = true
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	if u.settle > 0 {
		w.timer = time.AfterFunc(u.settle, func() { fyne.Do(func() { u.applyPending(w) }) })
	}
	w.mu.Unlock()
	if u.settle == 0 {
		u.applyPending(w)
	}
}

// applyPending reads the changed files in the background, then updates the
// list. While a scan runs it waits: the scan's end calls it again.
func (u *ui) applyPending(w *folderWatch) {
	if u.watch != w || u.cancel != nil || u.watchBusy {
		return
	}
	w.mu.Lock()
	paths := make([]string, 0, len(w.pending))
	for p := range w.pending {
		paths = append(paths, p)
	}
	clear(w.pending)
	w.mu.Unlock()
	if len(paths) == 0 {
		return
	}
	u.watchBusy = true
	u.async(func() {
		ch := readChanges(w.root, paths, u.cache)
		fyne.Do(func() {
			u.watchBusy = false
			if u.watch == w && u.cancel == nil {
				u.applyChanges(ch)
			}
			u.applyPending(w) // anything that arrived meanwhile
		})
	})
}

// folderChanges is what changed in the music folder.
type folderChanges struct {
	rootGone bool
	tracks   []scanResult               // files added or changed
	gone     []string                   // files and folders deleted
	folders  map[string]map[string]bool // folders read again, with the audio files now in them
}

// readChanges looks at each changed path. It runs off the UI thread.
func readChanges(root string, paths []string, cache *libraryCache) folderChanges {
	ch := folderChanges{folders: map[string]map[string]bool{}}
	if _, err := os.Stat(root); err != nil {
		ch.rootGone = true // don't mistake a disconnected disk for deleted files
		return ch
	}
	sort.Strings(paths) // folders come before what is in them
	var read []string   // folders read whole, which cover their contents
	for _, p := range paths {
		if !within(p, root) || hiddenWithin(p, root) {
			continue
		}
		if slices.ContainsFunc(read, func(d string) bool { return within(p, d) }) {
			continue
		}
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			ch.gone = append(ch.gone, p)
		case fi.IsDir():
			read = append(read, p)
			found := map[string]bool{}
			filepath.WalkDir(p, func(f string, d fs.DirEntry, err error) error {
				switch {
				case err != nil:
				case d.IsDir() && f != p && hidden(f):
					return filepath.SkipDir
				case !d.IsDir() && isAudioFile(f) && !isEmpty(d):
					found[f] = true
					ch.tracks = append(ch.tracks, scanFile(cache, f))
				}
				return nil
			})
			ch.folders[p] = found
		case isAudioFile(p) && fi.Size() > 0:
			ch.tracks = append(ch.tracks, scanFile(cache, p))
		}
	}
	return ch
}

// hiddenWithin reports whether any part of path below root is hidden, as
// in root/.Trashes/501/song.mp3: the scan leaves those out too.
func hiddenWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(strings.Split(rel, string(filepath.Separator)), func(part string) bool {
		return hidden(part) && part != "." && part != ".."
	})
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// applyChanges updates the list, the cache and the status line.
func (u *ui) applyChanges(ch folderChanges) {
	if ch.rootGone {
		u.status.SetText(fmt.Sprintf("%s is disconnected. Its songs will be back when it is connected again.",
			filepath.Base(u.watch.root)))
		return
	}
	had := make(map[string]bool, len(u.all))
	for _, t := range u.all {
		had[t.Path] = true
	}
	var removed []string
	u.all = slices.DeleteFunc(u.all, func(t Track) bool {
		gone := slices.ContainsFunc(ch.gone, func(g string) bool { return within(t.Path, g) })
		for dir, found := range ch.folders {
			if within(t.Path, dir) && !found[t.Path] {
				gone = true
			}
		}
		if gone {
			removed = append(removed, t.Path)
		}
		return gone
	})
	index := make(map[string]int, len(u.all))
	for i, t := range u.all {
		index[t.Path] = i
	}
	added, seen := 0, map[string]cacheEntry{}
	var changed []string
	for _, r := range ch.tracks {
		if i, ok := index[r.track.Path]; ok {
			u.all[i] = r.track
		} else {
			index[r.track.Path] = len(u.all)
			u.all = append(u.all, r.track)
		}
		if !had[r.track.Path] {
			added++
		} else if !r.cached {
			changed = append(changed, r.track.Path) // re-read: the file is different now
		}
		if r.modTime != 0 {
			seen[r.track.Path] = cacheEntry{Size: r.size, ModTime: r.modTime, Track: r.track}
		}
	}
	sort.SliceStable(u.all, func(i, j int) bool { return libraryLess(u.all[i], u.all[j]) })
	u.applyView()

	if added > 0 || len(removed) > 0 {
		var parts []string
		if added > 0 {
			parts = append(parts, plural(added, "track")+" added")
		}
		if len(removed) > 0 {
			parts = append(parts, plural(len(removed), "track")+" removed")
		}
		u.status.SetText("Library updated: " + strings.Join(parts, " · "))
	}
	// A changed or deleted file's quality verdict no longer applies.
	stale := append(removed, changed...)
	for _, p := range stale {
		delete(u.levels, p)
	}
	root, cache, store := u.watch.root, u.cache, u.qualityStore
	u.async(func() {
		cache.update(root, seen, false)
		cache.forget(removed)
		if err := cache.save(); err != nil {
			fyne.LogError("saving library cache", err)
		}
		if len(stale) > 0 {
			store.forget(stale)
			store.save()
		}
	})
}

// watchConnection notices when the folder's disk is disconnected or comes
// back, which FSEvents doesn't report. When it is back, the folder is
// scanned again, as anything may have changed meanwhile.
func (u *ui) watchConnection(w *folderWatch) {
	connected := true
	for {
		time.Sleep(u.reconnectEvery)
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return
		}
		_, err := os.Stat(w.root)
		switch {
		case err != nil && connected:
			connected = false
			fyne.Do(func() {
				if u.watch == w {
					u.applyChanges(folderChanges{rootGone: true})
				}
			})
		case err == nil && !connected:
			fyne.Do(func() {
				if u.watch == w {
					u.openFolder(w.root, false)
				}
			})
			return // the scan starts a new watch
		}
	}
}
