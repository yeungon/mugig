package main

// The Quality column: each song's saved verdict, in the song list.

import (
	"mugig/internal/audio"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// lossyFormats need no check to be called lossy: the format says it.
var lossyFormats = map[string]bool{"MP3": true, "OGG": true}

// levelOf returns t's verdict: saved from a check, or Lossy by its format.
//
// Verdicts are kept by file, so the songs of a disc image share its one.
func (u *ui) levelOf(t Track) qualityLevel {
	if l, ok := u.levels[audio.FileOf(t.Path)]; ok {
		return l
	}
	if lossyFormats[t.Format] {
		return qualityLossy
	}
	return qualityUnknown
}

// levelNames are the column's words for each verdict; unchecked songs stay
// blank.
var levelNames = map[qualityLevel]string{
	qualityLossy:    "Lossy",
	qualityLossless: "Lossless",
	qualityHiRes:    "Hi-Res",
	qualitySuspect:  "Lossless?",
	qualityDamaged:  "Damaged",
}

// levelColours match the badges, in text-safe colours.
var levelColours = map[qualityLevel]fyne.ThemeColorName{
	qualityLossy:    theme.ColorNameDisabled,
	qualityLossless: colorNameAccent,
	qualityHiRes:    colorNameAccent,
	qualitySuspect:  theme.ColorNameWarning,
	qualityDamaged:  theme.ColorNameError,
}

// levelRank orders the column so problems sort first and unchecked songs
// last.
var levelRank = map[qualityLevel]int{
	qualityDamaged:  0,
	qualitySuspect:  1,
	qualityLossy:    2,
	qualityLossless: 3,
	qualityHiRes:    4,
	qualityUnknown:  5,
}

// setLevel records a song's new verdict and shows it.
func (u *ui) setLevel(path string, l qualityLevel) {
	u.levels[audio.FileOf(path)] = l
	u.table.Refresh()
}
