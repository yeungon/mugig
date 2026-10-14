package main

import (
	"strings"
	"testing"
)

// The Equalizer window: a slider is heard at once and saved when let go,
// presets set all the bands, the boxes take a band's frequency and width,
// and the switch turns it all off.
func TestEqualizerWindow(t *testing.T) {
	u, fp, a := newTestUI(t)
	if fp.eq.On || !fp.eq.flat() || fp.eq.Bands[5].Freq != 1000 {
		t.Fatalf("starts %+v, want off and flat", fp.eq)
	}
	setLibrary(u, fiveTracks()...)
	u.togglePlay()
	u.showEqualizer()
	v := u.eqView
	if v == nil {
		t.Fatal("no window")
	}
	t.Cleanup(func() { // closing a test window twice hangs the test driver
		if u.eqView != nil {
			u.eqView.win.Close()
		}
	})

	v.gains[1].Value = 4.5 // dragged, and not yet let go
	v.gains[1].OnChanged(4.5)
	if !fp.eq.On || fp.eq.Bands[1].Gain != 4.5 {
		t.Errorf("player has %+v", fp.eq.Bands[1])
	}
	if a.Preferences().String(prefEQ) != "" {
		t.Error("saved while still dragging")
	}
	v.gains[1].OnChangeEnded(4.5)
	if !strings.Contains(a.Preferences().String(prefEQ), `"Gain":4.5`) {
		t.Errorf("saved %q", a.Preferences().String(prefEQ))
	}
	if v.values[1].Text != "+4.5" || v.preset.Selected != "" || !strings.Contains(v.headroom.Text, "Turned down 4.") {
		t.Errorf("shows %q, preset %q, %q", v.values[1].Text, v.preset.Selected, v.headroom.Text)
	}
	if !strings.Contains(u.npOutput.Text, "with the equalizer") {
		t.Errorf("Now Playing says %q", u.npOutput.Text)
	}

	v.preset.SetSelected("Treble Boost")
	if g := fp.eq.Bands; g[1].Gain != 0 || g[9].Gain != 6 || v.gains[9].Value != 6 {
		t.Errorf("preset gave %+v", g)
	}

	v.freqs[5].SetText("1.5k")
	v.qs[5].SetText("2")
	if b := fp.eq.Bands[5]; b.Freq != 1500 || b.Q != 2 {
		t.Errorf("band %+v", b)
	}
	if v.preset.Selected != "" {
		t.Error("a band moved off its usual frequency is no longer the preset")
	}
	v.freqs[5].SetText("lots")
	v.qs[5].SetText("0")
	if b := fp.eq.Bands[5]; b.Freq != 1500 || b.Q != 2 {
		t.Errorf("nonsense taken: %+v", b)
	}

	v.on.SetChecked(false)
	if fp.eq.On || fp.eq.Bands[9].Gain != 6 {
		t.Errorf("off: %+v", fp.eq)
	}
	if !strings.HasSuffix(u.npOutput.Text, "unconverted") || !strings.HasPrefix(v.headroom.Text, "Off") {
		t.Errorf("off: %q, %q", u.npOutput.Text, v.headroom.Text)
	}

	// Remembered, and the window is opened once.
	fp2 := &fakePlayer{}
	newUI(u.win, a.Preferences(), uiDeps{player: fp2, cache: u.cache})
	if fp2.eq.On || fp2.eq.Bands[9].Gain != 6 || fp2.eq.Bands[5].Freq != 1500 {
		t.Errorf("restored %+v", fp2.eq)
	}
	u.showEqualizer()
	if u.eqView != v {
		t.Error("a second window opened")
	}
	v.win.Close()
	if u.eqView != nil {
		t.Error("closing should forget the window")
	}
}

func TestParseFreq(t *testing.T) {
	for in, want := range map[string]float64{"62": 62, "1500": 1500, "1.5k": 1500, "1.5 kHz": 1500, " 16K ": 16000, "20000": 20000} {
		if got, ok := parseFreq(in); !ok || got != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "abc", "10", "25k", "-5"} {
		if _, ok := parseFreq(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}
