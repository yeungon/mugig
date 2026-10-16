package audio

import (
	"fmt"
	"math"
	"mugig/internal/audiotest"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// station serves a station's sound: each connection gets the next of
// answers, the last repeating, as http.Handlers.
type station struct {
	mu      sync.Mutex
	answers []http.HandlerFunc
	calls   atomic.Int32
	hungUp  chan struct{} // gets a value whenever a listener hangs up
}

func newStation(t *testing.T, answers ...http.HandlerFunc) (*station, string) {
	s := &station{answers: answers, hungUp: make(chan struct{}, 100)}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv.URL + "/live"
}

func (s *station) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := int(s.calls.Add(1)) - 1
	s.mu.Lock()
	h := s.answers[min(n, len(s.answers)-1)]
	s.mu.Unlock()
	h(w, r)
	s.hungUp <- struct{}{}
}

// mp3Answer sends the sample MP3 over and over, loops times, naming
// title in an Icecast note.
func mp3Answer(t *testing.T, loops int, title string) http.HandlerFunc {
	data, err := os.ReadFile(audiotest.Sample("sample.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("icy-metaint", "4096")
		w.Header().Set("icy-name", "Test FM")
		var body []byte
		for range loops {
			body = append(body, data...)
		}
		note := "StreamTitle='" + title + "';"
		for i := 0; len(body) > 0; i++ {
			n := min(4096, len(body))
			if _, err := w.Write(body[:n]); err != nil {
				return
			}
			body = body[n:]
			if n < 4096 {
				return
			}
			if i > 0 {
				w.Write([]byte{0})
				continue
			}
			size := (len(note) + 15) / 16
			w.Write(append([]byte{byte(size)}, append([]byte(note), make([]byte, size*16-len(note))...)...))
		}
	}
}

func unavailable(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "down", http.StatusServiceUnavailable)
}

// infoLog records a station's news.
type infoLog struct {
	mu    sync.Mutex
	infos []LiveInfo
}

func (l *infoLog) add(i LiveInfo) {
	l.mu.Lock()
	l.infos = append(l.infos, i)
	l.mu.Unlock()
}

func (l *infoLog) all() []LiveInfo {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]LiveInfo(nil), l.infos...)
}

// listen plays d of a station, as fast as it decodes, and returns its
// left channel, stopping early if it ends.
func listen(t *testing.T, l *Live, d time.Duration) []float64 {
	t.Helper()
	l.ra.wait = true // wait for decoding rather than play silence
	var out []float64
	buf := make([][2]float64, 1024)
	for len(out) < l.format.SampleRate.N(d) {
		n, ok := l.ra.Stream(buf)
		for _, s := range buf[:n] {
			out = append(out, s[0])
		}
		if !ok {
			break
		}
	}
	return out
}

func monoRMS(s []float64) float64 {
	var sum float64
	for _, x := range s {
		sum += x * x
	}
	return math.Sqrt(sum / float64(max(len(s), 1)))
}

// shortWaits makes connecting again quick.
func shortWaits(t *testing.T) {
	first, longest := firstWait, longestWait
	firstWait, longestWait = time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { firstWait, longestWait = first, longest })
}

// eachMP3Decoder runs a test with each decoder stations' MP3 can go to:
// macOS's, where there is one, and the MP3 library.
func eachMP3Decoder(t *testing.T, test func(t *testing.T)) {
	t.Run("library", func(t *testing.T) {
		liveApple = false
		defer func() { liveApple = AppleDecoding }()
		test(t)
	})
	if AppleDecoding {
		t.Run("macOS", test)
	}
}

// An MP3 station plays, naming what is on.
func TestLiveMP3(t *testing.T) { eachMP3Decoder(t, testLiveMP3) }

func testLiveMP3(t *testing.T) {
	_, link := newStation(t, mp3Answer(t, 20, "Artist - Song"))
	var log infoLog
	l, err := OpenLive(link, log.add)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if l.Codec != "MP3" || l.Name != "Test FM" || l.HLS || l.SampleRate() != 44100 {
		t.Errorf("station %+v at %d Hz", l, l.SampleRate())
	}
	got := listen(t, l, 2*time.Second)
	if len(got) < 88200 || monoRMS(got) < 0.01 {
		t.Errorf("%d frames at %.3f RMS", len(got), monoRMS(got))
	}
	if infos := log.all(); len(infos) == 0 || infos[0] != (LiveInfo{Title: "Artist - Song"}) {
		t.Errorf("news %+v", infos)
	}
}

