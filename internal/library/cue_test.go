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
