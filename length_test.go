package main

import (
	"mugig/internal/audio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The Time column shows each song's length, sorts by it, and the library's
// total is in the stats line.
func TestTimeColumn(t *testing.T) {
	u, _, _ := newTestUI(t)
	songs := fiveTracks()
	for i, l := range []time.Duration{3 * time.Minute, 65 * time.Second, 0, 6 * time.Minute, 2 * time.Hour} {
		songs[i].Length = l
	}
	setLibrary(u, songs...)
	if columns[timeCol] != "Time" || cellText(songs[1], timeCol) != "1:05" || cellText(songs[2], timeCol) != "" {
		t.Errorf("Time cells: %q, %q", cellText(songs[1], timeCol), cellText(songs[2], timeCol))
	}
	u.sortBy(timeCol)
	if got := titles(u.tracks); !slices.Equal(got, []string{"Hey Jude", "Help", "Come Together", "Bohemian Rhapsody", "Zebra"}) {
		t.Errorf("sorted by time: %v", got)
	}
	if !strings.HasSuffix(u.count.Text, "· 2 h 10 min") {
		t.Errorf("stats %q should give the total time", u.count.Text)
	}
}

// Songs read before lengths were, and still unchanged, get their length
// on the next scan without their tags being read again.
func TestScanMeasuresCachedSongs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.flac")
	data, err := os.ReadFile(filepath.Join("testdata", "sample.flac"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	cache := loadCache("")
	old := Track{Path: path, Title: "Cached Title", Format: "FLAC"} // no Length
	cache.update(dir, map[string]cacheEntry{path: {Size: st.Size(), ModTime: st.ModTime().UnixNano(), Track: old}}, true)

	r := scanFile(cache, path)
	if !r.cached || r.track.Title != "Cached Title" {
		t.Errorf("tags should come from the cache: %+v", r.track)
	}
	if r.track.Length != audio.FileLength(path) || r.track.Length == 0 {
		t.Errorf("length %v, want %v", r.track.Length, audio.FileLength(path))
	}
	if got := readTrack(path).Length; got != r.track.Length {
		t.Errorf("readTrack length %v, want %v", got, r.track.Length)
	}
}
