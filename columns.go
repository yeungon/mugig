package main

// Showing and hiding the song list's columns: right-click a column header,
// or View > Columns. Title always shows. The choice is saved by column
// name, so it survives columns being added.

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

const prefHiddenCols = "hiddenColumns" // names of the columns hidden

// columnWidths are the columns' widths, in columns' order.
var columnWidths = []float32{250, 170, 190, 70, 70, 80, 85, 100, 400}

// loadColumns reads which columns are shown.
func (u *ui) loadColumns() {
	hidden := u.prefs.StringList(prefHiddenCols)
	u.shownCols = u.shownCols[:0]
	for col, name := range columns {
		if col == titleCol || !slices.Contains(hidden, name) {
			u.shownCols = append(u.shownCols, col)
		}
	}
}

// colAt returns the column shown at position i, or -1.
func (u *ui) colAt(i int) int {
	if i < 0 || i >= len(u.shownCols) {
		return -1
	}
	return u.shownCols[i]
}

// setColumnShown shows or hides col, and saves the choice. Hiding the
// column the list is sorted by puts it back in library order.
func (u *ui) setColumnShown(col int, on bool) {
	if col == titleCol || on == slices.Contains(u.shownCols, col) {
		return
	}
	if on {
		u.shownCols = append(u.shownCols, col)
		slices.Sort(u.shownCols)
	} else {
		u.shownCols = slices.DeleteFunc(u.shownCols, func(c int) bool { return c == col })
	}
	var hidden []string
	for c, name := range columns {
		if !slices.Contains(u.shownCols, c) {
			hidden = append(hidden, name)
		}
	}
	u.prefs.SetStringList(prefHiddenCols, hidden)
	if !on && u.sortCol == col {
		u.sortBy(col) // back to library order
		u.sortBy(col)
	}
	u.applyColumns()
}

// applyColumns sizes the shown columns and ticks them in View > Columns.
func (u *ui) applyColumns() {
	for i, col := range u.shownCols {
		u.table.SetColumnWidth(i, columnWidths[col])
	}
	u.table.Refresh()
	for col, item := range u.columnItems {
		item.Checked = slices.Contains(u.shownCols, col)
	}
	if u.mainMenu != nil {
		u.mainMenu.Refresh()
	}
}

// buildColumnItems creates the View > Columns items, one per column.
func (u *ui) buildColumnItems() *fyne.MenuItem {
	u.columnItems = nil
	for col, name := range columns {
		item := fyne.NewMenuItem(name, func() { u.setColumnShown(col, !slices.Contains(u.shownCols, col)) })
		item.Disabled = col == titleCol
		u.columnItems = append(u.columnItems, item)
	}
	sub := fyne.NewMenuItem("Columns", nil)
	sub.ChildMenu = fyne.NewMenu("", u.columnItems...)
	return sub
}

// columnsMenu is the column header's right-click menu.
func (u *ui) columnsMenu() *fyne.Menu {
	return fyne.NewMenu("", u.columnItems...)
}

// headerCell is a column header: a flat button that sorts by its column,
// and offers the columns to show on right-click.
type headerCell struct {
	widget.Button
	onMenu func(at fyne.Position)
}

func newHeader() fyne.CanvasObject {
	h := &headerCell{}
	h.Alignment = widget.ButtonAlignLeading
	h.IconPlacement = widget.ButtonIconTrailingText
	h.Importance = widget.LowImportance
	h.ExtendBaseWidget(h)
	return h
}

func (h *headerCell) TappedSecondary(e *fyne.PointEvent) {
	if h.onMenu != nil {
		h.onMenu(e.AbsolutePosition)
	}
}
