package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
	mewflac "github.com/mewkiz/flac"
)

// audioPlayer is what the UI needs from playback. The real one is player;
// tests use a fake so they make no sound.
type audioPlayer interface {
	Play(path string, onEnd func(error)) error
	TogglePause() bool
	Stop()
	Position() time.Duration
	Duration() time.Duration
	Seek(d time.Duration) error
	SetVolume(level float64)
	// Samples fills dst with the most recent audio, mono and oldest first,
	// and returns its sample rate, or 0 if nothing is playing.
	Samples(dst []float64) int
	// Output describes where the sound goes: the device's rate, and the
	// track's rate, which differ if the audio is converted. 0s if stopped.
	Output() (deviceRate, trackRate int)
	// Devices lists the output devices, and SetDevice picks one by ID, ""
	// for the system default. SetMatchRate turns switching the device to
	// each track's rate on or off.
	Devices() []outputDevice
	SetDevice(id string)
	SetMatchRate(on bool)
	// DevicesChanged is called when devices come or go, or the system's
	// default output changes.
	DevicesChanged()
}

// player plays one track at a time through out. Its fields are only
// touched on the UI thread, except what the audio thread reads through
// out's lock.
type player struct {
	out output

	stream beep.StreamSeekCloser // decoder for the current track; owns the file
	format beep.Format
	tap    *tap
	vol    *effects.Volume
	ctrl   *beep.Ctrl
	gen    int     // bumped on every Stop so stale end-of-track callbacks are ignored
	level  float64 // volume from 0 (silent) to 1 (full), kept across tracks

	device    string             // chosen output device's ID, "" for the system default
	matchRate bool               // switch the device to each track's rate
	original  map[string]float64 // device rates before they were switched, to restore
}

func newPlayer() *player {
	return &player{level: 1, matchRate: true, original: map[string]float64{}}
}

// Play stops any current track and starts the one at path. onEnd is called
// on the UI thread when the track ends: with nil if it played through, or
// with why it stopped early.
func (p *player) Play(path string, onEnd func(error)) error {
	p.Stop()
	s, format, err := decode(path)
	if err != nil {
		return err
	}
	if err := p.openOutput(int(format.SampleRate)); err != nil {
		s.Close()
		return fmt.Errorf("cannot play %s: %w", filepath.Base(path), err)
	}
	s = &safeStream{StreamSeekCloser: s}

	gen := p.gen
	p.stream, p.format = s, format
	// The tap sits before the volume so the visualizer still moves when
	// the sound is turned down.
	p.tap = newTap(s, 8192)
	p.vol = &effects.Volume{Streamer: p.tap, Base: 2}
	p.applyLevel()
	p.ctrl = &beep.Ctrl{Streamer: beep.Seq(p.vol, beep.Callback(func() {
		// Runs on the audio thread with the output locked, so hand off to
		// the UI thread from a goroutine that doesn't hold the lock.
		go onUI(func() {
			if p.gen == gen {
				err := s.Err()
				p.release()
				p.out.idle()
				onEnd(err)
			}
		})
	}))}
	p.out.play(p.ctrl)
	return nil
}

// openOutput readies the device for a track at rate, first switching the
// device itself to rate if allowed and possible.
func (p *player) openOutput(rate int) error {
	if p.matchRate {
		uid := deviceUID(p.device)
		if _, saved := p.original[uid]; !saved {
			if r := nominalRate(uid); r > 0 {
				p.original[uid] = r
			}
		}
		setNominalRate(uid, float64(rate))
	}
	return p.out.open(rate, p.device)
}

// decode opens path and returns a decoder for it, which owns the file. The
// format is taken from the file's contents, so a misnamed file still plays,
// falling back to its extension.
func decode(path string) (beep.StreamSeekCloser, beep.Format, error) {
	kind := sniff(path)
	if kind == unknownFile {
		kind = map[string]fileKind{".mp3": mp3File, ".flac": flacFile, ".wav": wavFile, ".ogg": oggFile}[strings.ToLower(filepath.Ext(path))]
	}
	return decodeAs(path, kind)
}

