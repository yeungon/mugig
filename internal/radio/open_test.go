package radio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// sound stands in for a station's MP3: frame syncs, then bytes that
// count up, so anything lost or added shows.
func sound(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	b[0], b[1] = 0xFF, 0xFB // an MPEG-1 Layer III frame header
	return b
}

// icyBody inserts a note every `every` bytes of s, naming titles[i] at the
// i-th, or none where it is "".
func icyBody(s []byte, every int, titles []string) []byte {
	var out []byte
	for i := 0; len(s) > 0; i++ {
		n := min(every, len(s))
		out = append(out, s[:n]...)
		s = s[n:]
		if n < every {
			break
		}
		note := ""
		if i < len(titles) && titles[i] != "" {
			note = "StreamTitle='" + titles[i] + "';StreamUrl='';"
		}
		size := (len(note) + 15) / 16
		out = append(out, byte(size))
		out = append(out, note...)
		out = append(out, make([]byte, size*16-len(note))...)
	}
	return out
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An Icecast stream plays as it comes, its notes taken out and their
// titles passed on as they change, apostrophes and Latin-1 included.
func TestOpenIcecast(t *testing.T) {
	want := sound(10000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Icy-MetaData") != "1" {
			t.Error("titles weren't asked for")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("icy-metaint", "1000")
		w.Header().Set("icy-br", "128,128")
		w.Header().Set("icy-name", "Test FM ")
		w.Write(icyBody(want, 1000, []string{"", "Artist - It's Mine", "", "Artist - It's Mine", "Caf\xe9 Song"}))
	}))
	defer srv.Close()

	var titles []string
	s, err := Open(context.Background(), srv.URL+"/live", func(t string) { titles = append(titles, t) })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Codec != MP3 || s.Kbps != 128 || s.Name != "Test FM" || s.HLS {
		t.Errorf("stream %+v", s)
	}
	if got := readAll(t, s); !bytes.Equal(got, want) {
		t.Errorf("read %d bytes, want the %d sent, notes taken out", len(got), len(want))
	}
	if !slices.Equal(titles, []string{"Artist - It's Mine", "Café Song"}) {
		t.Errorf("titles %q", titles)
	}
}

// Old Shoutcast servers answer "ICY 200 OK", which still plays.
func TestOpenShoutcast(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	want := sound(3000)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		http.ReadRequest(bufio.NewReader(c))
		fmt.Fprint(c, "ICY 200 OK\r\nicy-name:Old Radio\r\ncontent-type:audio/mpeg\r\n\r\n")
		c.Write(want)
	}()
	s, err := Open(context.Background(), "http://"+ln.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Name != "Old Radio" || s.Codec != MP3 {
		t.Errorf("stream %+v", s)
	}
	if got := readAll(t, s); !bytes.Equal(got, want) {
		t.Errorf("read %d bytes, want %d", len(got), len(want))
	}
}

// A .pls or .m3u playlist leads to the first of its streams that plays.
func TestOpenPlaylists(t *testing.T) {
	adts := []byte{0xFF, 0xF1, 0x50, 0x80, 0x02, 0x1F, 0xFC, 1, 2, 3}
	mux := http.NewServeMux()
	mux.HandleFunc("/gone", http.NotFound)
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream") // found from the bytes
		w.Write(adts)
	})
	mux.HandleFunc("/radio/station.pls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/x-scpls")
		fmt.Fprint(w, "[playlist]\nNumberOfEntries=2\nFile1=/gone\nTitle1=Gone\nFile2=../stream\n")
	})
	mux.HandleFunc("/list.m3u", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "#EXTM3U\r\n#EXTINF:-1,Station\r\n/radio/station.pls\r\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, link := range []string{"/radio/station.pls", "/list.m3u"} {
		s, err := Open(context.Background(), srv.URL+link, nil)
		if err != nil {
			t.Errorf("%s: %v", link, err)
			continue
		}
		if got := readAll(t, s); s.Codec != AAC || !bytes.Equal(got, adts) {
			t.Errorf("%s: %s stream of %d bytes", link, s.Codec, len(got))
		}
		s.Close()
	}
}