// When a station hangs up, it is called back: the sound carries on, and
// the news says it is reconnecting, then not.
func TestLiveReconnects(t *testing.T) {
	shortWaits(t)
	st, link := newStation(t, mp3Answer(t, 2, "First"), unavailable, mp3Answer(t, 20, "Second"))
	var log infoLog
	l, err := OpenLive(link, log.add)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	got := listen(t, l, 3*time.Second) // the first answer lasts about one
	if len(got) < 3*44100 {
		t.Fatalf("stopped after %d frames: %v", len(got), l.ra.Err())
	}
	if n := st.calls.Load(); n != 3 {
		t.Errorf("connected %d times, want 3", n)
	}
	var reconnecting, back bool
	for _, i := range log.all() {
		reconnecting = reconnecting || i.Reconnecting
		back = reconnecting && !i.Reconnecting
	}
	if !reconnecting || !back {
		t.Errorf("news %+v", log.all())
	}
}

// A station that stays down is given up on after a few tries, saying why.
func TestLiveGivesUp(t *testing.T) {
	shortWaits(t)
	st, link := newStation(t, mp3Answer(t, 1, ""), unavailable)
	l, err := OpenLive(link, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	listen(t, l, time.Minute)
	if err := l.ra.Err(); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("ended with %v", err)
	}
	if n := st.calls.Load(); n != 1+maxReconnects {
		t.Errorf("connected %d times, want %d", n, 1+maxReconnects)
	}
}

// A connection that keeps breaking soon after it is made is given up on
// too, rather than made again for ever.
func TestLiveGivesUpOnFlakyStation(t *testing.T) {
	shortWaits(t)
	st, link := newStation(t, mp3Answer(t, 1, ""))
	l, err := OpenLive(link, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	listen(t, l, time.Minute)
	if l.ra.Err() == nil {
		t.Error("no reason given")
	}
	if n := st.calls.Load(); n != 1+maxReconnects {
		t.Errorf("connected %d times, want %d", n, 1+maxReconnects)
	}
}

// An HLS broadcast that has ended plays to its end, and isn't called
// back.
func TestLiveHLSEnds(t *testing.T) { eachMP3Decoder(t, testLiveHLSEnds) }

func testLiveHLSEnds(t *testing.T) {
	data, err := os.ReadFile(audiotest.Sample("sample.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/rec.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:0.55,\na.mp3\n#EXTINF:0.55,\nb.mp3\n#EXT-X-ENDLIST\n")
	})
	var pieces atomic.Int32
	mux.HandleFunc("/a.mp3", func(w http.ResponseWriter, r *http.Request) { pieces.Add(1); w.Write(data) })
	mux.HandleFunc("/b.mp3", func(w http.ResponseWriter, r *http.Request) { pieces.Add(1); w.Write(data) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	l, err := OpenLive(srv.URL+"/rec.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	got := listen(t, l, time.Minute)
	if secs := float64(len(got)) / 44100; secs < 0.9 || secs > 1.2 || l.ra.Err() != nil {
		t.Errorf("played %.2f s, ended with %v; want both pieces, about 1.1 s", secs, l.ra.Err())
	}
	if n := pieces.Load(); n != 2 {
		t.Errorf("fetched %d pieces, want 2", n)
	}
}

// An Ogg Vorbis station plays too.
func TestLiveOgg(t *testing.T) {
	data, err := os.ReadFile(audiotest.Sample("sample.ogg"))
	if err != nil {
		t.Fatal(err)
	}
	shortWaits(t)
	_, link := newStation(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/ogg")
		w.Write(data)
	})
	l, err := OpenLive(link, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if l.Codec != "Ogg Vorbis" {
		t.Errorf("codec %q", l.Codec)
	}
	// The sample lasts half a second; each time it ends, the station is
	// called back.
	if got := listen(t, l, time.Second); len(got) < l.format.SampleRate.N(time.Second) || monoRMS(got) < 0.01 {
		t.Errorf("%d frames at %.3f RMS: %v", len(got), monoRMS(got), l.ra.Err())
	}
}

// The player plays a station at its rate, at normal speed whatever the
// speed is set to, with no length and no seeking. Stopping hangs up.
func TestPlayerPlaysLive(t *testing.T) {
	st, link := newStation(t, mp3Answer(t, 1000, ""))
	p := testPlayer(t)
	p.SetSpeed(1.5)
	l, err := OpenLive(link, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.PlayLive(l, func(error) {}); err != nil {
		t.Fatal(err)
	}
	if dev, track := p.Output(); dev != 44100 || track != 44100 {
		t.Errorf("output %d/%d", dev, track)
	}
	if p.Duration() != 0 {
		t.Errorf("duration %v", p.Duration())
	}
	if err := p.Seek(time.Second); err == nil {
		t.Error("seeking a station worked")
	}
	cur := p.current()
	if cur.stretch.tempo != 1 {
		t.Errorf("playing at %vx", cur.stretch.tempo)
	}
	p.SetSpeed(2)
	if cur.stretch.tempo != 1 {
		t.Errorf("speed changed to %vx", cur.stretch.tempo)
	}
	time.Sleep(300 * time.Millisecond)
	if p.Position() <= 0 {
		t.Error("not playing")
	}
	p.Stop()
	select {
	case <-st.hungUp:
	case <-time.After(5 * time.Second):
		t.Error("stopping didn't hang up")
	}
}
