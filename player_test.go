package main

import (
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
)

// nullBackend is miniaudio's null device. malgo's own BackendNull is off by
// one: its list lacks miniaudio's "custom" entry, which comes just before.
const nullBackend = malgo.BackendNull + 1

// testPlayer plays through miniaudio's null device, which takes audio in
// real time but makes no sound. It never switches a real device's rate.
func testPlayer(t *testing.T) *player {
	t.Helper()
	newTestApp(t)
	p := newPlayer()
	p.out.backends = []malgo.Backend{nullBackend}
	p.matchRate = false
	t.Cleanup(p.Close)
	return p
}

func TestPlayerPlaysToTheEnd(t *testing.T) {
	p := testPlayer(t)
	ended := make(chan struct{}, 1)
	if err := p.Play("testdata/sample.flac", func() { ended <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the track never ended")
	}
	if dev, track := p.Output(); dev != 0 || track != 0 {
		t.Errorf("output %d/%d after the end", dev, track)
	}
}

// longFLAC writes seconds of noise at rate, long enough not to end
// during a test.
func longFLAC(t *testing.T, rate, bits, seconds int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "long.flac")
	writeFLAC(t, path, rate, bits, quantize(noise(seconds*rate, float64(rate), pink), bits))
	return path
}

// Each track opens the device at its own rate; tracks at the same rate
// keep the open device.
func TestPlayerOpensDeviceAtTrackRate(t *testing.T) {
	p := testPlayer(t)
	hires, cd, cd2 := longFLAC(t, 96000, 24, 4), longFLAC(t, 44100, 16, 4), longFLAC(t, 44100, 16, 4)

	if err := p.Play(hires, func() {}); err != nil {
		t.Fatal(err)
	}
	if d := p.Duration(); d != 4*time.Second {
		t.Errorf("duration %v", d)
	}
	if dev, track := p.Output(); dev != 96000 || track != 96000 {
		t.Errorf("96 kHz track: device %d, track %d", dev, track)
	}
	first := p.out.dev
	p.Play(cd, func() {})
	if dev, _ := p.Output(); dev != 44100 || p.out.dev == first {
		t.Errorf("44.1 kHz track: device at %d, reopened %v", dev, p.out.dev != first)
	}
	second := p.out.dev
	p.Play(cd2, func() {})
	if p.out.dev != second {
		t.Error("a track at the same rate should keep the device")
	}
}

func TestPlayerPauseAndSeek(t *testing.T) {
	p := testPlayer(t)
	p.Play(longFLAC(t, 44100, 16, 5), func() {})

	time.Sleep(300 * time.Millisecond)
	if p.Position() == 0 {
		t.Fatal("not playing")
	}
	if !p.TogglePause() {
		t.Fatal("not paused")
	}
	at := p.Position()
	time.Sleep(200 * time.Millisecond)
	if p.Position() != at {
		t.Error("position moved while paused")
	}
	p.Seek(3 * time.Second)
	if pos := p.Position(); pos < 3*time.Second || pos > 3100*time.Millisecond {
		t.Errorf("position after seeking %v", pos)
	}
	p.TogglePause()
	time.Sleep(200 * time.Millisecond)
	if p.Position() <= 3*time.Second {
		t.Error("not playing after resuming")
	}
}

// At full volume samples reach the device unchanged: the output adds
// nothing but the conversion to 32-bit float, which is exact for audio of
// up to 24 bits.
func TestFullVolumeIsBitExact(t *testing.T) {
	in := make([][2]float64, 256)
	for i := range in {
		v := float64(i*65537%(1<<23)-(1<<22)) / (1 << 23) // 24-bit values
		in[i] = [2]float64{v, -v}
	}
	src := beep.StreamerFunc(func(s [][2]float64) (int, bool) { return copy(s, in), true })
	p := newPlayer() // the volume stage as Play builds it, at full volume
	p.vol = &effects.Volume{Streamer: newTap(src, 1024), Base: 2}
	p.SetVolume(1)

	var o output
	o.play(p.vol)
	out := make([]byte, len(in)*8)
	o.fill(out, nil, uint32(len(in)))
	for i, want := range in {
		l := math.Float32frombits(binary.LittleEndian.Uint32(out[i*8:]))
		r := math.Float32frombits(binary.LittleEndian.Uint32(out[i*8+4:]))
		if float64(l) != want[0] || float64(r) != want[1] {
			t.Fatalf("sample %d: got %v %v, want %v", i, l, r, want)
		}
	}
}

func TestPlayerDevices(t *testing.T) {
	p := testPlayer(t)
	if len(p.Devices()) == 0 {
		t.Fatal("the null backend should list a device")
	}
	// A device that has gone falls back to the default.
	p.SetDevice("ffff")
	if err := p.Play(longFLAC(t, 44100, 16, 3), func() {}); err != nil {
		t.Errorf("unplugged device: %v", err)
	}
}
