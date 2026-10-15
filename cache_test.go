package main

import (
	"context"
	"mugig/internal/audiotest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A real scan, twice: the second should take every track from the cache.
// runScan is called directly because the test driver runs fyne.Do inline,
// so a background scan would race with the test.
func TestRescanUsesCache(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := t.TempDir()
	audiotest.WriteFile(t, filepath.Join(dir, "a.mp3"), audiotest.ID3v23(map[string]string{"TIT2": "A"}))
	audiotest.WriteFile(t, filepath.Join(dir, "sub", "b.mp3"), audiotest.ID3v23(map[string]string{"TIT2": "B"}))
	audiotest.WriteFile(t, filepath.Join(dir, "notes.txt"), nil)

	for _, want := range []string{"0 unchanged", "2 unchanged"} {
		u.runScan(context.Background(), dir, 2) // the second time, the tracks are updated, not added again
		if got := titles(u.tracks); !slices.Equal(got, []string{"A", "B"}) || !strings.Contains(u.status.Text, want) {
			t.Errorf("after scan: tracks %v, status %q (want %q)", got, u.status.Text, want)
		}
	}
}
