package audio

import (
	"math"
	"mugig/internal/audiotest"
	"path/filepath"
	"testing"
)

// Playing through macOS's decoder gives the same sound as the FLAC
// library, and can seek.
func TestApplePlaybackMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	s := audiotest.Quantize(audiotest.Noise(2*44100, 44100, audiotest.Pink), 16)
	audiotest.WriteFLAC(t, path, 44100, 16, s, s)
	a, format, err := appleDecode(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if format.SampleRate != 44100 || a.Len() != len(s) {
		t.Fatalf("rate %v, length %d", format.SampleRate, a.Len())
	}
	a.Seek(1000)
	buf := make([][2]float64, 100)
	n, _ := a.Stream(buf)
	for i := range n {
		if want := float64(s[1000+i]) / 32768; math.Abs(buf[i][0]-want) > 1e-9 || buf[i][1] != buf[i][0] {
			t.Fatalf("sample %d: %v, want %v", 1000+i, buf[i], want)
		}
	}
	if a.Position() != 1100 {
		t.Errorf("position %d", a.Position())
	}
}
