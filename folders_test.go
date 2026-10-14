package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func folderPaths(u *ui) []string {
	var out []string
	for _, f := range u.folders {
		out = append(out, f.path)
	}
	return out
}

// twoFolders is a library of two sample folders, three songs each.
func twoFolders(t *testing.T) (u *ui, a, b string) {
	t.Helper()
	u, _, _ = newTestUI(t)
	a, b = sampleFolder(t), sampleFolder(t)
	u.addFolder(a)
	u.addFolder(b)
	if len(u.all) != 6 {
		t.Fatalf("%d tracks", len(u.all))
	}
	return u, a, b
}

// Adding a folder adds to the library rather than replacing it, and the
// sidebar lists each folder with its songs.
func TestFoldersMakeOneLibrary(t *testing.T) {
	u, a, b := twoFolders(t)
	if !slices.Equal(folderPaths(u), []string{a, b}) {
		t.Errorf("folders %q", folderPaths(u))
	}
	for _, f := range u.folders {
		if f.row.count.Text != "3" || f.row.name.Text != filepath.Base(f.path) {
			t.Errorf("row for %s: %q, %q", f.path, f.row.name.Text, f.row.count.Text)
		}
	}
	if len(u.folderList.Objects) != 2 {
		t.Errorf("%d rows", len(u.folderList.Objects))
	}
	if !strings.HasPrefix(u.status.Text, "Scanned 3 tracks") {
		t.Errorf("status %q", u.status.Text)
	}

	u.scanLibrary() // Rescan
	if len(u.all) != 6 || !strings.HasPrefix(u.status.Text, "Scanned 6 tracks in 2 folders") ||
		!strings.Contains(u.status.Text, "6 unchanged") {
		t.Errorf("rescan: %d tracks, status %q", len(u.all), u.status.Text)
	}
}

// Clicking a folder shows only its songs; Songs shows them all again.
func TestFolderNarrowsList(t *testing.T) {
	u, a, b := twoFolders(t)
	copyFile(t, "testdata/sample.mp3", filepath.Join(b, "Album", "extra.mp3"))
	u.scanLibrary()

	test.Tap(u.folders[1].row)
	if u.filter != b || len(u.tracks) != 4 || slices.ContainsFunc(u.tracks, func(t Track) bool { return within(t.Path, a) }) {
		t.Errorf("filter %q, %d shown", u.filter, len(u.tracks))
	}
	if u.pageTitle.Text != filepath.Base(b) || !strings.HasPrefix(u.count.Text, "4 tracks") {
		t.Errorf("title %q, count %q", u.pageTitle.Text, u.count.Text)
	}
	if !u.folders[1].row.chosen || u.folders[0].row.chosen || u.navSongs.Importance != widget.LowImportance {
		t.Error("the chosen folder isn't marked")
	}

	u.search.SetText("sample")
	if !strings.HasPrefix(u.count.Text, "3 of 4 tracks") { // extra.mp3 has no title
		t.Errorf("searching in a folder: %q", u.count.Text)
	}
	u.search.SetText("")

	// Now Playing unmarks the folder; Songs shows everything.
	u.showVisualizer(true)
	if u.folders[1].row.chosen {
		t.Error("folder still marked on Now Playing")
	}
	test.Tap(u.navSongs)
	if u.filter != "" || len(u.tracks) != 7 || u.pageTitle.Text != "Songs" || !u.songsPage.Visible() {
		t.Errorf("filter %q, %d shown, title %q", u.filter, len(u.tracks), u.pageTitle.Text)
	}
	if u.navSongs.Importance != widget.MediumImportance {
		t.Error("Songs isn't marked")
	}
}

// A folder can't be added twice, or inside one already there; adding the
// folder around others takes their place, listing each song once.
func TestAddFolderOverlapping(t *testing.T) {
	u, _, _ := newTestUI(t)
	parent := t.TempDir()
	inner := filepath.Join(parent, "Rock")
	for _, name := range []string{"Rock/a.mp3", "Rock/b.mp3", "Jazz/c.mp3"} {
		copyFile(t, "testdata/sample.mp3", filepath.Join(parent, name))
	}
	u.addFolder(inner)
	test.Tap(u.folders[0].row)
	u.addFolder(parent)
	if !slices.Equal(folderPaths(u), []string{parent}) || len(u.all) != 3 || u.filter != "" {
		t.Fatalf("folders %q, %d tracks, filter %q", folderPaths(u), len(u.all), u.filter)
	}

	for _, again := range []string{inner, parent, parent + "/"} {
		u.addFolder(again)
		top := u.win.Canvas().Overlays().Top()
		if top == nil || !strings.Contains(allText(top), "already in your library") {
			t.Errorf("adding %s again: no explanation", again)
		} else {
			top.Hide()
		}
	}
	if len(u.folders) != 1 || len(u.all) != 3 {
		t.Errorf("folders %q, %d tracks", folderPaths(u), len(u.all))
	}
}

