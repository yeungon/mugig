package audio

// How long a song lasts, for the Time column. It is read from the file's
// headers while scanning, so it must not decode the audio: the decoders
// work out an MP3's length by reading every frame of it.

import (
	"bytes"
	"encoding/binary"
	"os"
	"time"
)

// fileLength opens path and returns how long its audio lasts, or 0.
func FileLength(path string) time.Duration {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	return TrackLength(f)
}

// trackLength returns how long the audio in f lasts, or 0 if its headers
// don't say. It reads with ReadAt, so it leaves f's offset where it was.
func TrackLength(f *os.File) time.Duration {
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	size := st.Size()
	kind, start := sniffFile(f)
	switch kind {
	case FLAC:
		// A damaged metadata block further on doesn't matter here: the
		// length is in the first.
		info, _ := probeFLAC(f, start, size)
		if info.SampleRate == 0 {
			return 0
		}
		return time.Duration(info.Samples) * time.Second / time.Duration(info.SampleRate)
	case WAV:
		return wavLength(f, size)
	case Ogg:
		return oggLength(f, size)
	case MP3:
		return mp3Length(f, start, size)
	}
	return 0
}

// wavLength is the WAV header's length, but no longer than the file holds:
// a recording that was cut short, or written as a stream, can claim more.
func wavLength(f *os.File, size int64) time.Duration {
	info, err := probeWAV(f, size)
	if err != nil || !info.Lossless || info.SampleBytes == 0 {
		return 0
	}
	bytes := min(info.DataSize, size-info.DataStart)
	samples := bytes / int64(info.SampleBytes*info.Channels)
	return time.Duration(samples) * time.Second / time.Duration(info.SampleRate)
}

// oggLength reads the sample rate from the first page and the position of
// the last sample from the last page.
func oggLength(f *os.File, size int64) time.Duration {
	var page [27 + 255 + 64]byte
	n, _ := f.ReadAt(page[:], 0)
	if n < 28 || 27+int(page[26]) > n {
		return 0
	}
	packet := page[27+int(page[26]) : n]
	var rate, skip int64
	switch {
	case bytes.HasPrefix(packet, []byte("\x01vorbis")) && len(packet) >= 16:
		rate = int64(binary.LittleEndian.Uint32(packet[12:]))
	case bytes.HasPrefix(packet, []byte("OpusHead")) && len(packet) >= 12:
		rate, skip = 48000, int64(binary.LittleEndian.Uint16(packet[10:])) // Opus always counts at 48 kHz
	case bytes.HasPrefix(packet, []byte("\x7fFLAC")) && len(packet) >= 30:
		info := packet[17:] // past the mapping header, "fLaC" and the block header
		rate = int64(info[10])<<12 | int64(info[11])<<4 | int64(info[12])>>4
	}
	if rate == 0 {
		return 0
	}
	serial := binary.LittleEndian.Uint32(page[14:])

	// The last page is within the last 64 KB: a page holds at most 255
	// segments of 255 bytes.
	tail := make([]byte, min(size, 65536+27))
	if _, err := f.ReadAt(tail, size-int64(len(tail))); err != nil {
		return 0
	}
	for i := bytes.LastIndex(tail, []byte("OggS")); i >= 0; i = bytes.LastIndex(tail[:i], []byte("OggS")) {
		if i+27 > len(tail) || tail[i+4] != 0 || binary.LittleEndian.Uint32(tail[i+14:]) != serial {
			continue
		}
		granule := int64(binary.LittleEndian.Uint64(tail[i+6:]))
		if granule <= 0 { // -1: no packet ends on this page
			continue
		}
		return time.Duration(max(granule-skip, 0)) * time.Second / time.Duration(rate)
	}
	return 0
}

