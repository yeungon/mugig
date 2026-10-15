package ui

// The Quality column: each song's saved verdict, in the song list.

import (
	"mugig/internal/audio"
	"mugig/internal/library"
	"mugig/internal/quality"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// lossyFormats need no check to be called lossy: the format says it.
var lossyFormats = map[string]bool{"MP3": true, "OGG": true}

// levelOf returns t's verdict: saved from a check, or Lossy by its format.
//
// Verdicts are kept by file, so the songs of a disc image share its one.
func (u *ui) levelOf(t library.Track) quality.Level {
	if l, ok := u.levels[audio.FileOf(t.Path)]; ok {
		return l
	}
	if lossyFormats[t.Format] {
		return quality.Lossy
	}
	return quality.Unknown
}

// levelColours match the badges, in text-safe colours.
var levelColours = map[quality.Level]fyne.ThemeColorName{
	quality.Lossy:    theme.ColorNameDisabled,
	quality.Lossless: colorNameAccent,
	quality.HiRes:    colorNameAccent,
	quality.Suspect:  theme.ColorNameWarning,
	quality.Damaged:  theme.ColorNameError,
}

// setLevel records a song's new verdict and shows it.
func (u *ui) setLevel(path string, l quality.Level) {
	u.levels[audio.FileOf(path)] = l
	u.table.Refresh()
}
