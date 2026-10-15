//go:build darwin

package quality

import (
	"context"
	"math"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"testing"
)

// macOS's decoder gives the exact samples: forced through it, files still
// match the checksum their encoder stored, at 16 and 24 bits.
func TestAppleDecoderIsBitExact(t *testing.T) {
	for _, bits := range []int{16, 24} {
		path := filepath.Join(t.TempDir(), "x.flac")
		s := audiotest.Noise(3*44100, 44100, audiotest.Pink)
		audiotest.WriteFLAC(t, path, 44100, bits, audiotest.Quantize(s, bits), audiotest.Quantize(s, bits))
		info, _ := audio.Probe(path)
		lv := newLevels(44100, 2)
		rep, spec, err := verifyWithApple(context.Background(), path, info, func(int64) {}, lv)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Checksum != ChecksumOK || rep.ReadBy != "macOS" || rep.UsedBits != bits || rep.Damaged != "" {
			t.Errorf("%d-bit: %+v", bits, rep)
		}
		if res := spec.result(); !res.Analysed || res.Cutoff != 0 {
			t.Errorf("%d-bit spectrum %+v", bits, res)
		}
		if r := lv.result(); !r.Measured || math.IsInf(r.LUFS, -1) {
			t.Errorf("%d-bit levels %+v", bits, r)
		}
	}
}

// A frame whose own checksum is corrupt, around intact audio: Mugig's FLAC
// library stops there, macOS's decoder reads on, and the file's stored
// checksum shows the audio is whole.
func TestSecondOpinionFromMacOS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	s := audiotest.Quantize(audiotest.Noise(3*44100, 44100, audiotest.Pink), 16)
	audiotest.WriteFLAC(t, path, 44100, 16, s, s)
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 0xFF // the last frame's CRC-16
	os.WriteFile(path, data, 0o644)

	info, rep := checkFile(t, path)
	a := Assess(info, &rep)
	t.Logf("%+v", a.Checks)
	switch rep.ReadBy {
	case "macOS": // macOS doesn't check frame checksums
		if rep.Checksum != ChecksumOK || rep.OwnStopAt == 0 || a.Level == Damaged ||
			!hasCheck(a, CheckInfo, "stopped at") {
			t.Errorf("report %+v, assessed %+v", rep, a)
		}
	default: // macOS refused it too
		if a.Level != Damaged {
			t.Errorf("both decoders failed but not damaged: %+v", a)
		}
	}
}
