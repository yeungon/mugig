package main

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fileKind is the kind of file, found from its first bytes.
type Kind int

// audioInfo describes a file's audio as its headers state it.
type Info struct {
	Codec      string // e.g. "FLAC", "WAV", "MP3", "Ogg Vorbis"
	Lossless   bool
	Float      bool // floating-point samples, which have no fixed bit depth
	SampleRate int
	Bits       int // bit depth; 0 for lossy formats
	Channels   int
	Kbps       int // average bitrate
	Duration   time.Duration

	// Mismatch is the file's extension when it doesn't match the content,
	// e.g. ".flac" for an MP3 renamed to .flac.
	Mismatch string

	Kind        Kind
	MD5         [md5.Size]byte // FLAC's checksum of the audio, all zero if absent
	Samples     int64          // per channel, 0 if unknown
	DataStart   int64          // WAV: where the samples start
	DataSize    int64          // WAV: bytes of samples the header promises
	format      uint16         // WAV: format code (1 integer PCM, 3 float)
	SampleBytes int            // WAV: bytes per sample as stored
}

// sniff identifies a file from its first bytes, or returns unknownFile.
func sniff(path string) Kind {
	f, err := os.Open(path)
	if err != nil {
		return Unknown
	}
	defer f.Close()
	kind, _ := sniffFile(f)
	return kind
}

// sniffFile also returns where the audio starts, past any ID3v2 tag.
func sniffFile(f *os.File) (Kind, int64) {
	start := skipID3(f)
	var magic [12]byte
	if _, err := f.ReadAt(magic[:], start); err != nil {
		return Unknown, start
	}
	switch {
	case string(magic[:4]) == "fLaC":
		return FLAC, start
	case string(magic[:4]) == "RIFF" && string(magic[8:12]) == "WAVE":
		return WAV, start
	case string(magic[:4]) == "OggS":
		return Ogg, start
	case magic[0] == 0xFF && magic[1]&0xE0 == 0xE0: // MPEG audio frame sync
		return MP3, start
	}
	return Unknown, start
}

