package audio

import (
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// lengthOf opens path and reads its length from the headers.
func lengthOf(t *testing.T, path string) time.Duration {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return TrackLength(f)
}

// The lengths read from headers match what the decoders find by reading
// all the audio.
func TestTrackLengthMatchesDecoder(t *testing.T) {
	for _, name := range []string{"sample.flac", "sample.mp3", "sample.ogg", "sample.wav"} {
		path := audiotest.Sample(name)
		s, format, err := DecodeAs(path, sniff(path))
		if err != nil {
			t.Fatal(name, err)
		}
		want := format.SampleRate.D(s.Len())
		s.Close()
		got := lengthOf(t, path)
		if diff := (got - want).Abs(); want == 0 || diff > 30*time.Millisecond {
			t.Errorf("%s: length %v, decoder says %v", name, got, want)
		}
	}
}

// An MP3 without a Xing tag has a constant bitrate, so its length comes
// from its size; a trailing ID3v1 tag isn't audio.
func TestMP3LengthFromBitrate(t *testing.T) {
	// MPEG-1 layer III, 128 kbps, 44.1 kHz, stereo: 417 bytes a frame.
	frame := make([]byte, 417)
	copy(frame, []byte{0xFF, 0xFB, 0x90, 0x00})
	var data []byte
	for range 100 {
		data = append(data, frame...)
	}
	tag := make([]byte, 128)
	copy(tag, "TAG")
	data = append(data, tag...)
	path := filepath.Join(t.TempDir(), "cbr.mp3")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	want := 100 * 417 * 8 * time.Second / 128000 // ≈ 100 frames of 26.1 ms
	if got := lengthOf(t, path); got != want {
		t.Errorf("length %v, want %v", got, want)
	}
}

// Files that aren't audio, or are cut off in the header, have no length
// rather than a wrong one.
func TestTrackLengthUnknown(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"text.mp3":  []byte("not audio at all"),
		"short.ogg": []byte("OggS\x00"),
		"cut.flac":  []byte("fLaC\x80\x00\x00\x22"),
		"empty.wav": []byte("RIFF\x00\x00\x00\x00WAVE"),
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := lengthOf(t, path); got != 0 {
			t.Errorf("%s: length %v, want 0", name, got)
		}
	}
}
