package library

import (
	"context"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Sizes come from the scan, for cached tracks too.
func TestScanRecordsSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(path, make([]byte, 12345), 0o644)
	cache := LoadCache("")
	first := ScanFile(cache, path)
	cache.Update(filepath.Dir(path), map[string]CacheEntry{path: {Size: first.size, ModTime: first.ModTime, Track: Track{Path: path, Title: "a"}}}, true)
	again := ScanFile(cache, path)
	if first.Track.Size != 12345 || !again.Cached || again.Track.Size != 12345 {
		t.Errorf("sizes %d, %d (cached %v)", first.Track.Size, again.Track.Size, again.Cached)
	}
}

// Songs read before lengths were, and still unchanged, get their length
// on the next scan without their tags being read again.
func TestScanMeasuresCachedSongs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.flac")
	data, err := os.ReadFile(audiotest.Sample("sample.flac"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	cache := LoadCache("")
	old := Track{Path: path, Title: "Cached Title", Format: "FLAC"} // no Length
	cache.Update(dir, map[string]CacheEntry{path: {Size: st.Size(), ModTime: st.ModTime().UnixNano(), Track: old}}, true)

	r := ScanFile(cache, path)
	if !r.Cached || r.Track.Title != "Cached Title" {
		t.Errorf("tags should come from the cache: %+v", r.Track)
	}
	if r.Track.Length != audio.FileLength(path) || r.Track.Length == 0 {
		t.Errorf("length %v, want %v", r.Track.Length, audio.FileLength(path))
	}
	if got := ReadTrack(path).Length; got != r.Track.Length {
		t.Errorf("readTrack length %v, want %v", got, r.Track.Length)
	}
}

func TestWalkFindsOnlyAudio(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.mp3", "b.FLAC", "c.txt", "sub/d.ogg", "sub/deeper/e.wav", "f.m4a"} {
		audiotest.WriteFile(t, filepath.Join(dir, name), []byte("x"))
	}
	audiotest.WriteFile(t, filepath.Join(dir, "empty.mp3"), nil) // an interrupted download: nothing to play

	paths := make(chan string)
	go Walk(context.Background(), dir, paths)
	var got []string
	for p := range paths {
		rel, _ := filepath.Rel(dir, p)
		got = append(got, rel)
	}
	slices.Sort(got)
	want := []string{"a.mp3", "b.FLAC", "sub/d.ogg", "sub/deeper/e.wav"}
	if audio.AppleDecoding { // M4A files play only on macOS
		want = []string{"a.mp3", "b.FLAC", "f.m4a", "sub/d.ogg", "sub/deeper/e.wav"}
	}
	for i := range want {
		want[i] = filepath.FromSlash(want[i])
	}
	if !slices.Equal(got, want) {
		t.Errorf("walk found %v, want %v", got, want)
	}
}

// walkAll lists what walk finds under root, relative to dir.
func walkAll(dir, root string) []string {
	paths := make(chan string)
	go Walk(context.Background(), root, paths)
	var got []string
	for p := range paths {
		rel, _ := filepath.Rel(dir, p)
		got = append(got, rel)
	}
	slices.Sort(got)
	return got
}

// The scan leaves out the sounds in apps, plug-ins and code, and Windows'
// system folders; but a folder added on purpose is scanned even if it is
// one of those.
func TestWalkSkipsFoldersThatArentMusic(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Album/song.mp3",
		"Game.app/Contents/Resources/click.wav",
		"Synth.component/Contents/x.mp3", // not a bundle type skipped
		"Plug.bundle/beep.ogg",
		"site/node_modules/pkg/test.mp3",
		"$RECYCLE.BIN/S-1-5-21/old.mp3",
		"System Volume Information/x.wav",
		"Apps I Like/song.flac",
	} {
		audiotest.WriteFile(t, filepath.Join(dir, name), []byte("x"))
	}
	want := []string{"Album/song.mp3", "Apps I Like/song.flac", "Synth.component/Contents/x.mp3"}
	if got := walkAll(dir, dir); !slices.Equal(got, want) {
		t.Errorf("walk found %v, want %v", got, want)
	}
	if got := walkAll(dir, filepath.Join(dir, "Plug.bundle")); !slices.Equal(got, []string{"Plug.bundle/beep.ogg"}) {
		t.Errorf("a folder added on purpose should be scanned: %v", got)
	}
}

func TestReadTrackTags(t *testing.T) {
	audio, err := os.ReadFile(audiotest.Sample("sample.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "file name.mp3")
	tag := audiotest.ID3v23(map[string]string{"TIT2": "Song", "TPE1": "Artist", "TALB": "Album", "TYER": "1999"})
	audiotest.WriteFile(t, path, append(tag, audio...))

	got := ReadTrack(path)
	want := Track{Path: path, Title: "Song", Artist: "Artist", Album: "Album", Year: 1999, Format: "MP3"}
	if got != want {
		t.Errorf("readTrack = %+v, want %+v", got, want)
	}
}

func TestReadTrackFallsBackToFileName(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "My Song.flac")
	audiotest.WriteFile(t, garbage, []byte("not really audio"))
	untitled := filepath.Join(dir, "Untitled.mp3")
	audiotest.WriteFile(t, untitled, audiotest.ID3v23(map[string]string{"TPE1": "Someone"}))

	if got := ReadTrack(garbage); got.Title != "My Song" || !got.NoTags || got.Format != "FLAC" {
		t.Errorf("unreadable file: %+v", got)
	}
	if got := ReadTrack(untitled); got.Title != "Untitled" || got.Artist != "Someone" || got.NoTags {
		t.Errorf("tag without title: %+v", got)
	}
	if got := ReadTrack(filepath.Join(dir, "missing.mp3")); got.Title != "missing" || !got.NoTags {
		t.Errorf("missing file: %+v", got)
	}
}
