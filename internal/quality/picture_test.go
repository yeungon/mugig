package quality

import (
	"context"
	"image"
	"math"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAnalyseReportsProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	audiotest.WriteFLAC(t, path, 44100, 16, audiotest.Quantize(audiotest.Noise(3*44100, 44100, audiotest.Pink), 16))
	info, _ := audio.Probe(path)
	var got []float64
	Analyse(context.Background(), path, info, true, func(f float64) { got = append(got, f) })
	if len(got) < 10 || got[len(got)-1] != 1 {
		t.Fatalf("progress %v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("progress went backwards: %v", got)
		}
	}
}

// The picture keeps levels, not colours: a tone 40 dB quieter than another
// is 40 dB lower in it.
func TestSpectrogramKeepsLevels(t *testing.T) {
	const rate = 44100
	peak := func(dbfs float64) float64 {
		s := sine(1000, math.Pow(10, dbfs/20), 2*rate)
		spec := newSpectrum(rate, int64(len(s)))
		spec.add(s)
		img := spec.picture()
		top := uint8(0)
		for _, v := range img.Pix {
			top = max(top, v)
		}
		return LevelDB(top)
	}
	loud, quiet := peak(-20), peak(-60)
	if d := loud - quiet; math.Abs(d-40) > 1 {
		t.Errorf("tones at -20 and -60 dBFS drawn at %.1f and %.1f dB", loud, quiet)
	}
}

// Levels are saved without loss.
func TestSpectrogramSavedExactly(t *testing.T) {
	store := LoadStore(filepath.Join(t.TempDir(), "quality.gob"))
	levels := image.NewGray(image.Rect(0, 0, PictureWidth, SpecRows))
	for i := range levels.Pix {
		levels.Pix[i] = uint8(i * 7)
	}
	store.PutPicture("/a.flac", levels)
	got, ok := store.Picture("/a.flac")
	if !ok || !slices.Equal(got.Pix, levels.Pix) {
		t.Error("saved levels differ")
	}
}

// Spectrograms saved as colour JPEGs by earlier versions are deleted, and
// their tracks analysed again when shown.
func TestOldSpectrogramsDropped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quality.gob")
	dir := filepath.Join(filepath.Dir(path), "spectrograms")
	os.MkdirAll(dir, 0o755)
	old := filepath.Join(dir, "0123456789abcdef01234567.jpg")
	os.WriteFile(old, []byte("jpeg"), 0o644)
	LoadStore(path)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old spectrogram kept")
	}
}

// sine is n samples of a tone at 44.1 kHz.
func sine(freq, amp float64, n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = amp * math.Sin(2*math.Pi*freq*float64(i)/44100)
	}
	return s
}
