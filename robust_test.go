package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gopxl/beep/v2"
)

// opusFile writes the start of an Ogg Opus file, as WhatsApp voice notes
// are: an Ogg page holding the OpusHead packet.
func opusFile(t *testing.T) string {
	t.Helper()
	head := []byte("OpusHead")
	head = append(head, 1, 2)                            // version, channels
	head = binary.LittleEndian.AppendUint16(head, 312)   // pre-skip
	head = binary.LittleEndian.AppendUint32(head, 48000) // input rate
	head = append(head, 0, 0, 0)                         // gain, mapping
	page := append([]byte("OggS"), 0, 2)                 // version, first page
	page = append(page, make([]byte, 8+4+4+4)...)        // granule, serial, sequence, CRC
	page = append(page, 1, byte(len(head)))              // one segment
	page = append(page, head...)
	path := filepath.Join(t.TempDir(), "WhatsApp Ptt.ogg")
	os.WriteFile(path, page, 0o644)
	return path
}

// Opus can't be played yet, but must be refused politely: handing it to
// the Vorbis decoder crashed the app.
func TestOpusRefusedNotCrashing(t *testing.T) {
	path := opusFile(t)
	_, _, err := decode(path)
	if err == nil || !strings.Contains(err.Error(), "Ogg Opus") {
		t.Errorf("got %v", err)
	}
	info, err := probe(path)
	if err != nil || info.Codec != "Ogg Opus" || canAnalyse(info) {
		t.Errorf("probe %+v %v", info, err)
	}
}

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

// Empty files, as left by interrupted downloads, and copies inside zip
// files' __MACOSX folders, aren't songs.
func TestEmptyAndMacOSXFilesSkipped(t *testing.T) {
	u, fw, _ := newTestUIWithHidden(t)
	dir := fw.root
	os.WriteFile(filepath.Join(dir, "14 Track 14.mp3"), nil, 0o644)
	copyFile(t, "testdata/sample.mp3", filepath.Join(dir, "__MACOSX", "Album", "01.mp3"))
	u.queueScan(dir)
	if got := paths(u.all); len(got) != 3 {
		t.Fatalf("scan listed %q", got)
	}
	empty := filepath.Join(dir, "new.flac")
	os.WriteFile(empty, nil, 0o644)
	fw.changed([]string{empty, filepath.Join(dir, "__MACOSX")})
	if len(u.all) != 3 {
		t.Errorf("watcher listed %q", paths(u.all))
	}
}

// When Mugig's decoder stops but macOS's reads the whole file, the verdict
// rests on the checksum: matching means intact; without one, the doubt is
// stated rather than a verdict of damage.
func TestSecondOpinionWording(t *testing.T) {
	info := audioInfo{Codec: "FLAC", Lossless: true, kind: flacFile, Bits: 16, SampleRate: 44100}
	base := qualityReport{ReadBy: "macOS", OwnStop: "CRC-16 checksum mismatch", OwnStopAt: 118e9, Spectrum: spectrumResult{Analysed: true}}

	ok := base
	ok.Checksum = checksumOK
	a := assess(info, &ok)
	if a.level != qualityLossless || !hasCheck(a, checkOK, "Intact") || !hasCheck(a, checkInfo, "stopped at 1:58") {
		t.Errorf("with checksum: %+v", a)
	}
	a = assess(info, &base)
	if a.level != qualityLossless || !hasCheck(a, checkWarn, "can't be ruled out") {
		t.Errorf("without checksum: %+v", a)
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
	writeFLAC(t, path, 44100, 16, quantize(noise(44100, 44100, pink), 16))
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
	s, _, err := decode(path)
	if err == nil {
		s.Stream(make([][2]float64, 512))
		s.Close()
	}
	runtime.ReadMemStats(&after)
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 64<<20 {
		t.Errorf("decoding allocated %d MB", grew>>20)
	}
	// The check reads past the tags and judges the audio as usual.
	if _, err := probe(path); err != nil {
		t.Errorf("probe: %v", err)
	}
}
