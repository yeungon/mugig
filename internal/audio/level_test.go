package audio

import (
	"math"
	"testing"
)

// A new levelling gain is reached by gliding 20 dB a second, in even
// steps of decibels, then held exactly; at 1 the sound is untouched.
func TestLevelGlides(t *testing.T) {
	const rate = 48000
	ones := func(n int) [][2]float64 {
		s := make([][2]float64, n)
		for i := range s {
			s[i] = [2]float64{1, -1}
		}
		return s
	}
	l := level{step: math.Pow(10, levelGlideDB/20/float64(rate))}
	l.set(1)
	s := ones(100)
	l.apply(s)
	if s[99] != [2]float64{1, -1} {
		t.Errorf("at 1: %v, want untouched", s[99])
	}
	l.target = 0.5 // down 6 dB: 0.3 s
	s = ones(rate * 15 / 100)
	l.apply(s)
	if g := dB(s[len(s)-1][0]); math.Abs(g+3) > 0.05 {
		t.Errorf("0.15 s into gliding down 6 dB: %.2f dB, want -3", g)
	}
	s = ones(rate * 16 / 100)
	l.apply(s)
	if s[len(s)-1] != [2]float64{0.5, -0.5} {
		t.Errorf("after the glide: %v, want exactly 0.5", s[len(s)-1])
	}
}

// The player levels each song by the gain asked for: at once when it
// starts, and for the song readied to follow; a song's gain can be asked
// for again, and turning levelling off goes back to 1.
func TestPlayerLevelling(t *testing.T) {
	p := testPlayer(t)
	a, b := longFLAC(t, 44100, 16, 3), longFLAC(t, 44100, 16, 3)
	gains := map[string]float64{a: -6, b: 3}
	p.SetLevelling(func(path string) float64 { return gains[path] })
	levels := func() (cur, next level) {
		p.out.mu.Lock()
		defer p.out.mu.Unlock()
		cur = p.decks.cur.level
		if p.decks.next != nil {
			next = p.decks.next.level
		}
		return cur, next
	}
	near := func(got, wantDB float64) bool { return math.Abs(dB(got)-wantDB) < 0.01 }

	if err := p.Play(a, func(error) {}); err != nil {
		t.Fatal(err)
	}
	if cur, _ := levels(); !near(cur.gain, -6) || cur.gain != cur.target {
		t.Errorf("playing: gain %v → %v, want -6 dB at once", dB(cur.gain), dB(cur.target))
	}
	p.Prepare(b)
	waitFor(t, "the next song to be readied", p.readied)
	if _, next := levels(); !near(next.gain, 3) {
		t.Errorf("readied: gain %v dB, want 3", dB(next.gain))
	}

	gains[a] = -12 // its loudness has just been measured
	p.Relevel(a)
	if cur, next := levels(); !near(cur.target, -12) || !near(next.target, 3) {
		t.Errorf("after relevelling a: %v and %v dB", dB(cur.target), dB(next.target))
	}
	p.SetLevelling(nil)
	if cur, next := levels(); cur.target != 1 || next.target != 1 {
		t.Errorf("levelling off: targets %v and %v, want 1", cur.target, next.target)
	}
}

// Joined without a gap, each song keeps its own levelling gain: the new
// one applies from the very first sample of the song after.
func TestLevelChangesAtTheJoin(t *testing.T) {
	a := openTestDeck(t, flatFLAC(t, "a.flac", 44100, 10000, 8000))
	b := openTestDeck(t, flatFLAC(t, "b.flac", 44100, 7000, 8000))
	a.level.set(0.5)
	b.level.set(0.25)
	ds := &decks{cur: a, next: b, switched: func(*deck) {}}
	var got []float64
	buf := make([][2]float64, 4096) // the join falls inside a buffer
	for {
		n, ok := ds.Stream(buf)
		for _, s := range buf[:n] {
			got = append(got, s[0])
		}
		if !ok {
			break
		}
	}
	if len(got) != 17000 {
		t.Fatalf("%d frames, want 17000", len(got))
	}
	for i, v := range got {
		want := 8000.0 / 32768 * 0.5
		if i >= 10000 {
			want = 8000.0 / 32768 * 0.25
		}
		if v != want {
			t.Fatalf("frame %d is %v, want %v", i, v, want)
		}
	}
}
