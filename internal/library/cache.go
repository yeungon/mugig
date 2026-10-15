package library

import (
	"mugig/internal/gobfile"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cache remembers each file's tags along with its size and
// modification time, so a rescan only opens files that have changed.
//
// It is read concurrently by the scan workers, and updated both after a
// scan and when the folder watcher sees changes, hence the lock.
type Cache struct {
	mu      sync.RWMutex
	path    string // where it is saved; "" keeps it in memory only
	entries map[string]CacheEntry
}

type CacheEntry struct {
	Size    int64
	ModTime int64 // UnixNano
	Track   Track
	// For a CUE sheet: its songs, and the size and modification time of
	// the disc images they are in, which change the songs too.
	Cue    []Track
	Images map[string][2]int64
}

// cacheVersion is bumped whenever Track or CacheEntry change shape, so old
// caches are ignored instead of misread.
const cacheVersion = 1

type cacheFile struct {
	Version int
	Entries map[string]CacheEntry
}

// LoadCache reads the cache saved at path. A missing, corrupt or outdated
// file just gives an empty cache.
func LoadCache(path string) *Cache {
	c := &Cache{path: path, entries: map[string]CacheEntry{}}
	var cf cacheFile
	if gobfile.Load(path, &cf) == nil && cf.Version == cacheVersion && cf.Entries != nil {
		c.entries = cf.Entries
	}
	return c
}

// Lookup returns the cached track for path if the file is unchanged.
func (c *Cache) Lookup(path string, size, modTime int64) (Track, bool) {
	e, ok := c.entry(path, size, modTime)
	return e.Track, ok
}

// entry returns the cache's entry for path if the file is unchanged.
func (c *Cache) entry(path string, size, modTime int64) (CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[path]
	if !ok || e.Size != size || e.ModTime != modTime {
		return CacheEntry{}, false
	}
	return e, true
}

// ModTime is when the file at path was last changed, as the cache has it,
// or the zero time if it isn't cached.
func (c *Cache) ModTime(path string) time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if e, ok := c.entries[path]; ok {
		return time.Unix(0, e.ModTime)
	}
	return time.Time{}
}

// Update records a scan of root. After a complete scan, entries under root
// that it didn't see are dropped, as those files are gone; a cancelled scan
// only adds. Entries for other folders are kept.
func (c *Cache) Update(root string, seen map[string]CacheEntry, complete bool) {
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

// Forget drops the entries for files that are gone.
func (c *Cache) Forget(paths []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range paths {
		delete(c.entries, p)
	}
}

// ForgetUnder drops the entries for files in dir, a folder taken out of
// the library.
func (c *Cache) ForgetUnder(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for p := range c.entries {
		if Within(p, dir) {
			delete(c.entries, p)
		}
	}
}

// Save writes the cache to disk.
func (c *Cache) Save() error {
	if c.path == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return gobfile.Save(c.path, cacheFile{Version: cacheVersion, Entries: c.entries})
}
