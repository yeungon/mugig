package main

import (
	"mugig/internal/library"
	"mugig/internal/quality"
	"slices"
	"testing"
	"time"
)

// An ordinary playlist lists its songs in its order, leaving out those not
// in the library; songs are added once, removed and moved.
func TestPlaylistSongs(t *testing.T) {
	lib := fiveTracks()
	p := &library.Playlist{Name: "Mix"}
	if n := p.Add(lib[3].Path, "/gone.mp3", lib[0].Path, lib[3].Path); n != 3 {
		t.Errorf("added %d, want 3: a song already in it isn't added twice", n)
	}
	facts := library.SongFacts{Level: func(library.Track) quality.Level { return quality.Unknown }, ModTime: func(string) time.Time { return time.Time{} }}
	got := p.Tracks(lib, nil, facts)
	if !slices.Equal(titles(got), []string{"Bohemian Rhapsody", "Come Together"}) {
		t.Errorf("listed %v", titles(got))
	}
	p.Move(lib[0].Path, -1, got)
	if got := titles(p.Tracks(lib, nil, facts)); !slices.Equal(got, []string{"Come Together", "Bohemian Rhapsody"}) {
		t.Errorf("after moving up: %v", got)
	}
	if !slices.Equal(p.Songs, []string{lib[0].Path, "/gone.mp3", lib[3].Path}) {
		t.Errorf("the missing song should keep its place: %v", p.Songs)
	}
	p.Remove(lib[0].Path)
	if len(p.Songs) != 2 {
		t.Errorf("songs %v", p.Songs)
	}

	smart := &library.Playlist{Smart: &library.SmartRules{Rules: []library.SmartRule{{Field: "Artist", Op: library.OpIs, Value: "beatles"}}}}
	if got := titles(smart.Tracks(lib, nil, facts)); !slices.Equal(got, []string{"Come Together", "Help", "Hey Jude"}) {
		t.Errorf("smart listed %v", got)
	}
}
