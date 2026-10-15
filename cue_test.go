package main

import (
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"mugig/internal/library"
	"mugig/internal/quality"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Scanning a folder lists the image's songs, with the sheet's details,
// and not the image; a rescan from the cache lists the same.
func TestScanCueSongs(t *testing.T) {
	dir, _ := audiotest.CueImage(t, "Album", nil)
	audiotest.CopyFile(t, "testdata/sample.flac", filepath.Join(dir, "Bonus.flac"))
	u, _, _ := newTestUI(t)
	u.addFolder(dir)
	check := func(when string) {
		t.Helper()
		if got := slices.Sorted(slices.Values(titles(u.all))); !slices.Equal(got, []string{"Bonus", "First", "Second", "Third"}) {
			t.Fatalf("%s: listed %v", when, got)
		}
		i := slices.IndexFunc(u.all, func(t library.Track) bool { return t.Title == "Second" })
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
	dir, want := audiotest.CueImage(t, "Album", nil)
	u, _, _ := newTestUI(t)
	u.addFolder(dir)
	image := filepath.Join(dir, "Album.wav")

	var got []float64
	buf := make([][2]float64, 3000)
	for n := 1; n <= 3; n++ {
		s, _, err := audio.Decode(library.CuePath(image, n))
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
	b, _, err := audio.Decode(library.CuePath(image, 2))
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
	cueDir, _ := audiotest.CueImage(t, "Album", nil)
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
	dir, _ := audiotest.CueImage(t, "Album", nil)
	u, _, a := newTestUI(t)
	u.addFolder(dir)
	image := filepath.Join(dir, "Album.wav")
	u.setLevel(library.CuePath(image, 2), quality.Suspect)
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
