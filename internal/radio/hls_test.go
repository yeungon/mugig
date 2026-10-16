package radio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// tsPackets splits data into transport stream packets of pid, padding
// the last with an adaptation field.
func tsPackets(pid int, data []byte) []byte {
	var out []byte
	for first := true; first || len(data) > 0; first = false {
		p := make([]byte, tsPacket)
		p[0], p[1], p[2] = 0x47, byte(pid>>8&0x1F), byte(pid)
		if first {
			p[1] |= 0x40
		}
		n := min(len(data), 184)
		if n < 184 {
			af := 184 - n // the adaptation field, its length byte included
			p[3], p[4] = 0x30, byte(af-1)
			for i := 5; i < 4+af; i++ {
				p[i] = 0xFF
			}
			if af > 1 {
				p[5] = 0
			}
			copy(p[4+af:], data[:n])
		} else {
			p[3] = 0x10
			copy(p[4:], data[:n])
		}
		data = data[n:]
		out = append(out, p...)
	}
	return out
}

// tsMux makes a transport stream of one program, as HLS pieces are,
// whose streams are of kinds (stream types); the first carries sound.
func tsMux(sound []byte, kinds ...byte) []byte {
	pat := []byte{0, 0x00, 0xB0, 13, 0, 1, 0xC1, 0, 0, 0, 1, 0xF0, 0x00, 0, 0, 0, 0}
	pmt := []byte{0, 0x02, 0xB0, byte(13 + 5*len(kinds)), 0, 1, 0xC1, 0, 0, 0xE1, 0x00, 0xF0, 0x00}
	for i, k := range kinds {
		pid := 0x100 + i
		pmt = append(pmt, k, 0xE0|byte(pid>>8), byte(pid), 0xF0, 0x00)
	}
	pmt = append(pmt, 0, 0, 0, 0)
	pes := append([]byte{0, 0, 1, 0xC0, 0, 0, 0x80, 0x80, 5, 0x21, 0, 1, 0, 1}, sound...)
	out := append(tsPackets(0, pat), tsPackets(0x1000, pmt)...)
	return append(out, tsPackets(0x100, pes)...)
}

// The sound comes out of a transport stream whole, found by the tables.
func TestTSDemux(t *testing.T) {
	want := sound(5000)
	var d tsDemuxer
	got, err := d.extract(tsMux(want, 0x0F))
	if err != nil || !bytes.Equal(got, want) || d.codec != AAC {
		t.Fatalf("got %d bytes of %s, %v; want %d", len(got), d.codec, err, len(want))
	}
	// A later piece without the tables is read by what was learnt.
	if got, err := d.extract(tsPackets(0x100, append([]byte{0, 0, 1, 0xC0, 0, 0, 0x80, 0, 0}, 7, 8, 9))); err != nil || !bytes.Equal(got, []byte{7, 8, 9}) {
		t.Errorf("second piece: % x, %v", got, err)
	}

	var mp3 tsDemuxer
	if _, err := mp3.extract(tsMux(want, 0x1B, 0x03)); err != nil || mp3.codec != MP3 || mp3.audio != 0x101 {
		t.Errorf("MP3 after video: %s at %#x, %v", mp3.codec, mp3.audio, err)
	}
	var ac3 tsDemuxer
	if _, err := ac3.extract(tsMux(want, 0x81)); err == nil || !strings.Contains(err.Error(), "AC-3") {
		t.Errorf("AC-3: %v", err)
	}
}

// adtsFrames splits an ADTS stream into its frames' headers and contents.
func adtsFrames(t *testing.T, b []byte) (headers, frames [][]byte) {
	t.Helper()
	for len(b) > 0 {
		if len(b) < 7 || b[0] != 0xFF || b[1]&0xF6 != 0xF0 {
			t.Fatalf("not an ADTS frame: % x", b[:min(len(b), 8)])
		}
		size := int(b[3]&3)<<11 | int(b[4])<<3 | int(b[5]>>5)
		header := 7
		if b[1]&1 == 0 {
			header = 9
		}
		headers, frames = append(headers, b[:header]), append(frames, b[header:size])
		b = b[size:]
	}
	return headers, frames
}

