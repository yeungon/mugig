package audio

import (
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
)

// Songs from CUE sheets are known by their path everywhere, but playing
// one, or anything else that reads it, needs its image and where in it
// the song is. The scan records them here.
var cueSpans sync.Map // song path → cueSpan

type Span struct {
	File       string
	Start, End time.Duration // end 0 for the end of the file
}

// rememberSpan records that the song at path is a stretch of an image.
func RememberSpan(path string, s Span) { cueSpans.Store(path, s) }

// forgetSpans forgets every song recorded, as if starting afresh.
func ForgetSpans() { cueSpans.Clear() }

// spanOf returns where the song at path is: its image and the stretch of
// it, or for any other song, the file itself, whole.
func spanOf(path string) Span {
	if v, ok := cueSpans.Load(path); ok {
		return v.(Span)
	}
	return Span{File: path}
}

// fileOf is the file a song is in: its image, for a song from a CUE sheet.
func FileOf(path string) string { return spanOf(path).File }

// fromCue reports whether the song at path is part of an image.
func FromCue(path string) bool { return spanOf(path).File != path }

// segment plays a stretch of s, as if it were all there is.
type segment struct {
	beep.StreamSeekCloser
	skip, n int
}

// cut narrows s, of format f, to the span, if it is a part of the file.
func (sp Span) cut(s beep.StreamSeekCloser, f beep.Format) (beep.StreamSeekCloser, error) {
	if sp.Start == 0 && sp.End == 0 {
		return s, nil
	}
	skip := min(samplesAt(sp.Start, f.SampleRate), s.Len())
	n := s.Len() - skip
	if sp.End > 0 {
		n = min(samplesAt(sp.End, f.SampleRate)-skip, n)
	}
	if err := s.Seek(skip); err != nil {
		return nil, err
	}
	return &segment{StreamSeekCloser: s, skip: skip, n: max(n, 0)}, nil
}

// samplesAt is the sample at d, rounded: a CUE sheet's times, in 75ths of
// a second, fall on whole samples at every usual rate, but held as
// nanoseconds they are a fraction short, and beep's N would truncate that
// to the sample before, overlapping the song before by one.
func samplesAt(d time.Duration, rate beep.SampleRate) int {
	return int((int64(d)*int64(rate) + int64(time.Second)/2) / int64(time.Second))
}

func (g *segment) Stream(samples [][2]float64) (int, bool) {
	left := g.n - g.Position()
	if left <= 0 {
		return 0, false
	}
	return g.StreamSeekCloser.Stream(samples[:min(len(samples), left)])
}

func (g *segment) Len() int { return g.n }

func (g *segment) Position() int { return max(g.StreamSeekCloser.Position()-g.skip, 0) }

func (g *segment) Seek(p int) error {
	return g.StreamSeekCloser.Seek(min(max(p, 0), g.n) + g.skip)
}
