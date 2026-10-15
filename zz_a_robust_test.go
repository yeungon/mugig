package main

import (
	"encoding/binary"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gopxl/beep/v2"
)

type panicky struct{ beep.StreamSeekCloser }

func (panicky) Stream([][2]float64) (int, bool) { panic("bad frame") }

// A decoder panicking mid-track ends the track instead of the app.
func TestDecoderPanicEndsTrack(t *testing.T) {
	s := &safeStream{StreamSeekCloser: panicky{}}
	n, ok := s.Stream(make([][2]float64, 10))
	if n != 0 || ok || s.Err() == nil {
		t.Errorf("n=%d ok=%v err=%v", n, ok, s.Err())
	}
}

func TestFLACReadable(t *testing.T) {
	f, err := os.Open("testdata/sample.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !flacReadable(f) {
		t.Error("sample.flac should be readable")
	}
	if pos, _ := f.Seek(0, 1); pos != 0 {
		t.Errorf("file left at %d", pos)
	}
}

// corruptTagFLAC writes a valid FLAC file, then adds a tag block (Vorbis
// comment) whose vendor-string length claims 2 GB though the block holds a
// few bytes, as a corrupting disk can produce.
func corruptTagFLAC(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.flac")
	audiotest.WriteFLAC(t, path, 44100, 16, audiotest.Quantize(audiotest.Noise(44100, 44100, audiotest.Pink), 16))
	data, _ := os.ReadFile(path)
	head, rest := data[:4+4+34], data[4+4+34:]
	head[4] &^= 0x80 // STREAMINFO is no longer the last block
	body := binary.LittleEndian.AppendUint32(nil, 0x7fffffff)
	body = append(body, "junk"...)
	block := append([]byte{0x80 | 4, 0, 0, byte(len(body))}, body...) // last block: VORBIS_COMMENT
	out := append(append(append([]byte{}, head...), block...), rest...)
	os.WriteFile(path, out, 0o644)
	return path
}

// A corrupt tag block must not make playback allocate gigabytes, which
// would freeze or crash the app (fixed in mewkiz/flac 1.0.14).
func TestCorruptTagBlockIsCheap(t *testing.T) {
	path := corruptTagFLAC(t)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	s, _, err := Decode(path)
	if err == nil {
		s.Stream(make([][2]float64, 512))
		s.Close()
	}
	runtime.ReadMemStats(&after)
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 64<<20 {
		t.Errorf("decoding allocated %d MB", grew>>20)
	}
	// The check reads past the tags and judges the audio as usual.
	if _, err := Probe(path); err != nil {
		t.Errorf("probe: %v", err)
	}
}
