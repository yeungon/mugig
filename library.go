package main

// The library (u.all) is everything scanned, kept in artist/album/title order
// once a scan finishes. The table shows a view of it (u.tracks), narrowed by
// the search box and ordered by the column header the user clicked.

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// searchKey is what the search box matches against.
func searchKey(t Track) string {
	return strings.ToLower(t.Title + "\x00" + t.Artist + "\x00" + t.Album)
}

func (u *ui) searchWords() []string {
	return strings.Fields(strings.ToLower(u.search.Text))
}

// matches reports whether t contains every search word, in any field.
func matches(t Track, words []string) bool {
	for _, w := range words {
		if !strings.Contains(t.search, w) {
			return false
		}
	}
	return true
}

// applyView rebuilds the visible list from the library using the current
// search and sort. Called on the UI thread.
func (u *ui) applyView() {
	words := u.searchWords()
	view := make([]Track, 0, len(u.all))
	for _, t := range u.all {
		if matches(t, words) {
			view = append(view, t)
		}
	}
	if u.sortCol >= 0 {
		// Stable, so ties keep library order.
		sort.SliceStable(view, func(i, j int) bool {
			c := u.compareColumn(view[i], view[j], u.sortCol)
			if u.sortDesc {
				c = -c
			}
			return c < 0
		})
	}
	u.tracks = view
	u.queueValid = false // shuffle starts a fresh round over the new list
	u.table.Refresh()
	u.updateCount()
}

// updateCount shows library stats under the page title, or how many
// tracks the search matched.
func (u *ui) updateCount() {
	if len(u.searchWords()) == 0 {
		if u.statsFor != len(u.all) || u.stats == "" { // only recount when the library changed
			u.stats, u.statsFor = libraryStats(u.all), len(u.all)
		}
		u.count.SetText(u.stats)
		return
	}
	u.count.SetText(fmt.Sprintf("%s of %s match", thousands(len(u.tracks)), plural(len(u.all), "track")))
}

// sortBy cycles a column through ascending, descending, then back to library
// order.
func (u *ui) sortBy(col int) {
	switch {
	case col != u.sortCol:
		u.sortCol, u.sortDesc = col, false
	case !u.sortDesc:
		u.sortDesc = true
	default:
		u.sortCol, u.sortDesc = -1, false
	}
	name := "none"
	if u.sortCol >= 0 {
		name = columns[u.sortCol]
	}
	u.prefs.SetString(prefSortCol, name)
	u.prefs.SetBool(prefSortDesc, u.sortDesc)
	u.applyView()
}

// Column headers are flat buttons that sort by their column and show an
// arrow on the active one.
func newHeader() fyne.CanvasObject {
	b := widget.NewButton("", nil)
	b.Alignment = widget.ButtonAlignLeading
	b.IconPlacement = widget.ButtonIconTrailingText
	b.Importance = widget.LowImportance
	return b
}

func (u *ui) updateHeader(id widget.TableCellID, o fyne.CanvasObject) {
	if id.Col < 0 || id.Col >= len(columns) {
		return
	}
	b := o.(*widget.Button)
	b.SetText(strings.ToUpper(columns[id.Col]))
	switch {
	case id.Col != u.sortCol:
		b.SetIcon(nil)
	case u.sortDesc:
		b.SetIcon(theme.MenuDropDownIcon())
	default:
		b.SetIcon(theme.MenuDropUpIcon())
	}
	col := id.Col
	b.OnTapped = func() { u.sortBy(col) }
}

func (u *ui) compareColumn(a, b Track, col int) int {
	switch col {
	case yearCol:
		return cmp.Compare(a.Year, b.Year)
	case sizeCol:
		return cmp.Compare(a.Size, b.Size)
	case qualityCol:
		return cmp.Compare(levelRank[u.levelOf(a)], levelRank[u.levelOf(b)])
	}
	return compareFold(cellText(a, col), cellText(b, col))
}

// libraryLess is the default order: artist, then album, then title.
func libraryLess(a, b Track) bool {
	if c := compareFold(a.Artist, b.Artist); c != 0 {
		return c < 0
	}
	if c := compareFold(a.Album, b.Album); c != 0 {
		return c < 0
	}
	return compareFold(a.Title, b.Title) < 0
}

func compareFold(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// oldColumns is the column order before Size was added, when the sorting
// column was saved by its index.
var oldColumns = []string{"Title", "Artist", "Album", "Year", "Format", "Quality", "Path"}

// savedSortColumn returns the column the list was last sorted by, or -1.
// It is saved by name, so adding columns doesn't change it; an index saved
// by an earlier version is read by the old order.
func savedSortColumn(prefs fyne.Preferences) int {
	name := prefs.String(prefSortCol)
	if name == "" {
		if i := prefs.IntWithFallback("sortColumn", -1); i >= 0 && i < len(oldColumns) {
			name = oldColumns[i]
		}
	}
	return slices.Index(columns, name)
}
