package main

// Playlists. An ordinary playlist is songs in the order they were added.
// A smart playlist is rules instead, such as "Format is FLAC and Quality
// is Lossless?", and lists the songs that match them now, so it keeps
// itself up to date as music is added or checked. Playlists are saved as
// JSON next to the library cache.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// playlist is one playlist.
type playlist struct {
	Name  string
	Songs []string    `json:",omitempty"` // paths, in order, for an ordinary playlist
	Smart *smartRules `json:",omitempty"` // the rules, for a smart playlist

	row *folderRow // in the sidebar
}

// smartRules are a smart playlist's rules: the songs matching all of them,
// or with Any, at least one.
type smartRules struct {
	Any   bool
	Rules []smartRule
}

// smartRule is one rule, such as Year / is less than / 1980.
type smartRule struct {
	Field string
	Op    string
	Value string
}

// A rule's field is one of these, and each kind of field has its own
// comparisons.
type fieldKind int

const (
	textField     fieldKind = iota
	numberField             // the value is a number
	minutesField            // the song's length, in minutes
	megabyteField           // the file's size, in MB
	qualityField            // one of the Quality column's verdicts
	daysField               // the file's date, within the last so many days
)

var smartFields = []struct {
	name string
	kind fieldKind
}{
	{"Title", textField},
	{"Artist", textField},
	{"Album", textField},
	{"Year", numberField},
	{"Format", textField},
	{"Quality", qualityField},
	{"Time", minutesField},
	{"Size", megabyteField},
	{"Folder", textField},
	{"File Date", daysField},
}

const (
	opContains    = "contains"
	opNotContains = "does not contain"
	opIs          = "is"
	opIsNot       = "is not"
	opStartsWith  = "starts with"
	opMore        = "is more than"
	opLess        = "is less than"
	opWithin      = "is in the last"
	opNotWithin   = "is not in the last"
)

var kindOps = map[fieldKind][]string{
	textField:     {opContains, opNotContains, opIs, opIsNot, opStartsWith},
	numberField:   {opIs, opIsNot, opMore, opLess},
	minutesField:  {opMore, opLess},
	megabyteField: {opMore, opLess},
	qualityField:  {opIs, opIsNot},
	daysField:     {opWithin, opNotWithin},
}

// kindUnits are shown after a rule's value.
var kindUnits = map[fieldKind]string{minutesField: "minutes", megabyteField: "MB", daysField: "days"}

// notCheckedChoice is the Quality rule's value for songs with no verdict.
const notCheckedChoice = "Not checked"

// qualityChoices are the Quality rule's values, in the column's order.
func qualityChoices() []string {
	return []string{levelNames[qualityDamaged], levelNames[qualitySuspect], levelNames[qualityLossy],
		levelNames[qualityLossless], levelNames[qualityHiRes], notCheckedChoice}
}

func fieldKindOf(name string) (fieldKind, bool) {
	for _, f := range smartFields {
		if f.name == name {
			return f.kind, true
		}
	}
	return 0, false
}

// songFacts is what rules need to know about a song beyond its tags.
type songFacts struct {
	level   func(Track) qualityLevel
	modTime func(path string) time.Time // zero if unknown
	now     time.Time
}

