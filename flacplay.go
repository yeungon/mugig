package main

// Playing FLAC files that are partly damaged. A FLAC file is a series of
// frames, each a fraction of a second long, starting with a sync code and
// carrying its own checksums and its place in the song. So when one frame
// is corrupt, the next good one can be found and playback carry on, with
// silence in place of the lost part, as other players do. Stopping at the
// first bad byte would cut off the rest of a song over one glitch.
//
// The quality check still reads strictly (see verifyFLAC), so damaged files
// are reported as such.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/gopxl/beep/v2"
	mewflac "github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// flacStream plays a FLAC file, skipping damaged frames.
type flacStream struct {
	f    *os.File
	r    *offsetReader
	info *meta.StreamInfo
	data int64 // offset of the first frame
	size int64 // file size

	frame *frame.Frame // the frame being played, nil before the first
	pos   int          // next sample in it
	gap   int          // silence still to play in place of a damaged part
	next  int64        // sample number the next frame should start at; -1 after seeking
	at    int64        // position, in samples
	ended bool
	err   error // a read error: the disk, not the file's contents

	skipped int // damaged parts skipped so far
}

// newFLACStream reads f's headers and readies it to play. It owns f.
func newFLACStream(f *os.File) (*flacStream, beep.Format, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, beep.Format{}, err
	}
	s, err := mewflac.New(f)
	if err != nil {
		return nil, beep.Format{}, err
	}
	data, err := flacDataStart(f)
	if err != nil {
		return nil, beep.Format{}, err
	}
	d := &flacStream{f: f, r: newOffsetReader(f), info: s.Info, data: data, size: st.Size()}
	if err := d.r.seek(data); err != nil {
		return nil, beep.Format{}, err
	}
	format := beep.Format{
		SampleRate:  beep.SampleRate(d.info.SampleRate),
		NumChannels: int(d.info.NChannels),
		Precision:   int(d.info.BitsPerSample+7) / 8,
	}
	return d, format, nil
}

// flacDataStart finds where the frames begin: after any ID3 tag, the
// "fLaC" marker and the metadata blocks, each of which gives its length.
func flacDataStart(f *os.File) (int64, error) {
	var off int64
	head := make([]byte, 10)
	if _, err := f.ReadAt(head, 0); err != nil {
		return 0, err
	}
	if bytes.HasPrefix(head, []byte("ID3")) {
		size := int64(head[6]&0x7F)<<21 | int64(head[7]&0x7F)<<14 | int64(head[8]&0x7F)<<7 | int64(head[9]&0x7F)
		off = 10 + size
		if head[5]&0x10 != 0 { // a footer follows
			off += 10
		}
	}
	if _, err := f.ReadAt(head[:4], off); err != nil || string(head[:4]) != "fLaC" {
		return 0, errors.New("not a FLAC file")
	}
	off += 4
	for {
		if _, err := f.ReadAt(head[:4], off); err != nil {
			return 0, err
		}
		last := head[0]&0x80 != 0
		off += 4 + int64(binary.BigEndian.Uint32(head[:4])&0xFFFFFF)
		if last {
			return off, nil
		}
	}
}

func (d *flacStream) Stream(samples [][2]float64) (n int, ok bool) {
	for len(samples) > 0 && !d.ended {
		switch {
		case d.gap > 0:
			k := min(d.gap, len(samples))
			clear(samples[:k])
			d.gap -= k
			d.advance(k, &n, &samples)
		case d.frame != nil && d.pos < int(d.frame.BlockSize):
			k := min(int(d.frame.BlockSize)-d.pos, len(samples))
			d.fill(samples[:k])
			d.pos += k
			d.advance(k, &n, &samples)
		default:
			d.load()
		}
	}
	return n, n > 0
}

func (d *flacStream) advance(k int, n *int, samples *[][2]float64) {
	d.at += int64(k)
	*n += k
	*samples = (*samples)[k:]
}

// fill converts the frame's samples from d.pos into samples.
func (d *flacStream) fill(samples [][2]float64) {
	q := 1 / float64(int64(1)<<(d.frame.BitsPerSample-1))
	left := d.frame.Subframes[0].Samples[d.pos:]
	right := left
	if len(d.frame.Subframes) > 1 {
		right = d.frame.Subframes[1].Samples[d.pos:]
	}
	for i := range samples {
		samples[i] = [2]float64{float64(left[i]) * q, float64(right[i]) * q}
	}
}

// load reads the next frame. If it is damaged, the next good one is found,
// and the lost part is played as silence.
func (d *flacStream) load() {
	for {
		fr, err := d.nextGood()
		if err != nil {
			d.ended = true // the end, or the disk failed
			return
		}
		if d.next < 0 || d.sampleNumber(fr) >= d.next {
			d.use(fr)
			return
		}
		// A frame from earlier in the song can only be damage that happens
		// to look like one.
	}
}

// nextGood reads the frame at the current offset or, if it is damaged, the
// first good frame after.
func (d *flacStream) nextGood() (*frame.Frame, error) {
	start := d.r.off
	fr, err := d.parse()
	if err == nil || err == io.EOF || d.err != nil {
		return fr, err
	}
	fr, _, err = d.find(start+1, d.size)
	if err == nil {
		d.skipped++
	}
	return fr, err
}

// use makes fr the frame being played, first filling any gap since the last
// one with silence.
func (d *flacStream) use(fr *frame.Frame) {
	s := d.sampleNumber(fr)
	if d.next >= 0 && s > d.next {
		d.gap = int(s - d.next)
	}
	d.frame, d.pos, d.next = fr, 0, s+int64(fr.BlockSize)
}

