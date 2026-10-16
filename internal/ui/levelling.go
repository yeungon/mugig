package ui

// Volume levelling (Controls > Volume Levelling) plays every song at about
// the same loudness (see quality.Loudness). A song's loudness comes from
// its quality check, or else is measured in the background when it starts
// or is readied to follow, which takes under a second. Until then it plays
// at the gain of the last song levelled, as songs played together tend to
// be alike, and glides to its own when the measurement is in.
// Measurements are saved, so each song is measured once. Off unless turned
// on, as it changes the sound.

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"

	"mugig/internal/audio"
	"mugig/internal/quality"
)

const prefLevelling = "levelling"

// measureSlots limits the measurements made at once, so skipping through
// songs doesn't pile up reads of a slow disk.
var measureSlots = make(chan struct{}, 2)

// buildLevellingItem creates Controls > Volume Levelling, and applies the
// choice last made.
func (u *ui) buildLevellingItem() *fyne.MenuItem {
	u.levellingItem = fyne.NewMenuItem("Volume Levelling", func() {
		u.setLevelling(!u.levellingItem.Checked)
		u.mainMenu.Refresh()
	})
	u.setLevelling(u.prefs.Bool(prefLevelling))
	return u.levellingItem
}

// setLevelling turns levelling on or off, for the song playing too.
func (u *ui) setLevelling(on bool) {
	u.levellingItem.Checked = on
	u.prefs.SetBool(prefLevelling, on)
	if !on {
		u.player.SetLevelling(nil)
	} else {
		u.player.SetLevelling(u.levelGain)
		if u.playing.Path != "" {
			u.levelSong(u.playing.Path)
		}
	}
	if u.npOutput != nil {
		u.updateOutputInfo()
	}
}

// loudness returns how loud the song at path is, if known.
func (u *ui) loudness(path string) (quality.Loudness, bool) {
	file := audio.FileOf(path)
	return u.qualityStore.Loudness(path, file, quality.FileStamp(file))
}

// levelGain is the song at path's levelling gain in dB, for the player:
// while its loudness isn't known, that of the last song levelled.
func (u *ui) levelGain(path string) float64 {
	if l, ok := u.loudness(path); ok {
		u.lastGain = l.Gain()
	}
	return u.lastGain
}

// levelSong makes sure the song at path can be levelled: if levelling is
// on and its loudness isn't known, it is measured in the background, and
// the player told when it is.
func (u *ui) levelSong(path string) {
	if !u.levellingItem.Checked || u.measuring[path] || isLink(path) {
		return
	}
	if _, ok := u.loudness(path); ok {
		return
	}
	u.measuring[path] = true
	file := audio.FileOf(path)
	stamp := quality.FileStamp(file)
	u.async(func() {
		measureSlots <- struct{}{}
		l, err := quality.MeasureLoudness(context.Background(), path)
		<-measureSlots
		onUI(func() {
			delete(u.measuring, path)
			if err != nil {
				return // it doesn't play either, and playing says why
			}
			u.qualityStore.PutLoudness(path, stamp, l)
			u.player.Relevel(path)
			if u.playing.Path == path {
				u.updateOutputInfo()
			}
		})
	})
}

// levellingNote describes the playing song's levelling for Now Playing:
// "" when levelling is off.
func (u *ui) levellingNote() string {
	if !u.levellingItem.Checked || u.playing.Path == "" {
		return ""
	}
	if u.station != nil {
		return ", not levelled, as radio's loudness can't be measured ahead"
	}
	if l, ok := u.loudness(u.playing.Path); ok {
		if g := l.Gain(); g != 0 {
			return fmt.Sprintf(", levelled by %+.1f dB", g)
		}
		return ", at the level wanted already"
	}
	if u.measuring[u.playing.Path] {
		return ", measuring its loudness to level it"
	}
	return ""
}
