package library

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// M3U playlists, for other players: the extended form, in UTF-8 (.m3u8),
// with each song's length and "Artist - Title" before its path. Songs in
// the folder the playlist is saved in, or below it, are written relative
// to it, so a playlist kept on a USB disk works wherever the disk is
// plugged in; others are written in full.
//
// A song from a CUE sheet can't be written: M3U can only name whole
// files, and its image would play the whole album.

// SaveM3U writes songs to path as an M3U playlist named name. It returns
// how many songs it left out, being from CUE sheets.
func SaveM3U(path, name string, songs []Track) (skipped int, err error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "#EXTM3U\n#PLAYLIST:%s\n", oneLine(name))
	dir := filepath.Dir(path)
	for _, t := range songs {
		if t.File != "" {
			skipped++
			continue
		}
		secs := -1 // unknown, as the format has it
		if t.Length > 0 {
			secs = int(math.Round(t.Length.Seconds()))
		}
		title := t.Title
		if t.Artist != "" {
			title = t.Artist + " - " + t.Title
		}
		fmt.Fprintf(w, "#EXTINF:%d,%s\n%s\n", secs, oneLine(title), m3uPath(dir, t.Path))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return skipped, err
	}
	return skipped, f.Close()
}

// m3uPath is path as the playlist in dir names it: relative if it is in
// dir or below, in full otherwise.
func m3uPath(dir, path string) string {
	if Within(path, dir) {
		if rel, err := filepath.Rel(dir, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return path
}

// oneLine keeps a title on its line: a line break in it would end the
// entry early.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
