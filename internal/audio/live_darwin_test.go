package audio

import (
	"math"
	"mugig/internal/audiotest"
	"net/http"
	"os"
	"testing"
	"time"
)

// toneLevel is the strength of the frequency f in s, at rate, from 0 to 1 for
// a full-scale sine (Goertzel's algorithm).
func toneLevel(s []float64, f, rate float64) float64 {
	w := 2 * math.Pi * f / rate
	var s1, s2 float64
	for _, x := range s {
		s1, s2 = x+2*math.Cos(w)*s1-s2, s1
	}
	return 2 * math.Sqrt(s1*s1+s2*s2-2*math.Cos(w)*s1*s2) / float64(len(s))
}

// AAC stations play through macOS's decoder: plain AAC, and HE-AAC at its
// full rate, twice that its ADTS headers state.
func TestLiveAAC(t *testing.T) {
	for _, name := range []string{"sample.aac", "sample-he.aac"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(audiotest.Sample(name))
			if err != nil {
				t.Fatal(err)
			}
			_, link := newStation(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "audio/aacp")
				for range 10 {
					if _, err := w.Write(data); err != nil {
						return
					}
				}
			})
			l, err := OpenLive(link, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			if l.Codec != "AAC" || l.SampleRate() != 48000 {
				t.Errorf("%s at %d Hz", l.Codec, l.SampleRate())
			}
			got := listen(t, l, 3*time.Second)
			if len(got) < 3*48000 {
				t.Fatalf("%d frames", len(got))
			}
			mid := got[48000/2 : 48000] // the tone, past the decoder's start
			// The tone was made at 0.4 of full scale.
			if tone, off := toneLevel(mid, 1000, 48000), toneLevel(mid, 3000, 48000); tone < 0.3 || off > 0.02 {
				t.Errorf("1 kHz at %.3f, 3 kHz at %.3f", tone, off)
			}
		})
	}
}
