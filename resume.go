package main

// Picking up where the user left off. On quit, Mugig remembers the song
// that was playing, how far into it, and Up Next. On the next launch, once
// the library has been scanned, the song waits in the player bar at that
// point, paused; Play carries on from there. Nothing plays by itself.

import (
	"mugig/internal/humanize"
	"mugig/internal/library"
	"time"

	"fyne.io/fyne/v2/widget"
)

// Preference keys for the session.
const (
	prefResumeSong = "resumeSong"     // path of the song playing at quit
	prefResumeAt   = "resumePosition" // seconds into it
	prefUpNext     = "upNext"         // paths of the songs in Up Next
)

// cuedSong is a song waiting in the player bar to be resumed.
type cuedSong struct {
	track library.Track
	at    time.Duration
}

// saveSession remembers the song playing, or waiting to be resumed, and
// how far into it. It is called on quit and when playback pauses.
func (u *ui) saveSession() {
	song, at := u.playing.Path, u.player.Position()
	if song == "" && u.cued != nil {
		song, at = u.cued.track.Path, u.cued.at
	}
	u.prefs.SetString(prefResumeSong, song)
	u.prefs.SetFloat(prefResumeAt, at.Seconds())
}

// saveQueue remembers Up Next. It waits until the saved one has been
// restored, so as not to overwrite it at startup.
func (u *ui) saveQueue() {
	if u.resumePending {
		return
	}
	paths := make([]string, len(u.queued))
	for i, t := range u.queued {
		paths[i] = t.Path
	}
	u.prefs.SetStringList(prefUpNext, paths)
}

// restoreSession brings back the last session, once, after the first
// scans: Up Next, and the song in the player bar where it was left. Songs
// no longer in the library are left out. If something is already playing,
// it carries on, and Up Next's saved songs go before any queued since.
func (u *ui) restoreSession() {
	if !u.resumePending {
		return
	}
	u.resumePending = false
	var saved []library.Track
	for _, p := range u.prefs.StringList(prefUpNext) {
		if t, ok := u.inLibrary(p); ok {
			saved = append(saved, t)
		}
	}
	u.queued = append(saved, u.queued...)
	u.queueChanged()
	if u.playing.Path != "" {
		return
	}
	if t, ok := u.inLibrary(u.prefs.String(prefResumeSong)); ok {
		at := time.Duration(u.prefs.Float(prefResumeAt) * float64(time.Second))
		if t.Length > 0 && at >= t.Length {
			at = 0
		}
		u.cue(t, max(at, 0))
	}
}

// cue shows t in the player bar, paused at at, ready for Play.
func (u *ui) cue(t library.Track, at time.Duration) {
	u.cued = &cuedSong{t, at}
	u.nowTitle.SetText(t.Title)
	u.nowArtist.SetText(t.Artist)
	if t.Length > 0 {
		u.seek.Max = t.Length.Seconds()
		u.seek.Enable()
		u.total.SetText(humanize.Clock(t.Length))
	}
	u.sizeTimes(t.Length)
	u.setSeek(at)
	if i := u.indexOf(t.Path); i >= 0 {
		u.table.ScrollTo(widget.TableCellID{Row: i, Col: 0})
	}
	u.async(func() {
		data := library.CoverArt(t.Path)
		onUI(func() {
			if u.cued != nil && u.cued.track.Path == t.Path {
				u.showArt(data)
			}
		})
	})
}

// playCued starts the cued song where it was left.
func (u *ui) playCued() {
	c := u.cued
	u.cued = nil
	u.jump(c.track)
	if c.at > 0 && u.playing.Path == c.track.Path {
		u.seekTo(c.at)
	}
}