// Removing a folder, after asking, takes its songs off the list and out of
// the cache, and leaves the files alone.
func TestRemoveFolder(t *testing.T) {
	u, a, b := twoFolders(t)
	test.Tap(u.folders[0].row)
	file := filepath.Join(a, "sample.mp3")

	menu := u.folderMenu(u.folders[0])
	var labels []string
	for _, it := range menu.Items {
		labels = append(labels, it.Label)
	}
	if want := "Rescan|" + revealLabel + "||Remove from Library…"; strings.Join(labels, "|") != want {
		t.Errorf("menu %q", strings.Join(labels, "|"))
	}
	menu.Items[3].Action()
	top := u.win.Canvas().Overlays().Top()
	if top == nil || !strings.Contains(allText(top), "Its 3 songs are taken off the list. The files stay where they are.") {
		t.Fatal("no confirmation")
	}
	if len(u.folders) != 2 {
		t.Fatal("removed before confirming")
	}
	test.Tap(findButton(t, top, "Remove"))

	if !slices.Equal(folderPaths(u), []string{b}) || len(u.all) != 3 || u.filter != "" || len(u.tracks) != 3 {
		t.Errorf("folders %q, %d tracks, filter %q", folderPaths(u), len(u.all), u.filter)
	}
	if got := u.prefs.StringList(prefFolders); !slices.Equal(got, []string{b}) {
		t.Errorf("saved folders %q", got)
	}
	if _, ok := u.cache.lookup(file, sizeOnDisk(t, file), fileModTime(t, file)); ok {
		t.Error("removed folder still cached")
	}
	if _, err := os.Stat(file); err != nil {
		t.Error("the file was touched")
	}
}

// The one folder of earlier versions becomes the first in the list.
func TestFoldersFromEarlierVersion(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := sampleFolder(t)
	u.prefs.SetString(prefFolder, dir)
	u.loadFolders()
	if !slices.Equal(folderPaths(u), []string{dir}) || !slices.Equal(u.prefs.StringList(prefFolders), []string{dir}) {
		t.Errorf("folders %q", folderPaths(u))
	}
	if u.prefs.String(prefFolder) != "" {
		t.Error("old setting kept")
	}
	u.loadFolders() // and only once
	if len(u.folders) != 1 {
		t.Errorf("folders %q", folderPaths(u))
	}
}

// A folder that isn't there at startup is listed as such, and scanned as
// soon as it is back.
func TestFolderMissingAtStartup(t *testing.T) {
	u, _, _ := newTestUI(t)
	there := sampleFolder(t)
	away := filepath.Join(t.TempDir(), "USB Music")
	u.prefs.SetStringList(prefFolders, []string{there, away})
	u.loadFolders()
	u.rebuildFolderList()
	u.scanLibrary()

	f := u.folders[1]
	if f.connected || f.missing != "Not found" || f.row.detail.Text != "Not found" || f.row.count.Text != "" {
		t.Errorf("missing folder: %+v, row %q %q", f, f.row.detail.Text, f.row.count.Text)
	}
	if len(u.all) != 3 {
		t.Errorf("%d tracks", len(u.all))
	}
	test.Tap(f.row)
	if !strings.Contains(u.count.Text, "Its songs will be back when it is connected again") {
		t.Errorf("count %q", u.count.Text)
	}

	// The disk is plugged in.
	os.Rename(sampleFolder(t), away)
	u.checkConnections()
	if !f.connected || len(u.all) != 6 || len(u.tracks) != 3 || f.row.count.Text != "3" {
		t.Errorf("after connecting: connected %v, %d tracks, %d shown", f.connected, len(u.all), len(u.tracks))
	}

	// And unplugged again.
	os.Rename(away, away+"-gone")
	u.checkConnections()
	if f.connected || len(u.all) != 3 || len(u.tracks) != 0 {
		t.Errorf("after unplugging: connected %v, %d tracks, %d shown", f.connected, len(u.all), len(u.tracks))
	}
}