func sample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// mp4 builds MP4 boxes.
func mp4box(kind string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), append([]byte(kind), body...)...)
}

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }

// fmp4Init is an initialization section for one track of AAC described
// by config, behind a track of video.
func fmp4Init(config []byte) []byte {
	trak := func(id uint32, handler string, entry []byte) []byte {
		return mp4box("trak",
			mp4box("tkhd", u32(7), u32(0), u32(0), u32(id), make([]byte, 68)),
			mp4box("mdia", mp4box("hdlr", u32(0), u32(0), []byte(handler), make([]byte, 13)),
				mp4box("minf", mp4box("stbl", mp4box("stsd", u32(0), u32(1), entry)))))
	}
	dsi := append([]byte{5, byte(len(config))}, config...)
	dcd := append([]byte{4, byte(13 + len(dsi)), 0x40, 0x15, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0}, dsi...)
	es := append([]byte{3, byte(3 + len(dcd)), 0, 1, 0}, dcd...)
	mp4a := mp4box("mp4a", make([]byte, 6), u16(1), make([]byte, 8), u16(2), u16(16), u16(0), u16(0), u32(48000<<16),
		mp4box("esds", u32(0), es))
	return append(mp4box("ftyp", []byte("iso6"), u32(0), []byte("iso6cmfc")),
		mp4box("moov",
			trak(1, "vide", mp4box("avc1", make([]byte, 70))),
			trak(2, "soun", mp4a),
			mp4box("mvex", mp4box("trex", u32(0), u32(2), u32(1), u32(1024), u32(0), u32(0))))...)
}

// fmp4Piece is a piece holding frames of track 2.
func fmp4Piece(frames [][]byte) []byte {
	moof := func(offset uint32) []byte {
		trun := [][]byte{u32(0x201), u32(uint32(len(frames))), u32(offset)}
		for _, f := range frames {
			trun = append(trun, u32(uint32(len(f))))
		}
		return mp4box("moof", mp4box("mfhd", u32(0), u32(1)),
			mp4box("traf", mp4box("tfhd", u32(0x020000), u32(2)), mp4box("trun", trun...)))
	}
	m := moof(0)
	m = moof(uint32(len(m) + 8))
	return append(append(mp4box("styp", []byte("msdh")), m...), mp4box("mdat", frames...)...)
}

// AAC in fragmented MP4 comes out as the same ADTS stream it would be
// sent as.
func TestFMP4(t *testing.T) {
	headers, frames := adtsFrames(t, sample(t, "sample.aac"))
	h := headers[0]
	// The AudioSpecificConfig the ADTS header describes.
	aot, freq, ch := int(h[2]>>6)+1, int(h[2]>>2&0xF), int(h[2]&1)<<2|int(h[3]>>6)
	config := []byte{byte(aot<<3 | freq>>1), byte(freq&1<<7 | ch<<3)}

	f, err := parseInit(fmp4Init(config))
	if err != nil {
		t.Fatal(err)
	}
	if f.track != 2 || f.codec != AAC || f.defaultSize != 0 {
		t.Errorf("init: %+v", f)
	}
	got, err := f.extract(fmp4Piece(frames[:5]))
	if err != nil {
		t.Fatal(err)
	}
	gotHeaders, gotFrames := adtsFrames(t, got)
	if len(gotFrames) != 5 {
		t.Fatalf("%d frames", len(gotFrames))
	}
	for i := range gotFrames {
		// The same kind of AAC, rate and channels.
		if !bytes.Equal(gotFrames[i], frames[i]) || gotHeaders[i][2] != headers[i][2] || gotHeaders[i][3]>>6 != headers[i][3]>>6 {
			t.Errorf("frame %d: header % x, want % x", i, gotHeaders[i], headers[i])
		}
	}
}

