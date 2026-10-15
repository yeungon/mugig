package audio

import (
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeSystem stands in for macOS: devices whose own rate can be read and
// switched, and taken for Mugig alone. Nothing real changes.
type fakeSystem struct {
	mu       sync.Mutex
	rates    map[string]float64 // each device's rate, by UID ("" the default)
	hogged   uint32             // the device taken, 0 for none
	hogFails bool               // another app has the device
	unhogs   int
}

func (f *fakeSystem) system() system {
	return system{
		rate: func(uid string) float64 {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.rates[uid]
		},
		setRate: func(uid string, rate float64) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.rates[uid] = rate
			return true
		},
		hog: func(string) uint32 {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.hogFails {
				return 0
			}
			f.hogged = 7
			return 7
		},
		unhog: func(dev uint32) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if dev != 0 {
				f.hogged = 0
				f.unhogs++
			}
		},
	}
}

func (f *fakeSystem) rate() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rates[""]
}

func (f *fakeSystem) hog() uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hogged
}

// fakePlayer is a test player whose device rate and exclusive access are
// fake, the default output starting at 48 kHz.
func fakePlayer(t *testing.T) (*Player, *fakeSystem) {
	t.Helper()
	p := testPlayer(t)
	fs := &fakeSystem{rates: map[string]float64{"": 48000}}
	p.sys = fs.system()
	return p, fs
}

// With rate matching on, the device follows each song's rate, and is put
// back as it was when matching is turned off; its first rate is the one
// kept, not one Mugig set.
func TestPlayerSwitchesAndRestoresDeviceRate(t *testing.T) {
	p, fs := fakePlayer(t)
	hires, cd := longFLAC(t, 96000, 24, 3), longFLAC(t, 44100, 16, 3)

	if err := p.Play(hires, func(error) {}); err != nil {
		t.Fatal(err)
	}
	if fs.rate() != 48000 {
		t.Errorf("matching off changed the device to %v", fs.rate())
	}

	p.SetMatchRate(true) // reopens the playing song at its own rate
	if fs.rate() != 96000 {
		t.Errorf("device at %v, want the song's 96000", fs.rate())
	}
	if err := p.Play(cd, func(error) {}); err != nil {
		t.Fatal(err)
	}
	if fs.rate() != 44100 {
		t.Errorf("device at %v, want the song's 44100", fs.rate())
	}
	p.SetMatchRate(false)
	if fs.rate() != 48000 {
		t.Errorf("device left at %v, want its own 48000 back", fs.rate())
	}
}

// Closing the player, as quitting does, puts the device's rate back too.
func TestPlayerCloseRestoresDeviceRate(t *testing.T) {
	p, fs := fakePlayer(t)
	p.SetMatchRate(true)
	if err := p.Play(longFLAC(t, 96000, 24, 3), func(error) {}); err != nil {
		t.Fatal(err)
	}
	if fs.rate() != 96000 {
		t.Fatalf("device at %v, want 96000", fs.rate())
	}
	p.Close()
	if fs.rate() != 48000 {
		t.Errorf("device left at %v after closing, want 48000", fs.rate())
	}
}

// Exclusive access is taken while a song plays or is paused, and given back
// on Stop, at the end of the songs, and when turned off. If another app has
// the device, the song still plays, without it.
func TestPlayerExclusiveAccess(t *testing.T) {
	p, fs := fakePlayer(t)
	p.SetExclusive(true)
	if fs.hog() != 0 || p.Exclusive() {
		t.Error("nothing plays: the device shouldn't be taken yet")
	}
	song := longFLAC(t, 44100, 16, 3)
	if err := p.Play(song, func(error) {}); err != nil {
		t.Fatal(err)
	}
	if fs.hog() == 0 || !p.Exclusive() {
		t.Fatal("playing should take the device")
	}
	p.TogglePause()
	if !p.Exclusive() {
		t.Error("pausing should keep the device")
	}
	p.Stop()
	if fs.hog() != 0 || p.Exclusive() {
		t.Error("Stop should give the device back")
	}

	p.Play(song, func(error) {})
	p.SetExclusive(false)
	if fs.hog() != 0 || p.Exclusive() {
		t.Error("turning it off should give the device back at once")
	}

	// To the end of a song: the device is given back.
	p.SetExclusive(true)
	ended := make(chan struct{}, 1)
	p.Play(longFLAC(t, 44100, 16, 1), func(error) { ended <- struct{}{} })
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the song never ended")
	}
	if fs.hog() != 0 {
		t.Error("the end of the songs should give the device back")
	}

	fs.mu.Lock()
	fs.hogFails = true
	fs.mu.Unlock()
	if err := p.Play(song, func(error) {}); err != nil {
		t.Errorf("another app has the device: %v", err)
	}
	if p.Exclusive() {
		t.Error("Exclusive should say the device couldn't be had")
	}
}

// When the chosen device is unplugged, the song carries on through the
// default output.
func TestPlayerDeviceUnplugged(t *testing.T) {
	p := testPlayer(t)
	devices := p.Devices()
	if len(devices) == 0 {
		t.Fatal("the null backend should list a device")
	}
	p.SetDevice(devices[0].ID)
	if err := p.Play(longFLAC(t, 44100, 16, 3), func(error) {}); err != nil {
		t.Fatal(err)
	}
	p.device = "ffff" // as if the chosen device had gone
	p.DevicesChanged()
	if dev, track := p.Output(); dev == 0 || track != 44100 {
		t.Errorf("output %d/%d after the device went, want it playing on", dev, track)
	}
	before := p.Position()
	waitFor(t, "the song to carry on", func() bool { return p.Position() > before })
}

// Samples gives the visualizer the latest sound and its rate while a song
// plays, and nothing when stopped.
func TestPlayerSamples(t *testing.T) {
	p := testPlayer(t)
	buf := make([]float64, 1024)
	if rate := p.Samples(buf); rate != 0 {
		t.Errorf("stopped: rate %d, want 0", rate)
	}
	if err := p.Play(longFLAC(t, 44100, 16, 3), func(error) {}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sound to reach the visualizer", func() bool {
		return p.Samples(buf) == 44100 && slices.ContainsFunc(buf, func(v float64) bool { return v != 0 })
	})
	p.Stop()
	if rate := p.Samples(buf); rate != 0 {
		t.Errorf("after Stop: rate %d, want 0", rate)
	}
}
