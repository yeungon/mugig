//go:build darwin

package main

import (
	"os"
	"testing"
)

// Taking the real default output for Mugig alone, and giving it back. It
// silences other apps for a moment, so it runs only when asked:
// MUGIG_HOG_TEST=1 go test -run TestHogDefaultOutput
func TestHogDefaultOutput(t *testing.T) {
	if os.Getenv("MUGIG_HOG_TEST") == "" {
		t.Skip("set MUGIG_HOG_TEST=1 to take the default output for a moment")
	}
	dev := hogDevice("")
	if dev == 0 {
		t.Fatal("couldn't take the default output")
	}
	defer unhogDevice(dev)
	if owner := hogOwner(dev); owner != os.Getpid() {
		t.Errorf("owner %d, want this process %d", owner, os.Getpid())
	}
	if again := hogDevice(""); again != dev {
		t.Errorf("taking it again gave %d, want %d", again, dev)
	}
	unhogDevice(dev)
	if owner := hogOwner(dev); owner != -1 {
		t.Errorf("owner %d after giving it back, want -1", owner)
	}
	unhogDevice(dev) // giving back what isn't held does nothing
	if owner := hogOwner(dev); owner != -1 {
		t.Errorf("owner %d after giving it back twice", owner)
	}
}

// With exclusive access on, the player has the device while a song plays
// and gives it back when it stops.
func TestPlayerExclusive(t *testing.T) {
	if os.Getenv("MUGIG_HOG_TEST") == "" {
		t.Skip("set MUGIG_HOG_TEST=1 to take the default output for a moment")
	}
	p := testPlayer(t) // plays to the null device, but takes the real one
	p.SetExclusive(true)
	if err := p.Play(longFLAC(t, 44100, 16, 3), func(error) {}); err != nil {
		t.Fatal(err)
	}
	dev := p.hogged
	if !p.Exclusive() || hogOwner(dev) != os.Getpid() {
		t.Fatal("the device should be Mugig's while playing")
	}
	p.Stop()
	if p.Exclusive() || hogOwner(dev) != -1 {
		t.Error("the device should be given back on stopping")
	}
}
