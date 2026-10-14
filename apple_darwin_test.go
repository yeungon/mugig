//go:build darwin

package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// macOS's decoder gives the exact samples: forced through it, files still
// match the checksum their encoder stored, at 16 and 24 bits.
func TestAppleDecoderIsBitExact(t *testing.T) {
	for _, bits := range []int{16, 24} {
		path := filepath.Join(t.TempDir(), "x.flac")
		s := noise(3*44100, 44100, pink)
		writeFLAC(t, path, 44100, bits, quantize(s, bits), quantize(s, bits))
		info, _ := probe(path)
		lv := newLevels(44100, 2)
		rep, spec, err := verifyWithApple(context.Background(), path, info, func(int64) {}, lv)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Checksum != checksumOK || rep.ReadBy != "macOS" || rep.UsedBits != bits || rep.Damaged != "" {
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

// Playing through macOS's decoder gives the same sound as the FLAC
// library, and can seek.
func TestApplePlaybackMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	s := quantize(noise(2*44100, 44100, pink), 16)
	writeFLAC(t, path, 44100, 16, s, s)
	a, format, err := appleDecode(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if format.SampleRate != 44100 || a.Len() != len(s) {
		t.Fatalf("rate %v, length %d", format.SampleRate, a.Len())
	}
	a.Seek(1000)
	buf := make([][2]float64, 100)
	n, _ := a.Stream(buf)
	for i := range n {
		if want := float64(s[1000+i]) / 32768; math.Abs(buf[i][0]-want) > 1e-9 || buf[i][1] != buf[i][0] {
			t.Fatalf("sample %d: %v, want %v", 1000+i, buf[i], want)
		}
	}
	if a.Position() != 1100 {
		t.Errorf("position %d", a.Position())
	}
}

// A frame whose own checksum is corrupt, around intact audio: Mugig's FLAC
// library stops there, macOS's decoder reads on, and the file's stored
// checksum shows the audio is whole.
func TestSecondOpinionFromMacOS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	s := quantize(noise(3*44100, 44100, pink), 16)
	writeFLAC(t, path, 44100, 16, s, s)
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 0xFF // the last frame's CRC-16
	os.WriteFile(path, data, 0o644)

	info, rep := checkFile(t, path)
	a := assess(info, &rep)
	t.Logf("%+v", a.checks)
	switch rep.ReadBy {
	case "macOS": // macOS doesn't check frame checksums
		if rep.Checksum != checksumOK || rep.OwnStopAt == 0 || a.level == qualityDamaged ||
			!hasCheck(a, checkInfo, "stopped at") {
			t.Errorf("report %+v, assessed %+v", rep, a)
		}
	default: // macOS refused it too
		if a.level != qualityDamaged {
			t.Errorf("both decoders failed but not damaged: %+v", a)
		}
	}
}
