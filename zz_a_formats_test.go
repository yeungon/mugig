package main

import (
	"path/filepath"
	"testing"
)

func TestProbeSamples(t *testing.T) {
	for _, c := range []struct {
		file, codec string
		lossless    bool
	}{
		{"sample.flac", "FLAC", true},
		{"sample.wav", "WAV", true},
		{"sample.mp3", "MP3", false},
		{"sample.ogg", "Ogg Vorbis", false},
	} {
		info, err := Probe(filepath.Join("testdata", c.file))
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if info.Codec != c.codec || info.Lossless != c.lossless || info.SampleRate == 0 || info.Channels == 0 || info.Mismatch != "" {
			t.Errorf("%s: got %+v", c.file, info)
		}
		if c.lossless && info.Bits == 0 {
			t.Errorf("%s: no bit depth", c.file)
		}
	}
}
