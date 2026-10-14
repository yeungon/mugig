package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

// coverNames are image file names (without extension, lower case) commonly
// used for album art saved next to the tracks.
var coverNames = map[string]bool{
	"cover": true, "folder": true, "front": true, "album": true, "albumart": true,
}

// coverArt finds a picture for the track at path: one embedded in its tags,
// or else a cover image in its folder. It returns nil if there is none.
func coverArt(path string) []byte {
	if f, err := os.Open(path); err == nil {
		m, err := tag.ReadFrom(f)
		f.Close()
		if err == nil {
			if p := m.Picture(); p != nil && len(p.Data) > 0 {
				return p.Data
			}
		}
	}

	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		ext := filepath.Ext(name)
		if (ext == ".jpg" || ext == ".jpeg" || ext == ".png") && coverNames[strings.TrimSuffix(name, ext)] {
			if data, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil {
				return data
			}
		}
	}
	return nil
}
