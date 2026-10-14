package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

// fakePicker answers Open panels with paths, recording what was asked.
type fakePicker struct {
	asked []pickOptions
	paths []string
}

func (f *fakePicker) pick(_ fyne.Window, opts pickOptions, done func([]string)) {
	f.asked = append(f.asked, opts)
	done(f.paths)
}

// musicFolder copies the sample files into a fresh folder.
func musicFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"sample.mp3", "sample.flac", "sample.wav"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, name), data, 0o644)
	}
	return dir
}

func TestChooseFolderScansIt(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := musicFolder(t)
	fp := &fakePicker{paths: []string{dir}}
	u.pick = fp.pick

	u.chooseFolder()
	if len(fp.asked) != 1 || !fp.asked[0].folders || fp.asked[0].files {
		t.Fatalf("asked %+v", fp.asked)
	}
	if len(u.all) != 3 || u.lastFolder() != dir || u.folderName.Text != filepath.Base(dir) {
		t.Errorf("scanned %d tracks, folder %q", len(u.all), u.lastFolder())
	}

	// Cancelling the panel changes nothing.
	fp.paths = nil
	u.chooseFolder()
	if len(u.all) != 3 {
		t.Error("cancelling the panel changed the library")
	}
}

// Open… takes files, which are added and played, or a folder, which is
// scanned.
func TestOpenFilesOrFolder(t *testing.T) {
	u, player, _ := newTestUI(t)
	dir := musicFolder(t)
	setLibrary(u, fiveTracks()...)
	flac, wav := filepath.Join(dir, "sample.flac"), filepath.Join(dir, "sample.wav")
	notes := filepath.Join(t.TempDir(), "notes.txt") // not audio: ignored
	os.WriteFile(notes, []byte("hi"), 0o644)
	fp := &fakePicker{paths: []string{flac, wav, notes}}
	u.pick = fp.pick

	u.openItems()
	if o := fp.asked[0]; !o.files || !o.folders || !o.multiple {
		t.Errorf("asked %+v", o)
	}
	if len(u.all) != 7 || player.played[len(player.played)-1] != flac || u.playing.Path != flac {
		t.Errorf("library %d tracks, playing %q", len(u.all), u.playing.Path)
	}

	// Opening them again doesn't add them twice.
	u.openItems()
	if len(u.all) != 7 {
		t.Errorf("library %d tracks after opening again", len(u.all))
	}

	fp.paths = []string{dir}
	u.openItems()
	if len(u.all) != 3 || u.lastFolder() != dir {
		t.Errorf("opening a folder: %d tracks, folder %q", len(u.all), u.lastFolder())
	}
}

func TestDroppingFilesAndFolders(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := musicFolder(t)
	u.dropped(fyne.Position{}, []fyne.URI{storage.NewFileURI(filepath.Join(dir, "sample.mp3"))})
	if u.playing.Path != filepath.Join(dir, "sample.mp3") {
		t.Errorf("dropped file not played: %q", u.playing.Path)
	}
	u.dropped(fyne.Position{}, []fyne.URI{storage.NewFileURI(dir)})
	if len(u.all) != 3 || u.lastFolder() != dir {
		t.Errorf("dropped folder: %d tracks", len(u.all))
	}
}

// A folder that can't be read, such as a disk macOS hasn't given access
// to, is explained rather than scanned as empty.
func TestUnreadableFolderExplained(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := musicFolder(t)
	setLibrary(u, fiveTracks()...)
	os.Chmod(dir, 0)
	defer os.Chmod(dir, 0o755)

	u.openFolder(dir, false)
	if len(u.all) != 5 || !strings.Contains(u.status.Text, "Privacy & Security") {
		t.Errorf("library %d tracks, status %q", len(u.all), u.status.Text)
	}

	gone := filepath.Join(t.TempDir(), "unplugged")
	u.openFolder(gone, false)
	if !strings.Contains(u.status.Text, "connect the disk") {
		t.Errorf("status %q", u.status.Text)
	}
}