// HE-AAC's config, with SBR, makes the header of the plain AAC under it.
func TestADTSHeaderForHEAAC(t *testing.T) {
	// SBR (5), 24 kHz (6), stereo (2), 48 kHz (3), then AAC LC (2).
	var bits uint64
	for _, f := range [][2]uint64{{5, 5}, {6, 4}, {2, 4}, {3, 4}, {2, 5}} {
		bits = bits<<f[1] | f[0]
	}
	bits <<= 64 - 22
	config := binary.BigEndian.AppendUint64(nil, bits)[:3]
	h, err := adtsHeader(config)
	if err != nil {
		t.Fatal(err)
	}
	if profile, freq, ch := h[2]>>6, h[2]>>2&0xF, h[2]&1<<2|h[3]>>6; profile != 1 || freq != 6 || ch != 2 {
		t.Errorf("profile %d, rate index %d, channels %d", profile, freq, ch)
	}
	if _, err := adtsHeader([]byte{0x0F, 0x80}); err == nil { // an explicit rate
		t.Error("an unusual rate was accepted")
	}
}

func TestChooseVariant(t *testing.T) {
	base, _ := url.Parse("https://radio.example/live/master.m3u8")
	for name, c := range map[string]struct{ text, want string }{
		"best sound": {`#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=48000,CODECS="mp4a.40.5"
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=320000,CODECS="mp4a.40.2"
high.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=96000,CODECS="mp4a.40.5"
mid.m3u8`, "https://radio.example/live/high.m3u8"},
		"sound kept apart from video": {`#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="Commentary",URI="audio/commentary.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="English",DEFAULT=YES,URI="audio/en.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=800000,CODECS="avc1.4d401f,mp4a.40.2",AUDIO="aud"
video/800.m3u8`, "https://radio.example/live/audio/en.m3u8"},
		"smallest video": {`#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,CODECS="avc1.4d401f,mp4a.40.2"
800.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=400000,RESOLUTION=640x360,CODECS="avc1.4d401f,mp4a.40.2"
/400.m3u8`, "https://radio.example/400.m3u8"},
	} {
		m, pl := parseHLS(c.text, base)
		if m == nil || pl != nil {
			t.Errorf("%s: not read as a master playlist", name)
			continue
		}
		if got, _ := m.choose(); got != c.want {
			t.Errorf("%s: chose %s, want %s", name, got, c.want)
		}
	}
}

// liveServer is a live HLS broadcast: a new piece every `every`, the
// playlist listing the latest six. Each piece holds its number.
type liveServer struct {
	start time.Time
	every time.Duration
	piece func(n int) []byte
	mu    sync.Mutex
	asked []int // the pieces fetched, in order
}

func (s *liveServer) newest() int { return 10 + int(time.Since(s.start)/s.every) }

func (s *liveServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		n := s.newest()
		fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n", int(s.every.Seconds()+0.5), n-5)
		for i := n - 5; i <= n; i++ {
			fmt.Fprintf(w, "#EXTINF:%.3f,\npiece-%d.ts\n", s.every.Seconds(), i)
		}
		return
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/piece-"), ".ts"))
	if err != nil || n > s.newest() || n < s.newest()-5 {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	s.asked = append(s.asked, n)
	s.mu.Unlock()
	w.Write(s.piece(n))
}

// A live broadcast starts three pieces from the newest and plays each
// piece once, in order, fetching the playlist again for more.
func TestHLSLive(t *testing.T) {
	live := &liveServer{start: time.Now(), every: 500 * time.Millisecond, piece: func(n int) []byte {
		return tsMux([]byte(fmt.Sprintf("piece %d;", n)), 0x0F)
	}}
	srv := httptest.NewServer(live)
	defer srv.Close()

	s, err := Open(context.Background(), srv.URL+"/live.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Codec != AAC || !s.HLS {
		t.Errorf("stream %+v", s)
	}
	var got []string
	buf := make([]byte, 64)
	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, err := s.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Split(strings.TrimSuffix(string(buf[:n]), ";"), ";")...)
	}
	s.Close()
	if len(got) < 5 {
		t.Fatalf("only %q", got)
	}
	for i, p := range got {
		if want := fmt.Sprintf("piece %d", 8+i); p != want {
			t.Fatalf("read %q, want each piece once from piece 8", got)
		}
	}
}

