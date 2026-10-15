package main

import (
	"mugig/internal/humanize"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// shownNames lists the headers of the columns shown.
func shownNames(u *ui) []string {
	var names []string
	for i := range u.shownCols {
		h := newHeader().(*headerCell)
		u.updateHeader(widget.TableCellID{Row: -1, Col: i}, h)
		names = append(names, h.Text)
	}
	return names
}

// Hiding columns takes them out of the list, with no gap; the cells move
// up to fill it. The choice is saved, and Title can't be hidden.
func TestHideAndShowColumns(t *testing.T) {
	u, _, a := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.setColumnShown(pathCol, false)
	u.setColumnShown(formatCol, false)
	u.setColumnShown(titleCol, false)
	want := []string{"TITLE", "ARTIST", "ALBUM", "TIME", "YEAR", "SIZE", "QUALITY"}
	if got := shownNames(u); !slices.Equal(got, want) {
		t.Errorf("headers %v", got)
	}
	if _, cols := u.table.Length(); cols != 7 {
		t.Errorf("table has %d columns", cols)
	}
	cell := u.table.CreateCell().(*trackCell)
	u.table.UpdateCell(widget.TableCellID{Row: 3, Col: 5}, cell) // Size, where Format was
	if cell.seg.Text != humanize.FileSize(u.tracks[3].Size) {
		t.Errorf("column 5 shows %q", cell.seg.Text)
	}
	if item := u.columnItems[formatCol]; item.Checked || !u.columnItems[albumCol].Checked || !u.columnItems[titleCol].Disabled {
		t.Error("View > Columns should tick the shown columns, with Title fixed")
	}
	if got := a.Preferences().StringList(prefHiddenCols); !slices.Equal(got, []string{"Format", "Path"}) {
		t.Errorf("saved %v", got)
	}

	u2 := newUI(u.win, a.Preferences(), uiDeps{player: &fakePlayer{}, cache: u.cache})
	if got := shownNames(u2); !slices.Equal(got, want) {
		t.Errorf("after reopening: %v", got)
	}

	u.setColumnShown(pathCol, true)
	if got := shownNames(u); got[len(got)-1] != "PATH" || len(got) != 8 {
		t.Errorf("Path should come back in its place: %v", got)
	}
}

// Hiding the column the list is sorted by goes back to library order.
func TestHideSortedColumn(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.sortBy(yearCol)
	u.setColumnShown(yearCol, false)
	if u.sortCol != -1 || u.tracks[0].Title != "Come Together" {
		t.Errorf("sortCol %d, first %q", u.sortCol, u.tracks[0].Title)
	}
}

// Right-clicking a header offers the columns.
func TestHeaderMenu(t *testing.T) {
	u, _, _ := newTestUI(t)
	h := newHeader().(*headerCell)
	u.updateHeader(widget.TableCellID{Row: -1, Col: 2}, h)
	test.TapSecondary(h)
	top := u.win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no menu")
	}
	var labels []string
	for _, it := range u.columnsMenu().Items {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, ",") != strings.Join(columns, ",") {
		t.Errorf("menu %v", labels)
	}
}
