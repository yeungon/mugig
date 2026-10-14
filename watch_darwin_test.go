//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The real FSEvents watcher reports files added to, and removed from,
// anywhere in the tree, with paths under the root as given (t.TempDir is
// under /var, which FSEvents reports as /private/var).
func TestFSEventsReportsChanges(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Artist", "Album"), 0o755)
	got := make(chan []string, 100)
	stop, err := watchFolder(root, func(p []string) { got <- p })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	time.Sleep(300 * time.Millisecond) // let the stream start

	await := func(want string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case paths := <-got:
				if slices.Contains(paths, want) {
					return
				}
			case <-deadline:
				t.Fatalf("no event for %s", want)
			}
		}
	}
	song := filepath.Join(root, "Artist", "Album", "01.flac")
	os.WriteFile(song, []byte("x"), 0o644)
	await(song)
	os.Remove(song)
	await(song)
}
