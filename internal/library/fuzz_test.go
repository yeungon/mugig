package library

import (
	"mugig/internal/audiotest"
	"testing"
)

// FuzzReadTrack covers tag reading, which the scan does for every file.
func FuzzReadTrack(f *testing.F) {
	audiotest.FuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := audiotest.FuzzFile(t, data, ext)
		audiotest.Timed(t, "readTrack", func() {
			if tr := ReadTrack(path); tr.Title == "" {
				t.Error("a track needs a title, if only its file name")
			}
		})
	})
}
