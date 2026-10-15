package audio

// Playback speed, keeping the pitch. Playing samples faster would raise the
// pitch too, like a record spun too fast. Instead the sound is cut into
// short overlapping pieces, about a twentieth of a second each, which are
// played at their own speed but taken from further apart in the song (to
// speed up) or closer together (to slow down), each blended into the last.
// This is WSOLA (waveform similarity overlap-add): each piece is taken
// from where its sound best lines up with the end of the one before, so
// the blend doesn't smear or cancel the sound.
//
// At normal speed the audio passes through untouched.

import (
	"math"
)

// speeds offered, as multiples of normal.
var Speeds = []float64{0.5, 0.75, 1, 1.25, 1.5, 1.75, 2}

// Piece lengths, in milliseconds. Longer pieces suit slowing down, shorter
// ones speeding up, as the pieces repeat or skip more of the sound.
const (
	pieceSlowMs = 100 // at half speed
	pieceFastMs = 50  // at double speed
	searchMs    = 15  // how far to look for the best place to join
	blendMs     = 10  // the blend between pieces
	searchRate  = 11025
)

// stretcher plays src at a speed, keeping the pitch. It is used on the
// audio thread; setTempo is called with the output locked.
type stretcher struct {
	src   *readAhead
	tempo float64
	gen   int // src's seek count when the buffers were filled

	rate                  int
	piece, search, blend  int // in frames
	coarse                int // search step for the first, rough look
	in                    [][2]float64
	mid                   [][2]float64 // the end of the last piece, to blend into the next; nil at first
	out                   [][2]float64 // stretched, not yet played
	skip                  float64      // fraction of a frame carried over
	ended                 bool
	weights, monoA, monoB []float64
}

func newStretcher(src *readAhead, rate int, tempo float64) *stretcher {
	s := &stretcher{src: src, rate: rate, tempo: 1, gen: src.seeks()}
	ms := func(m float64) int { return max(1, int(float64(rate)*m/1000)) }
	s.search, s.blend = ms(searchMs), ms(blendMs)
	s.coarse = max(1, rate/searchRate)
	// Weight the middle of the blend most when comparing, so the join is
	// judged where it is heard most.
	s.weights = make([]float64, s.blend)
	for i := range s.weights {
		s.weights[i] = math.Sin(math.Pi * (float64(i) + 0.5) / float64(s.blend))
	}
	s.setTempo(tempo)
	return s
}

// setTempo changes the speed. Going back to normal speed blends into the
// unchanged sound, then passes it through.
func (s *stretcher) setTempo(t float64) {
	s.tempo = t
	if t == 1 {
		return
	}
	// Piece length from the speed, between the slow and fast lengths.
	f := min(max((t-0.5)/1.5, 0), 1)
	s.piece = max(int(float64(s.rate)*(pieceSlowMs+(pieceFastMs-pieceSlowMs)*f)/1000), 3*s.blend)
}

func (s *stretcher) Stream(samples [][2]float64) (int, bool) {
	if g := s.src.seeks(); g != s.gen { // what is buffered is from before a seek
		s.reset(g)
	}
	if s.tempo == 1 && len(s.out) == 0 && len(s.in) == 0 && s.mid == nil {
		return s.pass(samples)
	}
	for len(s.out) < len(samples) && !s.ended {
		if s.tempo == 1 {
			s.backToNormal()
			break
		}
		if !s.fill(max(s.search+s.piece, int(s.tempo*float64(s.piece-s.blend))+1)) {
			// The end of the song: too little is left for a piece, so play
			// the rest as it is.
			s.backToNormal()
			s.ended = true
			break
		}
		s.next()
	}
	n := copy(samples, s.out)
	s.out = append(s.out[:0], s.out[n:]...)
	if n < len(samples) && s.tempo == 1 && !s.ended {
		m, ok := s.pass(samples[n:])
		return n + m, ok || n > 0
	}
	return n, n > 0 || !s.ended
}

