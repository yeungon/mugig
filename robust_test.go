package main

import (
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"testing"
)

// Empty files, as left by interrupted downloads, and copies inside zip
// files' __MACOSX folders, aren't songs.
func TestEmptyAndMacOSXFilesSkipped(t *testing.T) {
	u, fw, _ := newTestUIWithHidden(t)
	dir := fw.root
	os.WriteFile(filepath.Join(dir, "14 Track 14.mp3"), nil, 0o644)
	audiotest.CopyFile(t, "testdata/sample.mp3", filepath.Join(dir, "__MACOSX", "Album", "01.mp3"))
	u.queueScan(dir)
	if got := paths(u.all); len(got) != 3 {
		t.Fatalf("scan listed %q", got)
	}
	empty := filepath.Join(dir, "new.flac")
	os.WriteFile(empty, nil, 0o644)
	fw.changed([]string{empty, filepath.Join(dir, "__MACOSX")})
	if len(u.all) != 3 {
		t.Errorf("watcher listed %q", paths(u.all))
	}
}
