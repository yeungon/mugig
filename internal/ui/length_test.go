package ui

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// The Time column shows each song's length, sorts by it, and the library's
// total is in the stats line.
func TestTimeColumn(t *testing.T) {
	u, _, _ := newTestUI(t)
	songs := fiveTracks()
	for i, l := range []time.Duration{3 * time.Minute, 65 * time.Second, 0, 6 * time.Minute, 2 * time.Hour} {
		songs[i].Length = l
	}
	setLibrary(u, songs...)
	if columns[timeCol] != "Time" || cellText(songs[1], timeCol) != "1:05" || cellText(songs[2], timeCol) != "" {
		t.Errorf("Time cells: %q, %q", cellText(songs[1], timeCol), cellText(songs[2], timeCol))
	}
	u.sortBy(timeCol)
	if got := titles(u.tracks); !slices.Equal(got, []string{"Hey Jude", "Help", "Come Together", "Bohemian Rhapsody", "Zebra"}) {
		t.Errorf("sorted by time: %v", got)
	}
	if !strings.HasSuffix(u.count.Text, "· 2 h 10 min") {
		t.Errorf("stats %q should give the total time", u.count.Text)
	}
}