func decodeAs(path string, kind fileKind) (s beep.StreamSeekCloser, format beep.Format, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, beep.Format{}, err
	}
	// A malformed file can make a decoder panic; that must not close the app.
	defer func() {
		if r := recover(); r != nil {
			f.Close()
			s, format, err = nil, beep.Format{}, fmt.Errorf("cannot play %s: the file is damaged", filepath.Base(path))
		}
	}()
	if kind == oggFile {
		if codec := oggCodec(f); codec != "Ogg Vorbis" {
			f.Close()
			return nil, beep.Format{}, fmt.Errorf("cannot play %s: %s files aren't supported yet", filepath.Base(path), codec)
		}
	}
	if kind == flacFile && !flacReadable(f) && appleDecoding {
		f.Close()
		return appleDecode(path) // the FLAC library can't read this one; macOS can
	}
	switch kind {
	case mp3File:
		s, format, err = mp3.Decode(f)
	case flacFile:
		var fs *flacStream
		if fs, format, err = newFLACStream(f); err == nil {
			s = fs
		}
	case wavFile:
		s, format, err = wav.Decode(f)
	case oggFile:
		s, format, err = vorbis.Decode(f)
	default:
		err = fmt.Errorf("unsupported format")
	}
	if err != nil {
		f.Close()
		return nil, beep.Format{}, fmt.Errorf("cannot play %s: %w", filepath.Base(path), err)
	}
	return s, format, nil
}

// flacReadable reports whether the FLAC library can read the file's first
// frame. A few files it can't read at all, though they play elsewhere.
func flacReadable(f *os.File) bool {
	defer f.Seek(0, io.SeekStart)
	s, err := mewflac.New(bufio.NewReader(f))
	if err != nil {
		return false
	}
	_, err = s.ParseNext()
	return err == nil || err == io.EOF
}

// TogglePause pauses or resumes the current track and reports whether it is
// now paused.
func (p *player) TogglePause() bool {
	if p.ctrl == nil {
		return false
	}
	p.out.mu.Lock()
	defer p.out.mu.Unlock()
	p.ctrl.Paused = !p.ctrl.Paused
	return p.ctrl.Paused
}

// Position reports how far into the current track playback is.
func (p *player) Position() time.Duration {
	if p.stream == nil {
		return 0
	}
	p.out.mu.Lock()
	defer p.out.mu.Unlock()
	return p.format.SampleRate.D(p.stream.Position())
}

// Duration reports the length of the current track, or 0 if unknown.
func (p *player) Duration() time.Duration {
	if p.stream == nil {
		return 0
	}
	p.out.mu.Lock()
	defer p.out.mu.Unlock()
	return p.format.SampleRate.D(max(p.stream.Len(), 0))
}

// Seek jumps to d within the current track.
func (p *player) Seek(d time.Duration) error {
	if p.stream == nil {
		return nil
	}
	p.out.mu.Lock()
	defer p.out.mu.Unlock()
	n := min(max(p.format.SampleRate.N(d), 0), p.stream.Len())
	return p.stream.Seek(n)
}

// SetVolume sets the volume from 0 (silent) to 1 (full).
func (p *player) SetVolume(level float64) {
	p.level = level
	if p.vol == nil {
		return
	}
	p.out.mu.Lock()
	defer p.out.mu.Unlock()
	p.applyLevel()
}

// applyLevel maps level onto a base-2 exponent, which tracks perceived
// loudness better than scaling the samples linearly. The bottom of the range
// is about -36 dB. At full volume the gain is exactly 1, so the samples
// pass through unchanged.
func (p *player) applyLevel() {
	p.vol.Silent = p.level <= 0
	p.vol.Volume = (p.level - 1) * 6
}

func (p *player) Samples(dst []float64) int {
	if p.tap == nil {
		return 0
	}
	p.tap.latest(dst)
	return int(p.format.SampleRate)
}

