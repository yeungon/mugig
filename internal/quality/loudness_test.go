package quality

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"mugig/internal/audio"
	"mugig/internal/audiotest"
)

// Songs are brought to -14 LUFS: loud ones down, quiet ones up, but never
// up past -1 dBTP; a song too close to its peak to rise isn't cut for it.
func TestLoudnessGain(t *testing.T) {
	for _, c := range []struct {
		l    Loudness
		want float64
	}{
		{Loudness{LUFS: -9, TruePeak: 0.5}, -5},                   // a loud master: down 5 dB
		{Loudness{LUFS: -25, TruePeak: -3}, 2},                    // quiet, but its peaks allow only +2
		{Loudness{LUFS: -25, TruePeak: -15}, 11},                  // quiet with room: up the full 11
		{Loudness{LUFS: -15, TruePeak: -0.2}, 0},                  // no room to rise, no reason to fall
		{Loudness{LUFS: -14, TruePeak: -5}, 0},                    // just right
		{Loudness{LUFS: math.Inf(-1), TruePeak: math.Inf(-1)}, 0}, // silence
	} {
		if got := c.l.Gain(); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%+v: gain %v, want %v", c.l, got, c.want)
		}
	}
}

// Measuring gives the loudness a full quality check gives, for a whole
// file; a song from a CUE sheet is measured on its own.
func TestMeasureLoudness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noise.flac")
	ch := audiotest.Quantize(audiotest.Noise(5*44100, 44100, audiotest.Pink), 16)
	audiotest.WriteFLAC(t, path, 44100, 16, ch, ch)
	l, err := MeasureLoudness(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := audio.Probe(path)
	rep, _, err := Analyse(context.Background(), path, info, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(l.LUFS-rep.Levels.LUFS) > 0.01 || math.Abs(l.TruePeak-rep.Levels.TruePeak) > 0.01 {
		t.Errorf("measured %+v, the check found %v LUFS, %v dBTP", l, rep.Levels.LUFS, rep.Levels.TruePeak)
	}

	dir, _ := audiotest.CueImage(t, "Album", nil)
	image := filepath.Join(dir, "Album.wav")
	audio.RememberSpan(image+"#02", audio.Span{File: image, Start: 0, End: 0}) // the whole image, as a song
	defer audio.ForgetSpans()
	if _, err := MeasureLoudness(context.Background(), image+"#02"); err != nil {
		t.Errorf("a song from a sheet: %v", err)
	}
	if _, err := MeasureLoudness(context.Background(), filepath.Join(dir, "missing.flac")); err == nil {
		t.Error("measuring a missing file should fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureLoudness(ctx, path); err == nil {
		t.Error("a cancelled measurement should stop")
	}
}

// Loudness measured for levelling is saved with the file's stamp, and
// forgotten when the file changes; a quality check's measurement serves
// for a whole file, not for a song in a disc image.
func TestStoreLoudness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quality.gob")
	s := LoadStore(path)
	s.PutLoudness("/m/a.flac", "stamp1", Loudness{LUFS: -10, TruePeak: -0.5})
	s.PutLoudness("/m/Disc.wav#02", "stamp2", Loudness{LUFS: -20, TruePeak: -6})

	s = LoadStore(path) // as at the next start
	if l, ok := s.Loudness("/m/a.flac", "/m/a.flac", "stamp1"); !ok || l.LUFS != -10 {
		t.Errorf("saved loudness: %+v %v", l, ok)
	}
	if _, ok := s.Loudness("/m/a.flac", "/m/a.flac", "changed"); ok {
		t.Error("a changed file's loudness should be unknown")
	}
	s.Put("/m/Disc.wav", "stamp2", Report{Levels: levelsResult{Measured: true, LUFS: -14, TruePeak: -1}}, Lossless)
	s.Put("/m/b.flac", "stamp3", Report{Levels: levelsResult{Measured: true, LUFS: -12, TruePeak: -2}}, Lossless)
	if l, ok := s.Loudness("/m/b.flac", "/m/b.flac", "stamp3"); !ok || l.LUFS != -12 || l.TruePeak != -2 {
		t.Errorf("from a quality check: %+v %v", l, ok)
	}
	if l, ok := s.Loudness("/m/Disc.wav#02", "/m/Disc.wav", "stamp2"); !ok || l.LUFS != -20 {
		t.Errorf("a song in a disc image should have its own loudness, not the album's: %+v %v", l, ok)
	}
	if _, ok := s.Loudness("/m/Disc.wav#03", "/m/Disc.wav", "stamp2"); ok {
		t.Error("an unmeasured song in a disc image shouldn't take the album's loudness")
	}
	s.Forget([]string{"/m/Disc.wav"})
	if _, ok := s.Loudness("/m/Disc.wav#02", "/m/Disc.wav", "stamp2"); ok {
		t.Error("a forgotten image's songs should lose their loudness")
	}
}
