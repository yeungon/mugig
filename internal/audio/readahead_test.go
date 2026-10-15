package audio

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// slowSource is a song whose frame i holds the value i/1e6, read from a
// disk that stalls at given frames, as a failing disk does at bad spots.
type slowSource struct {
	mu     sync.Mutex
	pos, n int
	stalls map[int]time.Duration // frame → how long reading it takes
	err    error                 // returned once the end is reached
	closed bool
}

func (s *slowSource) Stream(samples [][2]float64) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pos >= s.n {
		return 0, false
	}
	k := min(len(samples), s.n-s.pos)
	for f, d := range s.stalls {
		if f >= s.pos && f < s.pos+k {
			time.Sleep(d)
		}
	}
	for i := range k {
		v := float64(s.pos+i) / 1e6
		samples[i] = [2]float64{v, -v}
	}
	s.pos += k
	return k, true
}

func (s *slowSource) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pos >= s.n {
		return s.err
	}
	return nil
}
func (s *slowSource) Len() int      { return s.n }
func (s *slowSource) Position() int { s.mu.Lock(); defer s.mu.Unlock(); return s.pos }
func (s *slowSource) Seek(p int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pos = p
	return nil
}
func (s *slowSource) Close() error { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true; return nil }

const testRateRA = beep.SampleRate(44100)

// playRealTime plays r as the audio thread does: 20 ms at a time, every
// 20 ms (sped up 4 times, or twice on shared CI machines, which are slow
// to fill the buffer), noting the slowest call and checking each frame.
func playRealTime(t *testing.T, r *readAhead, frames int) (slowest time.Duration) {
	t.Helper()
	buf := make([][2]float64, testRateRA.N(20*time.Millisecond))
	next := 0
	for next < frames {
		start := time.Now()
		n, ok := r.Stream(buf)
		slowest = max(slowest, time.Since(start))
		for i := range n {
			if want := float64(next+i) / 1e6; buf[i][0] != float64(float32(want)) {
				t.Fatalf("frame %d is %v, want %v", next+i, buf[i][0], want)
			}
		}
		next += n
		if !ok {
			break
		}
		if os.Getenv("CI") != "" {
			time.Sleep(10 * time.Millisecond)
		} else {
			time.Sleep(5 * time.Millisecond)
		}
	}
	return slowest
}

// Stalls of half a second while reading don't reach the audio thread.
func TestReadAheadRidesOutStalls(t *testing.T) {
	src := &slowSource{n: 10 * 44100, stalls: map[int]time.Duration{
		2 * 44100:      500 * time.Millisecond,
		5 * 44100:      300 * time.Millisecond,
		5*44100 + 5000: 300 * time.Millisecond, // two close together
	}}
	r := newReadAhead(src, testRateRA)
	defer r.Close()
	slowest := playRealTime(t, r, src.n)
	// Well within the 20 ms the audio thread has, even under the race
	// detector, against stalls of 300 to 500 ms.
	if slowest > 15*time.Millisecond || r.underran != 0 {
		t.Errorf("slowest call %v, %d underruns", slowest, r.underran)
	}
	if _, ok := r.Stream(make([][2]float64, 10)); ok {
		t.Error("still playing after the end")
	}
}

// If decoding can't keep up, playback waits in silence rather than ending
// the song, and carries on where it was.
func TestReadAheadUnderrunKeepsPlaying(t *testing.T) {
	// The chunk from frame 9 × 4096 on stalls, so 36864 frames come first.
	const before = 9 * readAheadChunk
	src := &slowSource{n: 44100, stalls: map[int]time.Duration{before + 10: 400 * time.Millisecond}}
	r := newReadAhead(src, testRateRA)
	defer r.Close()
	time.Sleep(50 * time.Millisecond) // the decoder is now stuck in the stall
	buf := make([][2]float64, before)
	if n, _ := r.Stream(buf); n != before {
		t.Fatalf("decoded ahead %d frames, want %d", n, before)
	}
	n, ok := r.Stream(buf[:1000])
	if !ok || n != 1000 || buf[0][0] != 0 || r.underran != 1 || r.Position() != before {
		t.Errorf("n %d ok %v first %v underruns %d position %d", n, ok, buf[0][0], r.underran, r.Position())
	}
	time.Sleep(500 * time.Millisecond)
	if n, _ := r.Stream(buf[:10]); n != 10 || buf[0][0] != float64(float32(before/1e6)) {
		t.Errorf("after the stall: %v", buf[0][0])
	}
}

func TestReadAheadSeek(t *testing.T) {
	src := &slowSource{n: 10 * 44100}
	r := newReadAhead(src, testRateRA)
	defer r.Close()
	time.Sleep(50 * time.Millisecond) // let it read ahead
	buf := make([][2]float64, 100)
	for _, p := range []int{7 * 44100, 1000, 0, 10*44100 - 50} {
		if err := r.Seek(p); err != nil {
			t.Fatal(err)
		}
		if r.Position() != p {
			t.Errorf("seek to %d: at %d", p, r.Position())
		}
		n, _ := r.Stream(buf)
		if n == 0 || buf[0][0] != float64(float32(float64(p)/1e6)) {
			t.Errorf("seek to %d: first frame %v", p, buf[0][0])
		}
	}
}

// An error that ended the source is reported once what came before has
// played.
func TestReadAheadErrAfterAudio(t *testing.T) {
	src := &slowSource{n: 1000, err: errors.New("the file couldn't be read")}
	r := newReadAhead(src, testRateRA)
	defer r.Close()
	if r.Err() != nil {
		t.Error("error before the audio before it has played")
	}
	r.Stream(make([][2]float64, 2000))
	if r.Err() == nil {
		t.Error("error lost")
	}
}

func TestReadAheadCloseClosesSource(t *testing.T) {
	src := &slowSource{n: 10 * 44100}
	r := newReadAhead(src, testRateRA)
	r.Close()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		src.mu.Lock()
		closed := src.closed
		src.mu.Unlock()
		if closed {
			return
		}
	}
	t.Error("source not closed")
}
