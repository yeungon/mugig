package main

// Audio output through miniaudio (malgo). The device is opened at the
// sample rate of the track playing, so the audio reaches it unconverted
// where the device allows: on macOS the device itself is switched to that
// rate first (see coreaudio_darwin.go), as Apple Music does. Tracks at the
// same rate on the same device share it, so they follow without a gap.

import (
	"encoding/binary"
	hexenc "encoding/hex"
	"errors"
	"math"
	"slices"
	"sync"

	"github.com/gen2brain/malgo"
	"github.com/gopxl/beep/v2"
)

// outputDevice is a device that can play sound.
type outputDevice struct {
	ID        string // malgo's device ID in hex; stable for a device
	Name      string
	IsDefault bool
}

// output plays one beep.Streamer at a time. The audio thread reads src
// with mu held, so changing what plays, or anything it reads, takes mu.
type output struct {
	backends []malgo.Backend // nil for the system's; tests use the null backend

	mu  sync.Mutex
	src beep.Streamer // nil plays silence
	buf [][2]float64

	ctx    *malgo.AllocatedContext
	dev    *malgo.Device
	rate   int    // the open device's sample rate, as asked for
	device string // the open device's ID, "" for the system default
}

func (o *output) context() (*malgo.AllocatedContext, error) {
	if o.ctx == nil {
		ctx, err := malgo.InitContext(o.backends, malgo.ContextConfig{}, nil)
		if err != nil {
			return nil, err
		}
		o.ctx = ctx
	}
	return o.ctx, nil
}

// open makes sure the device is open at rate and running, reopening it if
// the rate or device differs. A device that has gone, such as an unplugged
// DAC, falls back to the system default.
func (o *output) open(rate int, device string) error {
	if o.dev != nil && o.rate == rate && o.device == device {
		if !o.dev.IsStarted() {
			return o.dev.Start()
		}
		return nil
	}
	o.closeDevice()
	ctx, err := o.context()
	if err != nil {
		return err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 2
	cfg.SampleRate = uint32(rate)
	cfg.PeriodSizeInMilliseconds = 20
	cfg.Periods = 3
	// If the device can't run at rate, miniaudio converts; use its better
	// filter rather than the default.
	cfg.Resampling.Algorithm = malgo.ResampleAlgorithmLinear
	cfg.Resampling.Linear.LpfOrder = 8
	if device != "" {
		id, err := parseDeviceID(device)
		if err != nil {
			return err
		}
		cfg.Playback.DeviceID = id.Pointer()
	}
	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: o.fill})
	if err != nil {
		if device != "" {
			return o.open(rate, "") // the chosen device is gone
		}
		return err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return err
	}
	o.dev, o.rate, o.device = dev, rate, device
	return nil
}

// deviceRate is the rate the device itself runs at, which differs from the
// track's when miniaudio has to convert. 0 if no device is open.
func (o *output) deviceRate() int {
	if o.dev == nil {
		return 0
	}
	return int(o.dev.PlaybackInternalSampleRate())
}

// play swaps what is playing. Once it returns, the audio thread no longer
// reads the previous streamer.
func (o *output) play(s beep.Streamer) {
	o.mu.Lock()
	o.src = s
	o.mu.Unlock()
}

// idle stops the device while nothing plays, so it costs nothing.
func (o *output) idle() {
	o.play(nil)
	if o.dev != nil && o.dev.IsStarted() {
		o.dev.Stop()
	}
}

func (o *output) closeDevice() {
	if o.dev != nil {
		o.dev.Uninit()
		o.dev = nil
	}
}

func (o *output) close() {
	o.closeDevice()
	if o.ctx != nil {
		o.ctx.Uninit()
		o.ctx.Free()
		o.ctx = nil
	}
}

// fill is miniaudio's data callback, on its audio thread: it writes frames
// of stereo float32 into out.
func (o *output) fill(out, _ []byte, frames uint32) {
	n := int(frames)
	if cap(o.buf) < n {
		o.buf = make([][2]float64, n)
	}
	buf := o.buf[:n]
	o.mu.Lock()
	filled := 0
	for o.src != nil && filled < n {
		got, ok := o.src.Stream(buf[filled:])
		filled += got
		if !ok {
			o.src = nil // finished
		}
	}
	o.mu.Unlock()
	clear(buf[filled:])
	for i, s := range buf {
		binary.LittleEndian.PutUint32(out[i*8:], math.Float32bits(float32(s[0])))
		binary.LittleEndian.PutUint32(out[i*8+4:], math.Float32bits(float32(s[1])))
	}
}

// devices lists the devices that can play sound.
func (o *output) devices() ([]outputDevice, error) {
	ctx, err := o.context()
	if err != nil {
		return nil, err
	}
	infos, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, err
	}
	out := make([]outputDevice, len(infos))
	for i, d := range infos {
		out[i] = outputDevice{ID: d.ID.String(), Name: d.Name(), IsDefault: d.IsDefault != 0}
	}
	return out, nil
}

func parseDeviceID(s string) (malgo.DeviceID, error) {
	var id malgo.DeviceID
	b, err := hexenc.DecodeString(s)
	if err != nil || len(b) > len(id) {
		return id, errors.New("bad output device ID")
	}
	copy(id[:], b)
	return id, nil
}

// deviceUID returns the text inside a device ID, which for Core Audio
// devices is the device's UID.
func deviceUID(id string) string {
	b, _ := hexenc.DecodeString(id)
	if i := slices.Index(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
