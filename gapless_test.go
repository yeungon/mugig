package main

import (
	"mugig/internal/audiotest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// flatFLAC writes frames of a constant sample value at rate, so a test can
// tell where each song's audio is, and that no silence comes between.
func flatFLAC(t *testing.T, name string, rate, frames int, value int32) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	s := make([]int32, frames)
	for i := range s {
		s[i] = value
	}
	audiotest.WriteFLAC(t, path, rate, 16, s)
	return path
}

// openTestDeck opens a song whose reading ahead waits for the disk rather
// than playing silence, so the test sees only the song's own audio.
func openTestDeck(t *testing.T, path string) *deck {
	t.Helper()
	d, err := openDeck(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	d.stream.(*readAhead).wait = true
	t.Cleanup(func() { d.stream.Close() })
	return d
}

// The next song starts on the sample after the last of the one before,
// within the same buffer.
func TestDecksJoinWithoutAGap(t *testing.T) {
	a := openTestDeck(t, flatFLAC(t, "a.flac", 44100, 10000, 1000))
	b := openTestDeck(t, flatFLAC(t, "b.flac", 44100, 7000, 2000))
	var switched []string
	ds := &decks{cur: a, next: b, switched: func(old *deck) { switched = append(switched, old.path) }}

	var got []float64
	buf := make([][2]float64, 4096) // the boundary falls inside a buffer
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
		want := 1000.0 / 32768
		if i >= 10000 {
			want = 2000.0 / 32768
		}
		if v != want {
			t.Fatalf("frame %d is %v, want %v", i, v, want)
		}
	}
	if !slices.Equal(switched, []string{a.path}) || ds.cur != b {
		t.Errorf("switched from %v", switched)
	}
}

// waitFor polls cond, which reads the player under its lock, for up to 5 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (p *player) readied() bool {
	p.out.mu.Lock()
	defer p.out.mu.Unlock()
	return p.decks != nil && p.decks.next != nil
}

