package main

import (
	"mugig/internal/audiotest"
	"path/filepath"
	"testing"
)

func TestDecodeEveryFormat(t *testing.T) {
	for _, name := range []string{"sample.mp3", "sample.flac", "sample.wav", "sample.ogg"} {
		t.Run(name, func(t *testing.T) {
			s, format, err := Decode(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if format.SampleRate <= 0 || s.Len() <= 0 {
				t.Fatalf("rate %d, length %d", format.SampleRate, s.Len())
			}
			buf := make([][2]float64, 512)
			if n, ok := s.Stream(buf); n == 0 || !ok {
				t.Errorf("streamed %d samples, ok=%v", n, ok)
			}
			if err := s.Seek(s.Len() / 2); err != nil {
				t.Errorf("seek: %v", err)
			}
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "broken.mp3")
	audiotest.WriteFile(t, bad, []byte("garbage"))
	for _, path := range []string{bad, filepath.Join(dir, "missing.flac"), "testdata/README.md"} {
		if _, _, err := Decode(path); err == nil {
			t.Errorf("decode(%s) should fail", filepath.Base(path))
		}
	}
}
