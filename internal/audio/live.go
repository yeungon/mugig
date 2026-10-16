package audio

// Live radio. A station is connected to, and its first sound decoded,
// before playing starts (OpenLive), as that can take a moment; then it
// plays like a song that never ends (Player.PlayLive). If the connection
// drops, or the sound can't be decoded for a moment, it connects again,
// waiting a little longer each time, while the sound already decoded
// plays out. It only gives up after several tries in a row fail.
//
// Radio always plays at normal speed, since a station sends sound no
// faster than it is played, and without volume levelling, since its
// loudness can't be measured ahead.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"mugig/internal/radio"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
)

const (
	// maxReconnects is how many times in a row connecting again can fail
	// before playing stops: with the waits between, about 40 seconds.
	maxReconnects = 8
	// healthy is how long a connection has to play for before it counts as
	// working, so one that keeps breaking soon after it is made still ends.
	healthy = 10 * time.Second
)

// The wait before connecting again: none at first, then firstWait,
// doubling each time up to longestWait. Tests shorten them.
var (
	firstWait   = time.Second
	longestWait = 8 * time.Second
)

var errLiveSeek = errors.New("live radio can't be moved through")

// liveApple is whether stations' MP3 and AAC go to macOS's decoders, as
// they do where there are some. Tests turn it off to try the others.
var liveApple = AppleDecoding

// Live is a radio station, connected and ready to play.
type Live struct {
	Link  string
	Codec string // such as "AAC"
	Kbps  int    // the bitrate the station states, 0 if it doesn't
	Name  string // the station's name for itself, "" if it gives none
	HLS   bool

	ra     *readAhead
	format beep.Format
}

// LiveInfo is news from a station as it plays.
type LiveInfo struct {
	Title        string // what is playing, as the station names it; "" if it doesn't
	Reconnecting bool   // the connection dropped and is being made again
}

// OpenLive connects to the station at link and decodes the start of its
// sound. It can take several seconds, so it isn't for the UI thread.
// onInfo, if not nil, is called on another goroutine when the station
// names what is playing, and when the connection drops and is made again.
func OpenLive(link string, onInfo func(LiveInfo)) (*Live, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &liveStream{ctx: ctx, cancel: cancel, link: link, onInfo: onInfo}
	if err := s.connect(); err != nil {
		cancel()
		return nil, err
	}
	s.rate = s.format.SampleRate
	ra := newReadAhead(s, s.rate)
	ra.interrupt = cancel
	return &Live{
		Link:   link,
		Codec:  string(s.src.Codec),
		Kbps:   s.src.Kbps,
		Name:   s.src.Name,
		HLS:    s.src.HLS,
		ra:     ra,
		format: s.format,
	}, nil
}

// Close hangs up a station that was opened but not played. Once played,
// the player hangs up when it stops.
func (l *Live) Close() {
	if l.ra != nil {
		l.ra.Close()
	}
}

// SampleRate is the rate the station's sound is decoded at.
func (l *Live) SampleRate() int { return int(l.format.SampleRate) }

// PlayLive stops any current track and plays the station l. onEnd is
// called on the UI thread if it stops: with nil if the broadcast ended,
// or with why it stopped.
func (p *Player) PlayLive(l *Live, onEnd func(error)) error {
	wasPlaying := p.ctrl != nil && !p.ctrl.Paused && !p.vol.fadingOut()
	p.Stop()
	d := newDeck(l.Link, l.ra, l.format, 1)
	d.live = true
	return p.start(d, onEnd, wasPlaying)
}

// liveStream is a station's sound, decoded, connecting again when it
// breaks. It is read by the read-ahead's goroutine, so waiting for the
// station holds up nothing else.
type liveStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	link   string
	onInfo func(LiveInfo)
	title  string

	src    *radio.Stream
	ended  *endReader // src, noting whether it came to its end
	dec    beep.StreamSeekCloser
	format beep.Format     // dec's
	rate   beep.SampleRate // played at: the first connection's
	out    beep.Streamer   // dec, changed to rate if it differs

	pos    int // frames played
	played int // frames played since connecting
	fails  int // tries in a row that failed, or broke soon after
	err    error
}

