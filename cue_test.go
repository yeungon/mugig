package main

import (
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"mugig/internal/quality"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const testCue = `REM GENRE Pop
REM DATE 2005
PERFORMER "Some Singer"
TITLE "An Album"
FILE "%s" WAVE
  TRACK 01 AUDIO
    TITLE "First"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Second"
    PERFORMER "A Guest"
    INDEX 01 00:01:10
  TRACK 03 AUDIO
    TITLE "Third"
    INDEX 00 00:01:70
    INDEX 01 00:02:00
`

// cueImage writes a 3-second WAV disc image whose every sample can be
// told apart, and a sheet for it, encoded by enc (nil for UTF-8), in a
// folder of its own. It returns the folder and the image's samples.
func cueImage(t *testing.T, name string, encode func(string) []byte) (string, []int32) {
	t.Helper()
	dir := t.TempDir()
	samples := make([]int32, 3*44100)
	for i := range samples {
		samples[i] = int32(i%30000 - 15000)
	}
	audiotest.WriteWAV(t, filepath.Join(dir, name+".wav"), 44100, 16, 2, samples, samples)
	text := strings.ReplaceAll(testCue, "%s", name+".wav")
	data := []byte(text)
	if encode != nil {
		data = encode(text)
	}
	os.WriteFile(filepath.Join(dir, name+".cue"), data, 0o644)
	return dir, samples
}

func TestParseCue(t *testing.T) {
	sheets := parseCue(strings.ReplaceAll(testCue, "%s", `Disc 1\Album.wav`), "/music")
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
		dir, _ := cueImage(t, name, enc)
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
	dir, _ := cueImage(t, "Album", nil)
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

// Scanning a folder lists the image's songs, with the sheet's details,
// and not the image; a rescan from the cache lists the same.
func TestScanCueSongs(t *testing.T) {
	dir, _ := cueImage(t, "Album", nil)
	audiotest.CopyFile(t, "testdata/sample.flac", filepath.Join(dir, "Bonus.flac"))
	u, _, _ := newTestUI(t)
	u.addFolder(dir)
	check := func(when string) {
		t.Helper()
		if got := slices.Sorted(slices.Values(titles(u.all))); !slices.Equal(got, []string{"Bonus", "First", "Second", "Third"}) {
			t.Fatalf("%s: listed %v", when, got)
		}
		i := slices.IndexFunc(u.all, func(t Track) bool { return t.Title == "Second" })
		s := u.all[i]
		if s.Path != filepath.Join(dir, "Album.wav")+"#02" || s.Artist != "A Guest" || s.Album != "An Album" || s.Year != 2005 || s.Format != "WAV" {
			t.Errorf("%s: %+v", when, s)
		}
		if want := 2*time.Second - (time.Second + 10*time.Second/75); s.Length != want {
			t.Errorf("%s: length %v, want %v", when, s.Length, want)
		}
		if audio.FileOf(s.Path) != filepath.Join(dir, "Album.wav") || !audio.FromCue(s.Path) {
			t.Errorf("%s: its file is %q", when, audio.FileOf(s.Path))
		}
	}
	check("first scan")
	audio.ForgetSpans() // as at the next start
	u.scanLibrary()
	check("from the cache")
}

// Each song plays exactly its stretch of the image, and played one after
// another they are the image, sample for sample. (That the player joins
// songs on the very sample is tested with the player.)
func TestCueSongsPlayTheImageExactly(t *testing.T) {
	dir, want := cueImage(t, "Album", nil)
	u, _, _ := newTestUI(t)
	u.addFolder(dir)
	image := filepath.Join(dir, "Album.wav")

	var got []float64
	buf := make([][2]float64, 3000)
	for n := 1; n <= 3; n++ {
		s, _, err := audio.Decode(cuePath(image, n))
		if err != nil {
			t.Fatal(err)
		}
		if n == 1 && s.Len() != 44100+5880 {
			t.Errorf("the first song has %d samples, want %d", s.Len(), 44100+5880)
		}
		for {
			k, ok := s.Stream(buf)
			for _, x := range buf[:k] {
				got = append(got, x[0])
			}
			if !ok {
				break
			}
		}
		s.Close()
	}
	if len(got) != len(want) {
		t.Fatalf("%d samples, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != float64(want[i])/32768 {
			t.Fatalf("sample %d is %v, want %v", i, got[i]*32768, want[i])
		}
	}

	// Seeking is within the song.
	b, _, err := audio.Decode(cuePath(image, 2))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Seek(100)
	one := make([][2]float64, 1)
	b.Stream(one)
	if v := one[0][0] * 32768; v != float64(want[49980+100]) {
		t.Errorf("after seeking: %v, want %v", v, want[49980+100])
	}
}

// Changes to the sheet are followed: adding one lists its songs in place
// of the image, taking it away brings the image back.
func TestWatchCueSheet(t *testing.T) {
	u, fw, dir := watchedLibrary(t)
	cueDir, _ := cueImage(t, "Album", nil)
	album := filepath.Join(dir, "Album")
	audiotest.CopyFile(t, filepath.Join(cueDir, "Album.wav"), filepath.Join(album, "Album.wav"))
	fw.changed([]string{album})
	if !slices.Contains(titles(u.all), "Album") {
		t.Fatalf("the image alone should be a song: %v", titles(u.all))
	}
	audiotest.CopyFile(t, filepath.Join(cueDir, "Album.cue"), filepath.Join(album, "Album.cue"))
	fw.changed([]string{filepath.Join(album, "Album.cue")})
	if got := titles(u.all); slices.Contains(got, "Album") || !slices.Contains(got, "Second") || len(got) != 6 {
		t.Errorf("with the sheet: %v", got)
	}
	os.Remove(filepath.Join(album, "Album.cue"))
	fw.changed([]string{filepath.Join(album, "Album.cue")})
	if got := titles(u.all); !slices.Contains(got, "Album") || slices.Contains(got, "Second") || len(got) != 4 {
		t.Errorf("without the sheet: %v", got)
	}
}

// The songs of an image share its quality verdict, and checking them all
// checks the image once. Move to Trash is off, as it would take the whole
// album; Copy File Path gives the image.
func TestCueSongsShareTheImage(t *testing.T) {
	dir, _ := cueImage(t, "Album", nil)
	u, _, a := newTestUI(t)
	u.addFolder(dir)
	image := filepath.Join(dir, "Album.wav")
	u.setLevel(cuePath(image, 2), quality.Suspect)
	for _, tr := range u.all {
		if u.levelOf(tr) != quality.Suspect {
			t.Errorf("%s: %v", tr.Title, u.levelOf(tr))
		}
	}
	delete(u.levels, image)
	u.checkSongs(u.all)
	if u.batch == nil || u.batch.total != 1 {
		t.Errorf("checking %d files, want the image once", u.batch.total)
	}

	m := u.trackMenu(u.all[0])
	if !itemIn(t, m, trashLabel).Disabled {
		t.Error("Move to Trash should be off for a song from a CUE sheet")
	}
	itemIn(t, m, "Copy File Path").Action()
	if got := a.Clipboard().Content(); got != image {
		t.Errorf("copied %q", got)
	}
	var shown string
	u.reveal = func(p string) error { shown = p; return nil }
	u.revealTrack(u.all[1])
	if shown != image {
		t.Errorf("revealed %q", shown)
	}
	if d := u.showInfo(u.all[1]); d == nil || !strings.Contains(allText(u.win.Canvas().Overlays().Top()), "this song runs from") {
		t.Error("Get Info should say where in the image the song is")
	}
}
