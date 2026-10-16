package ui

// Integration with the operating system's media controls. On macOS that is
// the media keys, Control Center and the lock screen (nowplaying_darwin.go);
// elsewhere it does nothing yet (nowplaying_other.go).

import "time"

// remoteCommand is a request from the OS media controls.
type remoteCommand int

const (
	remotePlay remoteCommand = iota + 1
	remotePause
	remoteToggle
	remoteNext
	remotePrevious
	remoteStop
	remoteSeek // to the position sent with it
)

type playState int

const (
	stateStopped playState = iota
	statePlaying
	statePaused
)

// nowPlayingInfo is what the OS shows about the current track.
type nowPlayingInfo struct {
	Title, Artist, Album string
	Duration, Elapsed    time.Duration
	Rate                 float64 // playback speed: 1 is normal
	Live                 bool    // a radio station, with no length
	State                playState
	Art                  []byte // encoded image, or nil
}

// mediaCenter receives what's playing so the OS can show it.
type mediaCenter interface {
	Update(nowPlayingInfo)
}

// publishNowPlaying tells the OS the current state. The OS works out the
// moving position itself, so this is only needed when something changes.
func (u *ui) publishNowPlaying() {
	if u.media == nil {
		return
	}
	if u.playing.Path == "" {
		u.media.Update(nowPlayingInfo{State: stateStopped})
		return
	}
	state := statePlaying
	if u.paused {
		state = statePaused
	}
	if n := u.station; n != nil { // the song on, if the station says, by the station
		info := nowPlayingInfo{Title: n.s.Name, Artist: n.line(), Rate: 1, State: state, Live: true}
		if n.title != "" {
			info.Title, info.Artist = n.title, n.s.Name
		}
		u.media.Update(info)
		return
	}
	u.media.Update(nowPlayingInfo{
		Title:    u.playing.Title,
		Artist:   u.playing.Artist,
		Album:    u.playing.Album,
		Duration: u.player.Duration(),
		Elapsed:  u.player.Position(),
		Rate:     u.speed,
		State:    state,
		Art:      u.artData,
	})
}

// remote handles a command from the OS media controls. Called on the UI
// thread.
func (u *ui) remote(cmd remoteCommand, pos time.Duration) {
	stopped := u.playing.Path == ""
	switch cmd {
	case remotePlay:
		if stopped || u.paused {
			u.togglePlay()
		}
	case remotePause:
		if !stopped && !u.paused {
			u.togglePlay()
		}
	case remoteToggle:
		u.togglePlay()
	case remoteNext:
		if !stopped {
			u.next()
		}
	case remotePrevious:
		if !stopped {
			u.prev()
		}
	case remoteStop:
		u.stop()
	case remoteSeek:
		u.seekTo(pos)
	}
}