// A broadcast that has ended, such as a recording, plays to its end.
// Its pieces here are packed AAC behind an ID3 tag, from a master
// playlist.
func TestHLSEnded(t *testing.T) {
	aac := sample(t, "sample.aac")
	third := len(aac) / 3
	id3 := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x05"), "12345"...)
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=128000,CODECS=\"mp4a.40.2\"\nrec/index.m3u8\n")
	})
	mux.HandleFunc("/rec/index.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-PLAYLIST-TYPE:VOD\n")
		for i := range 3 {
			fmt.Fprintf(w, "#EXTINF:0.33,\n%d.aac\n", i)
		}
		fmt.Fprint(w, "#EXT-X-ENDLIST\n")
	})
	for i := range 3 {
		mux.HandleFunc(fmt.Sprintf("/rec/%d.aac", i), func(w http.ResponseWriter, r *http.Request) {
			end := (i + 1) * third
			if i == 2 {
				end = len(aac)
			}
			w.Write(append(id3, aac[i*third:end]...))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := Open(context.Background(), srv.URL+"/master.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Codec != AAC || s.Kbps != 128 {
		t.Errorf("stream %+v", s)
	}
	got, err := io.ReadAll(s)
	if err != nil || !bytes.Equal(got, aac) {
		t.Errorf("read %d bytes, %v; want the %d sent, ID3 taken out", len(got), err, len(aac))
	}
}

// Fragmented MP4 pieces are read using their playlist's initialization
// section, fetched once.
func TestHLSFragmentedMP4(t *testing.T) {
	headers, frames := adtsFrames(t, sample(t, "sample.aac"))
	h := headers[0]
	aot, freq, ch := int(h[2]>>6)+1, int(h[2]>>2&0xF), int(h[2]&1)<<2|int(h[3]>>6)
	config := []byte{byte(aot<<3 | freq>>1), byte(freq&1<<7 | ch<<3)}
	var inits int
	mux := http.NewServeMux()
	mux.HandleFunc("/a.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:0.5,\n0.m4s\n#EXTINF:0.5,\n1.m4s\n#EXT-X-ENDLIST\n")
	})
	mux.HandleFunc("/init.mp4", func(w http.ResponseWriter, r *http.Request) {
		inits++
		w.Write(fmp4Init(config))
	})
	mux.HandleFunc("/0.m4s", func(w http.ResponseWriter, r *http.Request) { w.Write(fmp4Piece(frames[:10])) })
	mux.HandleFunc("/1.m4s", func(w http.ResponseWriter, r *http.Request) { w.Write(fmp4Piece(frames[10:])) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := Open(context.Background(), srv.URL+"/a.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, gotFrames := adtsFrames(t, got); len(gotFrames) != len(frames) || inits != 1 || s.Codec != AAC {
		t.Errorf("%d frames of %d, %s, initialization fetched %d times", len(gotFrames), len(frames), s.Codec, inits)
	}
}

// Encrypted broadcasts are refused, saying why.
func TestHLSEncrypted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:6,\n0.ts\n")
	}))
	defer srv.Close()
	if _, err := Open(context.Background(), srv.URL+"/x.m3u8", nil); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Errorf("got %v", err)
	}
}

func TestSkipID3(t *testing.T) {
	tag := append([]byte("ID3\x04\x00\x10\x00\x00\x01\x00"), make([]byte, 128+10)...) // with a footer
	if got := skipID3(append(tag, 1, 2)); !bytes.Equal(got, []byte{1, 2}) {
		t.Errorf("% x", got)
	}
	if got := skipID3([]byte("ID3\x04\x00\x00\x00\x00\x7F\x7F")); got != nil {
		t.Errorf("a tag longer than the piece: % x", got)
	}
}

func TestRoundKbps(t *testing.T) {
	for in, want := range map[float64]int{97.3: 96, 125: 128, 64.2: 64, 500: 500, 71.5: 72} {
		if got := roundKbps(in); got != want {
			t.Errorf("%v: %d, want %d", in, got, want)
		}
	}
}
