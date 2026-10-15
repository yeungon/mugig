package ui

import (
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

// sampleFolder copies the sample files into a fresh folder.
func sampleFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"sample.mp3", "sample.flac", "sample.wav"} {
		data, err := os.ReadFile(audiotest.Sample(name))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, name), data, 0o644)
	}
	return dir
}

func TestAddFolderScansIt(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := sampleFolder(t)
	fp := &fakePicker{paths: []string{dir}}
	u.pick = fp.pick

	u.chooseFolderToAdd()
	if len(fp.asked) != 1 || !fp.asked[0].folders || fp.asked[0].files {
		t.Fatalf("asked %+v", fp.asked)
	}
	if len(u.all) != 3 || !slices.Equal(folderPaths(u), []string{dir}) {
		t.Errorf("scanned %d tracks, folders %q", len(u.all), folderPaths(u))
	}
	if got := u.prefs.StringList(prefFolders); !slices.Equal(got, []string{dir}) {
		t.Errorf("saved folders %q", got)
	}

	// The next panel starts beside it, where more music likely is.
	fp.paths = nil
	u.chooseFolderToAdd()
	if fp.asked[1].start != filepath.Dir(dir) {
		t.Errorf("second panel starts in %q", fp.asked[1].start)
	}
	// Cancelling the panel changes nothing.
	if len(u.all) != 3 || len(u.folders) != 1 {
		t.Error("cancelling the panel changed the library")
	}
}

// Open… takes files, which are added and played, or a folder, which is
// scanned.
func TestOpenFilesOrFolder(t *testing.T) {
	u, player, _ := newTestUI(t)
	dir := sampleFolder(t)
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

	// A folder is added to what is there, its songs listed once.
	fp.paths = []string{dir}
	u.openItems()
	if len(u.all) != 8 || !slices.Equal(folderPaths(u), []string{dir}) {
		t.Errorf("opening a folder: %d tracks, folders %q", len(u.all), folderPaths(u))
	}
}

func TestDroppingFilesAndFolders(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := sampleFolder(t)
	u.dropped(fyne.Position{}, []fyne.URI{storage.NewFileURI(filepath.Join(dir, "sample.mp3"))})
	if u.playing.Path != filepath.Join(dir, "sample.mp3") {
		t.Errorf("dropped file not played: %q", u.playing.Path)
	}
	other := sampleFolder(t)
	u.dropped(fyne.Position{}, []fyne.URI{storage.NewFileURI(dir), storage.NewFileURI(other)})
	if len(u.all) != 6 || !slices.Equal(folderPaths(u), []string{dir, other}) {
		t.Errorf("dropped folders: %d tracks, folders %q", len(u.all), folderPaths(u))
	}
}

// A folder that can't be read, such as a disk macOS hasn't given access
// to, is explained rather than scanned as empty.
func TestUnreadableFolderExplained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores the permissions this test takes away")
	}
	u, _, _ := newTestUI(t)
	dir := sampleFolder(t)
	setLibrary(u, fiveTracks()...)
	os.Chmod(dir, 0)
	defer os.Chmod(dir, 0o755)

	u.addFolder(dir)
	explanation := "isn't allowed to read it"
	if runtime.GOOS == "darwin" {
		explanation = "Privacy & Security"
	}
	if top := u.win.Canvas().Overlays().Top(); top == nil || !strings.Contains(allText(top), explanation) {
		t.Error("no explanation shown")
	}
	if len(u.all) != 5 || len(u.folders) != 0 {
		t.Errorf("library %d tracks, %d folders", len(u.all), len(u.folders))
	}
	u.win.Canvas().Overlays().Top().Hide()

	gone := filepath.Join(t.TempDir(), "unplugged")
	u.addFolder(gone)
	if top := u.win.Canvas().Overlays().Top(); top == nil || !strings.Contains(allText(top), "connect the disk") {
		t.Error("no explanation shown for a missing folder")
	}
}
