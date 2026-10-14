package main

import (
	"bufio"
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// libraryCache remembers each file's tags along with its size and
// modification time, so a rescan only opens files that have changed.
//
// It is read concurrently by the scan workers, and updated both after a
// scan and when the folder watcher sees changes, hence the lock.
type libraryCache struct {
	mu      sync.RWMutex
	path    string // where it is saved; "" keeps it in memory only
	entries map[string]cacheEntry
}

type cacheEntry struct {
	Size    int64
	ModTime int64 // UnixNano
	Track   Track
}

// cacheVersion is bumped whenever Track or cacheEntry change shape, so old
// caches are ignored instead of misread.
const cacheVersion = 1

type cacheFile struct {
	Version int
	Entries map[string]cacheEntry
}

// loadCache reads the cache saved at path. A missing, corrupt or outdated
// file just gives an empty cache.
func loadCache(path string) *libraryCache {
	c := &libraryCache{path: path, entries: map[string]cacheEntry{}}
	var cf cacheFile
	if loadGob(path, &cf) == nil && cf.Version == cacheVersion && cf.Entries != nil {
		c.entries = cf.Entries
	}
	return c
}

// lookup returns the cached track for path if the file is unchanged.
func (c *libraryCache) lookup(path string, size, modTime int64) (Track, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[path]
	if !ok || e.Size != size || e.ModTime != modTime {
		return Track{}, false
	}
	return e.Track, true
}

// update records a scan of root. After a complete scan, entries under root
// that it didn't see are dropped, as those files are gone; a cancelled scan
// only adds. Entries for other folders are kept.
func (c *libraryCache) update(root string, seen map[string]cacheEntry, complete bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if complete {
		prefix := filepath.Clean(root) + string(filepath.Separator)
		for p := range c.entries {
			if _, ok := seen[p]; !ok && strings.HasPrefix(p, prefix) {
				delete(c.entries, p)
			}
		}
	}
	for p, e := range seen {
		c.entries[p] = e
	}
}

// forget drops the entries for files that are gone.
func (c *libraryCache) forget(paths []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range paths {
		delete(c.entries, p)
	}
}

// save writes the cache to disk.
func (c *libraryCache) save() error {
	if c.path == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return saveGob(c.path, cacheFile{Version: cacheVersion, Entries: c.entries})
}

// saveGob writes v to a temporary file and renames it into place, so a
// crash mid-write can't leave a half-written file.
func saveGob(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, strings.TrimSuffix(filepath.Base(path), ".gob")+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	w := bufio.NewWriter(tmp)
	err = gob.NewEncoder(w).Encode(v)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// loadGob reads what saveGob wrote at path into v.
func loadGob(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewDecoder(bufio.NewReader(f)).Decode(v)
}
