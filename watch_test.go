package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeWatcher lets a test report folder changes by hand.
type fakeWatcher struct {
	root    string
	changed func([]string)
	stopped bool
}

func (f *fakeWatcher) watch(root string, changed func([]string)) (func(), error) {
	f.root, f.changed = root, changed
	return func() { f.stopped = true }, nil
}

// watchedLibrary scans a fresh music folder with a fake watcher, applying
// changes at once.
func watchedLibrary(t *testing.T) (*ui, *fakeWatcher, string) {
	t.Helper()
	u, _, _ := newTestUI(t)
	fw := &fakeWatcher{}
	u.watchFolder, u.settle = fw.watch, 0
	dir := sampleFolder(t)
	u.addFolder(dir)
	if fw.root != dir || len(u.all) != 3 {
		t.Fatalf("watching %q, %d tracks", fw.root, len(u.all))
	}
	return u, fw, dir
}

func paths(ts []Track) []string {
	var out []string
	for _, t := range ts {
		out = append(out, filepath.Base(t.Path))
	}
	slices.Sort(out)
	return out
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(to), 0o755)
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWatchAddsAndRemovesFiles(t *testing.T) {
	u, fw, dir := watchedLibrary(t)
	setLibraryView := func() []string { return paths(u.tracks) }

	added := filepath.Join(dir, "new.mp3")
	copyFile(t, "testdata/sample.mp3", added)
	fw.changed([]string{added})
	if got := setLibraryView(); !slices.Contains(got, "new.mp3") || len(got) != 4 {
		t.Errorf("after adding: %q", got)
	}
	if !strings.Contains(u.status.Text, "1 track added") {
		t.Errorf("status %q", u.status.Text)
	}
	if _, ok := u.cache.lookup(added, sizeOnDisk(t, added), fileModTime(t, added)); !ok {
		t.Error("added file not cached")
	}

	os.Remove(added)
	fw.changed([]string{added})
	if got := setLibraryView(); slices.Contains(got, "new.mp3") || len(got) != 3 {
		t.Errorf("after removing: %q", got)
	}
	if !strings.Contains(u.status.Text, "1 track removed") {
		t.Errorf("status %q", u.status.Text)
	}

	// Files that aren't audio are ignored.
	notes := filepath.Join(dir, "notes.txt")
	os.WriteFile(notes, []byte("hi"), 0o644)
	fw.changed([]string{notes})
	if len(u.all) != 3 {
		t.Errorf("%d tracks after adding a text file", len(u.all))
	}
}

// A copied album folder arrives as a folder; a deleted or renamed one
// takes its songs with it.
func TestWatchFolders(t *testing.T) {
	u, fw, dir := watchedLibrary(t)
	album := filepath.Join(dir, "Album")
	copyFile(t, "testdata/sample.flac", filepath.Join(album, "01.flac"))
	copyFile(t, "testdata/sample.wav", filepath.Join(album, "CD2", "02.wav"))
	fw.changed([]string{album, filepath.Join(album, "01.flac")}) // overlapping reports
	if len(u.all) != 5 {
		t.Fatalf("after adding an album: %q", paths(u.all))
	}

	renamed := filepath.Join(dir, "Album (2020)")
	os.Rename(album, renamed)
	fw.changed([]string{album, renamed})
	if len(u.all) != 5 || !slices.ContainsFunc(u.all, func(t Track) bool { return within(t.Path, renamed) }) ||
		slices.ContainsFunc(u.all, func(t Track) bool { return within(t.Path, album) }) {
		t.Errorf("after renaming: %v", u.all)
	}

	// Deleting a file inside a folder, reported only as the folder.
	os.Remove(filepath.Join(renamed, "01.flac"))
	fw.changed([]string{renamed})
	if len(u.all) != 4 {
		t.Errorf("after deleting inside a folder: %q", paths(u.all))
	}

	os.RemoveAll(renamed)
	fw.changed([]string{renamed})
	if len(u.all) != 3 {
		t.Errorf("after deleting the album: %q", paths(u.all))
	}
}