// matches reports whether t passes the rule. A rule that can't be read,
// such as a number that isn't one, matches nothing.
func (r smartRule) matches(t Track, f songFacts) bool {
	kind, ok := fieldKindOf(r.Field)
	if !ok {
		return false
	}
	value := strings.TrimSpace(r.Value)
	switch kind {
	case textField:
		have := map[string]string{"Title": t.Title, "Artist": t.Artist, "Album": t.Album,
			"Format": t.Format, "Folder": filepath.Dir(t.Path)}[r.Field]
		have, want := strings.ToLower(have), strings.ToLower(value)
		switch r.Op {
		case opContains:
			return strings.Contains(have, want)
		case opNotContains:
			return !strings.Contains(have, want)
		case opIs:
			return have == want
		case opIsNot:
			return have != want
		case opStartsWith:
			return strings.HasPrefix(have, want)
		}
	case qualityField:
		name := levelNames[f.level(t)]
		if name == "" {
			name = notCheckedChoice
		}
		switch r.Op {
		case opIs:
			return strings.EqualFold(name, value)
		case opIsNot:
			return !strings.EqualFold(name, value)
		}
	case daysField:
		days, err := strconv.ParseFloat(value, 64)
		mod := f.modTime(t.Path)
		if err != nil || mod.IsZero() {
			return false
		}
		within := f.now.Sub(mod) <= time.Duration(days*24*float64(time.Hour))
		switch r.Op {
		case opWithin:
			return within
		case opNotWithin:
			return !within
		}
	default:
		want, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return false
		}
		var have float64
		switch kind {
		case numberField:
			if t.Year == 0 {
				return false // no year: neither before nor after any
			}
			have = float64(t.Year)
		case minutesField:
			if t.Length == 0 {
				return false
			}
			have = t.Length.Minutes()
		case megabyteField:
			have = float64(t.Size) / 1e6
		}
		switch r.Op {
		case opIs:
			return have == want
		case opIsNot:
			return have != want
		case opMore:
			return have > want
		case opLess:
			return have < want
		}
	}
	return false
}

// matches reports whether t passes the rules. No rules match every song.
func (s *smartRules) matches(t Track, f songFacts) bool {
	if len(s.Rules) == 0 {
		return true
	}
	for _, r := range s.Rules {
		if r.matches(t, f) == s.Any {
			return s.Any
		}
	}
	return !s.Any
}

// tracks lists p's songs that are in the library: an ordinary playlist's
// in its order, a smart one's in library order.
// byPath maps paths to songs, and is made from all if nil.
func (p *playlist) tracks(all []Track, byPath map[string]Track, f songFacts) []Track {
	if p.Smart != nil {
		var out []Track
		for _, t := range all {
			if p.Smart.matches(t, f) {
				out = append(out, t)
			}
		}
		return out
	}
	if byPath == nil {
		byPath = tracksByPath(all)
	}
	out := make([]Track, 0, len(p.Songs))
	for _, path := range p.Songs {
		if t, ok := byPath[path]; ok {
			out = append(out, t)
		}
	}
	return out
}

func tracksByPath(all []Track) map[string]Track {
	m := make(map[string]Track, len(all))
	for _, t := range all {
		m[t.Path] = t
	}
	return m
}

// add appends songs not already in p.
func (p *playlist) add(paths ...string) int {
	added := 0
	for _, path := range paths {
		if !slices.Contains(p.Songs, path) {
			p.Songs = append(p.Songs, path)
			added++
		}
	}
	return added
}

// remove takes songs out of p.
func (p *playlist) remove(paths ...string) {
	p.Songs = slices.DeleteFunc(p.Songs, func(s string) bool { return slices.Contains(paths, s) })
}

// move moves the song at path by one place up (-1) or down (+1) among
// the songs listed, which are the playlist's that are in the library.
func (p *playlist) move(path string, by int, listed []Track) {
	i := slices.IndexFunc(listed, func(t Track) bool { return t.Path == path })
	j := i + by
	if i < 0 || j < 0 || j >= len(listed) {
		return
	}
	a := slices.Index(p.Songs, path)
	b := slices.Index(p.Songs, listed[j].Path)
	p.Songs[a], p.Songs[b] = p.Songs[b], p.Songs[a]
}

// playlistFile is the saved form.
type playlistFile struct {
	Version   int
	Playlists []*playlist
}

// loadPlaylists reads the playlists saved at path. A missing or unreadable
// file gives none.
func loadPlaylists(path string) []*playlist {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f playlistFile
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	return f.Playlists
}

// savePlaylists writes the playlists to path, through a temporary file so
// a crash can't leave half a file.
func savePlaylists(path string, ps []*playlist) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(playlistFile{Version: 1, Playlists: ps}, "", "\t")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// uniqueName returns name, or name 2, name 3… if a playlist has it.
func uniqueName(ps []*playlist, name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(ps, func(p *playlist) bool { return strings.EqualFold(p.Name, n) })
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s %d", name, i); !taken(n) {
			return n
		}
	}
}
