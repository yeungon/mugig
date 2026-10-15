package main

import (
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

// mixedLibrary writes a genuine FLAC, one made from an MP3, a damaged one,
// a WAV and an MP3.
func mixedLibrary(t *testing.T) (dir string, tracks map[string]library.Track) {
	t.Helper()
	dir = t.TempDir()
	const rate = 44100
	genuine := audiotest.Quantize(audiotest.Noise(3*rate, rate, audiotest.Pink), 16)
	audiotest.WriteFLAC(t, filepath.Join(dir, "genuine.flac"), rate, 16, genuine, genuine)
	fake := audiotest.Quantize(audiotest.Noise(3*rate, rate, audiotest.Lowpass(audiotest.Pink, 16000, 300)), 16)
	audiotest.WriteFLAC(t, filepath.Join(dir, "fake.flac"), rate, 16, fake, fake)
	audiotest.WriteFLAC(t, filepath.Join(dir, "damaged.flac"), rate, 16, genuine, genuine)
	data, _ := os.ReadFile(filepath.Join(dir, "damaged.flac"))
	data[len(data)/2] ^= 0x10
	os.WriteFile(filepath.Join(dir, "damaged.flac"), data, 0o644)
	audiotest.WriteWAV(t, filepath.Join(dir, "plain.wav"), rate, 16, 2, genuine, genuine)
	audiotest.CopyFile(t, "testdata/sample.mp3", filepath.Join(dir, "song.mp3"))

	tracks = map[string]library.Track{}
	for _, name := range []string{"genuine.flac", "fake.flac", "damaged.flac", "plain.wav", "song.mp3"} {
		tr := library.ReadTrack(filepath.Join(dir, name))
		tracks[name] = tr
	}
	return dir, tracks
}

func libraryOf(tracks map[string]library.Track) []library.Track {
	var all []library.Track
	for _, t := range tracks {
		all = append(all, t)
	}
	slices.SortFunc(all, func(a, b library.Track) int { return strings.Compare(a.Path, b.Path) })
	return all
}

func TestCheckAllSongs(t *testing.T) {
	u, _, _ := newTestUI(t)
	store := filepath.Join(t.TempDir(), "quality.gob")
	u.qualityStore = quality.LoadStore(store)
	_, tracks := mixedLibrary(t)
	setLibrary(u, libraryOf(tracks)...)

	u.checkAllSongs()
	want := map[string]quality.Level{
		"genuine.flac": quality.Lossless,
		"fake.flac":    quality.Suspect,
		"damaged.flac": quality.Damaged,
		"plain.wav":    quality.Lossless,
		"song.mp3":     quality.Lossy, // by its format: nothing to check
	}
	for name, l := range want {
		if got := u.levelOf(tracks[name]); got != l {
			t.Errorf("%s: %v, want %v", name, got, l)
		}
	}
	if _, checked := u.levels[tracks["song.mp3"].Path]; checked {
		t.Error("the MP3 was decoded, though lossy files have nothing to check")
	}
	b := u.batch
	if !b.finished || b.done != 4 || b.analysed != 4 || u.batchProgress.Visible() || u.checkAllBtn.Disabled() {
		t.Errorf("run: %+v", b)
	}
	for _, s := range []string{"Checked 4 of 4 songs", "1 damaged", "1 lossless?", "2 lossless", "Sort by Quality"} {
		if !strings.Contains(u.batchLabel.Text, s) {
			t.Errorf("summary %q lacks %q", u.batchLabel.Text, s)
		}
	}

	// Saved: the next session shows the verdicts, and checking again
	// reuses them rather than decoding anything.
	u2, _, _ := newTestUI(t)
	u2.qualityStore = quality.LoadStore(store)
	u2.levels = u2.qualityStore.Levels()
	setLibrary(u2, libraryOf(tracks)...)
	if u2.levelOf(tracks["fake.flac"]) != quality.Suspect {
		t.Error("verdicts not saved")
	}
	u2.checkAllSongs()
	if u2.batch.analysed != 0 || u2.batch.done != 4 {
		t.Errorf("second run decoded %d songs", u2.batch.analysed)
	}
}

// Sorting by Quality puts problems first and unchecked songs last.
func TestSortByQuality(t *testing.T) {
	u, _, _ := newTestUI(t)
	_, tracks := mixedLibrary(t)
	setLibrary(u, libraryOf(tracks)...)
	u.levels[tracks["fake.flac"].Path] = quality.Suspect
	u.levels[tracks["damaged.flac"].Path] = quality.Damaged
	u.levels[tracks["genuine.flac"].Path] = quality.Lossless
	u.sortBy(qualityCol)
	var got []string
	for _, tr := range u.tracks {
		got = append(got, filepath.Base(tr.Path))
	}
	if want := []string{"damaged.flac", "fake.flac", "song.mp3", "genuine.flac", "plain.wav"}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	if quality.LevelNames[u.levelOf(tracks["plain.wav"])] != "" {
		t.Error("an unchecked lossless song should be blank")
	}
}

func TestCheckAllSongsCancelled(t *testing.T) {
	u, _, _ := newTestUI(t)
	_, tracks := mixedLibrary(t)
	setLibrary(u, libraryOf(tracks)...)
	var pending []func()
	u.async = func(f func()) { pending = append(pending, f) }
	u.checkAllSongs()
	u.stopBatch() // before anything was checked
	pending[0]()
	if !u.batch.finished || u.batch.done != 0 || !strings.HasPrefix(u.batchLabel.Text, "Stopped after 0 of 4") {
		t.Errorf("label %q, run %+v", u.batchLabel.Text, u.batch)
	}
	u.stopBatch() // now dismisses the summary
	if u.batchBar.Visible() {
		t.Error("summary not dismissed")
	}
}

// A file that changes loses its verdict, which was about the old contents.
func TestChangedFileLosesVerdict(t *testing.T) {
	u, fw, dir := watchedLibrary(t)
	path := filepath.Join(dir, "sample.flac")
	u.levels[path] = quality.Lossless
	u.qualityStore.Put(path, quality.FileStamp(path), quality.Report{}, quality.Lossless)
	later := time.Now().Add(time.Hour)
	os.Chtimes(path, later, later)
	fw.changed([]string{path})
	if _, ok := u.levels[path]; ok {
		t.Error("verdict kept for a changed file")
	}
	if _, ok := u.qualityStore.Levels()[path]; ok {
		t.Error("saved verdict kept for a changed file")
	}
}

func TestRoughly(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "under a minute",
		25 * time.Minute: "about 25 min",
		90 * time.Minute: "about 1.5 hours",
	} {
		if got := roughly(d); got != want {
			t.Errorf("%v: %q", d, got)
		}
	}
}