// When the disk is disconnected, its songs are hidden but not forgotten:
// they are back, from the cache, when it is connected again.
func TestWatchHidesSongsWhenDiskGoes(t *testing.T) {
	u, fw, dir := watchedLibrary(t)
	gone := dir + "-away"
	os.Rename(dir, gone)
	fw.changed([]string{dir})
	if len(u.all) != 0 || !strings.Contains(u.status.Text, "disconnected") || u.folders[0].connected {
		t.Errorf("%d tracks, status %q", len(u.all), u.status.Text)
	}
	if fw.stopped != true {
		t.Error("still watching a disconnected folder")
	}

	os.Rename(gone, dir)
	u.setConnected(dir, true) // as watchConnections does
	if len(u.all) != 3 || !strings.Contains(u.status.Text, "3 unchanged") {
		t.Errorf("after reconnecting: %d tracks, status %q", len(u.all), u.status.Text)
	}
}

// Changes that come during a scan wait for it to finish, then apply.
func TestWatchWaitsForScan(t *testing.T) {
	u, fw, dir := watchedLibrary(t)
	added := filepath.Join(dir, "new.mp3")
	copyFile(t, "testdata/sample.mp3", added)
	u.cancel = func() {} // as if a scan were running
	fw.changed([]string{added})
	if len(u.all) != 3 {
		t.Fatal("applied during a scan")
	}
	u.finish(dir, scanTally{cancelled: true})
	if len(u.all) != 4 {
		t.Errorf("%d tracks after the scan finished", len(u.all))
	}
}

// Each folder has its own watch; a removed folder's is stopped, and late
// news from it ignored.
func TestWatchEachFolder(t *testing.T) {
	u, first, dir := watchedLibrary(t)
	other := sampleFolder(t)
	second := &fakeWatcher{}
	u.watchFolder = second.watch
	u.addFolder(other)
	if first.stopped || second.root != other || len(u.all) != 6 {
		t.Fatalf("first watch stopped %v, second on %q, %d tracks", first.stopped, second.root, len(u.all))
	}

	added := filepath.Join(other, "new.mp3")
	copyFile(t, "testdata/sample.mp3", added)
	second.changed([]string{added})
	if len(u.all) != 7 {
		t.Errorf("%d tracks after adding to the second folder", len(u.all))
	}

	u.removeFolder(dir)
	if !first.stopped || len(u.all) != 4 {
		t.Errorf("first watch stopped %v, %d tracks", first.stopped, len(u.all))
	}
	copyFile(t, "testdata/sample.mp3", filepath.Join(dir, "late.mp3"))
	first.changed([]string{filepath.Join(dir, "late.mp3")})
	if len(u.all) != 4 {
		t.Errorf("%d tracks after news from a removed folder", len(u.all))
	}
}

func sizeOnDisk(t *testing.T, p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

func fileModTime(t *testing.T, p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime().UnixNano()
}

// macOS's "._" files on exFAT disks, and deleted songs in .Trashes, are
// not music: neither the scan nor the watcher lists them.
func TestHiddenFilesIgnored(t *testing.T) {
	u, fw, _ := newTestUIWithHidden(t)
	if got := paths(u.all); len(got) != 3 || slices.ContainsFunc(got, func(p string) bool { return strings.HasPrefix(p, ".") }) {
		t.Fatalf("scan listed %q", got)
	}
	dir := fw.root
	junk := filepath.Join(dir, "._new.flac")
	os.WriteFile(junk, []byte("Mac metadata"), 0o644)
	trashed := filepath.Join(dir, ".Trashes", "501", "old.mp3")
	copyFile(t, "testdata/sample.mp3", trashed)
	fw.changed([]string{junk, trashed, filepath.Join(dir, ".Trashes")})
	if len(u.all) != 3 {
		t.Errorf("watcher listed %q", paths(u.all))
	}
}

// newTestUIWithHidden scans a folder that also holds hidden junk.
func newTestUIWithHidden(t *testing.T) (*ui, *fakeWatcher, string) {
	t.Helper()
	u, _, _ := newTestUI(t)
	fw := &fakeWatcher{}
	u.watchFolder, u.settle = fw.watch, 0
	dir := sampleFolder(t)
	os.WriteFile(filepath.Join(dir, "._sample.mp3"), []byte("Mac metadata"), 0o644)
	copyFile(t, "testdata/sample.flac", filepath.Join(dir, ".Trashes", "501", "deleted.flac"))
	u.addFolder(dir)
	return u, fw, dir
}

func TestHiddenWithin(t *testing.T) {
	for p, want := range map[string]bool{
		"/m/a.mp3":              false,
		"/m/Album/._a.mp3":      true,
		"/m/.Trashes/501/a.mp3": true,
		"/m/.Trashes":           true,
		"/m":                    false,
	} {
		if got := hiddenWithin(p, "/m"); got != want {
			t.Errorf("%s: %v", p, got)
		}
	}
}
