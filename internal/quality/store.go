package quality

import (
	"crypto/sha256"
	hexenc "encoding/hex"
	"image"
	"mugig/internal/gobfile"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// qualityStore keeps check results on disk, so each file is checked only
// once. Entries are tied to the file's size and modification time, so an
// edited file is checked afresh.
type Store struct {
	mu       sync.Mutex
	path     string // where it is saved; "" keeps it in memory only
	entries  map[string]Entry
	pictures map[string][]byte // spectrograms, when kept in memory
}

type Entry struct {
	Stamp  string
	Report Report
	Level  Level // the verdict, for the song list's Quality column
}

// qualityVersion is bumped whenever qualityReport changes shape or the
// analysis measures differently, so old results are checked again.
const qualityVersion = 5

type qualityFile struct {
	Version int
	Entries map[string]Entry
}

// loadQualityStore reads the results saved at path. A missing, corrupt or
// outdated file just gives an empty store.
func LoadStore(path string) *Store {
	s := &Store{path: path, entries: map[string]Entry{}, pictures: map[string][]byte{}}
	var f qualityFile
	if path != "" && gobfile.Load(path, &f) == nil && f.Version == qualityVersion && f.Entries != nil {
		s.entries = f.Entries
	}
	s.dropOldPictures()
	return s
}

// lookup returns the saved entry for path, if the file is unchanged.
func (s *Store) Lookup(path, stamp string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[path]
	return e, ok && e.Stamp == stamp && stamp != ""
}

// get fills rep with the saved result for path, if the file is unchanged.
func (s *Store) Get(path, stamp string, rep *Report) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[path]
	if !ok || e.Stamp != stamp || stamp == "" {
		return false
	}
	*rep = e.Report
	return true
}

// put records a result and saves the store. Saving errors are ignored: the
// result is still remembered for this session.
func (s *Store) Put(path, stamp string, rep Report, level Level) {
	s.PutQuietly(path, stamp, rep, level)
	s.Save()
}

// putQuietly records a result without saving, for checking many files at
// once: saving after each would rewrite the whole store thousands of
// times. Call save now and then, and at the end.
func (s *Store) PutQuietly(path, stamp string, rep Report, level Level) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[path] = Entry{stamp, rep, level}
}

func (s *Store) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		gobfile.Save(s.path, qualityFile{Version: qualityVersion, Entries: s.entries})
	}
}

// forget drops the result for a file that has changed or gone.
func (s *Store) Forget(paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range paths {
		delete(s.entries, p)
	}
}

// levels returns every saved verdict, by path. Whether each file is still
// unchanged isn't checked here: that would mean reading thousands of files
// at startup. The watcher drops results for files that change.
func (s *Store) Levels() map[string]Level {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Level, len(s.entries))
	for p, e := range s.entries {
		out[p] = e.Level
	}
	return out
}

// Spectrograms are kept as PNG files of their levels beside the store,
// named after a hash of the track's path, and only read when shown.
func (s *Store) PicturePath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(s.pictureDir(), hexenc.EncodeToString(sum[:12])+".png")
}

func (s *Store) pictureDir() string {
	return filepath.Join(filepath.Dir(s.path), "spectrograms")
}

// dropOldPictures deletes spectrograms saved as coloured JPEGs by earlier
// versions, which can't be drawn for another range of levels. Their tracks
// are analysed again when next shown.
func (s *Store) dropOldPictures() {
	if s.path == "" {
		return
	}
	old, _ := filepath.Glob(filepath.Join(s.pictureDir(), "*.jpg"))
	for _, f := range old {
		os.Remove(f)
	}
}

// putPicture saves the spectrogram for path. Saving errors are ignored, as
// for put.
func (s *Store) PutPicture(path string, img *image.Gray) {
	if img == nil {
		return
	}
	data, err := encodePicture(img)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		s.pictures[path] = data
		return
	}
	file := s.PicturePath(path)
	if os.MkdirAll(filepath.Dir(file), 0o755) == nil {
		os.WriteFile(file, data, 0o644)
	}
}

// picture returns the saved spectrogram for path.
func (s *Store) Picture(path string) (*image.Gray, bool) {
	s.mu.Lock()
	data, ok := s.pictures[path]
	s.mu.Unlock()
	if !ok && s.path != "" {
		var err error
		data, err = os.ReadFile(s.PicturePath(path))
		ok = err == nil
	}
	if !ok {
		return nil, false
	}
	return decodePicture(data)
}

func FileStamp(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return st.ModTime().Format(time.RFC3339Nano) + "/" + strconv.FormatInt(st.Size(), 10)
}
