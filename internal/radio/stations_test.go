package radio

import (
	"os"
	"path/filepath"
	"testing"
)

// Stations are saved and read back as they were; a missing or damaged
// file reads as none.
func TestStationsSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stations.json")
	want := []*Station{
		{Name: "BBC World Service", Link: "https://example.com/bbc.m3u8", Format: "AAC · 96 kbps"},
		{Name: "Jazz", Link: "http://example.com:8000/jazz"},
	}
	if err := SaveStations(path, want); err != nil {
		t.Fatal(err)
	}
	got := LoadStations(path)
	if len(got) != 2 || *got[0] != *want[0] || *got[1] != *want[1] {
		t.Errorf("read back %+v", got)
	}
	if LoadStations(filepath.Join(t.TempDir(), "none.json")) != nil {
		t.Error("a missing file gave stations")
	}
	os.WriteFile(path, []byte("{oops"), 0o644)
	if LoadStations(path) != nil {
		t.Error("a damaged file gave stations")
	}
	if SaveStations("", want) != nil || LoadStations("") != nil {
		t.Error("an empty path should keep nothing")
	}
}