func (p *player) Output() (deviceRate, trackRate int) {
	if p.stream == nil {
		return 0, 0
	}
	return p.out.deviceRate(), int(p.format.SampleRate)
}

func (p *player) Devices() []outputDevice {
	d, err := p.out.devices()
	if err != nil {
		fyne.LogError("listing output devices", err)
	}
	return d
}

// SetDevice switches output to the device with id, carrying on with the
// current track there.
func (p *player) SetDevice(id string) {
	if id == p.device {
		return
	}
	p.device = id
	p.reopen()
}

func (p *player) SetMatchRate(on bool) {
	if on == p.matchRate {
		return
	}
	p.matchRate = on
	if !on {
		p.restoreRates()
	}
	p.reopen()
}

// DevicesChanged moves the track to the system's new default output, at the
// track's rate, or off a chosen device that was unplugged.
func (p *player) DevicesChanged() {
	if p.device == "" || !slices.ContainsFunc(p.Devices(), func(d outputDevice) bool { return d.ID == p.device }) {
		p.reopen()
	}
}

// reopen moves the current track to a freshly opened device, after the
// device or its rate has changed.
func (p *player) reopen() {
	if p.stream == nil {
		p.out.closeDevice() // the next track opens the right one
		return
	}
	p.out.closeDevice()
	if err := p.openOutput(int(p.format.SampleRate)); err != nil {
		fyne.LogError("reopening audio output", err)
		return
	}
	p.out.play(p.ctrl)
}

// Stop halts playback and closes the current file.
func (p *player) Stop() {
	p.gen++
	if p.stream == nil {
		return
	}
	p.out.idle() // once this returns the audio thread no longer reads the stream
	p.release()
}

func (p *player) release() {
	p.stream.Close()
	p.stream, p.tap, p.vol, p.ctrl = nil, nil, nil, nil
}

// Close stops playback, closes the device and puts back the device rates
// that were switched, leaving the system as it was.
func (p *player) Close() {
	p.Stop()
	p.out.close()
	p.restoreRates()
}

func (p *player) restoreRates() {
	for uid, rate := range p.original {
		setNominalRate(uid, rate)
	}
	clear(p.original)
}

// safeStream ends the track, rather than the app, if its decoder panics on
// a damaged part of the file. The panic would otherwise happen on the audio
// thread, where nothing could recover from it.
type safeStream struct {
	beep.StreamSeekCloser
	err error
}

func (s *safeStream) Stream(samples [][2]float64) (n int, ok bool) {
	if s.err != nil {
		return 0, false
	}
	defer func() {
		if r := recover(); r != nil {
			s.err = fmt.Errorf("decoder failed: %v", r)
			n, ok = 0, false
		}
	}()
	return s.StreamSeekCloser.Stream(samples)
}

func (s *safeStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.StreamSeekCloser.Err()
}

// tap passes audio through unchanged, keeping the most recent samples for
// the visualizer. It is written on the speaker goroutine and read on the UI
// thread, hence the lock.
type tap struct {
	s   beep.Streamer
	mu  sync.Mutex
	buf []float64 // ring of mono samples
	pos int       // where the next sample goes
}

func newTap(s beep.Streamer, size int) *tap {
	return &tap{s: s, buf: make([]float64, size)}
}

func (t *tap) Stream(samples [][2]float64) (int, bool) {
	n, ok := t.s.Stream(samples)
	t.mu.Lock()
	for _, s := range samples[:n] {
		t.buf[t.pos] = (s[0] + s[1]) / 2
		t.pos = (t.pos + 1) % len(t.buf)
	}
	t.mu.Unlock()
	return n, ok
}

func (t *tap) Err() error { return t.s.Err() }

// latest copies the most recent len(dst) samples into dst, oldest first.
// dst must be no longer than the ring.
func (t *tap) latest(dst []float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := (t.pos - len(dst) + len(t.buf)) % len(t.buf)
	n := copy(dst, t.buf[start:])
	copy(dst[n:], t.buf[:len(dst)-n])
}
