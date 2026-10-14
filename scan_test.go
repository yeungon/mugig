package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWalkFindsOnlyAudio(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.mp3", "b.FLAC", "c.txt", "sub/d.ogg", "sub/deeper/e.wav", "f.m4a"} {
		writeFile(t, filepath.Join(dir, name), []byte("x"))
	}
	writeFile(t, filepath.Join(dir, "empty.mp3"), nil) // an interrupted download: nothing to play

	paths := make(chan string)
	go walk(context.Background(), dir, paths)
	var got []string
	for p := range paths {
		rel, _ := filepath.Rel(dir, p)
		got = append(got, rel)
	}
	slices.Sort(got)
	want := []string{"a.mp3", "b.FLAC", "sub/d.ogg", "sub/deeper/e.wav"}
	if !slices.Equal(got, want) {
		t.Errorf("walk found %v, want %v", got, want)
	}
}

func TestReadTrackTags(t *testing.T) {
	audio, err := os.ReadFile("testdata/sample.mp3")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "file name.mp3")
	tag := id3v23(map[string]string{"TIT2": "Song", "TPE1": "Artist", "TALB": "Album", "TYER": "1999"})
	writeFile(t, path, append(tag, audio...))

	got := readTrack(path)
	want := Track{Path: path, Title: "Song", Artist: "Artist", Album: "Album", Year: 1999, Format: "MP3"}
	if got != want {
		t.Errorf("readTrack = %+v, want %+v", got, want)
	}
}

func TestReadTrackFallsBackToFileName(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "My Song.flac")
	writeFile(t, garbage, []byte("not really audio"))
	untitled := filepath.Join(dir, "Untitled.mp3")
	writeFile(t, untitled, id3v23(map[string]string{"TPE1": "Someone"}))

	if got := readTrack(garbage); got.Title != "My Song" || !got.NoTags || got.Format != "FLAC" {
		t.Errorf("unreadable file: %+v", got)
	}
	if got := readTrack(untitled); got.Title != "Untitled" || got.Artist != "Someone" || got.NoTags {
		t.Errorf("tag without title: %+v", got)
	}
	if got := readTrack(filepath.Join(dir, "missing.mp3")); got.Title != "missing" || !got.NoTags {
		t.Errorf("missing file: %+v", got)
	}
}

func TestDecodeEveryFormat(t *testing.T) {
	for _, name := range []string{"sample.mp3", "sample.flac", "sample.wav", "sample.ogg"} {
		t.Run(name, func(t *testing.T) {
			s, format, err := decode(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if format.SampleRate <= 0 || s.Len() <= 0 {
				t.Fatalf("rate %d, length %d", format.SampleRate, s.Len())
			}
			buf := make([][2]float64, 512)
			if n, ok := s.Stream(buf); n == 0 || !ok {
				t.Errorf("streamed %d samples, ok=%v", n, ok)
			}
			if err := s.Seek(s.Len() / 2); err != nil {
				t.Errorf("seek: %v", err)
			}
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "broken.mp3")
	writeFile(t, bad, []byte("garbage"))
	for _, path := range []string{bad, filepath.Join(dir, "missing.flac"), "testdata/README.md"} {
		if _, _, err := decode(path); err == nil {
			t.Errorf("decode(%s) should fail", filepath.Base(path))
		}
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// id3v23 builds a minimal ID3v2.3 tag with the given text frames, plus an
// embedded picture if frames["APIC"] holds image bytes.
func id3v23(frames map[string]string) []byte {
	var body bytes.Buffer
	for _, id := range []string{"TIT2", "TPE1", "TALB", "TYER", "APIC"} {
		v, ok := frames[id]
		if !ok {
			continue
		}
		data := append([]byte{0}, v...) // 0 = ISO-8859-1 text
		if id == "APIC" {
			// encoding, MIME type, picture type 3 (front cover), empty description
			data = append([]byte("\x00image/png\x00\x03\x00"), v...)
		}
		body.WriteString(id)
		binary.Write(&body, binary.BigEndian, uint32(len(data)))
		body.Write([]byte{0, 0}) // flags
		body.Write(data)
	}
	n := body.Len() // tag size is "syncsafe": 7 bits per byte
	header := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	return append(header, body.Bytes()...)
}