// Through the real output: the song readied takes over by itself, and
// Play for it carries on with it rather than starting it again.
func TestPlayerGapless(t *testing.T) {
	p := testPlayer(t)
	a := flatFLAC(t, "a.flac", 44100, 22050, 1000)  // half a second
	b := flatFLAC(t, "b.flac", 44100, 132300, 2000) // 3 s
	ends := make(chan error, 4)
	var claimed error
	first := true
	var onEnd func(error)
	onEnd = func(err error) { // as the UI does: move on to the next song
		if first {
			first = false
			claimed = p.Play(b, onEnd)
		}
		ends <- err
	}
	if err := p.Play(a, onEnd); err != nil {
		t.Fatal(err)
	}
	p.Prepare(b)
	waitFor(t, "the next song to be readied", p.readied)
	dev := p.out.dev
	select {
	case err := <-ends:
		if err != nil || claimed != nil {
			t.Fatalf("ended with %v, Play %v", err, claimed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first song never ended")
	}
	if pos := p.Position(); pos <= 0 {
		t.Error("the next song should already be under way, not start again")
	}
	if d := p.Duration(); d != 3*time.Second {
		t.Errorf("duration %v, want the next song's", d)
	}
	if p.out.dev != dev {
		t.Error("the device should stay open")
	}
	p.Stop()
}

// A song at another sample rate isn't joined on: the device has to be
// reopened for it, so the first song simply ends.
func TestPlayerNoJoinAcrossRates(t *testing.T) {
	p := testPlayer(t)
	a := flatFLAC(t, "a.flac", 44100, 22050, 1000)
	hires := flatFLAC(t, "hires.flac", 96000, 96000, 2000)
	ends := make(chan error, 1)
	p.Play(a, func(err error) { ends <- err })
	p.Prepare(hires)
	select {
	case <-ends:
	case <-time.After(5 * time.Second):
		t.Fatal("the first song never ended")
	}
	if p.Duration() != 0 {
		t.Error("nothing should play after the first song")
	}
	// Played normally, it opens the device at its own rate.
	if err := p.Play(hires, func(error) {}); err != nil {
		t.Fatal(err)
	}
	if _, rate := p.Output(); rate != 96000 {
		t.Errorf("rate %d", rate)
	}
}

// Asking for a different next song, or none, drops the one readied.
func TestPlayerPrepareChanges(t *testing.T) {
	p := testPlayer(t)
	p.Play(longFLAC(t, 44100, 16, 4), func(error) {})
	b := flatFLAC(t, "b.flac", 44100, 44100, 2000)
	p.Prepare(b)
	waitFor(t, "the next song to be readied", p.readied)
	p.Prepare("")
	if p.readied() {
		t.Error("Prepare(\"\") should drop the song readied")
	}
	p.Prepare(b)
	waitFor(t, "the next song to be readied again", p.readied)
	p.Stop()
	p.Prepare(b) // stopped: nothing to follow
	if p.readied() {
		t.Error("nothing should be readied while stopped")
	}
}

// following names the song trackEnded will play, in each mode.
func TestFollowingSong(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	follows := func() string {
		t.Helper()
		if f, ok := u.following(); ok {
			return f.Title
		}
		return ""
	}
	u.jump(u.tracks[1])
	if got := follows(); got != "Hey Jude" {
		t.Errorf("list order: %q", got)
	}
	u.playLater(u.tracks[4])
	if got := follows(); got != "Zebra" {
		t.Errorf("Up Next: %q", got)
	}
	u.clearQueue()
	u.setRepeatMode(repeatOne)
	if got := follows(); got != "Help" {
		t.Errorf("repeat one: %q", got)
	}
	u.setRepeatMode(repeatOff)
	u.jump(u.tracks[4])
	if got := follows(); got != "" {
		t.Errorf("end of the list: %q", got)
	}
	u.setRepeatMode(repeatAll)
	if got := follows(); got != "Come Together" {
		t.Errorf("repeat all: %q", got)
	}

	u.toggleShuffle()
	for range 12 { // it is what actually plays next, round after round
		want := follows()
		fp.finish(t)
		if fp.last() != want {
			t.Fatalf("following said %q, %q played", want, fp.last())
		}
	}
}

// The next song is readied only near the end, and again if what follows
// changes.
func TestPrepareNextNearTheEnd(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[0])
	u.prepareNext()
	if fp.next != "" {
		t.Errorf("readied %q three minutes early", fp.next)
	}
	fp.pos = 3*time.Minute - gaplessLead + time.Second
	u.prepareNext()
	if titleOf(fp.next) != "Help" {
		t.Errorf("readied %q", fp.next)
	}
	u.playNext(u.tracks[3])
	u.prepareNext()
	if titleOf(fp.next) != "Bohemian Rhapsody" {
		t.Errorf("after Play Next, readied %q", fp.next)
	}
	u.clearQueue()
	u.jump(u.tracks[4])
	fp.pos = 3*time.Minute - time.Second
	u.prepareNext()
	if fp.next != "" {
		t.Errorf("the last song: readied %q", fp.next)
	}
}

// readAll decodes the whole of path.
func readAll(t *testing.T, path string) (beep.StreamSeekCloser, [][2]float64) {
	t.Helper()
	s, _, err := decode(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var out [][2]float64
	buf := make([][2]float64, 1000)
	for {
		n, ok := s.Stream(buf)
		out = append(out, buf[:n]...)
		if !ok {
			return s, out
		}
	}
}

// An MP3 plays just the sound that was encoded: the encoder's silence and
// the decoder's delay are cut, so it lines up with the WAV it was made
// from, sample for sample, and is as long.
func TestMP3PlaysWithoutPadding(t *testing.T) {
	_, want := readAll(t, "testdata/sample.wav")
	s, got := readAll(t, "testdata/sample.mp3")
	if len(got) != len(want) || s.Len() != len(want) {
		t.Fatalf("%d frames (Len %d), want %d", len(got), s.Len(), len(want))
	}
	var diff, power float64
	for i := range want {
		d := got[i][0] - want[i][0]
		diff += d * d
		power += want[i][0] * want[i][0]
	}
	if diff > power/100 { // lossy, but within 1% (-20 dB) when aligned
		t.Errorf("the MP3 doesn't line up with the WAV: error %.3g of %.3g", diff, power)
	}
	// Right after a seek, too, the sound is the song's, not noise.
	for _, at := range []int{0, 5000, 10000, 15000, 20000} {
		if err := s.Seek(at); err != nil {
			t.Fatal(err)
		}
		buf := make([][2]float64, 1000)
		n, _ := s.Stream(buf)
		if s.Position() != at+n {
			t.Errorf("seeking to %d: position %d after %d", at, s.Position(), n)
		}
		var diff, power float64
		for i := range n {
			d := buf[i][0] - want[at+i][0]
			diff += d * d
			power += want[at+i][0] * want[at+i][0]
		}
		if diff > power/100 {
			t.Errorf("seeking to %d: error %.3g of %.3g", at, diff, power)
		}
	}
}
