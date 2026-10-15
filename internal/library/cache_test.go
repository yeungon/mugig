package library

import (
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
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
