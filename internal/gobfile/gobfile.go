// Package gobfile saves values to disk in Go's gob encoding, safely.
package gobfile

import (
	"bufio"
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
)

// Save writes v to a temporary file and renames it into place, so a
// crash mid-write can't leave a half-written file.
func Save(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, strings.TrimSuffix(filepath.Base(path), ".gob")+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	w := bufio.NewWriter(tmp)
	err = gob.NewEncoder(w).Encode(v)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Load reads what Save wrote at path into v.
func Load(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewDecoder(bufio.NewReader(f)).Decode(v)
}
