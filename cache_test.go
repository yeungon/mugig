package main

import (
	"context"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCacheSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "song.mp3")
	audiotest.WriteFile(t, song, id3v23(map[string]string{"TIT2": "First"}))
	cachePath := filepath.Join(t.TempDir(), "library.gob")

	c := loadCache(cachePath)
	r := scanFile(c, song)
	if r.cached || r.track.Title != "First" {
		t.Fatalf("first scan: %+v", r)
	}
	c.update(dir, map[string]cacheEntry{song: r.entry()}, true)
	if err := c.save(); err != nil {
		t.Fatal(err)
	}

	c = loadCache(cachePath) // as on the next launch
	r = scanFile(c, song)
	if !r.cached || r.track.Title != "First" || r.track.search != searchKey(r.track) {
		t.Fatalf("rescan should come from cache: %+v", r)
	}

	audiotest.WriteFile(t, song, id3v23(map[string]string{"TIT2": "Second"}))
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(song, later, later); err != nil {
		t.Fatal(err)
	}
	if r = scanFile(c, song); r.cached || r.track.Title != "Second" {
		t.Errorf("a changed file should be reread: %+v", r)
	}
}

func TestCacheDropsDeletedFilesOnlyAfterFullScan(t *testing.T) {
	c := loadCache("")
	c.entries = map[string]cacheEntry{
		"/music/a.mp3":      {},
		"/music/sub/b.mp3":  {},
		"/musical/c.mp3":    {}, // different folder that shares a prefix
		"/elsewhere/d.flac": {},
	}
	seenA := map[string]cacheEntry{"/music/a.mp3": {Size: 1}}

	c.update("/music", seenA, false) // cancelled scan
	if len(c.entries) != 4 || c.entries["/music/a.mp3"].Size != 1 {
		t.Errorf("cancelled scan should only add/refresh: %v", c.entries)
	}
	c.update("/music/", seenA, true)
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
	if c := loadCache(corrupt); len(c.entries) != 0 {
		t.Error("corrupt cache should load empty")
	}
	if c := loadCache(filepath.Join(dir, "missing.gob")); c.entries == nil {
		t.Error("missing cache should load empty but usable")
	}

	old := filepath.Join(dir, "old.gob")
	c := loadCache(old)
	c.entries["/x.mp3"] = cacheEntry{Size: 1}
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	if loaded := loadCache(old); len(loaded.entries) != 1 {
		t.Fatal("round trip failed")
	}
	// Leftover temp files would mean save isn't cleaning up.
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
		t.Errorf("temp files left behind: %v", m)
	}
}

// A real scan, twice: the second should take every track from the cache.
// runScan is called directly because the test driver runs fyne.Do inline,
// so a background scan would race with the test.
func TestRescanUsesCache(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := t.TempDir()
	audiotest.WriteFile(t, filepath.Join(dir, "a.mp3"), id3v23(map[string]string{"TIT2": "A"}))
	audiotest.WriteFile(t, filepath.Join(dir, "sub", "b.mp3"), id3v23(map[string]string{"TIT2": "B"}))
	audiotest.WriteFile(t, filepath.Join(dir, "notes.txt"), nil)

	for _, want := range []string{"0 unchanged", "2 unchanged"} {
		u.runScan(context.Background(), dir, 2) // the second time, the tracks are updated, not added again
		if got := titles(u.tracks); !slices.Equal(got, []string{"A", "B"}) || !strings.Contains(u.status.Text, want) {
			t.Errorf("after scan: tracks %v, status %q (want %q)", got, u.status.Text, want)
		}
	}
}
