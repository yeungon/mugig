package audio

import (
	"mugig/internal/audiotest"
	"testing"
)

// FuzzProbe covers Mugig's own header parsers: ID3, FLAC, WAV and Ogg.
func FuzzProbe(f *testing.F) {
	audiotest.FuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := audiotest.FuzzFile(t, data, ext)
		audiotest.Timed(t, "probe", func() {
			info, err := Probe(path)
			if err == nil && info.Codec == "" {
				t.Error("probe succeeded without naming a codec")
			}
			sniff(path)
		})
	})
}

// FuzzDecode covers playback: opening the decoder and streaming from it,
// as the audio thread does.
func FuzzDecode(f *testing.F) {
	audiotest.FuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte, ext uint8) {
		path := audiotest.FuzzFile(t, data, ext)
		audiotest.Timed(t, "decode", func() {
			s, _, err := Decode(path)
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
