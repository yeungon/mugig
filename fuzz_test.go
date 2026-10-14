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
	"os"
	"path/filepath"
	"testing"
	"time"
)

var fuzzExts = []string{".flac", ".wav", ".mp3", ".ogg"}

// fuzzSeeds adds the sample files, whole and cut short, as starting points,
// each under its own extension and under the wrong one.
func fuzzSeeds(f *testing.F) {
	for i, ext := range fuzzExts {
		data, err := os.ReadFile(filepath.Join("testdata", "sample"+ext))
		if err != nil {
			f.Fatal(err)
		}
		for _, cut := range []int{len(data), len(data) / 2, 64, 12} {
			f.Add(data[:min(cut, len(data))], uint8(i))
		}
		f.Add(data, uint8(i+1)) // misnamed
	}
}

// slowLimit is how long reading one small, corrupt file may take: longer
// would stall a scan of thousands of them.
const slowLimit = 2 * time.Second

func timed(t *testing.T, what string, f func()) {
	start := time.Now()
	f()
	if d := time.Since(start); d > slowLimit {
		t.Errorf("%s took %v", what, d)
	}
}

// fuzzFile writes data to a file with one of the audio extensions.
func fuzzFile(t *testing.T, data []byte, ext uint8) string {
	path := filepath.Join(t.TempDir(), "x"+fuzzExts[int(ext)%len(fuzzExts)])
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// FuzzProbe covers Mugig's own header parsers: ID3, FLAC, WAV and Ogg.
func FuzzProbe(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := fuzzFile(t, data, ext)
		timed(t, "probe", func() {
			info, err := probe(path)
			if err == nil && info.Codec == "" {
				t.Error("probe succeeded without naming a codec")
			}
			sniff(path)
		})
	})
}

// FuzzReadTrack covers tag reading, which the scan does for every file.
func FuzzReadTrack(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := fuzzFile(t, data, ext)
		timed(t, "readTrack", func() {
			if tr := readTrack(path); tr.Title == "" {
				t.Error("a track needs a title, if only its file name")
			}
		})
	})
}

// FuzzDecode covers playback: opening the decoder and streaming from it,
// as the audio thread does.
func FuzzDecode(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := fuzzFile(t, data, ext)
		timed(t, "decode", func() {
			s, _, err := decode(path)
			if err != nil {
				return
			}
			defer s.Close()
			safe := &safeStream{StreamSeekCloser: s}
			buf := make([][2]float64, 512)
			for range 64 {
				if _, ok := safe.Stream(buf); !ok {
					break
				}
			}
			safe.Seek(0)
		})
	})
}

// FuzzAnalyse covers the quality check: decoding in full, the checksum,
// the spectrum and the loudness meter.
func FuzzAnalyse(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := fuzzFile(t, data, ext)
		info, err := probe(path)
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
