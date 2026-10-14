package main

// CUE sheets. A CD is often ripped to one big file, a disc image, with a
// .cue file beside it saying where each song starts. Each song in the
// sheet becomes a song in the library, named after the image with its
// number, as "Album.wav#03", and plays its stretch of the image. The image
// itself is then left out of the list. Songs follow each other on the
// very sample, so the album plays without gaps, as on the CD.
//
// Sheets made on Chinese, Japanese or Western Windows are rarely UTF-8.
// The encoding is found by trying each likely one until the file the
// sheet names is found on disk.

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gopxl/beep/v2"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// cueEncodings are tried in turn for a sheet that isn't UTF-8.
var cueEncodings = []encoding.Encoding{
	simplifiedchinese.GB18030, japanese.ShiftJIS, traditionalchinese.Big5, korean.EUCKR, charmap.Windows1252,
}

func isCueFile(path string) bool {
	return !hidden(path) && strings.EqualFold(filepath.Ext(path), ".cue")
}

// cueSheet is what a sheet says about one file it names.
type cueSheet struct {
	album, performer string
	year             int
	file             string // the image's path
	tracks           []cueTrack
}

type cueTrack struct {
	number           int
	title, performer string
	start            time.Duration // INDEX 01: the gap before a song stays with the song before
}

// readCue reads the sheet at path, and returns the images it names that
// are on disk and hold two songs or more.
func readCue(path string) ([]cueSheet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF")) // UTF-8's byte order mark
	dir := filepath.Dir(path)
	texts := []string{string(raw)}
	if !utf8.Valid(raw) {
		texts = texts[:0]
		for _, enc := range cueEncodings {
			if b, err := enc.NewDecoder().Bytes(raw); err == nil {
				texts = append(texts, string(b))
			}
		}
	}
	// The first reading whose files are on disk is the right one. With
	// none, a sheet named like the one image beside it still names it.
	for _, text := range texts {
		if sheets := parseCue(text, dir); len(sheets) > 0 && allFound(sheets) {
			return keep(sheets), nil
		}
	}
	if len(texts) > 0 {
		sheets := parseCue(texts[0], dir)
		if len(sheets) == 1 {
			if image := imageBeside(path); image != "" {
				sheets[0].file = image
				return keep(sheets), nil
			}
		}
	}
	return nil, nil
}

func allFound(sheets []cueSheet) bool {
	for _, s := range sheets {
		if fi, err := os.Stat(s.file); err != nil || fi.IsDir() {
			return false
		}
	}
	return true
}

// keep leaves out files with fewer than two songs: those are songs of
// their own already, as in sheets kept from a rip split into tracks.
func keep(sheets []cueSheet) []cueSheet {
	var out []cueSheet
	for _, s := range sheets {
		if len(s.tracks) >= 2 && isAudioFile(s.file) {
			out = append(out, s)
		}
	}
	return out
}

// imageBeside finds the audio file named like the sheet, as "Album.wav"
// for "Album.cue".
func imageBeside(cue string) string {
	base := strings.TrimSuffix(cue, filepath.Ext(cue))
	for ext := range audioExts {
		for _, e := range []string{ext, strings.ToUpper(ext)} {
			if fi, err := os.Stat(base + e); err == nil && !fi.IsDir() {
				return base + e
			}
		}
	}
	return ""
}

