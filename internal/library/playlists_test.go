package library

import (
	"mugig/internal/quality"
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
	facts := SongFacts{
		Level:   func(Track) quality.Level { return quality.Suspect },
		ModTime: func(string) time.Time { return now.Add(-10 * 24 * time.Hour) },
		Now:     now,
	}
	for _, c := range []struct {
		field, op, value string
		want             bool
	}{
		{"Artist", OpContains, "beatles", true},
		{"Artist", OpNotContains, "beatles", false},
		{"Artist", OpIs, "the beatles", true},
		{"Artist", OpIsNot, "Beatles", true},
		{"Title", OpStartsWith, "hey", true},
		{"Folder", OpContains, "/rock", true},
		{"Format", OpIs, "flac", true},
		{"Year", OpLess, "1970", true},
		{"Year", OpMore, "1970", false},
		{"Year", OpIs, "1968", true},
		{"Year", OpLess, "soon", false}, // not a number: matches nothing
		{"Time", OpMore, "7", true},
		{"Time", OpLess, "7", false},
		{"Size", OpMore, "25", true},
		{"Quality", OpIs, "lossless?", true},
		{"Quality", OpIsNot, "Lossless?", false},
		{"File Date", OpWithin, "30", true},
		{"File Date", OpWithin, "7", false},
		{"File Date", OpNotWithin, "7", true},
		{"Colour", OpIs, "blue", false}, // no such field
	} {
		if got := (SmartRule{c.field, c.op, c.value}).Matches(song, facts); got != c.want {
			t.Errorf("%s %s %q: %v", c.field, c.op, c.value, got)
		}
	}

	noYear := song
	noYear.Year, noYear.Length = 0, 0
	for _, r := range []SmartRule{{"Year", OpLess, "2000"}, {"Year", OpMore, "0"}, {"Time", OpLess, "100"}} {
		if r.Matches(noYear, facts) {
			t.Errorf("a song without a %s matched %s %s", r.Field, r.Op, r.Value)
		}
	}
	unchecked := SongFacts{Level: func(Track) quality.Level { return quality.Unknown }, ModTime: facts.ModTime, Now: now}
	if !(SmartRule{"Quality", OpIs, notCheckedChoice}).Matches(song, unchecked) {
		t.Error("an unchecked song should match Quality is Not checked")
	}

	all := SmartRules{Rules: []SmartRule{{"Artist", OpContains, "beatles"}, {"Year", OpMore, "1970"}}}
	if all.Matches(song, facts) {
		t.Error("all: one rule fails, so the song shouldn't match")
	}
	all.Any = true
	if !all.Matches(song, facts) {
		t.Error("any: one rule passes, so the song should match")
	}
	if !(&SmartRules{}).Matches(song, facts) {
		t.Error("no rules should match every song")
	}
}

// Playlists are saved and read back; a damaged file gives none.
func TestSavePlaylists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playlists.json")
	ps := []*Playlist{
		{Name: "Mix", Songs: []string{"/a.flac", "/b.mp3"}},
		{Name: "Old", Smart: &SmartRules{Any: true, Rules: []SmartRule{{"Year", OpLess, "1970"}}}},
	}
	if err := SavePlaylists(path, ps); err != nil {
		t.Fatal(err)
	}
	got := LoadPlaylists(path)
	if len(got) != 2 || got[0].Name != "Mix" || !slices.Equal(got[0].Songs, ps[0].Songs) ||
		got[1].Smart == nil || !got[1].Smart.Any || got[1].Smart.Rules[0] != ps[1].Smart.Rules[0] {
		t.Errorf("read back %+v %+v", got[0], got[1])
	}
	os.WriteFile(path, []byte("{not json"), 0o644)
	if LoadPlaylists(path) != nil {
		t.Error("a damaged file should give no playlists")
	}
	if LoadPlaylists(filepath.Join(t.TempDir(), "none.json")) != nil {
		t.Error("no file should give no playlists")
	}
}

func TestUniqueName(t *testing.T) {
	ps := []*Playlist{{Name: "Mix"}, {Name: "mix 2"}}
	if got := UniqueName(ps, "Mix"); got != "Mix 3" {
		t.Errorf("%q", got)
	}
	if got := UniqueName(ps, "Jazz"); got != "Jazz" {
		t.Errorf("%q", got)
	}
}

