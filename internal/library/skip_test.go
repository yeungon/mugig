package library

import "testing"

func TestSkippedWithin(t *testing.T) {
	for p, want := range map[string]bool{
		"/m/a.mp3":                    false,
		"/m/Album/._a.mp3":            true,
		"/m/.Trashes/501/a.mp3":       true,
		"/m/.Trashes":                 true,
		"/m":                          false,
		"/m/node_modules":             true,
		"/m/x/node_modules/y/a.wav":   true,
		"/m/Game.app/Contents/a.ogg":  true,
		"/m/$RECYCLE.BIN/S-1-5/a.mp3": true,
		"/m/Apps and Plugins/a.mp3":   false,
	} {
		if got := skippedWithin(p, "/m"); got != want {
			t.Errorf("%s: %v", p, got)
		}
	}
}
