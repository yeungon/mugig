package main

// Reading ahead. The audio thread must hand over sound every few
// milliseconds; if it has to wait for the disk, the sound breaks up. Most
// reads take a millisecond, but a USB disk waking up, a network share, or a
// failing disk retrying a bad spot can take half a second. So a goroutine
// decodes a few seconds ahead into memory, and the audio thread only ever
// copies from there.

import (
	"sync"

	"github.com/gopxl/beep/v2"
)

const (
	readAheadSeconds = 3    // decoded ahead of what is playing
	readAheadPrefill = 0.25 // seconds decoded before playing starts, and after seeking
	readAheadChunk   = 4096 // frames decoded at a time
)

// readAhead decodes src ahead of playback. Stream is called on the audio
// thread; Seek, Position, Len and Close on the UI thread.
type readAhead struct {
	srcMu sync.Mutex // held while src is used; taken before mu
	src   beep.StreamSeekCloser
	tmp   [][2]float64 // decoding buffer, used with srcMu held
	n     int          // src's length, which doesn't change
	rate  beep.SampleRate

	mu       sync.Mutex
	more     *sync.Cond // signalled when there is room, or on closing
	ring     [][2]float32
	head     int  // next frame to play
	size     int  // frames decoded and not yet played
	pos      int  // the next frame's place in the song
	gen      int  // bumped by Seek, so audio decoded before it is dropped
	done     bool // src has ended
	err      error
	closed   bool
	underran int // times playback caught up with decoding
}

func newReadAhead(src beep.StreamSeekCloser, rate beep.SampleRate) *readAhead {
	r := &readAhead{
		src:  src,
		tmp:  make([][2]float64, readAheadChunk),
		n:    src.Len(),
		rate: rate,
		pos:  src.Position(),
		ring: make([][2]float32, max(rate.N(readAheadSeconds*1e9), 2*readAheadChunk)),
	}
	r.more = sync.NewCond(&r.mu)
	r.prefill()
	go r.decode()
	return r
}

// prefill decodes the start before playing, so playback doesn't open with
// silence. The caller holds srcMu or has the only reference.
func (r *readAhead) prefill() {
	want := r.rate.N(readAheadPrefill * 1e9)
	for {
		r.mu.Lock()
		enough, gen := r.done || r.size >= want, r.gen
		r.mu.Unlock()
		if enough {
			return
		}
		r.decodeChunk(gen)
	}
}

// decode runs until closed, keeping the ring full.
func (r *readAhead) decode() {
	for {
		r.mu.Lock()
		for !r.closed && (r.done || len(r.ring)-r.size < readAheadChunk) {
			r.more.Wait()
		}
		if r.closed {
			r.mu.Unlock()
			r.srcMu.Lock()
			r.src.Close()
			r.srcMu.Unlock()
			return
		}
		gen := r.gen
		r.mu.Unlock()

		r.srcMu.Lock()
		r.decodeChunk(gen)
		r.srcMu.Unlock()
	}
}

// decodeChunk decodes up to a chunk into the ring, unless a seek since gen
// has made it stale. The caller holds srcMu, or has the only reference.
func (r *readAhead) decodeChunk(gen int) {
	r.mu.Lock()
	room := min(len(r.ring)-r.size, len(r.tmp))
	r.mu.Unlock()
	n, ok := r.src.Stream(r.tmp[:room])
	err := r.src.Err()

	r.mu.Lock()
	defer r.mu.Unlock()
	if gen != r.gen {
		return
	}
	for i, s := range r.tmp[:n] {
		r.ring[(r.head+r.size+i)%len(r.ring)] = [2]float32{float32(s[0]), float32(s[1])}
	}
	r.size += n
	if !ok || (n == 0 && room > 0) {
		r.done, r.err = true, err
	}
}

// Stream plays decoded frames. If decoding has fallen behind, it plays
// silence until it catches up, rather than ending the song.
func (r *readAhead) Stream(samples [][2]float64) (int, bool) {
	r.mu.Lock()
	defer r.more.Signal()
	defer r.mu.Unlock()
	n := min(len(samples), r.size)
	for i := range n {
		s := r.ring[(r.head+i)%len(r.ring)]
		samples[i] = [2]float64{float64(s[0]), float64(s[1])}
	}
	r.head = (r.head + n) % len(r.ring)
	r.size -= n
	r.pos += n
	if n == len(samples) {
		return n, true
	}
	if r.done {
		return n, n > 0
	}
	clear(samples[n:])
	r.underran++
	return len(samples), true
}

func (r *readAhead) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 {
		return nil // still playing what came before
	}
	return r.err
}

func (r *readAhead) Len() int { return r.n }

func (r *readAhead) Position() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pos
}

// Seek moves to frame p, dropping what was decoded ahead and decoding a
// little from there before returning. It waits for a chunk being decoded,
// so it can be slow on a slow disk, but the audio thread is never kept
// waiting.
func (r *readAhead) Seek(p int) error {
	r.srcMu.Lock()
	defer r.srcMu.Unlock()
	err := r.src.Seek(p)

	r.mu.Lock()
	r.gen++
	r.head, r.size, r.done, r.err = 0, 0, false, nil
	r.pos = r.src.Position()
	r.mu.Unlock()
	if err != nil {
		return err
	}
	r.prefill()
	r.more.Signal()
	return nil
}

// Close stops decoding. The source is closed once a chunk being decoded is
// done, so this doesn't wait for the disk.
func (r *readAhead) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.more.Broadcast()
	return nil
}