// connect connects to the station and starts decoding.
func (s *liveStream) connect() error {
	src, err := radio.Open(s.ctx, s.link, s.titled)
	if err != nil {
		return err
	}
	ended := &endReader{r: src}
	dec, format, err := decodeLive(ended, src.Codec)
	if err != nil {
		src.Close()
		return err
	}
	s.src, s.ended, s.dec, s.format, s.out, s.played = src, ended, dec, format, dec, 0
	if s.rate != 0 && format.SampleRate != s.rate {
		s.out = beep.Resample(4, format.SampleRate, s.rate, dec)
	}
	return nil
}

// decodeLive starts decoding a station's sound. MP3 and AAC go to macOS's
// decoders where there are some, which also play HE-AAC and the odd MP3
// the MP3 library can't.
func decodeLive(r io.Reader, codec radio.Codec) (s beep.StreamSeekCloser, f beep.Format, err error) {
	defer func() {
		if recover() != nil {
			s, f, err = nil, beep.Format{}, errors.New("the station's sound is damaged")
		}
	}()
	rc := io.NopCloser(r) // the station is hung up separately
	switch {
	case codec == radio.Ogg:
		return vorbis.Decode(rc)
	case liveApple:
		return appleLiveDecode(r, codec)
	case codec == radio.MP3:
		return mp3.Decode(rc)
	}
	return nil, beep.Format{}, fmt.Errorf("Mugig plays %s stations only on macOS for now", codec)
}

func (s *liveStream) Stream(samples [][2]float64) (int, bool) {
	for s.err == nil {
		n, ok := s.play(samples)
		if n > 0 {
			s.pos += n
			s.played += n
			return n, true
		}
		if ok {
			continue
		}
		s.reconnect()
	}
	return 0, false
}

// play reads from the decoder, treating a panic, from damaged sound, as
// the stream breaking.
func (s *liveStream) play(samples [][2]float64) (n int, ok bool) {
	defer func() {
		if recover() != nil {
			n, ok = 0, false
		}
	}()
	return s.out.Stream(samples)
}

// reconnect connects again after the stream broke or ended. An HLS
// broadcast that ended has nothing more to give; a stream that ended is
// a station that hung up, and is called back.
func (s *liveStream) reconnect() {
	why := s.dec.Err()
	if why == nil {
		why = errors.New("the station hung up")
	}
	s.hangUp()
	if s.src.HLS && s.ended.done {
		s.err = io.EOF
		return
	}
	if s.played >= s.rate.N(healthy) {
		s.fails = 0
	}
	s.notify(true)
	for s.fails < maxReconnects {
		if s.fails > 0 {
			wait := min(time.Duration(math.Pow(2, float64(s.fails-1)))*firstWait, longestWait)
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-s.ctx.Done():
				t.Stop()
			}
		}
		if s.ctx.Err() != nil {
			s.err = s.ctx.Err()
			return
		}
		s.fails++
		err := s.connect()
		if err == nil {
			s.notify(false)
			return
		}
		why = err
	}
	s.err = fmt.Errorf("lost the station: %w", why)
}

func (s *liveStream) hangUp() {
	if s.dec != nil {
		s.dec.Close()
		s.src.Close()
		s.dec = nil
	}
}

// titled is told what the station says is playing.
func (s *liveStream) titled(t string) {
	s.title = t
	s.notify(false)
}

func (s *liveStream) notify(reconnecting bool) {
	if s.onInfo != nil {
		s.onInfo(LiveInfo{Title: s.title, Reconnecting: reconnecting})
	}
}

func (s *liveStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *liveStream) Len() int       { return 0 }
func (s *liveStream) Position() int  { return s.pos }
func (s *liveStream) Seek(int) error { return errLiveSeek }

func (s *liveStream) Close() error {
	s.cancel()
	s.hangUp()
	return nil
}

// endReader notes whether r came to its end, rather than breaking.
type endReader struct {
	r    io.Reader
	done bool
}

func (e *endReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err == io.EOF {
		e.done = true
	}
	return n, err
}
