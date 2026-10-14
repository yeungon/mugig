package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Each kind of rule compares as it says, without regard to case.
func TestSmartRules(t *testing.T) {
	song := Track{Path: "/music/Rock/a.flac", Title: "Hey Jude", Artist: "The Beatles", Album: "Hey Jude",
		Year: 1968, Format: "FLAC", Size: 30e6, Length: 7*time.Minute + 11*time.Second}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	facts := songFacts{
		level:   func(Track) qualityLevel { return qualitySuspect },
		modTime: func(string) time.Time { return now.Add(-10 * 24 * time.Hour) },
		now:     now,
	}
	for _, c := range []struct {
		field, op, value string
		want             bool
	}{
		{"Artist", opContains, "beatles", true},
		{"Artist", opNotContains, "beatles", false},
		{"Artist", opIs, "the beatles", true},
		{"Artist", opIsNot, "Beatles", true},
		{"Title", opStartsWith, "hey", true},
		{"Folder", opContains, "/rock", true},
		{"Format", opIs, "flac", true},
		{"Year", opLess, "1970", true},
		{"Year", opMore, "1970", false},
		{"Year", opIs, "1968", true},
		{"Year", opLess, "soon", false}, // not a number: matches nothing
		{"Time", opMore, "7", true},
		{"Time", opLess, "7", false},
		{"Size", opMore, "25", true},
		{"Quality", opIs, "lossless?", true},
		{"Quality", opIsNot, "Lossless?", false},
		{"File Date", opWithin, "30", true},
		{"File Date", opWithin, "7", false},
		{"File Date", opNotWithin, "7", true},
		{"Colour", opIs, "blue", false}, // no such field
	} {
		if got := (smartRule{c.field, c.op, c.value}).matches(song, facts); got != c.want {
			t.Errorf("%s %s %q: %v", c.field, c.op, c.value, got)
		}
	}

	noYear := song
	noYear.Year, noYear.Length = 0, 0
	for _, r := range []smartRule{{"Year", opLess, "2000"}, {"Year", opMore, "0"}, {"Time", opLess, "100"}} {
		if r.matches(noYear, facts) {
			t.Errorf("a song without a %s matched %s %s", r.Field, r.Op, r.Value)
		}
	}
	unchecked := songFacts{level: func(Track) qualityLevel { return qualityUnknown }, modTime: facts.modTime, now: now}
	if !(smartRule{"Quality", opIs, notCheckedChoice}).matches(song, unchecked) {
		t.Error("an unchecked song should match Quality is Not checked")
	}

	all := smartRules{Rules: []smartRule{{"Artist", opContains, "beatles"}, {"Year", opMore, "1970"}}}
	if all.matches(song, facts) {
		t.Error("all: one rule fails, so the song shouldn't match")
	}
	all.Any = true
	if !all.matches(song, facts) {
		t.Error("any: one rule passes, so the song should match")
	}
	if !(&smartRules{}).matches(song, facts) {
		t.Error("no rules should match every song")
	}
}

// An ordinary playlist lists its songs in its order, leaving out those not
// in the library; songs are added once, removed and moved.
func TestPlaylistSongs(t *testing.T) {
	lib := fiveTracks()
	p := &playlist{Name: "Mix"}
	if n := p.add(lib[3].Path, "/gone.mp3", lib[0].Path, lib[3].Path); n != 3 {
		t.Errorf("added %d, want 3: a song already in it isn't added twice", n)
	}
	facts := songFacts{level: func(Track) qualityLevel { return qualityUnknown }, modTime: func(string) time.Time { return time.Time{} }}
	got := p.tracks(lib, nil, facts)
	if !slices.Equal(titles(got), []string{"Bohemian Rhapsody", "Come Together"}) {
		t.Errorf("listed %v", titles(got))
	}
	p.move(lib[0].Path, -1, got)
	if got := titles(p.tracks(lib, nil, facts)); !slices.Equal(got, []string{"Come Together", "Bohemian Rhapsody"}) {
		t.Errorf("after moving up: %v", got)
	}
	if !slices.Equal(p.Songs, []string{lib[0].Path, "/gone.mp3", lib[3].Path}) {
		t.Errorf("the missing song should keep its place: %v", p.Songs)
	}
	p.remove(lib[0].Path)
	if len(p.Songs) != 2 {
		t.Errorf("songs %v", p.Songs)
	}

	smart := &playlist{Smart: &smartRules{Rules: []smartRule{{"Artist", opIs, "beatles"}}}}
	if got := titles(smart.tracks(lib, nil, facts)); !slices.Equal(got, []string{"Come Together", "Help", "Hey Jude"}) {
		t.Errorf("smart listed %v", got)
	}
}

// Playlists are saved and read back; a damaged file gives none.
func TestSavePlaylists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playlists.json")
	ps := []*playlist{
		{Name: "Mix", Songs: []string{"/a.flac", "/b.mp3"}},
		{Name: "Old", Smart: &smartRules{Any: true, Rules: []smartRule{{"Year", opLess, "1970"}}}},
	}
	if err := savePlaylists(path, ps); err != nil {
		t.Fatal(err)
	}
	got := loadPlaylists(path)
	if len(got) != 2 || got[0].Name != "Mix" || !slices.Equal(got[0].Songs, ps[0].Songs) ||
		got[1].Smart == nil || !got[1].Smart.Any || got[1].Smart.Rules[0] != ps[1].Smart.Rules[0] {
		t.Errorf("read back %+v %+v", got[0], got[1])
	}
	os.WriteFile(path, []byte("{not json"), 0o644)
	if loadPlaylists(path) != nil {
		t.Error("a damaged file should give no playlists")
	}
	if loadPlaylists(filepath.Join(t.TempDir(), "none.json")) != nil {
		t.Error("no file should give no playlists")
	}
}

func TestUniqueName(t *testing.T) {
	ps := []*playlist{{Name: "Mix"}, {Name: "mix 2"}}
	if got := uniqueName(ps, "Mix"); got != "Mix 3" {
		t.Errorf("%q", got)
	}
	if got := uniqueName(ps, "Jazz"); got != "Jazz" {
		t.Errorf("%q", got)
	}
}
