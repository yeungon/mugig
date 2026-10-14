package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestFileSizeWording(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 bytes", 999: "999 bytes", 1000: "1 KB", 912_400: "912 KB", 999_600: "1.00 MB",
		38_213_456: "38.2 MB", 9_217: "9 KB", 1_240_000_000: "1.24 GB", 412_000_000_000: "412 GB",
	} {
		if got := fileSize(n); got != want {
			t.Errorf("fileSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSizeColumn(t *testing.T) {
	u, _, _ := newTestUI(t)
	a, b, c := track("A", "X", "", 0), track("B", "X", "", 0), track("C", "X", "", 0)
	a.Size, b.Size, c.Size = 5_000_000, 40_000_000, 912_000
	setLibrary(u, a, b, c)
	if columns[sizeCol] != "Size" || cellText(b, sizeCol) != "40.0 MB" {
		t.Errorf("column %q, cell %q", columns[sizeCol], cellText(b, sizeCol))
	}
	u.sortBy(sizeCol)
	if got := strings.Join(titles(u.tracks), ""); got != "CAB" {
		t.Errorf("by size: %s", got)
	}
	if !strings.HasSuffix(u.count.Text, " · 45.9 MB") {
		t.Errorf("count %q", u.count.Text)
	}
}

// Sizes come from the scan, for cached tracks too.
func TestScanRecordsSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(path, make([]byte, 12345), 0o644)
	cache := loadCache("")
	first := scanFile(cache, path)
	cache.update(filepath.Dir(path), map[string]cacheEntry{path: {Size: first.size, ModTime: first.modTime, Track: Track{Path: path, Title: "a"}}}, true)
	again := scanFile(cache, path)
	if first.track.Size != 12345 || !again.cached || again.track.Size != 12345 {
		t.Errorf("sizes %d, %d (cached %v)", first.track.Size, again.track.Size, again.cached)
	}
}

// The sort column is saved by name, and one saved by index before Size was
// added still means the same column.
func TestSortColumnSavedByName(t *testing.T) {
	u, _, a := newTestUI(t)
	u.sortBy(qualityCol)
	if got := a.Preferences().String(prefSortCol); got != "Quality" {
		t.Errorf("saved %q", got)
	}
	old := test.NewTempApp(t).Preferences()
	old.SetInt("sortColumn", 5) // Quality, in the old order
	if got := savedSortColumn(old); got != qualityCol {
		t.Errorf("old index 5 read as column %d", got)
	}
	old.SetString(prefSortCol, "none")
	if got := savedSortColumn(old); got != -1 {
		t.Errorf("none read as %d", got)
	}
}

func TestRevealAndCopyPath(t *testing.T) {
	u, _, _ := newTestUI(t)
	tr := genuineFLAC(t, "song")
	var revealed []string
	u.reveal = func(p string) error { revealed = append(revealed, p); return nil }
	menu := u.trackMenu(tr)
	item := func(label string) *fyne.MenuItem {
		for _, it := range menu.Items {
			if it.Label == label {
				return it
			}
		}
		t.Fatalf("no %q item", label)
		return nil
	}
	item(revealLabel).Action()
	if len(revealed) != 1 || revealed[0] != tr.Path {
		t.Errorf("revealed %q", revealed)
	}
	item("Copy File Path").Action()
	if got := fyne.CurrentApp().Clipboard().Content(); got != tr.Path {
		t.Errorf("clipboard %q", got)
	}

	// A file that has gone, as on an unplugged disk, is explained.
	gone := Track{Path: filepath.Join(t.TempDir(), "gone.flac"), Title: "gone"}
	u.revealTrack(gone)
	if len(revealed) != 1 {
		t.Error("tried to show a missing file")
	}
	if top := u.win.Canvas().Overlays().Top(); top == nil || !strings.Contains(allText(top), "Can't find gone.flac") {
		t.Error("no message for a missing file")
	}
}

func TestGetInfo(t *testing.T) {
	u, _, _ := newTestUI(t)
	tr := genuineFLAC(t, "song")
	tr.Artist, tr.Year = "Someone", 1999
	u.setLevel(tr.Path, qualityLossless)
	u.showInfo(tr)
	top := u.win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no window")
	}
	var form *widget.Form
	visit(top, func(o fyne.CanvasObject) {
		if f, ok := o.(*widget.Form); ok {
			form = f
		}
	})
	got := map[string]string{}
	for _, it := range form.Items {
		got[it.Text] = it.Widget.(*widget.Label).Text
	}
	st, _ := os.Stat(tr.Path)
	for field, want := range map[string]string{
		"Name": "song.flac", "Where": filepath.Dir(tr.Path), "Artist": "Someone", "Year": "1999",
		"Format": "FLAC · 16-bit · 44.1 kHz · Mono", "Length": "0:02", "Quality": "Lossless",
		"Size": fileSize(st.Size()),
	} {
		if !strings.HasPrefix(got[field], want) {
			t.Errorf("%s: %q, want %q", field, got[field], want)
		}
	}
}
