package main

// Keeping the library in step with its folders: when files are added,
// changed or deleted, only those are read again, so the list updates in
// place without a rescan. If a folder's disk is disconnected, its songs are
// hidden, and the folder is scanned again when the disk comes back.

import (
	"mugig/internal/humanize"
	"mugig/internal/library"
	"os"
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
// changes arrive on other goroutines, except busy, which is the UI's.
type folderWatch struct {
	root    string
	stop    func() // stops the watcher; nil if it couldn't start
	mu      sync.Mutex
	pending map[string]bool // changed paths not yet applied
	timer   *time.Timer
	closed  bool
	busy    bool // changes are being read
}

// startWatching follows f for changes, replacing any previous watch.
func (u *ui) startWatching(f *musicFolder) {
	u.stopWatching(f)
	w := &folderWatch{root: f.path, pending: map[string]bool{}}
	f.watch = w
	stop, err := u.watchFolder(f.path, func(paths []string) { u.folderChanged(w, paths) })
	if err != nil {
		fyne.LogError("watching "+f.path, err)
		return
	}
	w.stop = stop
}

func (u *ui) stopAllWatching() {
	for _, f := range u.folders {
		u.stopWatching(f)
	}
}

func (u *ui) stopWatching(f *musicFolder) {
	w := f.watch
	if w == nil {
		return
	}
	f.watch = nil
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
		w.timer = time.AfterFunc(u.settle, func() { onUI(func() { u.applyPending(w) }) })
	}
	w.mu.Unlock()
	if u.settle == 0 {
		u.applyPending(w)
	}
}

// applyPending reads the changed files in the background, then updates the
// list. While a scan runs it waits: the scan's end calls it again.
func (u *ui) applyPending(w *folderWatch) {
	if !u.watching(w) || u.cancel != nil || w.busy {
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
	w.busy = true
	u.async(func() {
		ch := library.ReadChanges(w.root, paths, u.cache)
		onUI(func() {
			w.busy = false
			if u.watching(w) && u.cancel == nil {
				u.applyChanges(w.root, ch)
			}
			u.applyPending(w) // anything that arrived meanwhile
		})
	})
}

// watching reports whether w is still a library folder's watch.
func (u *ui) watching(w *folderWatch) bool {
	f := u.folderAt(w.root)
	return f != nil && f.watch == w
}

// applyChanges updates the list, the cache and the status line.
func (u *ui) applyChanges(root string, ch library.Changes) {
	if ch.RootGone {
		u.setConnected(root, false)
		return
	}
	had := make(map[string]bool, len(u.all))
	for _, t := range u.all {
		had[t.Path] = true
	}
	var removed []string
	u.all = slices.DeleteFunc(u.all, func(t library.Track) bool {
		gone := slices.ContainsFunc(ch.Gone, func(g string) bool { return library.Within(library.SongFile(t), g) })
		for dir, found := range ch.Folders {
			if library.Within(t.Path, dir) && !found[t.Path] {
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
	added, seen := 0, map[string]library.CacheEntry{}
	var changed []string
	for _, r := range ch.Scanned {
		for _, t := range r.Songs() {
			if i, ok := index[t.Path]; ok {
				u.all[i] = t
			} else {
				index[t.Path] = len(u.all)
				u.all = append(u.all, t)
			}
			if had[t.Path] && !r.Cached && t.File == "" {
				changed = append(changed, t.Path) // re-read: the file is different now
			}
		}
		if r.ModTime != 0 {
			seen[r.Path] = r.Entry()
		}
	}
	u.all = library.DropImages(u.all)
	for _, t := range u.all {
		if !had[t.Path] {
			added++
		}
	}
	sort.SliceStable(u.all, func(i, j int) bool { return library.Less(u.all[i], u.all[j]) })
	u.applyView()

	if added > 0 || len(removed) > 0 {
		var parts []string
		if added > 0 {
			parts = append(parts, humanize.Plural(added, "track")+" added")
		}
		if len(removed) > 0 {
			parts = append(parts, humanize.Plural(len(removed), "track")+" removed")
		}
		u.status.SetText("Library updated: " + strings.Join(parts, " · "))
	}
	// A changed or deleted file's quality verdict no longer applies.
	stale := append(removed, changed...)
	for _, p := range stale {
		delete(u.levels, p)
	}
	cache, store := u.cache, u.qualityStore
	u.async(func() {
		cache.Update(root, seen, false)
		cache.Forget(removed)
		if err := cache.Save(); err != nil {
			fyne.LogError("saving library cache", err)
		}
		if len(stale) > 0 {
			store.Forget(stale)
			store.Save()
		}
	})
}

// watchConnections notices when a folder's disk is disconnected or comes
// back, which FSEvents doesn't report. When it is back, the folder is
// scanned again, as anything may have changed meanwhile. It runs for as
// long as the app does.
func (u *ui) watchConnections() {
	for u.reconnectEvery > 0 {
		time.Sleep(u.reconnectEvery)
		u.checkConnections()
	}
}

// checkConnections looks for each folder, off the UI thread, and tells the
// UI of any that came or went.
func (u *ui) checkConnections() {
	states := u.folderStates.Load()
	if states == nil {
		return
	}
	for _, f := range *states {
		_, err := os.Stat(f.path)
		if there := err == nil; there != f.connected {
			onUI(func() { u.setConnected(f.path, there) })
		}
	}
}
