package quality

import (
	"context"
	"testing"

	"mugig/internal/audio"
	"mugig/internal/audiotest"
)

// Apple Lossless is checked as WAV is, having no checksum: read in full,
// its loudness measured. AAC is lossy, and still gets its loudness and a
// spectrogram.
func TestCheckM4A(t *testing.T) {
	for _, c := range []struct {
		file  string
		level Level
		check string
	}{
		{"sample-alac.m4a", Lossless, "Complete and readable (Apple Lossless files have no checksum)"},
		{"sample-aac.m4a", Lossy, "Lossy: some of the sound was removed to make the file smaller"},
	} {
		path := audiotest.Sample(c.file)
		info, err := audio.Probe(path)
		if err != nil {
			t.Fatal(err)
		}
		if !CanAnalyse(info) {
			t.Errorf("%s can't be analysed", c.file)
			continue
		}
		rep, img, err := Analyse(context.Background(), path, info, true, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		a := Assess(info, &rep)
		if a.Level != c.level || !hasCheck(a, CheckOK, c.check) && !hasCheck(a, CheckInfo, c.check) {
			t.Errorf("%s: %v %+v", c.file, a.Level, a.Checks)
		}
		if rep.ReadBy != "" || rep.Damaged != "" || rep.Missing != 0 {
			t.Errorf("%s: read by %q, damaged %q, missing %v", c.file, rep.ReadBy, rep.Damaged, rep.Missing)
		}
		if rep.Levels.TruePeak == 0 || img == nil {
			t.Errorf("%s: peak %v, picture %v", c.file, rep.Levels.TruePeak, img != nil)
		}
	}
}
