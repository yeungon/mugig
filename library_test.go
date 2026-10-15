package main

import (
	"mugig/internal/library"
	"slices"
	"testing"

	"fyne.io/fyne/v2/widget"
)

func TestSearchFiltersByAllWords(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"Come Together", "Help", "Hey Jude", "Bohemian Rhapsody", "Zebra"}},
		{"HEY", []string{"Hey Jude"}},
		{"beatles help", []string{"Help"}},                  // words may match different fields
		{"  queen   opera ", []string{"Bohemian Rhapsody"}}, // extra spaces are ignored
		{"abbey", []string{"Come Together"}},                // album
		{"nothing", nil},
	}
	for _, c := range cases {
		u.search.SetText(c.query)
		if got := titles(u.tracks); !slices.Equal(got, c.want) {
			t.Errorf("search %q = %v, want %v", c.query, got, c.want)
		}
	}
}

func TestSearchCount(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	u.search.SetText("beatles")
	if want := "3 of 5 tracks match"; u.count.Text != want {
		t.Errorf("count = %q, want %q", u.count.Text, want)
	}
	u.search.SetText("")
	// Zebra has no artist or album; the Beatles' three albums are distinct.
	if want := "5 tracks · 2 artists · 4 albums"; u.count.Text != want {
		t.Errorf("stats = %q, want %q", u.count.Text, want)
	}
}

func TestLibraryStatsWording(t *testing.T) {
	if got := libraryStats(nil); got != "No music yet. Add a folder with the + beside Folders." {
		t.Errorf("empty library: %q", got)
	}
	one := []library.Track{track("A", "Solo", "Only", 0)}
	if got := libraryStats(one); got != "1 track · 1 artist · 1 album" {
		t.Errorf("one track: %q", got)
	}
}

func TestSortCyclesAscDescOff(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	const year = yearCol

	u.sortBy(year)
	if got := titles(u.tracks); !slices.Equal(got, []string{"Zebra", "Help", "Hey Jude", "Come Together", "Bohemian Rhapsody"}) {
		t.Errorf("year ascending = %v", got)
	}
	u.sortBy(year)
	if got := titles(u.tracks); got[0] != "Bohemian Rhapsody" || got[4] != "Zebra" {
		t.Errorf("year descending = %v", got)
	}
	u.sortBy(year)
	if u.sortCol != -1 {
		t.Errorf("third click: sortCol = %d, want -1", u.sortCol)
	}
	if got := titles(u.tracks); got[0] != "Come Together" {
		t.Errorf("library order = %v", got)
	}
}

func TestSortIsCaseInsensitiveAndStable(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u,
		track("b", "Same", "", 0),
		track("A", "same", "", 0),
		track("c", "SAME", "", 0),
	)
	u.sortBy(1) // artist: all equal ignoring case, so library order is kept
	if got := titles(u.tracks); !slices.Equal(got, []string{"b", "A", "c"}) {
		t.Errorf("artist sort = %v, want library order kept", got)
	}
	u.sortBy(0)
	if got := titles(u.tracks); !slices.Equal(got, []string{"A", "b", "c"}) {
		t.Errorf("title sort = %v", got)
	}
}

func TestHeaderShowsSortArrow(t *testing.T) {
	u, _, _ := newTestUI(t)
	header := func(col int) *headerCell {
		b := newHeader().(*headerCell)
		u.updateHeader(widget.TableCellID{Row: -1, Col: col}, b)
		return b
	}

	u.sortBy(2)
	if b := header(2); b.Text != "ALBUM" || b.Icon == nil {
		t.Errorf("sorted header: text %q, icon %v", b.Text, b.Icon)
	}
	if b := header(0); b.Icon != nil {
		t.Error("unsorted header should have no arrow")
	}
	header(0).OnTapped()
	if u.sortCol != 0 {
		t.Errorf("tapping header: sortCol = %d, want 0", u.sortCol)
	}
}

func TestLibraryOrder(t *testing.T) {
	in := []library.Track{
		track("b", "Queen", "Z", 0),
		track("a", "queen", "Z", 0),
		track("x", "ABBA", "Gold", 0),
		track("y", "Queen", "A", 0),
	}
	slices.SortStableFunc(in, func(a, b library.Track) int {
		switch {
		case library.Less(a, b):
			return -1
		case library.Less(b, a):
			return 1
		}
		return 0
	})
	if got := titles(in); !slices.Equal(got, []string{"x", "y", "a", "b"}) {
		t.Errorf("library order = %v", got)
	}
}

// Results arriving during a scan are filtered straight away, then sorted
// once at the end.
func TestScanBatchesRespectSearchAndSort(t *testing.T) {
	u, _, _ := newTestUI(t)
	u.search.SetText("beatles")
	u.sortBy(yearCol)

	lib := fiveTracks()
	u.flush("/music", lib[:3])
	u.flush("/music", lib[3:])
	if len(u.all) != 5 || len(u.tracks) != 3 {
		t.Fatalf("during scan: %d in library, %d shown; want 5 and 3", len(u.all), len(u.tracks))
	}
	u.finish("/music", scanTally{complete: true, found: map[string]bool{lib[0].Path: true, lib[1].Path: true, lib[2].Path: true, lib[3].Path: true, lib[4].Path: true}})
	if got := titles(u.tracks); !slices.Equal(got, []string{"Help", "Hey Jude", "Come Together"}) {
		t.Errorf("after scan = %v", got)
	}
}
