package main

// Fuzz tests for everything that reads a file's bytes. Files come from
// disks that may hand back corrupt data, so no input may crash the app,
// hang it, or make it allocate wildly. Run one with, for example:
//
//	go test -run '^$' -fuzz FuzzProbe -fuzztime 2m
//
// Plain `go test` runs each against its seeds and any crashers found
// before (saved under testdata/fuzz).

import (
	"context"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"testing"
	"time"
)

// FuzzReadTrack covers tag reading, which the scan does for every file.
func FuzzReadTrack(f *testing.F) {
	audiotest.FuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := audiotest.FuzzFile(t, data, ext)
		audiotest.Timed(t, "readTrack", func() {
			if tr := readTrack(path); tr.Title == "" {
				t.Error("a track needs a title, if only its file name")
			}
		})
	})
}

// FuzzAnalyse covers the quality check: decoding in full, the checksum,
// the spectrum and the loudness meter.
func FuzzAnalyse(f *testing.F) {
	audiotest.FuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := audiotest.FuzzFile(t, data, ext)
		info, err := audio.Probe(path)
		if err != nil || !canAnalyse(info) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rep, _, err := analyse(ctx, path, info, false, nil)
		if ctx.Err() != nil {
			t.Fatalf("analysis took over 10 s on %d bytes", len(data))
		}
		if err == nil {
			assess(info, &rep)
		}
	})
}