// MPEG audio frame header tables, indexed by version: 0 MPEG-2.5, 2
// MPEG-2, 3 MPEG-1 (1 is reserved).
var (
	mpegRates = [4][3]int{{11025, 12000, 8000}, {}, {22050, 24000, 16000}, {44100, 48000, 32000}}
	// kbps by [MPEG-1?][layer 1, 2, 3][index]
	mpegBitrates = [2][3][16]int{
		{ // MPEG-2 and 2.5
			{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
			{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
			{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
		},
		{ // MPEG-1
			{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
			{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
			{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
		},
	}
)

// mpegFrame is what an MPEG audio frame header says.
type mpegFrame struct {
	mpeg1    bool
	layer    int // 1, 2 or 3
	kbps     int
	rate     int
	samples  int // per frame
	sideInfo int // bytes between the header and a Xing tag
	size     int // bytes, including the header
}

// parseMPEGHeader reads the 4-byte frame header in h, or returns false if
// it isn't one.
func parseMPEGHeader(h []byte) (mpegFrame, bool) {
	if len(h) < 4 || h[0] != 0xFF || h[1]&0xE0 != 0xE0 {
		return mpegFrame{}, false
	}
	version, layerBits := h[1]>>3&3, h[1]>>1&3
	rateIdx, kbpsIdx := h[2]>>2&3, h[2]>>4
	if version == 1 || layerBits == 0 || kbpsIdx == 0 || kbpsIdx == 15 || rateIdx == 3 {
		return mpegFrame{}, false // reserved values, or "free" bitrate
	}
	fr := mpegFrame{mpeg1: version == 3, layer: 4 - int(layerBits), rate: mpegRates[version][rateIdx]}
	v := 0
	if fr.mpeg1 {
		v = 1
	}
	fr.kbps = mpegBitrates[v][fr.layer-1][kbpsIdx]
	padding := int(h[2] >> 1 & 1)
	mono := h[3]>>6 == 3
	switch {
	case fr.layer == 1:
		fr.samples = 384
		fr.size = (12*fr.kbps*1000/fr.rate + padding) * 4
	case fr.layer == 3 && !fr.mpeg1:
		fr.samples = 576
		fr.size = 72*fr.kbps*1000/fr.rate + padding
	default:
		fr.samples = 1152
		fr.size = 144*fr.kbps*1000/fr.rate + padding
	}
	switch {
	case fr.mpeg1 && !mono:
		fr.sideInfo = 32
	case fr.mpeg1, !mono:
		fr.sideInfo = 17
	default: // MPEG-2 or 2.5, mono
		fr.sideInfo = 9
	}
	return fr, true
}

// firstMPEGFrame finds the first frame in buf whose next frame follows
// where it says, so stray bytes that happen to look like a header aren't
// taken for one. at is -1 if there is none.
func firstMPEGFrame(buf []byte) (at int, fr mpegFrame) {
	for i := 0; i+4 <= len(buf); i++ {
		h, ok := parseMPEGHeader(buf[i:])
		if !ok {
			continue
		}
		if next := i + h.size; next+4 <= len(buf) {
			if _, ok := parseMPEGHeader(buf[next:]); !ok {
				continue
			}
		}
		return i, h
	}
	return -1, mpegFrame{}
}

// xingTag is what the Xing or Info tag in an MP3's first frame says. LAME,
// and FFmpeg which uses it, add a note of the silence the encoder added
// before and after the music.
type xingTag struct {
	frames         int // not counting the frame the tag is in
	delay, padding int // samples added before and after; 0 without the note
}

// readXing reads the Xing or Info tag in the frame at at, if it has the
// frame count.
func readXing(buf []byte, at int, fr mpegFrame) (xingTag, bool) {
	x := at + 4 + fr.sideInfo
	if x+12 > len(buf) || (string(buf[x:x+4]) != "Xing" && string(buf[x:x+4]) != "Info") {
		return xingTag{}, false
	}
	flags := binary.BigEndian.Uint32(buf[x+4:])
	if flags&1 == 0 {
		return xingTag{}, false
	}
	tag := xingTag{frames: int(binary.BigEndian.Uint32(buf[x+8:]))}
	lame := x + 12
	for bit, size := range []int{4, 100, 4} { // byte count, seek table, quality
		if flags&(2<<bit) != 0 {
			lame += size
		}
	}
	if lame+24 > len(buf) || fr.layer != 3 {
		return tag, true
	}
	switch string(buf[lame : lame+4]) {
	case "LAME", "Lavc", "Lavf", "L3.9":
		v := buf[lame+21:]
		delay, padding := int(v[0])<<4|int(v[1])>>4, int(v[1]&15)<<8|int(v[2])
		if delay+padding < tag.frames*fr.samples {
			tag.delay, tag.padding = delay, padding
		}
	}
	return tag, true
}

// mp3Length counts the frames from the Xing or VBRI tag that encoders put
// in the first frame. Without one the file has a constant bitrate, so its
// size gives the length.
func mp3Length(f *os.File, start, size int64) time.Duration {
	buf := make([]byte, 16<<10)
	n, _ := f.ReadAt(buf, start)
	buf = buf[:n]
	at, fr := firstMPEGFrame(buf)
	if at < 0 {
		return 0
	}
	samples := func(n int) time.Duration {
		return time.Duration(n) * time.Second / time.Duration(fr.rate)
	}
	if x, ok := readXing(buf, at, fr); ok {
		return samples(x.frames*fr.samples - x.delay - x.padding)
	}
	if v := at + 4 + 32; v+18 <= len(buf) && string(buf[v:v+4]) == "VBRI" {
		return samples(int(binary.BigEndian.Uint32(buf[v+14:])) * fr.samples)
	}
	audio := size - start - int64(at)
	var tail [128]byte
	if _, err := f.ReadAt(tail[:], size-128); err == nil && string(tail[:3]) == "TAG" {
		audio -= 128 // ID3v1 tag
	}
	if audio <= 0 {
		return 0
	}
	return time.Duration(audio*8) * time.Second / time.Duration(fr.kbps*1000)
}