// A folder removed while it is being scanned doesn't come back as the
// scan winds down, and other folders' scans go on.
func TestRemoveFolderDuringScan(t *testing.T) {
	u, _, _ := newTestUI(t)
	var later []func()
	u.async = func(f func()) { later = append(later, f) }
	step := func() {
		f := later[0]
		later = later[1:]
		f()
	}
	a, b, c := sampleFolder(t), sampleFolder(t), sampleFolder(t)
	u.addFolder(a)
	u.addFolder(b)
	u.addFolder(c)
	if u.scanning != a || !slices.Equal(u.scanQueue, []string{b, c}) {
		t.Fatalf("scanning %q, queue %q", u.scanning, u.scanQueue)
	}
	step() // a's scan; then b's starts, and waits
	if u.scanning != b || len(u.all) != 3 {
		t.Fatalf("scanning %q, %d tracks", u.scanning, len(u.all))
	}
	u.removeFolder(b)
	u.flush(b, []Track{{Path: filepath.Join(b, "sample.mp3"), Title: "late"}})
	for len(later) > 0 {
		step() // b's scan, cancelled; then c's
	}
	if u.scanning != "" || len(u.all) != 6 || slices.ContainsFunc(u.all, func(t Track) bool { return within(t.Path, b) }) {
		t.Errorf("scanning %q, %d tracks", u.scanning, len(u.all))
	}
	if !strings.HasPrefix(u.status.Text, "Scanned") {
		t.Errorf("status %q", u.status.Text)
	}
}

// Cancel Scan stops the scans waiting too.
func TestCancelScanStopsQueue(t *testing.T) {
	u, _, _ := newTestUI(t)
	var later []func()
	u.async = func(f func()) { later = append(later, f) }
	a, b := sampleFolder(t), sampleFolder(t)
	u.addFolder(a)
	u.addFolder(b)
	test.Tap(u.cancelBtn)
	later[0]()
	if u.scanning != "" || len(u.scanQueue) != 0 || u.cancel != nil || !strings.HasPrefix(u.status.Text, "Scan cancelled") {
		t.Errorf("scanning %q, queue %q, status %q", u.scanning, u.scanQueue, u.status.Text)
	}
	if u.rescanBtn.Disabled() || u.cancelBtn.Visible() {
		t.Error("scan controls not reset")
	}
}

// With no folders, the sidebar offers to add one.
func TestNoFoldersYet(t *testing.T) {
	u, _, _ := newTestUI(t)
	if len(u.folderList.Objects) != 1 {
		t.Fatalf("%d objects in the folder list", len(u.folderList.Objects))
	}
	add, ok := u.folderList.Objects[0].(*widget.Button)
	if !ok || add.Text != "Add Music Folder…" {
		t.Fatalf("folder list holds %T", u.folderList.Objects[0])
	}
	if !strings.Contains(u.count.Text, "Add a folder") || !strings.Contains(u.status.Text, "Add a folder") {
		t.Errorf("count %q, status %q", u.count.Text, u.status.Text)
	}
	dir := sampleFolder(t)
	fp := &fakePicker{paths: []string{dir}}
	u.pick = fp.pick
	test.Tap(add)
	if len(u.folders) != 1 || len(u.folderList.Objects) != 1 || len(u.all) != 3 {
		t.Errorf("%d folders, %d tracks", len(u.folders), len(u.all))
	}
	if _, isRow := u.folderList.Objects[0].(*folderRow); !isRow {
		t.Error("the add button stayed")
	}
}

func TestMissingText(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "Music", "Jazz")
	if got := missingText(gone); got != "Not found" {
		t.Errorf("deleted folder: %q", got)
	}
	if runtime.GOOS == "darwin" {
		if got := missingText("/Volumes/No Such Disk 7f3a/Music"); got != "Not connected" {
			t.Errorf("unplugged disk: %q", got)
		}
		if got := missingText("/Volumes/No Such Disk 7f3a"); got != "Not connected" {
			t.Errorf("unplugged disk itself: %q", got)
		}
	}
}

// findButton finds the button labelled text in o.
func findButton(t *testing.T, o fyne.CanvasObject, text string) *widget.Button {
	t.Helper()
	var found *widget.Button
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.Button:
			if v.Text == text {
				found = v
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(v).Objects() {
				walk(c)
			}
		case *fyne.Container:
			for _, c := range v.Objects {
				walk(c)
			}
		}
	}
	walk(o)
	if found == nil {
		t.Fatalf("no %q button", text)
	}
	return found
}

// A count that grows during a scan gets the room it needs.
func TestFolderRowMakesRoomForCount(t *testing.T) {
	u, _, _ := newTestUI(t)
	u.addFolder(sampleFolder(t))
	u.win.Resize(fyne.NewSize(1200, 740))
	r := u.folders[0].row
	for i := range 2728 {
		u.all = append(u.all, Track{Path: filepath.Join(u.folders[0].path, fmt.Sprint(i, ".mp3"))})
	}
	u.refreshFolders()
	if r.count.Text != "2,731" {
		t.Fatalf("count %q", r.count.Text)
	}
	if got, want := r.count.Size().Width, r.count.MinSize().Width; got < want {
		t.Errorf("count is %.0f wide, needs %.0f", got, want)
	}
	if right := r.count.Position().X + r.count.Size().Width + 2*theme.Padding(); right > r.Size().Width+0.5 {
		t.Errorf("count ends at %.0f, past the row's %.0f", right, r.Size().Width)
	}
}
