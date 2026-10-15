package main

import (
	"encoding/binary"
	"mugig/internal/audio"
	"mugig/internal/audiotest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	_, _, err := audio.Decode(path)
	if err == nil || !strings.Contains(err.Error(), "Ogg Opus") {
		t.Errorf("got %v", err)
	}
	info, err := audio.Probe(path)
	if err != nil || info.Codec != "Ogg Opus" || canAnalyse(info) {
		t.Errorf("probe %+v %v", info, err)
	}
}

// Empty files, as left by interrupted downloads, and copies inside zip
// files' __MACOSX folders, aren't songs.
func TestEmptyAndMacOSXFilesSkipped(t *testing.T) {
	u, fw, _ := newTestUIWithHidden(t)
	dir := fw.root
	os.WriteFile(filepath.Join(dir, "14 Track 14.mp3"), nil, 0o644)
	audiotest.CopyFile(t, "testdata/sample.mp3", filepath.Join(dir, "__MACOSX", "Album", "01.mp3"))
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
	info := audio.Info{Codec: "FLAC", Lossless: true, Kind: audio.FLAC, Bits: 16, SampleRate: 44100}
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
