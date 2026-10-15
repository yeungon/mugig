package library

// Playlists. An ordinary playlist is songs in the order they were added.
// A smart playlist is rules instead, such as "Format is FLAC and Quality
// is Lossless?", and lists the songs that match them now, so it keeps
// itself up to date as music is added or checked. Playlists are saved as
// JSON next to the library cache.

import (
	"encoding/json"
	"fmt"
	"mugig/internal/quality"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Playlist is one playlist.
type Playlist struct {
	Name  string
	Songs []string    `json:",omitempty"` // paths, in order, for an ordinary playlist
	Smart *SmartRules `json:",omitempty"` // the rules, for a smart playlist
}

// SmartRules are a smart playlist's rules: the songs matching all of them,
// or with Any, at least one.
type SmartRules struct {
	Any   bool
	Rules []SmartRule
}

// SmartRule is one rule, such as Year / is less than / 1980.
type SmartRule struct {
	Field string
	Op    string
	Value string
}

// A rule's field is one of these, and each kind of field has its own
// comparisons.
type FieldKind int

const (
	textField     FieldKind = iota
	numberField             // the value is a number
	minutesField            // the song's length, in minutes
	megabyteField           // the file's size, in MB
	QualityField            // one of the Quality column's verdicts
	daysField               // the file's date, within the last so many days
)

// SmartField is a field a rule can test, and what kind of value it holds.
type SmartField struct {
	Name string
	Kind FieldKind
}

var SmartFields = []SmartField{
	{"Title", textField},
	{"Artist", textField},
	{"Album", textField},
	{"Year", numberField},
	{"Format", textField},
	{"Quality", QualityField},
	{"Time", minutesField},
	{"Size", megabyteField},
	{"Folder", textField},
	{"File Date", daysField},
}

const (
	OpContains    = "contains"
	OpNotContains = "does not contain"
	OpIs          = "is"
	OpIsNot       = "is not"
	OpStartsWith  = "starts with"
	OpMore        = "is more than"
	OpLess        = "is less than"
	OpWithin      = "is in the last"
	OpNotWithin   = "is not in the last"
)

var KindOps = map[FieldKind][]string{
	textField:     {OpContains, OpNotContains, OpIs, OpIsNot, OpStartsWith},
	numberField:   {OpIs, OpIsNot, OpMore, OpLess},
	minutesField:  {OpMore, OpLess},
	megabyteField: {OpMore, OpLess},
	QualityField:  {OpIs, OpIsNot},
	daysField:     {OpWithin, OpNotWithin},
}

// KindUnits are shown after a rule's value.
var KindUnits = map[FieldKind]string{minutesField: "minutes", megabyteField: "MB", daysField: "days"}

// notCheckedChoice is the Quality rule's value for songs with no verdict.
const notCheckedChoice = "Not checked"

// QualityChoices are the Quality rule's values, in the column's order.
func QualityChoices() []string {
	return []string{quality.LevelNames[quality.Damaged], quality.LevelNames[quality.Suspect], quality.LevelNames[quality.Lossy],
		quality.LevelNames[quality.Lossless], quality.LevelNames[quality.HiRes], notCheckedChoice}
}

func FieldKindOf(name string) (FieldKind, bool) {
	for _, f := range SmartFields {
		if f.Name == name {
			return f.Kind, true
		}
	}
	return 0, false
}

// SongFacts is what rules need to know about a song beyond its tags.
type SongFacts struct {
	Level   func(Track) quality.Level
	ModTime func(path string) time.Time // zero if unknown
	Now     time.Time
}

// Matches reports whether t passes the rule. A rule that can't be read,
// such as a number that isn't one, or a comparison its field doesn't
// offer, matches nothing.
func (r SmartRule) Matches(t Track, f SongFacts) bool {
	kind, ok := FieldKindOf(r.Field)
	if !ok || !slices.Contains(KindOps[kind], r.Op) {
		return false
	}
	value := strings.TrimSpace(r.Value)
	switch kind {
	case textField:
		have := map[string]string{"Title": t.Title, "Artist": t.Artist, "Album": t.Album,
			"Format": t.Format, "Folder": filepath.Dir(t.Path)}[r.Field]
		have, want := strings.ToLower(have), strings.ToLower(value)
		switch r.Op {
		case OpContains:
			return strings.Contains(have, want)
		case OpNotContains:
			return !strings.Contains(have, want)
		case OpIs:
			return have == want
		case OpIsNot:
			return have != want
		case OpStartsWith:
			return strings.HasPrefix(have, want)
		}
	case QualityField:
		name := quality.LevelNames[f.Level(t)]
		if name == "" {
			name = notCheckedChoice
		}
		switch r.Op {
		case OpIs:
			return strings.EqualFold(name, value)
		case OpIsNot:
			return !strings.EqualFold(name, value)
		}
	case daysField:
		days, err := strconv.ParseFloat(value, 64)
		mod := f.ModTime(t.Path)
		if err != nil || mod.IsZero() {
			return false
		}
		within := f.Now.Sub(mod) <= time.Duration(days*24*float64(time.Hour))
		switch r.Op {
		case OpWithin:
			return within
		case OpNotWithin:
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
		case OpIs:
			return have == want
		case OpIsNot:
			return have != want
		case OpMore:
			return have > want
		case OpLess:
			return have < want
		}
	}
	return false
}

// Matches reports whether t passes the rules. No rules match every song.
func (s *SmartRules) Matches(t Track, f SongFacts) bool {
	if len(s.Rules) == 0 {
		return true
	}
	for _, r := range s.Rules {
		if r.Matches(t, f) == s.Any {
			return s.Any
		}
	}
	return !s.Any
}

// Tracks lists p's songs that are in the library: an ordinary playlist's
// in its order, a smart one's in library order.
// byPath maps paths to songs, and is made from all if nil.
func (p *Playlist) Tracks(all []Track, byPath map[string]Track, f SongFacts) []Track {
	if p.Smart != nil {
		var out []Track
		for _, t := range all {
			if p.Smart.Matches(t, f) {
				out = append(out, t)
			}
		}
		return out
	}
	if byPath == nil {
		byPath = TracksByPath(all)
	}
	out := make([]Track, 0, len(p.Songs))
	for _, path := range p.Songs {
		if t, ok := byPath[path]; ok {
			out = append(out, t)
		}
	}
	return out
}

func TracksByPath(all []Track) map[string]Track {
	m := make(map[string]Track, len(all))
	for _, t := range all {
		m[t.Path] = t
	}
	return m
}

// Add appends songs not already in p.
func (p *Playlist) Add(paths ...string) int {
	added := 0
	for _, path := range paths {
		if !slices.Contains(p.Songs, path) {
			p.Songs = append(p.Songs, path)
			added++
		}
	}
	return added
}

// Remove takes songs out of p.
func (p *Playlist) Remove(paths ...string) {
	p.Songs = slices.DeleteFunc(p.Songs, func(s string) bool { return slices.Contains(paths, s) })
}

// Move moves the song at path by one place up (-1) or down (+1) among
// the songs listed, which are the playlist's that are in the library.
func (p *Playlist) Move(path string, by int, listed []Track) {
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
	Playlists []*Playlist
}

// LoadPlaylists reads the playlists saved at path. A missing or unreadable
// file gives none.
func LoadPlaylists(path string) []*Playlist {
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

// SavePlaylists writes the playlists to path, through a temporary file so
// a crash can't leave half a file.
func SavePlaylists(path string, ps []*Playlist) error {
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

// UniqueName returns name, or name 2, name 3… if a playlist has it.
func UniqueName(ps []*Playlist, name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(ps, func(p *Playlist) bool { return strings.EqualFold(p.Name, n) })
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