// parseCue reads a sheet's text: the album's details, then for each FILE
// the songs in it.
func parseCue(text, dir string) []cueSheet {
	var album, performer string
	var year int
	var sheets []cueSheet
	var cur *cueTrack
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		cmd, rest := cueWord(strings.TrimSpace(sc.Text()))
		switch strings.ToUpper(cmd) {
		case "REM":
			key, value := cueWord(rest)
			if strings.EqualFold(key, "DATE") {
				if y, err := strconv.Atoi(strings.TrimSpace(value)[:min(4, len(strings.TrimSpace(value)))]); err == nil {
					year = y
				}
			}
		case "TITLE":
			if cur != nil {
				cur.title = cueString(rest)
			} else {
				album = cueString(rest)
			}
		case "PERFORMER":
			if cur != nil {
				cur.performer = cueString(rest)
			} else {
				performer = cueString(rest)
			}
		case "FILE":
			name := cueFileName(rest)
			name = strings.ReplaceAll(name, `\`, string(filepath.Separator)) // made on Windows
			if !filepath.IsAbs(name) {
				name = filepath.Join(dir, name)
			}
			sheets = append(sheets, cueSheet{file: name})
			cur = nil
		case "TRACK":
			if len(sheets) == 0 {
				continue
			}
			num, _ := cueWord(rest)
			n, _ := strconv.Atoi(num)
			s := &sheets[len(sheets)-1]
			s.tracks = append(s.tracks, cueTrack{number: n, start: -1})
			cur = &s.tracks[len(s.tracks)-1]
		case "INDEX":
			num, at := cueWord(rest)
			if cur != nil && num == "01" {
				if d, ok := cueTime(strings.TrimSpace(at)); ok {
					cur.start = d
				}
			}
		}
	}
	for i := range sheets {
		s := &sheets[i]
		s.album, s.performer, s.year = album, performer, year
		var ok []cueTrack
		for _, t := range s.tracks {
			if t.start >= 0 && (len(ok) == 0 || t.start > ok[len(ok)-1].start) {
				ok = append(ok, t)
			}
		}
		s.tracks = ok
	}
	return sheets
}

// cueWord splits off the first word.
func cueWord(s string) (word, rest string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// cueString reads a value, in quotes or not.
func cueString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' {
		if j := strings.LastIndexByte(s, '"'); j > 0 {
			return strings.TrimSpace(s[1:j])
		}
	}
	return s
}

// cueFileName reads FILE's name, leaving off the file type after it.
func cueFileName(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		return cueString(s)
	}
	if i := strings.LastIndexAny(s, " \t"); i > 0 {
		return strings.TrimSpace(s[:i]) // the type: WAVE, MP3…
	}
	return s
}

// cueTime reads mm:ss:ff, with 75 frames a second.
func cueTime(s string) (time.Duration, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0, false
		}
		n[i] = v
	}
	frames := (n[0]*60+n[1])*75 + n[2]
	return time.Duration(frames) * time.Second / 75, true
}

// cueSongs makes the songs of a sheet, given the image's own track, read
// from its tags, for its length, size and format.
func cueSongs(s cueSheet, image Track) []Track {
	var out []Track
	for i, ct := range s.tracks {
		end := image.Length
		if i+1 < len(s.tracks) {
			end = s.tracks[i+1].start
		}
		t := Track{
			Path:   cuePath(s.file, ct.number),
			File:   s.file,
			Start:  ct.start,
			End:    end,
			Title:  ct.title,
			Artist: ct.performer,
			Album:  s.album,
			Year:   s.year,
			Format: image.Format,
		}
		if t.Artist == "" {
			t.Artist = s.performer
		}
		if t.Album == "" {
			t.Album = image.Album
		}
		if t.Year == 0 {
			t.Year = image.Year
		}
		if t.Title == "" {
			t.Title = fmt.Sprintf("Track %02d", ct.number)
		}
		if end > ct.start {
			t.Length = end - ct.start
			if image.Length > 0 {
				t.Size = int64(float64(image.Size) * float64(t.Length) / float64(image.Length))
			}
		}
		t.search = searchKey(t)
		out = append(out, t)
	}
	return out
}

// cuePath names a song of an image: the image's path and the song's
// number.
func cuePath(image string, number int) string {
	return fmt.Sprintf("%s#%02d", image, number)
}

// Songs from CUE sheets are known by their path everywhere, but playing
// one, or anything else that reads it, needs its image and where in it
// the song is. The scan records them here.
var cueSpans sync.Map // song path → cueSpan

type cueSpan struct {
	file       string
	start, end time.Duration // end 0 for the end of the file
}

// rememberCue records where a song from a CUE sheet is.
func rememberCue(t Track) {
	if t.File != "" {
		cueSpans.Store(t.Path, cueSpan{t.File, t.Start, t.End})
	}
}

// spanOf returns where the song at path is: its image and the stretch of
// it, or for any other song, the file itself, whole.
func spanOf(path string) cueSpan {
	if v, ok := cueSpans.Load(path); ok {
		return v.(cueSpan)
	}
	return cueSpan{file: path}
}

// fileOf is the file a song is in: its image, for a song from a CUE sheet.
func fileOf(path string) string { return spanOf(path).file }

// fromCue reports whether the song at path is part of an image.
func fromCue(path string) bool { return spanOf(path).file != path }

// segment plays a stretch of s, as if it were all there is.
type segment struct {
	beep.StreamSeekCloser
	skip, n int
}

// cut narrows s, of format f, to the span, if it is a part of the file.
func (sp cueSpan) cut(s beep.StreamSeekCloser, f beep.Format) (beep.StreamSeekCloser, error) {
	if sp.start == 0 && sp.end == 0 {
		return s, nil
	}
	skip := min(samplesAt(sp.start, f.SampleRate), s.Len())
	n := s.Len() - skip
	if sp.end > 0 {
		n = min(samplesAt(sp.end, f.SampleRate)-skip, n)
	}
	if err := s.Seek(skip); err != nil {
		return nil, err
	}
	return &segment{StreamSeekCloser: s, skip: skip, n: max(n, 0)}, nil
}

// samplesAt is the sample at d, rounded: a CUE sheet's times, in 75ths of
// a second, fall on whole samples at every usual rate, but held as
// nanoseconds they are a fraction short, and beep's N would truncate that
// to the sample before, overlapping the song before by one.
func samplesAt(d time.Duration, rate beep.SampleRate) int {
	return int((int64(d)*int64(rate) + int64(time.Second)/2) / int64(time.Second))
}

func (g *segment) Stream(samples [][2]float64) (int, bool) {
	left := g.n - g.Position()
	if left <= 0 {
		return 0, false
	}
	return g.StreamSeekCloser.Stream(samples[:min(len(samples), left)])
}

func (g *segment) Len() int      { return g.n }
func (g *segment) Position() int { return max(g.StreamSeekCloser.Position()-g.skip, 0) }
func (g *segment) Seek(p int) error {
	return g.StreamSeekCloser.Seek(min(max(p, 0), g.n) + g.skip)
}

// scanCue reads the sheet at path, or takes its songs from the cache if
// neither it nor its images have changed.
func scanCue(cache *libraryCache, path string) scanResult {
	r := scanResult{path: path}
	fi, err := os.Stat(path)
	if err != nil {
		return r
	}
	r.size, r.modTime = fi.Size(), fi.ModTime().UnixNano()
	if e, ok := cache.entry(path, r.size, r.modTime); ok && imagesUnchanged(e.Images) {
		r.cue, r.images, r.cached = e.Cue, e.Images, true
	} else {
		sheets, _ := readCue(path)
		r.images = map[string][2]int64{}
		for _, s := range sheets {
			image := scanFile(cache, s.file)
			if image.modTime == 0 {
				continue
			}
			r.images[s.file] = [2]int64{image.size, image.modTime}
			r.cue = append(r.cue, cueSongs(s, image.track)...)
		}
	}
	for _, t := range r.cue {
		rememberCue(t)
	}
	return r
}

// imagesUnchanged reports whether each image still has the size and
// modification time recorded.
func imagesUnchanged(images map[string][2]int64) bool {
	for path, stamp := range images {
		fi, err := os.Stat(path)
		if err != nil || fi.Size() != stamp[0] || fi.ModTime().UnixNano() != stamp[1] {
			return false
		}
	}
	return true
}

// dropImages leaves out of songs the disc images whose songs, from their
// CUE sheets, are listed instead.
func dropImages(songs []Track) []Track {
	if images := cueImages(songs); len(images) > 0 {
		return withoutImages(songs, images)
	}
	return songs
}

// cueImages are the disc images that songs from CUE sheets are in.
func cueImages(songs []Track) map[string]bool {
	images := map[string]bool{}
	for _, t := range songs {
		if t.File != "" {
			images[t.File] = true
		}
	}
	return images
}

func withoutImages(songs []Track, images map[string]bool) []Track {
	return slices.DeleteFunc(songs, func(t Track) bool { return t.File == "" && images[t.Path] })
}

// songFile is the file t is in: its own, or its disc image.
func songFile(t Track) string {
	if t.File != "" {
		return t.File
	}
	return t.Path
}

// hasCueBeside reports whether the folder holding path has a CUE sheet,
// whose songs a change to path may change.
func hasCueBeside(path string) bool {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool { return isCueFile(e.Name()) })
}
