package audio

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mugig/internal/audiotest"
)

// samplesOf decodes the whole file at path, left channel.
func samplesOf(t *testing.T, path string) []float64 {
	t.Helper()
	s, _, err := Decode(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var out []float64
	buf := make([][2]float64, 1000)
	for {
		n, ok := s.Stream(buf)
		for _, x := range buf[:n] {
			out = append(out, x[0])
		}
		if !ok {
			return out
		}
	}
}

// M4A files are read through macOS: Apple Lossless with its bit depth,
// AAC as lossy with its bitrate, and their lengths.
func TestProbeM4A(t *testing.T) {
	alac, err := Probe(audiotest.Sample("sample-alac.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	if alac.Codec != "ALAC" || !alac.Lossless || alac.Bits != 16 || alac.SampleRate != 44100 || alac.Channels != 1 ||
		alac.Samples != 22050 || alac.Duration != 500*time.Millisecond || alac.Kind != M4A {
		t.Errorf("ALAC: %+v", alac)
	}
	aac, err := Probe(audiotest.Sample("sample-aac.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	if aac.Codec != "AAC" || aac.Lossless || aac.Bits != 0 || aac.Kbps < 64 || aac.Duration != 500*time.Millisecond {
		t.Errorf("AAC: %+v", aac)
	}
	for _, name := range []string{"sample-alac.m4a", "sample-aac.m4a"} {
		if d := FileLength(audiotest.Sample(name)); d != 500*time.Millisecond {
			t.Errorf("%s lasts %v, want 500ms", name, d)
		}
	}
}

// Apple Lossless plays exactly the audio it was made from; AAC plays
// close to it, and seeking lands on the right sample.
func TestM4APlays(t *testing.T) {
	wav := samplesOf(t, audiotest.Sample("sample.wav"))
	alac := samplesOf(t, audiotest.Sample("sample-alac.m4a"))
	if len(alac) != len(wav) {
		t.Fatalf("ALAC: %d samples, the WAV has %d", len(alac), len(wav))
	}
	for i := range wav {
		if alac[i] != wav[i] {
			t.Fatalf("ALAC sample %d is %v, the WAV's is %v", i, alac[i], wav[i])
		}
	}

	aac := samplesOf(t, audiotest.Sample("sample-aac.m4a"))
	if len(aac) != len(wav) {
		t.Fatalf("AAC: %d samples, want %d", len(aac), len(wav))
	}
	var diff, power float64
	for i := range wav {
		diff += (aac[i] - wav[i]) * (aac[i] - wav[i])
		power += wav[i] * wav[i]
	}
	if snr := 10 * math.Log10(power/diff); snr < 20 {
		t.Errorf("AAC is only %.1f dB above its difference from the original", snr)
	}

	s, _, err := Decode(audiotest.Sample("sample-alac.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Seek(11025); err != nil {
		t.Fatal(err)
	}
	one := make([][2]float64, 1)
	s.Stream(one)
	if one[0][0] != wav[11025] || s.Position() != 11026 {
		t.Errorf("after seeking: %v at %d, want %v at 11026", one[0][0], s.Position(), wav[11025])
	}
}

// An M4A file named otherwise still plays, and is said to be misnamed; a
// damaged one is refused without harm; the player plays M4A.
func TestM4AEdges(t *testing.T) {
	dir := t.TempDir()
	misnamed := filepath.Join(dir, "song.flac")
	audiotest.CopyFile(t, audiotest.Sample("sample-alac.m4a"), misnamed)
	if info, err := Probe(misnamed); err != nil || info.Codec != "ALAC" || info.Mismatch != ".flac" {
		t.Errorf("misnamed: %+v %v", info, err)
	}
	if got := samplesOf(t, misnamed); len(got) != 22050 {
		t.Errorf("misnamed: %d samples", len(got))
	}

	data, _ := os.ReadFile(audiotest.Sample("sample-alac.m4a"))
	for i := 200; i < len(data); i += 7 {
		data[i] ^= 0x5A
	}
	damaged := filepath.Join(dir, "damaged.m4a")
	os.WriteFile(damaged, data, 0o644)
	if s, _, err := Decode(damaged); err == nil {
		buf := make([][2]float64, 1000)
		for n := 0; n < 100; n++ {
			if _, ok := s.Stream(buf); !ok {
				break
			}
		}
		s.Close()
	}

	p := testPlayer(t)
	ended := make(chan error, 1)
	if err := p.Play(audiotest.Sample("sample-aac.m4a"), func(err error) { ended <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ended:
		if err != nil {
			t.Errorf("the AAC song ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the AAC song never ended")
	}
}

// writeMP2 writes seconds of silence as MPEG-1 Layer II, 128 kbps, 44.1
// kHz stereo: frames whose bit allocations are all zero, which decode to
// silence. The MP3 library reads only layer III.
func writeMP2(t *testing.T, path string, frames int) {
	t.Helper()
	frame := make([]byte, 144*128000/44100) // 417 bytes, no padding
	frame[0], frame[1], frame[2], frame[3] = 0xFF, 0xFD, 0x80, 0x00
	var data []byte
	for range frames {
		data = append(data, frame...)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// MPEG Layer II, as some recorders write, even under an .m4a or .mp3
// name, is beyond the MP3 library, so macOS reads and plays it.
func TestMP2PlaysThroughMacOS(t *testing.T) {
	for _, name := range []string{"recording.mp2", "named.m4a", "named.mp3"} {
		path := filepath.Join(t.TempDir(), name)
		writeMP2(t, path, 40) // 40 × 1152 samples
		info, err := Probe(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := time.Duration(40*1152) * time.Second / 44100
		if info.Codec != "MP2" || info.Lossless || info.SampleRate != 44100 || info.Channels != 2 ||
			(info.Duration-want).Abs() > 30*time.Millisecond || info.Kbps < 120 || info.Kbps > 136 {
			t.Errorf("%s: %+v, want MP2 lasting %v", name, info, want)
		}
		if d := FileLength(path); (d - want).Abs() > 30*time.Millisecond {
			t.Errorf("%s lasts %v, want %v", name, d, want)
		}
		got := samplesOf(t, path)
		if (len(got)-40*1152) > 1152 || (40*1152-len(got)) > 1152 {
			t.Errorf("%s: %d samples, want about %d", name, len(got), 40*1152)
		}
		for i, v := range got {
			if v != 0 {
				t.Fatalf("%s: sample %d is %v, want silence", name, i, v)
			}
		}
	}
}
