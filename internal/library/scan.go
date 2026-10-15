package library

import (
	"context"
	"io/fs"
	"mugig/internal/audio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
)

// Track holds what we report to the GUI for one audio file.
type Track struct {
	Path   string
	Title  string
	Artist string
	Album  string
	Year   int
	Format string
	Size   int64         // bytes
	Length time.Duration // 0 if unknown
	NoTags bool          // true if tags were missing or unreadable
	// A song from a CUE sheet is a stretch of a disc image: File is the
	// image, and the song runs from Start to End in it (see cue.go).
	File       string
	Start, End time.Duration
	search     string // lower-cased title, artist and album for the search box
}

// audioExts maps lower-case extensions to a display format name.
var audioExts = map[string]string{
	".mp3":  "MP3",
	".flac": "FLAC",
	".ogg":  "OGG",
	".wav":  "WAV", // usually has no tags, so the file name is the title
}

// Walk finds audio files and CUE sheets under root and sends their paths
// to the channel. It closes paths when finished or cancelled.
func Walk(ctx context.Context, root string, paths chan<- string) {
	defer close(paths)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err() // stop walking on cancel
		}
		if err != nil {
			return nil // unreadable file or folder: skip it, keep going
		}
		if d.IsDir() {
			if p != root && skipFolder(p) {
				return filepath.SkipDir // e.g. .Trashes, which holds deleted songs (see skip.go)
			}
			return nil
		}
		if !IsAudioFile(p) && !isCueFile(p) || isEmpty(d) {
			return nil
		}
		select {
		case paths <- p:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

// ScanResult is one scanned file, with what the cache needs to know. For
// a CUE sheet, track is empty and cue holds its songs.
type ScanResult struct {
	Track   Track
	cue     []Track
	images  map[string][2]int64 // a sheet's disc images: their size and modification time
	Path    string              // the file scanned
	size    int64
	ModTime int64 // 0 if the file couldn't be checked; not cached then
	Cached  bool  // tags came from the cache, not the file
}

// Songs are the songs the file gives the library.
func (r ScanResult) Songs() []Track {
	if r.Path != "" && isCueFile(r.Path) {
		return r.cue
	}
	return []Track{r.Track}
}

// Entry is what the cache keeps of the file.
func (r ScanResult) Entry() CacheEntry {
	return CacheEntry{Size: r.size, ModTime: r.ModTime, Track: r.Track, Cue: r.cue, Images: r.images}
}

// Worker reads tags for each path, unless the cache has them, and sends the
// result.
func Worker(ctx context.Context, cache *Cache, paths <-chan string, results chan<- ScanResult, wg *sync.WaitGroup) {
	defer wg.Done()
	for p := range paths {
		if ctx.Err() != nil {
			return
		}
		select {
		case results <- ScanFile(cache, p):
		case <-ctx.Done():
			return
		}
	}
}

func ScanFile(cache *Cache, path string) ScanResult {
	if isCueFile(path) {
		return scanCue(cache, path)
	}
	r := ScanResult{Path: path}
	if fi, err := os.Stat(path); err == nil {
		r.size, r.ModTime = fi.Size(), fi.ModTime().UnixNano()
		r.Track, r.Cached = cache.Lookup(path, r.size, r.ModTime)
	}
	if !r.Cached {
		r.Track = ReadTrack(path)
	} else if r.Track.Length == 0 {
		// Cached before lengths were read, or the disk didn't answer then.
		r.Track.Length = audio.FileLength(path)
	}
	r.Track.Size = r.size // also for tracks cached before sizes were shown
	r.Track.search = searchKey(r.Track)
	return r
}

// IsAudioFile reports whether path names a file the app can play. Hidden
// files are left out: macOS puts a "._" file beside every file it copies
// to an exFAT or FAT disk, such as most USB drives, and "._song.flac" holds
// metadata, not audio.
func IsAudioFile(path string) bool {
	return !hidden(path) && audioExts[strings.ToLower(filepath.Ext(path))] != ""
}

// isEmpty reports whether a file has no data, as left by an interrupted
// download or copy. There is nothing to play.
func isEmpty(d fs.DirEntry) bool {
	fi, err := d.Info()
	return err == nil && fi.Size() == 0
}

// hidden reports whether path's name starts with a dot, or is __MACOSX: the
// folder macOS adds to zip files, whose copies of the files hold metadata,
// not audio.
func hidden(path string) bool {
	name := filepath.Base(path)
	return strings.HasPrefix(name, ".") || name == "__MACOSX"
}

// ReadTrack opens one file and parses its tags. Files with missing or
// broken tags fall back to using the file name as the title.
func ReadTrack(path string) Track {
	t := Track{
		Path:   path,
		Format: audioExts[strings.ToLower(filepath.Ext(path))],
	}
	fallbackTitle := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	f, err := os.Open(path)
	if err != nil {
		t.Title, t.NoTags = fallbackTitle, true
		return t
	}
	defer f.Close()
	t.Length = audio.TrackLength(f)

	m, err := tag.ReadFrom(f) // reads only the tag section, not the audio
	if err != nil {
		t.Title, t.NoTags = fallbackTitle, true
		return t
	}

	t.Title = strings.TrimSpace(m.Title())
	t.Artist = strings.TrimSpace(m.Artist())
	t.Album = strings.TrimSpace(m.Album())
	t.Year = m.Year()
	if t.Title == "" {
		t.Title = fallbackTitle
	}
	return t
}

// searchKey is what the search box matches against, folded (see Fold).
func searchKey(t Track) string {
	return Fold(t.Title + "\x00" + t.Artist + "\x00" + t.Album)
}

// Matches reports whether t contains every search word, in any field.
// The words are folded, as SearchWords gives them.
func Matches(t Track, words []string) bool {
	key := t.search
	if key == "" && len(words) > 0 {
		key = searchKey(t) // a song made by hand rather than read from a file
	}
	for _, w := range words {
		if !strings.Contains(key, w) {
			return false
		}
	}
	return true
}

// Less is the default order: artist, then album, then title.
func Less(a, b Track) bool {
	if c := CompareFold(a.Artist, b.Artist); c != 0 {
		return c < 0
	}
	if c := CompareFold(a.Album, b.Album); c != 0 {
		return c < 0
	}
	return CompareFold(a.Title, b.Title) < 0
}

func CompareFold(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}
