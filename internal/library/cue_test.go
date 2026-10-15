package library

import (
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestParseCue(t *testing.T) {
	sheets := parseCue(strings.ReplaceAll(audiotest.CueSheet, "%s", `Disc 1\Album.wav`), "/music")
	if len(sheets) != 1 {
		t.Fatalf("%d files", len(sheets))
	}
	s := sheets[0]
	if s.album != "An Album" || s.performer != "Some Singer" || s.year != 2005 || s.file != "/music/Disc 1/Album.wav" {
		t.Errorf("sheet %+v", s)
	}
	if len(s.tracks) != 3 || s.tracks[1].performer != "A Guest" || s.tracks[1].start != time.Second+10*time.Second/75 {
		t.Errorf("tracks %+v", s.tracks)
	}
	if s.tracks[2].start != 2*time.Second {
		t.Errorf("track 3 starts at %v: INDEX 01, not the gap's INDEX 00", s.tracks[2].start)
	}
	if d, ok := cueTime("74:59:74"); !ok || d != 74*time.Minute+59*time.Second+74*time.Second/75 {
		t.Errorf("cueTime %v", d)
	}
	for _, bad := range []string{"1:2", "a:00:00", "00:-1:00"} {
		if _, ok := cueTime(bad); ok {
			t.Errorf("%q read as a time", bad)
		}
	}
}

// Sheets in UTF-8 with a byte order mark, and in Chinese GBK naming a
// file in Chinese, are read, the encoding found by the file being there.
func TestReadCueEncodings(t *testing.T) {
	gbk := func(s string) []byte {
		b, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	bom := func(s string) []byte { return append([]byte("\xEF\xBB\xBF"), s...) }
	for name, enc := range map[string]func(string) []byte{"plain": nil, "bom": bom, "童丽 - 枉凝眉": gbk} {
		dir, _ := audiotest.CueImage(t, name, enc)
		sheets, err := readCue(filepath.Join(dir, name+".cue"))
		if err != nil || len(sheets) != 1 || sheets[0].file != filepath.Join(dir, name+".wav") || len(sheets[0].tracks) != 3 {
			t.Errorf("%s: %+v %v", name, sheets, err)
		}
	}
}

// A sheet naming a file that isn't there still finds the one image named
// like it; a sheet kept from a rip split into one file per song gives no
// songs, as those files are songs already.
func TestReadCueFallbacks(t *testing.T) {
	dir, _ := audiotest.CueImage(t, "Album", nil)
	cue := filepath.Join(dir, "Album.cue")
	data, _ := os.ReadFile(cue)
	os.WriteFile(cue, []byte(strings.ReplaceAll(string(data), "Album.wav", "Old Name.wav")), 0o644)
	if sheets, _ := readCue(cue); len(sheets) != 1 || sheets[0].file != filepath.Join(dir, "Album.wav") {
		t.Errorf("fallback: %+v", sheets)
	}

	split := t.TempDir()
	var text strings.Builder
	for i, name := range []string{"01 A.wav", "02 B.wav"} {
		audiotest.WriteWAV(t, filepath.Join(split, name), 44100, 16, 2, []int32{1, 2}, []int32{1, 2})
		text.WriteString("FILE \"" + name + "\" WAVE\n  TRACK 0" + string(rune('1'+i)) + " AUDIO\n    INDEX 01 00:00:00\n")
	}
	os.WriteFile(filepath.Join(split, "Album.cue"), []byte(text.String()), 0o644)
	if sheets, _ := readCue(filepath.Join(split, "Album.cue")); len(sheets) != 0 {
		t.Errorf("split rip gave %+v", sheets)
	}
}

// The year is read however the ripper wrote the date; a date that isn't
// one gives none.
func TestParseCueDates(t *testing.T) {
	for date, want := range map[string]int{
		`1999`: 1999, `"1999"`: 1999, `1999-05-01`: 1999, `"1999/05/01"`: 1999,
		`unknown`: 0, `""`: 0, `99`: 0,
	} {
		sheets := parseCue("REM DATE "+date+"\nFILE \"a.wav\" WAVE\n TRACK 01 AUDIO\n  INDEX 01 00:00:00\n", "/m")
		if len(sheets) != 1 || sheets[0].year != want {
			t.Errorf("REM DATE %s: year %d, want %d", date, sheets[0].year, want)
		}
	}
}

// Sheets as rippers write them: Windows line endings, lower-case words, a
// name without quotes, several files in one sheet, an absolute path; and
// as they go wrong: a track with only a pregap, and tracks out of order,
// which are left out.
func TestParseCueVariants(t *testing.T) {
	text := strings.Join([]string{
		`title "Live"`,
		`file My Album.wav WAVE`,
		`  track 01 audio`,
		`    index 01 00:00:00`,
		`  TRACK 02 AUDIO`,
		`    INDEX 00 00:30:00`, // a pregap, and no start
		`  TRACK 03 AUDIO`,
		`    INDEX 01 01:00:00`,
		`  TRACK 04 AUDIO`,
		`    INDEX 01 00:40:00`, // before the track above it
		`FILE "/elsewhere/Disc 2.flac" WAVE`,
		`  TRACK 05 AUDIO`,
		`    TITLE "Encore"`,
		`    INDEX 01 00:00:00`,
	}, "\r\n")
	sheets := parseCue(text, "/music")
	if len(sheets) != 2 {
		t.Fatalf("%d files, want 2", len(sheets))
	}
	a, b := sheets[0], sheets[1]
	if a.file != "/music/My Album.wav" || a.album != "Live" || b.file != "/elsewhere/Disc 2.flac" || b.album != "Live" {
		t.Errorf("files %q %q, albums %q %q", a.file, b.file, a.album, b.album)
	}
	var numbers []int
	for _, tr := range a.tracks {
		numbers = append(numbers, tr.number)
	}
	if len(numbers) != 2 || numbers[0] != 1 || numbers[1] != 3 {
		t.Errorf("first file's tracks %v, want 1 and 3", numbers)
	}
	if len(b.tracks) != 1 || b.tracks[0].number != 5 || b.tracks[0].title != "Encore" {
		t.Errorf("second file's tracks %+v", b.tracks)
	}
}

// Songs take the image's details where the sheet has none, and a song
// with no title is named after its number. Each song's size is its share
// of the image's.
func TestCueSongsDetails(t *testing.T) {
	s := cueSheet{file: "/m/Album.flac", tracks: []cueTrack{
		{number: 1, start: 0, title: "One"},
		{number: 2, start: 60 * time.Second},
	}}
	image := Track{Album: "Tagged Album", Artist: "Tagged", Year: 2001, Format: "FLAC", Length: 4 * time.Minute, Size: 40_000_000}
	songs := cueSongs(s, image)
	if len(songs) != 2 {
		t.Fatalf("%d songs", len(songs))
	}
	one, two := songs[0], songs[1]
	if two.Title != "Track 02" || one.Album != "Tagged Album" || one.Year != 2001 || one.Format != "FLAC" {
		t.Errorf("songs %+v", songs)
	}
	if one.Length != time.Minute || two.Length != 3*time.Minute || one.Size != 10_000_000 || two.Size != 30_000_000 {
		t.Errorf("lengths %v %v, sizes %d %d", one.Length, two.Length, one.Size, two.Size)
	}
	if one.Path != "/m/Album.flac#01" || one.File != "/m/Album.flac" || !Matches(one, []string{"one", "tagged"}) {
		t.Errorf("song one %+v", one)
	}
}

// A sheet that runs past its image, as one kept beside a shorter rip
// does: the song the image ends in ends with it, and a song that would
// start after the end isn't listed.
func TestCueSheetLongerThanItsFile(t *testing.T) {
	dir, _ := audiotest.CueImage(t, "Album", nil) // 3 seconds
	cue := filepath.Join(dir, "Album.cue")
	os.WriteFile(cue, []byte("FILE \"Album.wav\" WAVE\n TRACK 01 AUDIO\n  INDEX 01 00:00:00\n"+
		" TRACK 02 AUDIO\n  INDEX 01 00:02:00\n TRACK 03 AUDIO\n  INDEX 01 00:05:00\n"), 0o644)
	songs := scanCue(LoadCache(""), cue).Songs()
	if len(songs) != 2 {
		t.Fatalf("%d songs, want 2: the third starts after the file ends", len(songs))
	}
	if songs[1].Length != time.Second || songs[1].End != 3*time.Second {
		t.Errorf("the last song runs %v to %v, want 1 s, to the end at 3 s", songs[1].Length, songs[1].End)
	}
}
