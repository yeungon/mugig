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
	fp := filepath.FromSlash // paths in the system's own form
	c := LoadCache("")
	c.entries = map[string]CacheEntry{
		fp("/music/a.mp3"):      {},
		fp("/music/sub/b.mp3"):  {},
		fp("/musical/c.mp3"):    {}, // different folder that shares a prefix
		fp("/elsewhere/d.flac"): {},
	}
	seenA := map[string]CacheEntry{fp("/music/a.mp3"): {Size: 1}}

	c.Update(fp("/music"), seenA, false) // cancelled scan
	if len(c.entries) != 4 || c.entries[fp("/music/a.mp3")].Size != 1 {
		t.Errorf("cancelled scan should only add/refresh: %v", c.entries)
	}
	c.Update(fp("/music/"), seenA, true)
	for _, p := range []string{fp("/music/a.mp3"), fp("/musical/c.mp3"), fp("/elsewhere/d.flac")} {
		if _, ok := c.entries[p]; !ok {
			t.Errorf("%s should be kept", p)
		}
	}
	if _, ok := c.entries[fp("/music/sub/b.mp3")]; ok {
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
	fp := filepath.FromSlash // paths in the system's own form
	c := LoadCache("")
	when := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	entry := func(path string) CacheEntry {
		return CacheEntry{Size: 1, ModTime: when.UnixNano(), Track: Track{Path: path}}
	}
	paths := []string{fp("/Music/a.flac"), fp("/Music/Album/b.flac"), fp("/Music Library/c.flac"), fp("/Musical/d.flac"), fp("/Other/e.flac")}
	seen := map[string]CacheEntry{}
	for _, p := range paths {
		seen[p] = entry(p)
	}
	c.Update(fp("/"), seen, false)
	cached := func() (out []string) {
		for _, p := range paths {
			if _, ok := c.Lookup(p, 1, when.UnixNano()); ok {
				out = append(out, p)
			}
		}
		return out
	}

	if got := c.ModTime(fp("/Other/e.flac")); !got.Equal(when) {
		t.Errorf("file date %v, want %v", got, when)
	}
	if got := c.ModTime(fp("/not/cached.flac")); !got.IsZero() {
		t.Errorf("an unknown file's date should be zero, not %v", got)
	}

	c.Update(fp("/Music"), map[string]CacheEntry{fp("/Music/a.flac"): entry(fp("/Music/a.flac"))}, true)
	if got := cached(); !slices.Equal(got, []string{fp("/Music/a.flac"), fp("/Music Library/c.flac"), fp("/Musical/d.flac"), fp("/Other/e.flac")}) {
		t.Errorf("after a complete scan of /Music: %v", got)
	}
	c.ForgetUnder(fp("/Music"))
	if got := cached(); !slices.Equal(got, []string{fp("/Music Library/c.flac"), fp("/Musical/d.flac"), fp("/Other/e.flac")}) {
		t.Errorf("after taking /Music out: %v", got)
	}
	c.Forget([]string{fp("/Other/e.flac"), fp("/never/cached.flac")})
	if got := cached(); !slices.Equal(got, []string{fp("/Music Library/c.flac"), fp("/Musical/d.flac")}) {
		t.Errorf("after forgetting e: %v", got)
	}
}