// Links that aren't streams say what they are.
func TestOpenNotAStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<!DOCTYPE html><html><body>Listen live!</body></html>")
	})
	mux.HandleFunc("/opus", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg")
		w.Write(append([]byte("OggS\x00\x02"), append(make([]byte, 22), "OpusHead"...)...))
	})
	mux.HandleFunc("/text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error": "no"}`)
	})
	mux.HandleFunc("/empty.pls", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "[playlist]\nNumberOfEntries=0\n")
	})
	mux.HandleFunc("/down", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for link, want := range map[string]string{
		"/page":         "web page",
		"/opus":         "Opus",
		"/text":         "application/json",
		"/empty.pls":    "no streams",
		"/down":         "503",
		"ftp://example": "isn't a web link",
		"example.com":   "isn't a web link",
	} {
		if strings.HasPrefix(link, "/") {
			link = srv.URL + link
		}
		_, err := Open(context.Background(), link, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want it to mention %q", link, err, want)
		}
	}
	if _, err := Open(context.Background(), "http://127.0.0.1:1/", nil); err == nil || !strings.Contains(err.Error(), "127.0.0.1 refused the connection") {
		t.Errorf("nothing there: %v", err)
	}
	if _, err := Open(context.Background(), "http://radio.invalid/", nil); err == nil || !strings.Contains(err.Error(), "couldn't find radio.invalid") {
		t.Errorf("no such site: %v", err)
	}
}

// Hanging up stops a read that is waiting for the station.
func TestCloseStopsWaiting(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(sound(600))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Open(ctx, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Read(make([]byte, 600))
	done := make(chan error)
	go func() {
		_, err := s.Read(make([]byte, 10))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the read succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read kept waiting after hanging up")
	}
	s.Close()
}

func TestStreamTitle(t *testing.T) {
	for note, want := range map[string]string{
		"StreamTitle='A - B';":                      "A - B",
		"StreamTitle='Rock 'n' Roll';StreamUrl='';": "Rock 'n' Roll",
		"StreamTitle='';":                           "",
		"StreamTitle='Caf\xe9';\x00\x00":            "Café",
		"StreamTitle='日本語';":                        "日本語",
	} {
		if got, ok := streamTitle([]byte(note)); !ok || got != want {
			t.Errorf("%q: %q, want %q", note, got, want)
		}
	}
	if _, ok := streamTitle([]byte("StreamUrl='x';")); ok {
		t.Error("a note without a title gave one")
	}
}

func TestSniffCodec(t *testing.T) {
	for _, c := range []struct {
		head []byte
		want Codec
	}{
		{[]byte{0xFF, 0xF1, 0x50}, AAC},
		{[]byte{0xFF, 0xF9, 0x50}, AAC},
		{[]byte{0xFF, 0xFB, 0x90}, MP3},
		{[]byte{0xFF, 0xF3, 0x90}, MP3},
		{[]byte{0, 0, 0xFF, 0xFB}, MP3},
		{[]byte("ID3\x04"), MP3},
		{[]byte("OggS"), Ogg},
	} {
		if got, ok := sniffCodec(c.head); !ok || got != c.want {
			t.Errorf("% x: %s, want %s", c.head, got, c.want)
		}
	}
	if _, ok := sniffCodec([]byte("hello")); ok {
		t.Error("text was taken for sound")
	}
}

// A station that stops sending is hung up on, rather than waited for
// for ever.
func TestStalledStation(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(sound(600))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // until hung up on
	}))
	defer srv.Close()
	old := stallTimeout
	stallTimeout = 200 * time.Millisecond
	defer func() { stallTimeout = old }()

	s, err := Open(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(s)
	if !errors.Is(err, errStalled) {
		t.Errorf("read ended with %v, want %v", err, errStalled)
	}
	s.Close()
	wg.Wait()
}
