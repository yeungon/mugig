package ui

// The library (u.all) is everything scanned, kept in artist/album/title order
// once a scan finishes. The table shows a view of it (u.tracks), narrowed to
// the folder chosen in the sidebar and by the search box, and ordered by the
// column header the user clicked.

import (
	"cmp"
	"fmt"
	"mugig/internal/humanize"
	"mugig/internal/library"
	"mugig/internal/quality"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (u *ui) searchWords() []string {
	return library.SearchWords(u.search.Text)
}

// applyView rebuilds the visible list from the library using the current
// folder, search and sort. Called on the UI thread.
func (u *ui) applyView() {
	words := u.searchWords()
	view := make([]library.Track, 0, len(u.all))
	for _, t := range u.shownBase() {
		if library.Matches(t, words) {
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
	u.pruneSelection()
	u.queueValid = false // shuffle starts a fresh round over the new list
	u.table.Refresh()
	u.updateCount()
}

// shownBase is what the song list shows before a search: a playlist's
// songs, a folder's, or the whole library.
func (u *ui) shownBase() []library.Track {
	switch {
	case u.playlist != nil:
		return u.playlist.Tracks(u.all, nil, u.songFacts())
	case u.filter != "":
		return slices.DeleteFunc(slices.Clone(u.all), func(t library.Track) bool { return !u.inFilter(t) })
	}
	return u.all
}

// updateCount shows the page title, and under it the library's or the
// folder's stats, or how many tracks the search matched. It also updates
// the sidebar's counts.
func (u *ui) updateCount() {
	defer u.refreshFolders()
	title := "Songs"
	switch {
	case u.playlist != nil:
		title = u.playlist.Name
	case u.filter != "":
		title = filepath.Base(u.filter)
	}
	if u.pageTitle != nil && u.pageTitle.Text != title {
		u.pageTitle.Text = title
		u.songsPage.Refresh()
	}
	shown := u.shownBase()
	if len(u.selected) > 0 {
		u.count.SetText(selectionStats(u.selectedTracks()))
		return
	}
	if len(u.searchWords()) > 0 {
		u.count.SetText(fmt.Sprintf("%s of %s match", humanize.Thousands(len(u.tracks)), humanize.Plural(len(shown), "track")))
		return
	}
	key := fmt.Sprintf("%s\x00%p\x00%d", u.filter, u.playlist, len(shown))
	if len(shown) == 0 || u.statsFor != key { // only recount when the list changed
		u.stats, u.statsFor = u.folderStats(shown), key
	}
	u.count.SetText(u.stats)
}

// folderStats summarises the songs shown, or says why there are none.
func (u *ui) folderStats(shown []library.Track) string {
	f := u.folderAt(u.filter)
	switch {
	case len(shown) > 0:
		return libraryStats(shown)
	case u.playlist != nil && u.playlist.Smart != nil:
		return "No songs match its rules."
	case u.playlist != nil:
		return "No songs yet. Right-click songs and choose Add to Playlist."
	case u.filter == "" && len(u.folders) == 0:
		return libraryStats(shown)
	case f != nil && !f.connected:
		return f.missing + ". Its songs will be back when it is connected again."
	case u.scanning != "" && (u.filter == "" || u.scanning == u.filter):
		return "Scanning…"
	case f != nil:
		return "No songs in this folder."
	}
	return "No songs. Connect the disks with your music, or add a folder."
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

// Column headers (see headerCell) sort by their column and show an arrow
// on the active one.
func (u *ui) updateHeader(id widget.TableCellID, o fyne.CanvasObject) {
	col := u.colAt(id.Col)
	if col < 0 {
		return
	}
	b := o.(*headerCell)
	b.onMenu = func(at fyne.Position) {
		widget.ShowPopUpMenuAtPosition(u.columnsMenu(), u.win.Canvas(), at)
	}
	b.SetText(strings.ToUpper(columns[col]))
	switch {
	case col != u.sortCol:
		b.SetIcon(nil)
	case u.sortDesc:
		b.SetIcon(theme.MenuDropDownIcon())
	default:
		b.SetIcon(theme.MenuDropUpIcon())
	}
	b.OnTapped = func() { u.sortBy(col) }
}

func (u *ui) compareColumn(a, b library.Track, col int) int {
	switch col {
	case timeCol:
		return cmp.Compare(a.Length, b.Length)
	case yearCol:
		return cmp.Compare(a.Year, b.Year)
	case sizeCol:
		return cmp.Compare(a.Size, b.Size)
	case qualityCol:
		return cmp.Compare(quality.LevelRank[u.levelOf(a)], quality.LevelRank[u.levelOf(b)])
	}
	return library.CompareFold(cellText(a, col), cellText(b, col))
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
