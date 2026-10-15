package quality

// Volume levelling plays every song at about the same loudness: each is
// turned up or down to TargetLUFS, the level YouTube, Spotify and Tidal
// play at, by the
// loudness measured here or in a quality check. A boost stops short of
// taking the song's true peak above -1 dBTP, so levelling never clips.

import (
	"context"
	"math"

	"mugig/internal/audio"
)

// TargetLUFS is the loudness levelling aims for. ReplayGain's -18 levels
// a little more evenly, but most music today is mastered far louder (a
// real library's median was -11 LUFS), so it turned the typical song down
// 7 dB; at -14 that is 3 dB, and songs still end up within about 2 dB of
// each other, against nearly 10 dB unlevelled.
const TargetLUFS = -14

// Loudness is how loud a song is.
type Loudness struct {
	LUFS     float64 // integrated loudness, ITU-R BS.1770-4; -Inf for silence
	TruePeak float64 // dBTP
}

// Gain is the change, in dB, that brings the song to TargetLUFS without
// its peaks going over -1 dBTP. Silence is left alone.
func (l Loudness) Gain() float64 {
	if math.IsInf(l.LUFS, 0) || math.IsNaN(l.LUFS) {
		return 0
	}
	g := TargetLUFS - l.LUFS
	if headroom := -1 - l.TruePeak; g > headroom {
		g = max(headroom, 0) // a boost up to the peak, but never a cut for it
	}
	return g
}

// MeasureLoudness decodes the song at path, just its stretch for a song
// from a CUE sheet, and measures how loud it is.
func MeasureLoudness(ctx context.Context, path string) (Loudness, error) {
	s, format, err := audio.Decode(path)
	if err != nil {
		return Loudness{}, err
	}
	defer s.Close()
	lv := newLevels(float64(format.SampleRate), min(format.NumChannels, 2))
	buf := make([][2]float64, 4096)
	for {
		if err := ctx.Err(); err != nil {
			return Loudness{}, err
		}
		n, ok := s.Stream(buf)
		for _, f := range buf[:n] {
			lv.add(f)
		}
		if !ok {
			break
		}
	}
	if err := s.Err(); err != nil {
		return Loudness{}, err
	}
	r := lv.result()
	return Loudness{LUFS: r.LUFS, TruePeak: r.TruePeak}, nil
}
