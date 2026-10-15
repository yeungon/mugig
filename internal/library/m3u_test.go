package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A playlist is written as an extended M3U in UTF-8: lengths, artists and
// titles, songs beside the playlist relative to it and others in full,
// and songs from CUE sheets left out and counted.
func TestSaveM3U(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Road Trip.m3u8")
	songs := []Track{
		{Path: filepath.Join(dir, "Trịnh Công Sơn", "Diễm Xưa.flac"), Title: "Diễm Xưa", Artist: "Trịnh Công Sơn", Length: 247*time.Second + 400*time.Millisecond},
		{Path: "/Volumes/Other/song.mp3", Title: "No Artist"},
		{Path: "/m/Album.wav#02", File: "/m/Album.wav", Title: "From a Sheet", Length: time.Minute},
		{Path: filepath.Join(dir, "a.flac"), Title: "Two\nLines", Artist: "X"},
	}
	skipped, err := SaveM3U(path, "Road Trip", songs)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Errorf("left out %d songs, want the one from a CUE sheet", skipped)
	}
	data, _ := os.ReadFile(path)
	want := strings.Join([]string{
		"#EXTM3U",
		"#PLAYLIST:Road Trip",
		"#EXTINF:247,Trịnh Công Sơn - Diễm Xưa",
		"Trịnh Công Sơn/Diễm Xưa.flac",
		"#EXTINF:-1,No Artist",
		"/Volumes/Other/song.mp3",
		"#EXTINF:-1,X - Two Lines",
		"a.flac",
	}, "\n") + "\n"
	if string(data) != want {
		t.Errorf("wrote:\n%s\nwant:\n%s", data, want)
	}

	// A folder whose name starts like the playlist's isn't inside it.
	if got := m3uPath("/Music", "/Music Library/a.flac"); got != "/Music Library/a.flac" {
		t.Errorf("m3uPath = %q", got)
	}
	if _, err := SaveM3U(filepath.Join(dir, "missing", "x.m3u8"), "x", songs); err == nil {
		t.Error("saving into a folder that doesn't exist should fail")
	}
}
