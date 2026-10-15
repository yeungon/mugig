//go:build !windows

package quality

import (
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"mugig/internal/audio"
	"mugig/internal/audiotest"
)

// openFiles counts the files this process has open.
func openFiles(t *testing.T) int {
	t.Helper()
	d, err := os.Open("/dev/fd")
	if err != nil {
		t.Skip("can't count open files here:", err)
	}
	defer d.Close()
	names, err := d.Readdirnames(-1) // names only: looking at each would fail on this listing's own
	if err != nil {
		t.Skip("can't count open files here:", err)
	}
	return len(names)
}

// Checking files leaves none of them open. (Windows can't delete an open
// file, so its tests catch this too, through their temporary folders.)
// The garbage collector is held off: it closes forgotten files, which
// would hide a leak.
func TestCheckClosesFiles(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"a.flac", "b.wav", "c.flac"} {
		path := filepath.Join(dir, name)
		ch := audiotest.Quantize(audiotest.Noise(44100, 44100, audiotest.Pink), 16)
		if filepath.Ext(name) == ".wav" {
			audiotest.WriteWAV(t, path, 44100, 16, 2, ch, ch)
		} else {
			audiotest.WriteFLAC(t, path, 44100, 16, ch, ch)
		}
		paths = append(paths, path)
	}
	paths = append(paths, audiotest.Sample("sample.mp3"), audiotest.Sample("sample.ogg"))
	before := openFiles(t)
	for _, path := range paths {
		info, err := audio.Probe(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := Analyse(context.Background(), path, info, true, nil); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if _, err := MeasureLoudness(context.Background(), path); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if after := openFiles(t); after != before {
		t.Errorf("%d files open before checking, %d after: checking leaves files open", before, after)
	}
}
