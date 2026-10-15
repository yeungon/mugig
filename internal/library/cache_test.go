package library

import (
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestCacheSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "song.mp3")
	audiotest.WriteFile(t, song, audiotest.ID3v23(map[string]string{"TIT2": "First"}))
	cachePath := filepath.Join(t.TempDir(), "library.gob")

	c := LoadCache(cachePath)
	r := ScanFile(c, song)
	if r.Cached || r.Track.Title != "First" {
		t.Fatalf("first scan: %+v", r)
	}
	c.Update(dir, map[string]CacheEntry{song: r.Entry()}, true)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	c = LoadCache(cachePath) // as on the next launch
	r = ScanFile(c, song)
	if !r.Cached || r.Track.Title != "First" || r.Track.search != searchKey(r.Track) {
		t.Fatalf("rescan should come from cache: %+v", r)
	}

	audiotest.WriteFile(t, song, audiotest.ID3v23(map[string]string{"TIT2": "Second"}))
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(song, later, later); err != nil {
		t.Fatal(err)
	}
	if r = ScanFile(c, song); r.Cached || r.Track.Title != "Second" {
		t.Errorf("a changed file should be reread: %+v", r)
	}
}

func TestCacheDropsDeletedFilesOnlyAfterFullScan(t *testing.T) {
	c := LoadCache("")
	c.entries = map[string]CacheEntry{
		"/music/a.mp3":      {},
		"/music/sub/b.mp3":  {},
		"/musical/c.mp3":    {}, // different folder that shares a prefix
		"/elsewhere/d.flac": {},
	}
	seenA := map[string]CacheEntry{"/music/a.mp3": {Size: 1}}

	c.Update("/music", seenA, false) // cancelled scan
	if len(c.entries) != 4 || c.entries["/music/a.mp3"].Size != 1 {
		t.Errorf("cancelled scan should only add/refresh: %v", c.entries)
	}
	c.Update("/music/", seenA, true)
	for _, p := range []string{"/music/a.mp3", "/musical/c.mp3", "/elsewhere/d.flac"} {
		if _, ok := c.entries[p]; !ok {
			t.Errorf("%s should be kept", p)
		}
	}
	if _, ok := c.entries["/music/sub/b.mp3"]; ok {
		t.Error("a file missing from a full scan should be dropped")
	}
}

func TestCacheIgnoresBadFiles(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.gob")
	audiotest.WriteFile(t, corrupt, []byte("definitely not gob"))
	if c := LoadCache(corrupt); len(c.entries) != 0 {
		t.Error("corrupt cache should load empty")
	}
	if c := LoadCache(filepath.Join(dir, "missing.gob")); c.entries == nil {
		t.Error("missing cache should load empty but usable")
	}

	old := filepath.Join(dir, "old.gob")
	c := LoadCache(old)
	c.entries["/x.mp3"] = CacheEntry{Size: 1}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if loaded := LoadCache(old); len(loaded.entries) != 1 {
		t.Fatal("round trip failed")
	}
	// Leftover temp files would mean save isn't cleaning up.
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
		t.Errorf("temp files left behind: %v", m)
	}
}

// Taking a folder out forgets just its songs, not those of a folder whose
// name starts the same; a complete scan of one folder leaves the others'
// songs alone too. File dates come from the cache.
func TestCacheForgetting(t *testing.T) {
	c := LoadCache("")
	when := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	entry := func(path string) CacheEntry {
		return CacheEntry{Size: 1, ModTime: when.UnixNano(), Track: Track{Path: path}}
	}
	paths := []string{"/Music/a.flac", "/Music/Album/b.flac", "/Music Library/c.flac", "/Musical/d.flac", "/Other/e.flac"}
	seen := map[string]CacheEntry{}
	for _, p := range paths {
		seen[p] = entry(p)
	}
	c.Update("/", seen, false)
	cached := func() (out []string) {
		for _, p := range paths {
			if _, ok := c.Lookup(p, 1, when.UnixNano()); ok {
				out = append(out, p)
			}
		}
		return out
	}

	if got := c.ModTime("/Other/e.flac"); !got.Equal(when) {
		t.Errorf("file date %v, want %v", got, when)
	}
	if got := c.ModTime("/not/cached.flac"); !got.IsZero() {
		t.Errorf("an unknown file's date should be zero, not %v", got)
	}

	c.Update("/Music", map[string]CacheEntry{"/Music/a.flac": entry("/Music/a.flac")}, true)
	if got := cached(); !slices.Equal(got, []string{"/Music/a.flac", "/Music Library/c.flac", "/Musical/d.flac", "/Other/e.flac"}) {
		t.Errorf("after a complete scan of /Music: %v", got)
	}
	c.ForgetUnder("/Music")
	if got := cached(); !slices.Equal(got, []string{"/Music Library/c.flac", "/Musical/d.flac", "/Other/e.flac"}) {
		t.Errorf("after taking /Music out: %v", got)
	}
	c.Forget([]string{"/Other/e.flac", "/never/cached.flac"})
	if got := cached(); !slices.Equal(got, []string{"/Music Library/c.flac", "/Musical/d.flac"}) {
		t.Errorf("after forgetting e: %v", got)
	}
}
