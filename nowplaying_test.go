package main

import (
	"testing"
	"time"
)

func TestNowPlayingFollowsPlayback(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	u.togglePlay()
	got := lastMedia(u)
	if got.State != statePlaying || got.Title != "Come Together" || got.Artist != "Beatles" ||
		got.Album != "Abbey Road" || got.Duration != 3*time.Minute {
		t.Errorf("playing: %+v", got)
	}

	fp.pos = 42 * time.Second
	u.togglePlay()
	if got := lastMedia(u); got.State != statePaused || got.Elapsed != 42*time.Second {
		t.Errorf("paused: %+v", got)
	}
	u.seekTo(time.Minute)
	if got := lastMedia(u); got.Elapsed != time.Minute || got.State != statePaused {
		t.Errorf("after seek: %+v", got)
	}
	u.stop()
	if got := lastMedia(u); got.State != stateStopped {
		t.Errorf("stopped: %+v", got)
	}
}

func TestRemoteCommands(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)

	u.remote(remoteNext, 0)
	if len(fp.played) != 0 {
		t.Error("next while stopped should do nothing")
	}
	u.remote(remotePlay, 0)
	if fp.last() != "Come Together" || u.paused {
		t.Fatal("play while stopped should start playback")
	}
	u.remote(remotePlay, 0)
	if fp.paused || len(fp.played) != 1 {
		t.Error("play while playing should change nothing")
	}
	u.remote(remotePause, 0)
	u.remote(remotePause, 0)
	if !fp.paused || !u.paused {
		t.Error("pause (twice) should leave it paused")
	}
	u.remote(remotePlay, 0)
	if fp.paused {
		t.Error("play should resume")
	}
	u.remote(remoteToggle, 0)
	if !fp.paused {
		t.Error("toggle should pause")
	}
	u.remote(remoteSeek, 90*time.Second)
	if fp.pos != 90*time.Second || u.seek.Value != 90 {
		t.Errorf("seek: player at %v, bar at %v", fp.pos, u.seek.Value)
	}
	u.remote(remoteNext, 0)
	if fp.last() != "Help" {
		t.Errorf("next played %q", fp.last())
	}
	u.remote(remotePrevious, 0)
	if fp.last() != "Come Together" {
		t.Errorf("previous played %q", fp.last())
	}
	u.remote(remoteStop, 0)
	if u.playing.Path != "" {
		t.Error("stop should stop")
	}
}

func TestSeekIsClamped(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.togglePlay()

	u.seekTo(time.Hour)
	if fp.pos != 3*time.Minute {
		t.Errorf("seek past the end went to %v", fp.pos)
	}
	u.seekTo(-time.Minute)
	if fp.pos != 0 {
		t.Errorf("seek before the start went to %v", fp.pos)
	}
}
