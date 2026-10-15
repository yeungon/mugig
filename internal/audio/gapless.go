package audio

// Gapless playback. Near the end of a song the player is told which song
// follows, opens it and starts reading it. When the song ends, the next
// one carries on from the very next sample, within the same buffer the
// device is filling, so live albums, classical works and mixes that run
// from one track into the next play as recorded. Only songs at the same
// sample rate can join like this: a song at another rate needs the device
// reopened, which leaves a short gap.

import (
	"math"
	"os"
	"time"

	"github.com/gopxl/beep/v2"
)

// GaplessLead is how long before a song ends the one after it is readied.
const GaplessLead = 20 * time.Second

// deck is a song readied to play: its decoder, reading ahead, and the
// stretcher that changes its speed.
type deck struct {
	path    string
	stream  beep.StreamSeekCloser // reads ahead; owns the file
	format  beep.Format
	stretch *stretcher
	level   level // volume levelling's gain for this song
}

func openDeck(path string, speed float64) (*deck, error) {
	s, format, err := Decode(path)
	if err != nil {
		return nil, err
	}
	ra := newReadAhead(&safeStream{StreamSeekCloser: s}, format.SampleRate)
	d := &deck{path: path, stream: ra, format: format, stretch: newStretcher(ra, int(format.SampleRate), speed)}
	d.level = level{gain: 1, target: 1, step: math.Pow(10, levelGlideDB/20/float64(max(int(format.SampleRate), 1)))}
	return d, nil
}

// levelGlideDB is how fast, in dB a second, a song's levelling gain moves
// to a new one, such as when its loudness is measured as it plays.
const levelGlideDB = 20

// level is a song's levelling gain, gliding to a new one rather than
// jumping. At 1, as with levelling off, the samples pass through as they
// are. It is changed with the output locked.
type level struct {
	gain, target float64
	step         float64 // the gain's multiple per sample while gliding
}

// set goes to gain at once, for a song not yet playing.
func (l *level) set(gain float64) { l.gain, l.target = gain, gain }

func (l *level) apply(samples [][2]float64) {
	if l.gain == l.target {
		if l.gain != 1 {
			for i := range samples {
				samples[i][0] *= l.gain
				samples[i][1] *= l.gain
			}
		}
		return
	}
	for i := range samples {
		if l.gain < l.target {
			l.gain = min(l.gain*l.step, l.target)
		} else {
			l.gain = max(l.gain/l.step, l.target)
		}
		samples[i][0] *= l.gain
		samples[i][1] *= l.gain
	}
}

// decks plays cur, then next if one is readied, without a gap. It is read
// on the audio thread, so changing it takes the output's lock.
type decks struct {
	cur, next *deck
	ended     bool
	// switched is called when next takes over from old, on the audio thread
	// with the output locked.
	switched func(old *deck)
}

func (d *decks) Stream(samples [][2]float64) (n int, ok bool) {
	for n < len(samples) && !d.ended {
		got, more := d.cur.stretch.Stream(samples[n:])
		d.cur.level.apply(samples[n : n+got])
		n += got
		switch {
		case more && got == 0:
			return n, true
		case more:
			continue
		case d.next == nil || d.cur.stream.Err() != nil:
			// The end, or the song stopped early: that is reported, rather
			// than moving on as if it had finished.
			d.ended = true
		default:
			old := d.cur
			d.cur, d.next = d.next, nil
			d.switched(old)
		}
	}
	return n, n > 0 || !d.ended
}

func (d *decks) Err() error { return d.cur.stream.Err() }

// An MP3 holds more sound than was encoded: the encoder adds a little
// silence before and after, and decoding adds 529 samples of delay. LAME,
// and FFmpeg which uses it, record how much in the first frame, which
// itself plays as a frame of silence. Without cutting these off, an MP3
// album has a short gap between its tracks.

// mp3DecoderDelay is the delay every standard MP3 decoder adds.
const mp3DecoderDelay = 529

// mp3Padding reads the encoder's note of its added silence from the file
// f, and returns where the music starts in the decoded sound and how many
// frames it lasts. ok is false if the file has no note.
func mp3Padding(f *os.File) (skip, frames int, ok bool) {
	_, start := sniffFile(f)
	buf := make([]byte, 16<<10)
	n, _ := f.ReadAt(buf, start)
	buf = buf[:n]
	at, fr := firstMPEGFrame(buf)
	if at < 0 {
		return 0, 0, false
	}
	x, ok := readXing(buf, at, fr)
	if !ok || x.delay+x.padding == 0 {
		return 0, 0, false
	}
	// The frame with the tag decodes as a frame of silence, before the rest.
	return fr.samples + x.delay + mp3DecoderDelay, x.frames*fr.samples - x.delay - x.padding, true
}

// mp3SeekPreroll is how far before a seek an MP3 is decoded from. Right
// after a seek the decoder lacks the frames before, which the next ones
// build on, and puts out a burst of noise for up to two frames.
const mp3SeekPreroll = 4 * 1152

// mp3Stream plays frames of an MP3 from skip on, as if that were all there
// is, and seeks without noise.
type mp3Stream struct {
	beep.StreamSeekCloser
	skip, n int
	tmp     [][2]float64
}

// newMP3Stream wraps s, decoded from f, cutting off the encoder's silence.
// The start is decoded and dropped rather than sought past.
func newMP3Stream(s beep.StreamSeekCloser, f *os.File) beep.StreamSeekCloser {
	m := &mp3Stream{StreamSeekCloser: s, n: s.Len(), tmp: make([][2]float64, 1024)}
	// With little padding, the end of the music is only decoded by
	// flushing the decoder, which this one doesn't do: play what there is.
	if skip, n, ok := mp3Padding(f); ok && skip < s.Len() {
		m.skip, m.n = skip, min(n, s.Len()-skip)
	}
	m.discard(m.skip)
	return m
}

// discard decodes n frames and drops them.
func (m *mp3Stream) discard(n int) {
	for n > 0 {
		got, ok := m.StreamSeekCloser.Stream(m.tmp[:min(n, len(m.tmp))])
		if !ok {
			return
		}
		n -= got
	}
}

func (m *mp3Stream) Stream(samples [][2]float64) (int, bool) {
	left := m.n - m.Position()
	if left <= 0 {
		return 0, false
	}
	return m.StreamSeekCloser.Stream(samples[:min(len(samples), left)])
}

func (m *mp3Stream) Len() int      { return m.n }
func (m *mp3Stream) Position() int { return max(m.StreamSeekCloser.Position()-m.skip, 0) }

func (m *mp3Stream) Seek(p int) error {
	p = min(max(p, 0), m.n) + m.skip
	from := max(p-mp3SeekPreroll, 0)
	if err := m.StreamSeekCloser.Seek(from); err != nil {
		return err
	}
	m.discard(p - from)
	return nil
}
