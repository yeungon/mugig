package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWalkFindsOnlyAudio(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.mp3", "b.FLAC", "c.txt", "sub/d.ogg", "sub/deeper/e.wav", "f.m4a"} {
		audiotest.WriteFile(t, filepath.Join(dir, name), []byte("x"))
	}
	audiotest.WriteFile(t, filepath.Join(dir, "empty.mp3"), nil) // an interrupted download: nothing to play

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

// walkAll lists what walk finds under root, relative to dir.
func walkAll(dir, root string) []string {
	paths := make(chan string)
	go walk(context.Background(), root, paths)
	var got []string
	for p := range paths {
		rel, _ := filepath.Rel(dir, p)
		got = append(got, rel)
	}
	slices.Sort(got)
	return got
}

// The scan leaves out the sounds in apps, plug-ins and code, and Windows'
// system folders; but a folder added on purpose is scanned even if it is
// one of those.
func TestWalkSkipsFoldersThatArentMusic(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Album/song.mp3",
		"Game.app/Contents/Resources/click.wav",
		"Synth.component/Contents/x.mp3", // not a bundle type skipped
		"Plug.bundle/beep.ogg",
		"site/node_modules/pkg/test.mp3",
		"$RECYCLE.BIN/S-1-5-21/old.mp3",
		"System Volume Information/x.wav",
		"Apps I Like/song.flac",
	} {
		audiotest.WriteFile(t, filepath.Join(dir, name), []byte("x"))
	}
	want := []string{"Album/song.mp3", "Apps I Like/song.flac", "Synth.component/Contents/x.mp3"}
	if got := walkAll(dir, dir); !slices.Equal(got, want) {
		t.Errorf("walk found %v, want %v", got, want)
	}
	if got := walkAll(dir, filepath.Join(dir, "Plug.bundle")); !slices.Equal(got, []string{"Plug.bundle/beep.ogg"}) {
		t.Errorf("a folder added on purpose should be scanned: %v", got)
	}
}

func TestReadTrackTags(t *testing.T) {
	audio, err := os.ReadFile("testdata/sample.mp3")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "file name.mp3")
	tag := id3v23(map[string]string{"TIT2": "Song", "TPE1": "Artist", "TALB": "Album", "TYER": "1999"})
	audiotest.WriteFile(t, path, append(tag, audio...))

	got := readTrack(path)
	want := Track{Path: path, Title: "Song", Artist: "Artist", Album: "Album", Year: 1999, Format: "MP3"}
	if got != want {
		t.Errorf("readTrack = %+v, want %+v", got, want)
	}
}

func TestReadTrackFallsBackToFileName(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "My Song.flac")
	audiotest.WriteFile(t, garbage, []byte("not really audio"))
	untitled := filepath.Join(dir, "Untitled.mp3")
	audiotest.WriteFile(t, untitled, id3v23(map[string]string{"TPE1": "Someone"}))

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
