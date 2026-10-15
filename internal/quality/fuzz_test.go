package quality

import (
	"context"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"testing"
	"time"
)

// FuzzAnalyse covers the quality check: decoding in full, the checksum,
// the spectrum and the loudness meter.
func FuzzAnalyse(f *testing.F) {
	audiotest.FuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := audiotest.FuzzFile(t, data, ext)
		info, err := audio.Probe(path)
		if err != nil || !CanAnalyse(info) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rep, _, err := Analyse(ctx, path, info, false, nil)
		if ctx.Err() != nil {
			t.Fatalf("analysis took over 10 s on %d bytes", len(data))
		}
		if err == nil {
			Assess(info, &rep)
		}
	})
}
