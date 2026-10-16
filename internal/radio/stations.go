package radio

// The stations the user has added, saved as JSON next to the playlists.

import (
	"encoding/json"
	"os"
)

// Station is a radio station in the sidebar.
type Station struct {
	Name string
	Link string
	// Format describes its sound when last played, such as "AAC · 96
	// kbps"; "" until then.
	Format string `json:",omitempty"`
}

type stationFile struct {
	Version  int
	Stations []*Station
}

// LoadStations reads the stations saved at path: none if there are none,
// or the file can't be read.
func LoadStations(path string) []*Station {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f stationFile
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	return f.Stations
}

// SaveStations writes the stations to path, through a temporary file so a
// crash can't leave half a file. An empty path saves nothing.
func SaveStations(path string, stations []*Station) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(stationFile{Version: 1, Stations: stations}, "", "\t")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
