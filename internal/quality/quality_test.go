package quality

import (
	"context"
	"math"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkFile probes and verifies path.
func checkFile(t *testing.T, path string) (audio.Info, Report) {
	t.Helper()
	info, err := audio.Probe(path)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := verify(context.Background(), path, info)
	if err != nil {
		t.Fatal(err)
	}
	return info, rep
}

func near(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

// A file's contents decide its format, not its name: an MP3 renamed .flac
// is reported as MP3, and still plays.
func TestMisnamedFile(t *testing.T) {
	data, err := os.ReadFile(audiotest.Sample("sample.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "song.flac")
	os.WriteFile(path, data, 0o644)
	info, err := audio.Probe(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "MP3" || info.Lossless || info.Mismatch != ".flac" {
		t.Errorf("got %+v", info)
	}
	s, _, err := audio.Decode(path)
	if err != nil {
		t.Fatalf("misnamed file doesn't play: %v", err)
	}
	s.Close()

	a := Assess(info, nil)
	if a.Level != Lossy || !strings.Contains(a.Checks[0].Text, ".flac") {
		t.Errorf("assessment %+v", a)
	}
}

func TestGenuineFLACPasses(t *testing.T) {
	const rate = 44100
	path := filepath.Join(t.TempDir(), "genuine.flac")
	l := audiotest.Quantize(audiotest.Noise(6*rate, rate, audiotest.Pink), 16)
	r := audiotest.Quantize(audiotest.Noise(6*rate, rate, audiotest.Pink), 16)
	audiotest.WriteFLAC(t, path, rate, 16, l, r)

	info, rep := checkFile(t, path)
	if info.SampleRate != rate || info.Bits != 16 || info.Channels != 2 || info.Duration.Seconds() != 6 || info.Kbps < 800 || info.Kbps > 1450 { // 16-bit stereo, compressed or not
		t.Errorf("info %+v", info)
	}
	if rep.Damaged != "" || rep.Checksum != ChecksumOK || rep.Missing != 0 || rep.UsedBits != 16 {
		t.Errorf("report %+v", rep)
	}
	if !rep.Spectrum.Analysed || rep.Spectrum.Cutoff != 0 {
		t.Errorf("spectrum %+v", rep.Spectrum)
	}
	if a := Assess(info, &rep); a.Level != Lossless {
		t.Errorf("assessed %+v", a)
	}
}

// Real recordings often fade out steeply at the top, which must not be
// mistaken for an encoder's cut-off.
func TestSteepNaturalRollOffPasses(t *testing.T) {
	const rate = 44100
	for _, slope := range []float64{6, 12, 20, 30} { // dB per kHz above 12 kHz
		gain := func(f float64) float64 {
			g := audiotest.Pink(f)
			if f > 12000 {
				g *= math.Pow(10, -slope*(f-12000)/1000/20)
			}
			return g
		}
		path := filepath.Join(t.TempDir(), "rolloff.flac")
		audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(audiotest.Noise(6*rate, rate, gain), 16))
		if _, rep := checkFile(t, path); rep.Spectrum.Cutoff != 0 {
			t.Errorf("%v dB/kHz: cut-off reported at %v Hz", slope, rep.Spectrum.Cutoff)
		}
	}
}

// Lossless files made from MP3s have nothing above the encoder's cut-off.
func TestLossySourceDetected(t *testing.T) {
	const rate = 44100
	for _, hz := range []float64{11000, 16000, 17000, 19000} {
		for _, width := range []float64{100, 300, 600} { // encoders' filters differ in steepness
			path := filepath.Join(t.TempDir(), "transcoded.flac")
			s := audiotest.Noise(6*rate, rate, audiotest.Lowpass(audiotest.Pink, hz, width))
			audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(s, 16), audiotest.Quantize(s, 16))

			info, rep := checkFile(t, path)
			if !near(rep.Spectrum.Cutoff, hz+width/2, 250) {
				t.Errorf("cut at %v Hz over %v Hz: found %v", hz, width, rep.Spectrum.Cutoff)
				continue
			}
			a := Assess(info, &rep)
			if a.Level != Suspect || !hasCheck(a, CheckWarn, "very likely converted from a lossy") {
				t.Errorf("cut at %v Hz over %v Hz: assessed %+v", hz, width, a)
			}
		}
	}
}

// A 96 kHz file with nothing above 22 kHz was upsampled from CD quality.
func TestUpsampledHiResDetected(t *testing.T) {
	const rate = 96000
	dir := t.TempDir()

	upsampled := filepath.Join(dir, "upsampled.flac")
	audiotest.WriteFLAC(t, upsampled, rate, 24, audiotest.Quantize(audiotest.Noise(4*rate, rate, audiotest.Lowpass(audiotest.Pink, 21000, 1050)), 24))
	info, rep := checkFile(t, upsampled)
	a := Assess(info, &rep)
	if a.Level != Suspect || !hasCheck(a, CheckWarn, "upsampled") {
		t.Errorf("upsampled: cut-off %v, assessed %+v", rep.Spectrum.Cutoff, a)
	}

	real := filepath.Join(dir, "hires.flac")
	audiotest.WriteFLAC(t, real, rate, 24, audiotest.Quantize(audiotest.Noise(4*rate, rate, audiotest.Pink), 24))
	info, rep = checkFile(t, real)
	if a := Assess(info, &rep); a.Level != HiRes || rep.UsedBits != 24 {
		t.Errorf("true hi-res: report %+v, assessed %+v", rep, a)
	}
}

// A 24-bit file made from a 16-bit one never uses its low 8 bits.
func TestPaddedBitsDetected(t *testing.T) {
	const rate = 48000
	s := audiotest.Quantize(audiotest.Noise(3*rate, rate, audiotest.Pink), 16)
	padded := make([]int32, len(s))
	for i, v := range s {
		padded[i] = v << 8
	}
	dir := t.TempDir()
	for _, path := range []string{filepath.Join(dir, "padded.flac"), filepath.Join(dir, "padded.wav")} {
		if strings.HasSuffix(path, ".flac") {
			audiotest.WriteFLAC(t, path, rate, 24, padded)
		} else {
			audiotest.WriteWAV(t, path, rate, 24, 3, padded)
		}
		info, rep := checkFile(t, path)
		a := Assess(info, &rep)
		if rep.UsedBits != 16 || a.Level != Suspect || !hasCheck(a, CheckWarn, "16 of its 24 bits") {
			t.Errorf("%s: used %d bits, assessed %+v", filepath.Base(path), rep.UsedBits, a)
		}
	}
}

func TestWAVFormats(t *testing.T) {
	const rate = 44100
	s := audiotest.Noise(2*rate, rate, audiotest.Pink)
	dir := t.TempDir()
	for _, c := range []struct{ bits, bytes int }{{8, 1}, {16, 2}, {24, 3}, {24, 4}, {32, 4}} {
		path := filepath.Join(dir, "x.wav")
		audiotest.WriteWAV(t, path, rate, c.bits, c.bytes, audiotest.Quantize(s, c.bits), audiotest.Quantize(s, c.bits))
		info, rep := checkFile(t, path)
		if !info.Lossless || info.Bits != c.bits || info.Channels != 2 || rep.UsedBits != c.bits || rep.Missing != 0 || rep.Damaged != "" {
			t.Errorf("%d-bit in %d bytes: info %+v report %+v", c.bits, c.bytes, info, rep)
		}
	}
}

func TestDamageDetected(t *testing.T) {
	const rate = 44100
	dir := t.TempDir()
	s := audiotest.Quantize(audiotest.Noise(3*rate, rate, audiotest.Pink), 16)

	// A flipped bit in the audio: FLAC's frame and MD5 checksums catch it.
	path := filepath.Join(dir, "flipped.flac")
	audiotest.WriteFLAC(t, path, rate, 16, s)
	data, _ := os.ReadFile(path)
	data[len(data)/2] ^= 0x10
	os.WriteFile(path, data, 0o644)
	info, rep := checkFile(t, path)
	if a := Assess(info, &rep); a.Level != Damaged || !hasCheck(a, CheckBad, "Damaged") {
		t.Errorf("flipped bit: report %+v, assessed %+v", rep, a)
	}

	// A WAV cut short, e.g. by an interrupted copy.
	path = filepath.Join(dir, "short.wav")
	audiotest.WriteWAV(t, path, rate, 16, 2, s)
	data, _ = os.ReadFile(path)
	os.WriteFile(path, data[:len(data)-rate*2], 0o644) // lose the last second
	info, rep = checkFile(t, path)
	if a := Assess(info, &rep); !near(rep.Missing.Seconds(), 1, 0.01) || a.Level != Damaged || !hasCheck(a, CheckBad, "missing") {
		t.Errorf("short WAV: report %+v, assessed %+v", rep, a)
	}
}

func TestVerifyStopsWhenCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	audiotest.WriteFLAC(t, path, 44100, 16, audiotest.Quantize(audiotest.Noise(44100, 44100, audiotest.Pink), 16))
	info, _ := audio.Probe(path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verify(ctx, path, info); err != context.Canceled {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestQuietAudioIsNotJudged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "silence.flac")
	audiotest.WriteFLAC(t, path, 44100, 16, make([]int32, 3*44100))
	info, rep := checkFile(t, path)
	a := Assess(info, &rep)
	if rep.Spectrum.Analysed || a.Level != Lossless || !hasCheck(a, CheckInfo, "quiet") {
		t.Errorf("report %+v, assessed %+v", rep, a)
	}
}

func hasCheck(a Assessment, kind CheckKind, text string) bool {
	for _, c := range a.Checks {
		if c.Kind == kind && strings.Contains(c.Text, text) {
			return true
		}
	}
	return false
}

func TestQualityStoreIgnoresBadFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quality.gob")
	os.WriteFile(path, []byte("garbage"), 0o644)
	s := LoadStore(path)
	s.Put("/a.flac", "stamp", Report{Checksum: ChecksumOK}, Lossless)
	var rep Report
	if !LoadStore(path).Get("/a.flac", "stamp", &rep) || rep.Checksum != ChecksumOK {
		t.Error("store not rewritten after a corrupt file")
	}
}

// Long files are sampled rather than analysed whole; a cut-off must still
// be found.
func TestLongFilesSampled(t *testing.T) {
	const rate = 44100
	s := audiotest.Noise(1<<18, rate, audiotest.Lowpass(audiotest.Pink, 16000, 300))
	spec := newSpectrum(rate, specSize*specMaxFrame*4) // as if 4 times the analysed length
	if spec.every != 4 {
		t.Fatalf("every = %d, want 4", spec.every)
	}
	for range 2 {
		spec.add(s)
	}
	if spec.frames < minAnalysed || spec.frames > 2*len(s)/specSize/4+1 {
		t.Errorf("analysed %d blocks", spec.frames)
	}
	if res := spec.result(); !near(res.Cutoff, 16150, 250) {
		t.Errorf("cut-off %v", res.Cutoff)
	}
}

// sample.flac came from the reference encoder, so this checks the MD5 is
// computed the same way.
func TestRealFLACChecksumMatches(t *testing.T) {
	if _, rep := checkFile(t, audiotest.Sample("sample.flac")); rep.Checksum != ChecksumOK || rep.Damaged != "" {
		t.Errorf("report %+v", rep)
	}
}

// A cut-off to a level only 35 dB lower, rather than to silence, is weaker
// evidence, and is worded so.
func TestWeakCutoffWordedCautiously(t *testing.T) {
	const rate = 44100
	gain := func(f float64) float64 {
		if f > 16000 {
			return audiotest.Pink(f) * math.Pow(10, -35.0/20)
		}
		return audiotest.Pink(f)
	}
	path := filepath.Join(t.TempDir(), "weak.flac")
	audiotest.WriteFLAC(t, path, rate, 16, audiotest.Quantize(audiotest.Noise(6*rate, rate, gain), 16))
	info, rep := checkFile(t, path)
	a := Assess(info, &rep)
	if rep.Spectrum.Drop < cutoffDrop || rep.Spectrum.Drop >= strongDrop || !hasCheck(a, CheckWarn, "probably converted") {
		t.Errorf("drop %v dB, assessed %+v", rep.Spectrum.Drop, a)
	}
}
