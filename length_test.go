package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// lengthOf opens path and reads its length from the headers.
func lengthOf(t *testing.T, path string) time.Duration {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return trackLength(f)
}

// The lengths read from headers match what the decoders find by reading
// all the audio.
func TestTrackLengthMatchesDecoder(t *testing.T) {
	for _, name := range []string{"sample.flac", "sample.mp3", "sample.ogg", "sample.wav"} {
		path := filepath.Join("testdata", name)
		s, format, err := decodeAs(path, sniff(path))
		if err != nil {
			t.Fatal(name, err)
		}
		want := format.SampleRate.D(s.Len())
		s.Close()
		got := lengthOf(t, path)
		if diff := (got - want).Abs(); want == 0 || diff > 30*time.Millisecond {
			t.Errorf("%s: length %v, decoder says %v", name, got, want)
		}
	}
}

// An MP3 without a Xing tag has a constant bitrate, so its length comes
// from its size; a trailing ID3v1 tag isn't audio.
func TestMP3LengthFromBitrate(t *testing.T) {
	// MPEG-1 layer III, 128 kbps, 44.1 kHz, stereo: 417 bytes a frame.
	frame := make([]byte, 417)
	copy(frame, []byte{0xFF, 0xFB, 0x90, 0x00})
	var data []byte
	for range 100 {
		data = append(data, frame...)
	}
	tag := make([]byte, 128)
	copy(tag, "TAG")
	data = append(data, tag...)
	path := filepath.Join(t.TempDir(), "cbr.mp3")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	want := 100 * 417 * 8 * time.Second / 128000 // ≈ 100 frames of 26.1 ms
	if got := lengthOf(t, path); got != want {
		t.Errorf("length %v, want %v", got, want)
	}
}

// Files that aren't audio, or are cut off in the header, have no length
// rather than a wrong one.
func TestTrackLengthUnknown(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"text.mp3":  []byte("not audio at all"),
		"short.ogg": []byte("OggS\x00"),
		"cut.flac":  []byte("fLaC\x80\x00\x00\x22"),
		"empty.wav": []byte("RIFF\x00\x00\x00\x00WAVE"),
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := lengthOf(t, path); got != 0 {
			t.Errorf("%s: length %v, want 0", name, got)
		}
	}
}

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
	for d, want := range map[time.Duration]string{
		40 * time.Second:             "1 min",
		48 * time.Minute:             "48 min",
		9*time.Hour + 12*time.Minute: "9 h 12 min",
		178 * time.Hour:              "7.4 days",
	} {
		if got := totalTime(d); got != want {
			t.Errorf("totalTime(%v) = %q, want %q", d, got, want)
		}
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
	if r.track.Length != lengthOf(t, path) || r.track.Length == 0 {
		t.Errorf("length %v, want %v", r.track.Length, lengthOf(t, path))
	}
	if got := readTrack(path).Length; got != r.track.Length {
		t.Errorf("readTrack length %v, want %v", got, r.track.Length)
	}
}
