package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// mixedLibrary writes a genuine FLAC, one made from an MP3, a damaged one,
// a WAV and an MP3.
func mixedLibrary(t *testing.T) (dir string, tracks map[string]Track) {
	t.Helper()
	dir = t.TempDir()
	const rate = 44100
	genuine := quantize(noise(3*rate, rate, pink), 16)
	writeFLAC(t, filepath.Join(dir, "genuine.flac"), rate, 16, genuine, genuine)
	fake := quantize(noise(3*rate, rate, lowpass(pink, 16000, 300)), 16)
	writeFLAC(t, filepath.Join(dir, "fake.flac"), rate, 16, fake, fake)
	writeFLAC(t, filepath.Join(dir, "damaged.flac"), rate, 16, genuine, genuine)
	data, _ := os.ReadFile(filepath.Join(dir, "damaged.flac"))
	data[len(data)/2] ^= 0x10
	os.WriteFile(filepath.Join(dir, "damaged.flac"), data, 0o644)
	writeWAV(t, filepath.Join(dir, "plain.wav"), rate, 16, 2, genuine, genuine)
	copyFile(t, "testdata/sample.mp3", filepath.Join(dir, "song.mp3"))

	tracks = map[string]Track{}
	for _, name := range []string{"genuine.flac", "fake.flac", "damaged.flac", "plain.wav", "song.mp3"} {
		tr := readTrack(filepath.Join(dir, name))
		tr.search = searchKey(tr)
		tracks[name] = tr
	}
	return dir, tracks
}

func libraryOf(tracks map[string]Track) []Track {
	var all []Track
	for _, t := range tracks {
		all = append(all, t)
	}
	slices.SortFunc(all, func(a, b Track) int { return strings.Compare(a.Path, b.Path) })
	return all
}

func TestCheckAllSongs(t *testing.T) {
	u, _, _ := newTestUI(t)
	store := filepath.Join(t.TempDir(), "quality.gob")
	u.qualityStore = loadQualityStore(store)
	_, tracks := mixedLibrary(t)
	setLibrary(u, libraryOf(tracks)...)

	u.checkAllSongs()
	want := map[string]qualityLevel{
		"genuine.flac": qualityLossless,
		"fake.flac":    qualitySuspect,
		"damaged.flac": qualityDamaged,
		"plain.wav":    qualityLossless,
		"song.mp3":     qualityLossy, // by its format: nothing to check
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
	u2.qualityStore = loadQualityStore(store)
	u2.levels = u2.qualityStore.levels()
	setLibrary(u2, libraryOf(tracks)...)
	if u2.levelOf(tracks["fake.flac"]) != qualitySuspect {
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
	u.levels[tracks["fake.flac"].Path] = qualitySuspect
	u.levels[tracks["damaged.flac"].Path] = qualityDamaged
	u.levels[tracks["genuine.flac"].Path] = qualityLossless
	u.sortBy(qualityCol)
	var got []string
	for _, tr := range u.tracks {
		got = append(got, filepath.Base(tr.Path))
	}
	if want := []string{"damaged.flac", "fake.flac", "song.mp3", "genuine.flac", "plain.wav"}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	if levelNames[u.levelOf(tracks["plain.wav"])] != "" {
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
	u.levels[path] = qualityLossless
	u.qualityStore.put(path, fileStamp(path), qualityReport{}, qualityLossless)
	later := time.Now().Add(time.Hour)
	os.Chtimes(path, later, later)
	fw.changed([]string{path})
	if _, ok := u.levels[path]; ok {
		t.Error("verdict kept for a changed file")
	}
	if _, ok := u.qualityStore.levels()[path]; ok {
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
