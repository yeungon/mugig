package audio

import (
	"math"
	"testing"
)

// toneSource is a song of n frames: a sine of hz on the left, and on the
// right, if stereo, the same an octave up; else silence. If silentFrom is
// set, it is silent from that frame on.
type toneSource struct {
	pos, n, rate int
	hz           float64
	stereo       bool
	silentFrom   int
}

func (s *toneSource) at(i int) [2]float64 {
	if s.silentFrom > 0 && i >= s.silentFrom {
		return [2]float64{}
	}
	t := float64(i) / float64(s.rate)
	l := 0.5 * math.Sin(2*math.Pi*s.hz*t)
	if !s.stereo {
		return [2]float64{l, 0}
	}
	return [2]float64{l, 0.5 * math.Sin(2*math.Pi*2*s.hz*t)}
}

func (s *toneSource) Stream(samples [][2]float64) (int, bool) {
	k := min(len(samples), s.n-s.pos)
	for i := range k {
		samples[i] = s.at(s.pos + i)
	}
	s.pos += k
	return k, k > 0
}

func (s *toneSource) Err() error       { return nil }
func (s *toneSource) Len() int         { return s.n }
func (s *toneSource) Position() int    { return s.pos }
func (s *toneSource) Seek(p int) error { s.pos = p; return nil }
func (s *toneSource) Close() error     { return nil }

const stretchRate = 44100

func stretched(t *testing.T, src *toneSource, tempo float64) (*stretcher, *readAhead) {
	t.Helper()
	r := newReadAhead(src, stretchRate)
	r.wait = true // faster than real time, so wait for decoding
	t.Cleanup(func() { r.Close() })
	s := newStretcher(r, stretchRate, tempo)
	return s, r
}

// playAllOf collects everything s plays.
func playAllOf(s interface {
	Stream([][2]float64) (int, bool)
}) [][2]float64 {
	var out [][2]float64
	buf := make([][2]float64, 882) // 20 ms, as the audio thread asks
	for {
		n, ok := s.Stream(buf)
		out = append(out, buf[:n]...)
		if !ok {
			return out
		}
	}
}

// frequency measures the frequency of channel c over frames, from its
// upward zero crossings.
func frequency(frames [][2]float64, c int) float64 {
	first, last, count := -1, -1, 0
	for i := 1; i < len(frames); i++ {
		if frames[i-1][c] < 0 && frames[i][c] >= 0 {
			if first < 0 {
				first = i
			}
			last = i
			count++
		}
	}
	return float64(count-1) * stretchRate / float64(last-first)
}

func rms(frames [][2]float64, c int) float64 {
	sum := 0.0
	for _, f := range frames {
		sum += f[c] * f[c]
	}
	return math.Sqrt(sum / float64(len(frames)))
}

// At any speed the song takes 1/speed as long, at the same pitch, with no
// clicks where pieces join and no loss of level from blending.
func TestStretchKeepsPitch(t *testing.T) {
	for _, tempo := range Speeds {
		if tempo == 1 {
			continue
		}
		src := &toneSource{n: 5 * stretchRate, rate: stretchRate, hz: 440, stereo: true}
		s, _ := stretched(t, src, tempo)
		out := playAllOf(s)

		want := float64(src.n) / tempo
		if got := float64(len(out)); math.Abs(got-want) > 0.15*stretchRate {
			t.Errorf("%v×: %d frames, want about %.0f", tempo, len(out), want)
		}
		body := out[stretchRate/10 : len(out)-stretchRate/10]
		if l, r := frequency(body, 0), frequency(body, 1); math.Abs(l-440) > 2 || math.Abs(r-880) > 4 {
			t.Errorf("%v×: %0.1f and %0.1f Hz, want 440 and 880", tempo, l, r)
		}
		if l := rms(body, 0); math.Abs(l-0.5/math.Sqrt2) > 0.02 {
			t.Errorf("%v×: level %.3f, want %.3f", tempo, l, 0.5/math.Sqrt2)
		}
		// A sine moves at most 2π·f/rate × amplitude per frame; a bad join
		// jumps further.
		limit := 1.3 * 2 * math.Pi * 880 / stretchRate * 0.5
		for i := 1; i < len(body); i++ {
			if d := math.Abs(body[i][1] - body[i-1][1]); d > limit {
				t.Errorf("%v×: jump of %.4f at frame %d (limit %.4f)", tempo, d, i, limit)
				break
			}
		}
	}
}

// At normal speed the sound is passed through exactly.
func TestStretchNormalSpeedUntouched(t *testing.T) {
	src := &toneSource{n: stretchRate, rate: stretchRate, hz: 440, stereo: true}
	s, _ := stretched(t, src, 1)
	out := playAllOf(s)
	if len(out) != src.n {
		t.Fatalf("%d frames, want %d", len(out), src.n)
	}
	for i, f := range out {
		w := src.at(i)
		if f != [2]float64{float64(float32(w[0])), float64(float32(w[1]))} {
			t.Fatalf("frame %d changed", i)
		}
	}
}

// Channels stay apart: silence on the right stays silent.
func TestStretchKeepsChannels(t *testing.T) {
	src := &toneSource{n: 2 * stretchRate, rate: stretchRate, hz: 300}
	s, _ := stretched(t, src, 1.5)
	if r := rms(playAllOf(s), 1); r != 0 {
		t.Errorf("right channel level %v, want silence", r)
	}
}

// Changing speed while playing, back to normal included, carries on
// smoothly and without losing the place.
func TestStretchChangeSpeed(t *testing.T) {
	src := &toneSource{n: 6 * stretchRate, rate: stretchRate, hz: 440, stereo: true}
	s, r := stretched(t, src, 1)
	buf := make([][2]float64, 882)
	var out [][2]float64
	for i := 0; ; i++ {
		switch i {
		case 50:
			s.setTempo(2)
		case 100:
			s.setTempo(0.75)
		case 150:
			s.setTempo(1)
		}
		n, ok := s.Stream(buf)
		out = append(out, buf[:n]...)
		if !ok {
			break
		}
	}
	limit := 1.3 * 2 * math.Pi * 880 / stretchRate * 0.5
	for i := 1; i < len(out); i++ {
		if d := math.Abs(out[i][1] - out[i-1][1]); d > limit {
			t.Errorf("jump of %.4f at frame %d (limit %.4f)", d, i, limit)
			break
		}
	}
	// 50 calls at 1×, 50 at 2× (100 calls' worth of song), 50 at 0.75×
	// (37.5), then the rest at 1×.
	want := float64(src.n) - 882*(50+100+37.5) + 882*150
	if math.Abs(float64(len(out))-want) > 0.1*stretchRate {
		t.Errorf("%d frames, want about %.0f", len(out), want)
	}
	if r.Position() != src.n {
		t.Errorf("finished at %d of %d", r.Position(), src.n)
	}
}

// After a seek, what plays is from the new place, not sound buffered from
// before.
func TestStretchSeek(t *testing.T) {
	// Silent from 5 s, so anything audible after seeking there is old sound.
	src := &toneSource{n: 10 * stretchRate, rate: stretchRate, hz: 440, stereo: true, silentFrom: 5 * stretchRate}
	s, r := stretched(t, src, 1.5)
	buf := make([][2]float64, 882)
	for range 20 {
		s.Stream(buf)
	}
	r.Seek(5 * stretchRate)
	s.Stream(buf)
	if l := rms(buf, 0); l != 0 {
		t.Errorf("level %v after seeking to silence: old sound played", l)
	}
}