// pass plays src unchanged.
func (s *stretcher) pass(samples [][2]float64) (int, bool) {
	n, ok, _ := s.src.streamGen(samples)
	return n, ok
}

// fill reads src until in holds n frames, or src ends. A seek empties the
// buffers, which hold sound from before it.
func (s *stretcher) fill(n int) bool {
	var chunk [1024][2]float64
	for len(s.in) < n {
		k, ok, gen := s.src.streamGen(chunk[:min(len(chunk), n-len(s.in))])
		if gen != s.gen {
			s.reset(gen)
		}
		s.in = append(s.in, chunk[:k]...)
		if !ok {
			return len(s.in) >= n
		}
	}
	return true
}

// next adds one piece to out: blended into the last, and taken from where
// it lines up with it best.
func (s *stretcher) next() {
	at := 0
	if s.mid != nil {
		at = s.bestJoin()
		for i := range s.blend {
			t := (float64(i) + 0.5) / float64(s.blend)
			a, b := s.mid[i], s.in[at+i]
			s.out = append(s.out, [2]float64{a[0]*(1-t) + b[0]*t, a[1]*(1-t) + b[1]*t})
		}
		s.out = append(s.out, s.in[at+s.blend:at+s.piece-s.blend]...)
	} else {
		s.out = append(s.out, s.in[:s.piece-s.blend]...)
	}
	s.mid = append(s.mid[:0], s.in[at+s.piece-s.blend:at+s.piece]...)

	// Move on through the song by the piece's length times the speed.
	s.skip += s.tempo * float64(s.piece-s.blend)
	k := int(s.skip)
	s.skip -= float64(k)
	s.in = append(s.in[:0], s.in[min(k, len(s.in)):]...)
}

// bestJoin returns where in in, within the search distance, the sound best
// matches the end of the last piece: roughly first, every few frames, then
// closely around the best found.
func (s *stretcher) bestJoin() int {
	s.monoA = mono(s.monoA, s.mid)
	s.monoB = mono(s.monoB, s.in[:s.search+s.blend])
	score := func(at, step int) float64 {
		var dot, energy float64
		for i := 0; i < s.blend; i += step {
			x := s.monoB[at+i]
			dot += s.weights[i] * s.monoA[i] * x
			energy += x * x
		}
		return dot / math.Sqrt(energy+1e-12)
	}
	best, bestScore := 0, math.Inf(-1)
	for at := 0; at < s.search; at += s.coarse {
		if v := score(at, s.coarse); v > bestScore {
			best, bestScore = at, v
		}
	}
	rough := best
	bestScore = math.Inf(-1)
	for at := max(rough-s.coarse+1, 0); at < min(rough+s.coarse, s.search); at++ {
		if v := score(at, 1); v > bestScore {
			best, bestScore = at, v
		}
	}
	return best
}

func mono(dst []float64, frames [][2]float64) []float64 {
	dst = dst[:0]
	for _, f := range frames {
		dst = append(dst, f[0]+f[1])
	}
	return dst
}

// backToNormal blends the last piece into the sound where the song has got
// to, plays the sound buffered from there, then passes src through.
func (s *stretcher) backToNormal() {
	if s.mid != nil && len(s.in) >= s.blend {
		for i := range s.blend {
			t := (float64(i) + 0.5) / float64(s.blend)
			a, b := s.mid[i], s.in[i]
			s.out = append(s.out, [2]float64{a[0]*(1-t) + b[0]*t, a[1]*(1-t) + b[1]*t})
		}
		s.out = append(s.out, s.in[s.blend:]...)
	} else {
		s.out = append(s.out, s.mid...)
		s.out = append(s.out, s.in...)
	}
	s.in, s.mid, s.skip = s.in[:0], nil, 0
}

// reset drops what is buffered, after the seek numbered gen.
func (s *stretcher) reset(gen int) {
	s.gen = gen
	s.in, s.mid, s.out, s.skip, s.ended = s.in[:0], nil, s.out[:0], 0, false
}

func (s *stretcher) Err() error { return s.src.Err() }
