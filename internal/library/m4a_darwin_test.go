package library

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"mugig/internal/audiotest"
)

// On macOS M4A files are found, and listed by their codec.
func TestScanM4A(t *testing.T) {
	dir := t.TempDir()
	audiotest.CopyFile(t, audiotest.Sample("sample-alac.m4a"), filepath.Join(dir, "Lossless.m4a"))
	audiotest.CopyFile(t, audiotest.Sample("sample-aac.m4a"), filepath.Join(dir, "Album", "Lossy.M4A"))
	paths := make(chan string, 10)
	Walk(context.Background(), dir, paths)
	var found []string
	for p := range paths {
		found = append(found, filepath.Base(p))
	}
	slices.Sort(found)
	if !slices.Equal(found, []string{"Lossless.m4a", "Lossy.M4A"}) {
		t.Errorf("found %v", found)
	}
	for name, format := range map[string]string{"Lossless.m4a": "ALAC", "Album/Lossy.M4A": "AAC"} {
		tr := ReadTrack(filepath.Join(dir, name))
		if tr.Format != format || tr.Length != 500*time.Millisecond || tr.Title == "" {
			t.Errorf("%s: %+v", name, tr)
		}
	}
}
