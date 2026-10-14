package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// The FLAC library's notes are dropped; other log messages still show.
func TestQuietLog(t *testing.T) {
	var out bytes.Buffer
	l := log.New(quietLog{&out}, "", log.LstdFlags)
	for range 3 {
		l.Printf("frame.Frame.parseHeader: The flac library test cases do not yet include any audio files with sample rate %d.", 176400)
	}
	l.Print("Fyne error: something real")
	if got := out.String(); bytes.Count(out.Bytes(), []byte("\n")) != 1 || !bytes.Contains(out.Bytes(), []byte("something real")) {
		t.Errorf("logged %q", got)
	}
}

// Decoding a 176.4 kHz FLAC makes the library log a note per frame, and
// none of them get through.
func TestQuietLogWhileDecoding(t *testing.T) {
	const rate = 176400
	path := filepath.Join(t.TempDir(), "x.flac")
	writeFLAC(t, path, rate, 24, quantize(noise(rate, rate, pink), 24))
	info, err := probe(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.SetOutput(os.Stderr)

	var raw, quiet bytes.Buffer
	log.SetOutput(&raw)
	analyse(context.Background(), path, info, false, nil)
	log.SetOutput(quietLog{&quiet})
	rep, _, err := analyse(context.Background(), path, info, false, nil)
	if raw.Len() == 0 {
		t.Fatal("the library logged nothing: this test no longer tests anything")
	}
	if quiet.Len() != 0 || err != nil || rep.Damaged != "" {
		t.Errorf("logged %q, err %v, damaged %q", quiet.String(), err, rep.Damaged)
	}
}