// probe reads the headers of the file at path.
func Probe(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	kind, start := sniffFile(f)
	var info Info
	switch kind {
	case FLAC:
		info, err = probeFLAC(f, start, st.Size())
	case WAV:
		info, err = probeWAV(f, st.Size())
	case Ogg:
		info, err = probeOgg(f, path, st.Size())
	case MP3:
		info, err = probeLossy(path, MP3, "MP3", st.Size()-start)
	default:
		return Info{}, errors.New("not a recognised audio format")
	}
	if err != nil {
		return Info{}, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	named := map[string]Kind{".flac": FLAC, ".wav": WAV, ".mp3": MP3, ".ogg": Ogg}
	if k, ok := named[ext]; ok && k != kind {
		info.Mismatch = ext
	}
	return info, nil
}

// skipID3 returns where the audio starts, past any ID3v2 tag. MP3s nearly
// always have one, and some FLAC files do too.
func skipID3(f *os.File) int64 {
	var h [10]byte
	if _, err := f.ReadAt(h[:], 0); err != nil || string(h[:3]) != "ID3" {
		return 0
	}
	size := int64(h[6]&0x7f)<<21 | int64(h[7]&0x7f)<<14 | int64(h[8]&0x7f)<<7 | int64(h[9]&0x7f)
	if h[5]&0x10 != 0 { // footer
		size += 10
	}
	return 10 + size
}

// probeFLAC reads the STREAMINFO block, and skips the other metadata (which
// may hold large pictures) to find how many bytes of audio there are.
func probeFLAC(f *os.File, start, size int64) (Info, error) {
	info := Info{Codec: "FLAC", Lossless: true, Kind: FLAC}
	pos := start + 4
	for found := false; ; {
		var h [4]byte
		if _, err := f.ReadAt(h[:], pos); err != nil {
			return info, errors.New("FLAC header is damaged")
		}
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		if h[0]&0x7f == 0 { // STREAMINFO
			var b [34]byte
			if n < 34 {
				return info, errors.New("FLAC header is damaged")
			}
			if _, err := f.ReadAt(b[:], pos+4); err != nil {
				return info, errors.New("FLAC header is damaged")
			}
			info.SampleRate = int(b[10])<<12 | int(b[11])<<4 | int(b[12])>>4
			info.Channels = int(b[12]>>1&7) + 1
			info.Bits = int(b[12]&1)<<4 | int(b[13]>>4) + 1
			info.Samples = int64(b[13]&0xf)<<32 | int64(binary.BigEndian.Uint32(b[14:18]))
			copy(info.MD5[:], b[18:34])
			found = true
		}
		pos += 4 + n
		if h[0]&0x80 != 0 { // last metadata block
			if !found || info.SampleRate == 0 {
				return info, errors.New("FLAC header is damaged")
			}
			break
		}
	}
	if info.Samples > 0 {
		info.Duration = time.Duration(info.Samples) * time.Second / time.Duration(info.SampleRate)
		info.Kbps = kbps(size-pos, info.Duration)
	}
	return info, nil
}

// probeWAV reads the format and finds the samples. WAV files can hold
// compressed audio too, so the format code decides whether it is lossless.
func probeWAV(f *os.File, size int64) (Info, error) {
	info := Info{Codec: "WAV", Kind: WAV}
	var blockAlign int
	for pos := int64(12); pos+8 <= size; {
		var h [8]byte
		if _, err := f.ReadAt(h[:], pos); err != nil {
			break
		}
		id, n := string(h[:4]), int64(binary.LittleEndian.Uint32(h[4:]))
		switch id {
		case "fmt ":
			b := make([]byte, min(n, 40))
			if len(b) < 16 {
				return info, errors.New("WAV header is damaged")
			}
			if _, err := f.ReadAt(b, pos+8); err != nil {
				return info, errors.New("WAV header is damaged")
			}
			info.format = binary.LittleEndian.Uint16(b[0:])
			info.Channels = int(binary.LittleEndian.Uint16(b[2:]))
			info.SampleRate = int(binary.LittleEndian.Uint32(b[4:]))
			info.Kbps = int(binary.LittleEndian.Uint32(b[8:])) * 8 / 1000
			blockAlign = int(binary.LittleEndian.Uint16(b[12:]))
			info.Bits = int(binary.LittleEndian.Uint16(b[14:]))
			if info.format == 0xFFFE && len(b) >= 26 { // WAVE_FORMAT_EXTENSIBLE
				if valid := int(binary.LittleEndian.Uint16(b[18:])); valid > 0 {
					info.Bits = valid
				}
				info.format = binary.LittleEndian.Uint16(b[24:]) // from the sub-format GUID
			}
		case "data":
			info.DataStart, info.DataSize = pos+8, n
		}
		if info.DataStart > 0 && blockAlign > 0 {
			break
		}
		pos += 8 + n + n&1 // chunks are padded to an even length
	}
	if blockAlign == 0 || info.DataStart == 0 || info.Channels == 0 || info.SampleRate == 0 {
		return info, errors.New("WAV header is damaged")
	}
	info.SampleBytes = blockAlign / info.Channels
	switch info.format {
	case 1:
		info.Lossless = info.SampleBytes >= 1 && info.SampleBytes <= 4
	case 3:
		info.Lossless, info.Float = true, true
		info.Codec = "WAV (floating point)"
	default:
		info.Codec = fmt.Sprintf("WAV (compressed, format %#x)", info.format)
		info.Bits = 0
	}
	if !info.Lossless {
		return info, nil
	}
	info.Samples = info.DataSize / int64(blockAlign)
	info.Duration = time.Duration(info.Samples) * time.Second / time.Duration(info.SampleRate)
	return info, nil
}

// oggCodec names the codec inside an Ogg file, from its first packet, or
// returns "" if it can't tell.
func oggCodec(f *os.File) string {
	var page [27 + 255 + 8]byte
	n, _ := f.ReadAt(page[:], 0)
	if n < 28 || 27+int(page[26]) > n {
		return ""
	}
	packet := string(page[27+int(page[26]) : n])
	switch {
	case strings.HasPrefix(packet, "\x01vorbis"):
		return "Ogg Vorbis"
	case strings.HasPrefix(packet, "OpusHead"):
		return "Ogg Opus"
	case strings.HasPrefix(packet, "\x7fFLAC"):
		return "FLAC in Ogg"
	}
	return ""
}

// probeOgg identifies the codec inside an Ogg file from its first packet.
func probeOgg(f *os.File, path string, size int64) (Info, error) {
	var page [27 + 255 + 8]byte
	n, _ := f.ReadAt(page[:], 0)
	if n < 28 || 27+int(page[26]) > n {
		return Info{}, errors.New("Ogg file is damaged")
	}
	packet := page[27+int(page[26]) : n]
	switch {
	case strings.HasPrefix(string(packet), "\x01vorbis"):
		info, err := probeLossy(path, Ogg, "Ogg Vorbis", size)
		if len(packet) >= 24 { // prefer the encoder's nominal bitrate: size also counts tags and art
			if nominal := int32(binary.LittleEndian.Uint32(packet[20:])); nominal > 0 {
				info.Kbps = int(nominal) / 1000
			}
		}
		return info, err
	case strings.HasPrefix(string(packet), "OpusHead"):
		return Info{Codec: "Ogg Opus", Kind: Ogg}, nil
	case strings.HasPrefix(string(packet), "\x7fFLAC"):
		return Info{Codec: "FLAC in Ogg", Lossless: true, Kind: Ogg}, nil
	}
	return Info{}, errors.New("unknown codec in Ogg file")
}

// probeLossy gets the length and format of an MP3 or Ogg Vorbis file from
// its decoder, and works out the average bitrate from the audio's size.
func probeLossy(path string, kind Kind, codec string, audioBytes int64) (Info, error) {
	info := Info{Codec: codec, Kind: kind}
	s, format, err := DecodeAs(path, kind)
	if err != nil {
		return info, err
	}
	defer s.Close()
	info.SampleRate, info.Channels = int(format.SampleRate), format.NumChannels
	if s.Len() > 0 {
		info.Samples = int64(s.Len())
		info.Duration = format.SampleRate.D(s.Len())
		info.Kbps = kbps(audioBytes, info.Duration)
	}
	return info, nil
}

func kbps(bytes int64, d time.Duration) int {
	if d <= 0 || bytes <= 0 {
		return 0
	}
	return int(math.Round(float64(bytes) * 8 / d.Seconds() / 1000))
}

const (
	Unknown Kind = iota
	FLAC
	WAV
	MP3
	Ogg
)