// parse reads a frame at the current offset, noting read errors from the
// disk apart from damage in the file.
func (d *flacStream) parse() (fr *frame.Frame, err error) {
	defer func() {
		if r := recover(); r != nil { // the decoder choked on damaged data
			fr, err = nil, fmt.Errorf("damaged frame: %v", r)
		}
	}()
	fr, err = frame.Parse(d.r)
	if d.r.err != nil && !errors.Is(d.r.err, io.EOF) {
		d.err = d.r.err
	}
	if err == nil && !d.fits(fr) {
		err = errors.New("frame doesn't match the stream")
	}
	return fr, err
}

// fits reports whether fr belongs to this stream: a corrupt frame might
// otherwise pass for a good one.
func (d *flacStream) fits(fr *frame.Frame) bool {
	return fr.BlockSize > 0 && len(fr.Subframes) == int(d.info.NChannels) &&
		fr.BitsPerSample == d.info.BitsPerSample &&
		(fr.SampleRate == 0 || fr.SampleRate == d.info.SampleRate) &&
		(d.info.NSamples == 0 || uint64(d.sampleNumber(fr)) < d.info.NSamples)
}

// sampleNumber is the first sample of fr. A fixed-size frame gives its
// number in the stream, and frame.SampleNumber gets the last, shorter,
// frame wrong, so the stream's block size is used.
func (d *flacStream) sampleNumber(fr *frame.Frame) int64 {
	if fr.HasFixedBlockSize {
		return int64(fr.Num) * int64(d.info.BlockSizeMax)
	}
	return int64(fr.Num)
}

// find returns the first good frame starting between from and to, and
// where it starts, leaving the reader just past it. Every frame starts with
// the sync code: 0xFF, then 0xF8 or 0xF9.
func (d *flacStream) find(from, to int64) (*frame.Frame, int64, error) {
	buf := make([]byte, 64<<10)
	for from < to-1 {
		n, err := d.f.ReadAt(buf[:min(int64(len(buf)), to-from)], from)
		if n < 2 {
			if err != nil && !errors.Is(err, io.EOF) {
				d.err = err
			}
			return nil, 0, io.EOF
		}
		for i := range n - 1 {
			if buf[i] != 0xFF || buf[i+1]&0xFE != 0xF8 {
				continue
			}
			at := from + int64(i)
			if err := d.r.seek(at); err != nil {
				d.err = err
				return nil, 0, err
			}
			if fr, err := d.parse(); err == nil {
				return fr, at, nil
			}
			if d.err != nil {
				return nil, 0, d.err
			}
		}
		from += int64(n - 1) // the last byte may start a sync code
	}
	return nil, 0, io.EOF
}

func (d *flacStream) Len() int      { return int(d.info.NSamples) }
func (d *flacStream) Position() int { return int(d.at) }

func (d *flacStream) Err() error {
	if d.err != nil {
		return fmt.Errorf("the file couldn't be read: %w", d.err)
	}
	return nil
}

func (d *flacStream) Close() error { return d.f.Close() }

// seekSpan is how close the search for a position gets before reading
// frames one by one from there.
const seekSpan = 64 << 10

// Seek moves to sample p. Seeking can't rely on the file's seek table or on
// reading every frame from the start, as either may lead through damage,
// so the frame is searched for by halving the part of the file it could be
// in, each step finding a good frame and reading its place in the song.
func (d *flacStream) Seek(p int) error {
	if d.err != nil {
		return d.Err()
	}
	if n := d.Len(); n > 0 {
		p = min(max(p, 0), n)
	}
	target := int64(p)
	lo, hi := d.data, d.size // a frame at or before p starts at lo
	for hi-lo > seekSpan {
		mid := lo + (hi-lo)/2
		fr, at, err := d.find(mid, hi)
		if d.err != nil {
			return d.Err()
		}
		if err != nil || d.sampleNumber(fr) > target {
			hi = mid
			continue
		}
		if d.sampleNumber(fr)+int64(fr.BlockSize) > target {
			return d.land(fr, target)
		}
		lo = at
	}
	// Read on from lo to the frame holding p.
	fr, _, err := d.find(lo, d.size)
	for err == nil && d.sampleNumber(fr)+int64(fr.BlockSize) <= target {
		fr, err = d.nextGood()
	}
	if d.err != nil {
		return d.Err()
	}
	if err != nil { // p is past the last good frame
		d.frame, d.gap, d.next, d.at, d.ended = nil, 0, -1, target, true
		return nil
	}
	return d.land(fr, target)
}

// land makes fr, which ends after sample p, the frame being played, from p.
func (d *flacStream) land(fr *frame.Frame, p int64) error {
	s := d.sampleNumber(fr)
	d.frame, d.next, d.at, d.ended = fr, s+int64(fr.BlockSize), p, false
	d.pos, d.gap = int(max(p-s, 0)), int(max(s-p, 0)) // p may fall in a damaged part
	return nil
}

// offsetReader reads a file through a buffer, keeping track of the offset,
// so frames can be located and returned to.
type offsetReader struct {
	f   *os.File
	br  *bufio.Reader
	off int64
	err error // the last read error
}

func newOffsetReader(f *os.File) *offsetReader {
	return &offsetReader{f: f, br: bufio.NewReaderSize(f, 64<<10)}
}

func (r *offsetReader) Read(p []byte) (int, error) {
	n, err := r.br.Read(p)
	r.off += int64(n)
	r.err = err
	if err != nil && !errors.Is(err, io.EOF) && !errors.As(err, new(*fs.PathError)) {
		r.err = nil // not from the disk
	}
	return n, err
}

func (r *offsetReader) seek(off int64) error {
	if _, err := r.f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	r.br.Reset(r.f)
	r.off, r.err = off, nil
	return nil
}