// Rules that can't be read match nothing, whatever the field: a value that
// isn't a number, a comparison the field doesn't have, a file whose date
// isn't known. Values are read without the spaces around them.
func TestSmartRulesThatCantBeRead(t *testing.T) {
	song := Track{Path: "/music/a.flac", Title: "A", Year: 1968, Size: 30e6, Length: 7 * time.Minute}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	known := SongFacts{
		Level:   func(Track) quality.Level { return quality.Lossless },
		ModTime: func(string) time.Time { return now.Add(-24 * time.Hour) },
		Now:     now,
	}
	unknownDate := known
	unknownDate.ModTime = func(string) time.Time { return time.Time{} }
	for _, c := range []struct {
		rule  SmartRule
		facts SongFacts
	}{
		{SmartRule{"Time", OpMore, "abc"}, known},
		{SmartRule{"Size", OpLess, ""}, known},
		{SmartRule{"File Date", OpWithin, "a week"}, known},
		{SmartRule{"File Date", OpWithin, "30"}, unknownDate},
		{SmartRule{"File Date", OpNotWithin, "30"}, unknownDate},
		{SmartRule{"Time", OpIs, "7"}, known},             // Time has only more and less
		{SmartRule{"Title", OpMore, "a"}, known},          // text has no more than
		{SmartRule{"Quality", OpContains, "loss"}, known}, // Quality is or is not
		{SmartRule{"Year", "", "1968"}, known},
	} {
		if c.rule.Matches(song, c.facts) {
			t.Errorf("%s %q %q matched", c.rule.Field, c.rule.Op, c.rule.Value)
		}
	}
	for _, r := range []SmartRule{{"Year", OpIs, " 1968 "}, {"Artist", OpIs, ""}, {"Title", OpIs, "  a "}} {
		if !r.Matches(song, known) {
			t.Errorf("%s %s %q should match", r.Field, r.Op, r.Value)
		}
	}
}

// Every field has comparisons, and the Quality rule offers every verdict
// the column shows, and songs not checked yet.
func TestSmartFieldChoices(t *testing.T) {
	for _, f := range SmartFields {
		if kind, ok := FieldKindOf(f.Name); !ok || kind != f.Kind || len(KindOps[f.Kind]) == 0 {
			t.Errorf("field %s: kind %v, comparisons %v", f.Name, kind, KindOps[f.Kind])
		}
	}
	if _, ok := FieldKindOf("Colour"); ok {
		t.Error("Colour isn't a field")
	}
	choices := QualityChoices()
	for l, name := range quality.LevelNames {
		if !slices.Contains(choices, name) {
			t.Errorf("verdict %v (%s) isn't offered", l, name)
		}
	}
	if choices[len(choices)-1] != notCheckedChoice || len(choices) != len(quality.LevelNames)+1 {
		t.Errorf("choices %v", choices)
	}
}

// Adding skips songs already there; removing takes out only those named;
// moving is among the songs the library has, skipping any on a disk not
// plugged in, and does nothing past either end.
func TestPlaylistEditing(t *testing.T) {
	p := &Playlist{Name: "Mix"}
	if n := p.Add("/a", "/b", "/a", "/x", "/c"); n != 4 || !slices.Equal(p.Songs, []string{"/a", "/b", "/x", "/c"}) {
		t.Fatalf("added %d: %v", n, p.Songs)
	}
	if n := p.Add("/b"); n != 0 {
		t.Errorf("added %d already there", n)
	}
	all := []Track{{Path: "/a"}, {Path: "/b"}, {Path: "/c"}} // /x's disk is unplugged
	listed := p.Tracks(all, nil, SongFacts{})
	if len(listed) != 3 {
		t.Fatalf("listed %v", listed)
	}
	p.Move("/b", +1, listed) // past /x, which isn't listed, to swap with /c
	if !slices.Equal(p.Songs, []string{"/a", "/c", "/x", "/b"}) {
		t.Errorf("after moving /b down: %v", p.Songs)
	}
	listed = p.Tracks(all, nil, SongFacts{})
	before := slices.Clone(p.Songs)
	p.Move("/a", -1, listed)    // already first
	p.Move("/b", +1, listed)    // already last
	p.Move("/x", +1, listed)    // not listed
	p.Move("/none", -1, listed) // not in the playlist
	if !slices.Equal(p.Songs, before) {
		t.Errorf("moves past the ends changed %v to %v", before, p.Songs)
	}
	p.Remove("/c", "/none")
	if !slices.Equal(p.Songs, []string{"/a", "/x", "/b"}) {
		t.Errorf("after removing: %v", p.Songs)
	}
}
