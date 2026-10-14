package main

import (
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestZZRealLengths(t *testing.T) {
	var paths []string
	for _, root := range []string{"/Volumes/OneTouch/1.Music A", "/Volumes/OneTouch/2.Music B", "/Volumes/VUONG_USB/MUSIC"} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && isAudioFile(p) {
				paths = append(paths, p)
			}
			return nil
		})
	}
	t.Logf("%d files", len(paths))
	start := time.Now()
	zero := 0
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil { continue }
		if trackLength(f) == 0 { zero++; t.Logf("no length: %s", p) }
		f.Close()
	}
	t.Logf("all lengths in %v, %d unknown", time.Since(start), zero)
	rand.Shuffle(len(paths), func(i, j int) { paths[i], paths[j] = paths[j], paths[i] })
	bad, n := 0, 0
	for _, p := range paths[:min(150, len(paths))] {
		s, format, err := decodeAs(p, sniff(p))
		if err != nil { continue }
		want := format.SampleRate.D(s.Len())
		s.Close()
		f, _ := os.Open(p)
		got := trackLength(f)
		f.Close()
		n++
		if d := (got - want).Abs(); d > time.Second {
			bad++
			t.Logf("off by %v: %v vs %v %s", d, got, want, p)
		}
	}
	t.Logf("%d of %d off by more than 1 s", bad, n)
}
