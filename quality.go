package main

// Audio quality: what a file really contains, read from its own headers
// rather than its name, and for lossless files a deeper check of the audio
// itself:
//
//   - integrity: FLAC files store an MD5 checksum of their audio, so decoding
//     everything and comparing proves the audio is intact. WAV files have no
//     checksum, but a truncated one can be spotted.
//   - lossy source: MP3 and AAC encoders throw away everything above a cut-off
//     frequency (about 16 kHz at 128 kbps), so a "lossless" file made from
//     one has a sudden drop to silence there. Real recordings fade out
//     gradually. The same test finds hi-res files upsampled from CD quality.
//   - padded bits: a 24-bit file made from a 16-bit one has its lowest 8 bits
//     always zero.
//
// The last two are educated guesses, so they are worded as such.

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unsafe"

	"github.com/mewkiz/flac"
)

// fileKind is the kind of file, found from its first bytes.
type fileKind int

const (
	unknownFile fileKind = iota
	flacFile
	wavFile
	mp3File
	oggFile
)

// audioInfo describes a file's audio as its headers state it.
type audioInfo struct {
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

	kind        fileKind
	md5         [md5.Size]byte // FLAC's checksum of the audio, all zero if absent
	samples     int64          // per channel, 0 if unknown
	dataStart   int64          // WAV: where the samples start
	dataSize    int64          // WAV: bytes of samples the header promises
	format      uint16         // WAV: format code (1 integer PCM, 3 float)
	sampleBytes int            // WAV: bytes per sample as stored
}

// sniff identifies a file from its first bytes, or returns unknownFile.
func sniff(path string) fileKind {
	f, err := os.Open(path)
	if err != nil {
		return unknownFile
	}
	defer f.Close()
	kind, _ := sniffFile(f)
	return kind
}

// sniffFile also returns where the audio starts, past any ID3v2 tag.
func sniffFile(f *os.File) (fileKind, int64) {
	start := skipID3(f)
	var magic [12]byte
	if _, err := f.ReadAt(magic[:], start); err != nil {
		return unknownFile, start
	}
	switch {
	case string(magic[:4]) == "fLaC":
		return flacFile, start
	case string(magic[:4]) == "RIFF" && string(magic[8:12]) == "WAVE":
		return wavFile, start
	case string(magic[:4]) == "OggS":
		return oggFile, start
	case magic[0] == 0xFF && magic[1]&0xE0 == 0xE0: // MPEG audio frame sync
		return mp3File, start
	}
	return unknownFile, start
}

// probe reads the headers of the file at path.
func probe(path string) (audioInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return audioInfo{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return audioInfo{}, err
	}
	kind, start := sniffFile(f)
	var info audioInfo
	switch kind {
	case flacFile:
		info, err = probeFLAC(f, start, st.Size())
	case wavFile:
		info, err = probeWAV(f, st.Size())
	case oggFile:
		info, err = probeOgg(f, path, st.Size())
	case mp3File:
		info, err = probeLossy(path, mp3File, "MP3", st.Size()-start)
	default:
		return audioInfo{}, errors.New("not a recognised audio format")
	}
	if err != nil {
		return audioInfo{}, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	named := map[string]fileKind{".flac": flacFile, ".wav": wavFile, ".mp3": mp3File, ".ogg": oggFile}
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
func probeFLAC(f *os.File, start, size int64) (audioInfo, error) {
	info := audioInfo{Codec: "FLAC", Lossless: true, kind: flacFile}
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
			info.samples = int64(b[13]&0xf)<<32 | int64(binary.BigEndian.Uint32(b[14:18]))
			copy(info.md5[:], b[18:34])
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
	if info.samples > 0 {
		info.Duration = time.Duration(info.samples) * time.Second / time.Duration(info.SampleRate)
		info.Kbps = kbps(size-pos, info.Duration)
	}
	return info, nil
}

// probeWAV reads the format and finds the samples. WAV files can hold
// compressed audio too, so the format code decides whether it is lossless.
func probeWAV(f *os.File, size int64) (audioInfo, error) {
	info := audioInfo{Codec: "WAV", kind: wavFile}
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
			info.dataStart, info.dataSize = pos+8, n
		}
		if info.dataStart > 0 && blockAlign > 0 {
			break
		}
		pos += 8 + n + n&1 // chunks are padded to an even length
	}
	if blockAlign == 0 || info.dataStart == 0 || info.Channels == 0 || info.SampleRate == 0 {
		return info, errors.New("WAV header is damaged")
	}
	info.sampleBytes = blockAlign / info.Channels
	switch info.format {
	case 1:
		info.Lossless = info.sampleBytes >= 1 && info.sampleBytes <= 4
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
	info.samples = info.dataSize / int64(blockAlign)
	info.Duration = time.Duration(info.samples) * time.Second / time.Duration(info.SampleRate)
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
func probeOgg(f *os.File, path string, size int64) (audioInfo, error) {
	var page [27 + 255 + 8]byte
	n, _ := f.ReadAt(page[:], 0)
	if n < 28 || 27+int(page[26]) > n {
		return audioInfo{}, errors.New("Ogg file is damaged")
	}
	packet := page[27+int(page[26]) : n]
	switch {
	case strings.HasPrefix(string(packet), "\x01vorbis"):
		info, err := probeLossy(path, oggFile, "Ogg Vorbis", size)
		if len(packet) >= 24 { // prefer the encoder's nominal bitrate: size also counts tags and art
			if nominal := int32(binary.LittleEndian.Uint32(packet[20:])); nominal > 0 {
				info.Kbps = int(nominal) / 1000
			}
		}
		return info, err
	case strings.HasPrefix(string(packet), "OpusHead"):
		return audioInfo{Codec: "Ogg Opus", kind: oggFile}, nil
	case strings.HasPrefix(string(packet), "\x7fFLAC"):
		return audioInfo{Codec: "FLAC in Ogg", Lossless: true, kind: oggFile}, nil
	}
	return audioInfo{}, errors.New("unknown codec in Ogg file")
}

// probeLossy gets the length and format of an MP3 or Ogg Vorbis file from
// its decoder, and works out the average bitrate from the audio's size.
func probeLossy(path string, kind fileKind, codec string, audioBytes int64) (audioInfo, error) {
	info := audioInfo{Codec: codec, kind: kind}
	s, format, err := decodeAs(path, kind)
	if err != nil {
		return info, err
	}
	defer s.Close()
	info.SampleRate, info.Channels = int(format.SampleRate), format.NumChannels
	if s.Len() > 0 {
		info.samples = int64(s.Len())
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

// ---------------------------------------------------------------------------
// Checking the audio
// ---------------------------------------------------------------------------

type checksumResult int

const (
	noChecksum checksumResult = iota
	checksumOK
	checksumBad
)

// qualityReport is what decoding a lossless file in full found. It holds
// measurements rather than verdicts, so results saved by an older version
// are judged by the current rules (see assess). Its fields are exported to
// be saved (see qualityStore).
type qualityReport struct {
	Damaged  string        // why decoding failed partway, if it did
	DamageAt time.Duration // where
	Checksum checksumResult
	Missing  time.Duration // audio the header promises but the file lacks
	UsedBits int           // bits that are ever non-zero, 0 if not known
	Spectrum spectrumResult
	Levels   levelsResult // loudness and peaks

	ReadBy    string        // "macOS" if Mugig's decoder failed and macOS's read the whole file
	OwnStop   string        // then why Mugig's own decoder stopped…
	OwnStopAt time.Duration // …and where
}

// verify decodes the whole of a lossless file. It stops early, returning
// ctx's error, if ctx is cancelled.
func verify(ctx context.Context, path string, info audioInfo) (qualityReport, error) {
	rep, _, err := analyse(ctx, path, info, false, nil)
	return rep, err
}

// analyse decodes the whole file, checking lossless audio (see verify) and,
// if picture is set, drawing a spectrogram of any audio. progress, if not
// nil, is called with the fraction done as decoding goes on.
func analyse(ctx context.Context, path string, info audioInfo, picture bool, progress func(float64)) (rep qualityReport, img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil { // a decoder choking on a damaged file
			rep, img, err = qualityReport{Damaged: fmt.Sprint("the decoder failed: ", r)}, nil, nil
		}
	}()
	report := progressReporter(info.samples, progress)
	lv := newLevels(float64(info.SampleRate), info.Channels)
	var spec *spectrum
	switch {
	case info.kind == flacFile:
		rep, spec, err = verifyFLAC(ctx, path, info, report, lv)
	case info.kind == wavFile && info.Lossless:
		rep, spec, err = verifyWAV(ctx, path, info, report, lv)
	case info.kind == mp3File || info.Codec == "Ogg Vorbis":
		spec, err = decodeLossy(ctx, path, info, report, lv)
	default:
		return rep, nil, errors.New("cannot check this format")
	}
	if err != nil {
		return rep, nil, err
	}
	rep.Spectrum = spec.result()
	rep.Levels = lv.result()
	if picture {
		img = spec.picture()
	}
	return rep, img, nil
}

// canAnalyse reports whether analyse can decode the file.
func canAnalyse(info audioInfo) bool {
	return canVerify(info) || info.kind == mp3File || info.Codec == "Ogg Vorbis"
}

// progressReporter turns a count of samples decoded into calls to f, at
// most once per percent.
func progressReporter(total int64, f func(float64)) func(done int64) {
	last := -1
	return func(done int64) {
		if f == nil || total <= 0 {
			return
		}
		if pc := int(min(done*100/total, 100)); pc > last {
			last = pc
			f(float64(pc) / 100)
		}
	}
}

// decodeLossy runs a lossy file through the player's decoder to draw its
// spectrogram. Lossy files have nothing to verify, but their spectrogram is
// useful to compare: it shows the encoder's cut-off.
func decodeLossy(ctx context.Context, path string, info audioInfo, report func(int64), lv *levels) (*spectrum, error) {
	s, format, err := decodeAs(path, info.kind)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	spec := newSpectrum(float64(format.SampleRate), int64(s.Len()))
	buf := make([][2]float64, 4096)
	mono := make([]float64, 4096)
	var decoded int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, ok := s.Stream(buf)
		for i := range n {
			mono[i] = (buf[i][0] + buf[i][1]) / 2
			lv.add(buf[i]) // a mono file's decoder repeats it in both: only the first is used
		}
		spec.add(mono[:n])
		decoded += int64(n)
		report(decoded)
		if !ok || n == 0 {
			break
		}
	}
	return spec, nil
}

func verifyFLAC(ctx context.Context, path string, info audioInfo, report func(int64), lv *levels) (qualityReport, *spectrum, error) {
	var rep qualityReport
	spec := newSpectrum(float64(info.SampleRate), info.samples)
	stream, err := flac.Open(path)
	if err != nil {
		return secondOpinion(ctx, path, info, report, lv, qualityReport{Damaged: err.Error()}, spec)
	}
	defer stream.Close()

	sum := md5.New()
	scale := 1 / float64(int64(1)<<(info.Bits-1))
	var used int32 // every sample ORed together
	var decoded int64
	var mono []float64
	for {
		if err := ctx.Err(); err != nil {
			return rep, nil, err
		}
		fr, err := stream.ParseNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			rep.Damaged = err.Error()
			rep.DamageAt = time.Duration(decoded) * time.Second / time.Duration(info.SampleRate)
			return secondOpinion(ctx, path, info, report, lv, rep, spec)
		}
		fr.Hash(sum)
		n := int(fr.BlockSize)
		mono = mono[:0]
		for i := range n {
			var s int64
			var frame [2]float64
			for c, sub := range fr.Subframes {
				used |= sub.Samples[i]
				s += int64(sub.Samples[i])
				if c < 2 {
					frame[c] = float64(sub.Samples[i]) * scale
				}
			}
			mono = append(mono, float64(s)*scale/float64(len(fr.Subframes)))
			lv.add(frame)
		}
		spec.add(mono)
		decoded += int64(n)
		report(decoded)
	}
	if rep.Damaged == "" {
		if info.md5 != [md5.Size]byte{} {
			rep.Checksum = checksumBad
			if [md5.Size]byte(sum.Sum(nil)) == info.md5 {
				rep.Checksum = checksumOK
			}
		}
		if info.samples > decoded {
			rep.Missing = time.Duration(info.samples-decoded) * time.Second / time.Duration(info.SampleRate)
		}
	}
	rep.UsedBits = usedBits(int64(used), info.Bits)
	return rep, spec, nil
}

// secondOpinion is called when the FLAC library fails partway through a
// file (own holds where). Where macOS's decoder is available it reads the
// file again from the start: if that succeeds, its result stands, noting
// where Mugig's own decoder stopped. Otherwise the file is damaged.
func secondOpinion(ctx context.Context, path string, info audioInfo, report func(int64), lv *levels, own qualityReport, spec *spectrum) (qualityReport, *spectrum, error) {
	if !appleDecoding {
		return own, spec, nil
	}
	fresh := newLevels(float64(info.SampleRate), info.Channels)
	rep, spec2, err := verifyWithApple(ctx, path, info, report, fresh)
	if err != nil {
		return rep, nil, err // cancelled
	}
	if rep.Damaged != "" {
		return own, spec, nil // both decoders fail: damaged
	}
	rep.OwnStop, rep.OwnStopAt = own.Damaged, own.DamageAt
	*lv = *fresh
	return rep, spec2, nil
}

// verifyWithApple checks a FLAC file with macOS's decoder. Its samples are
// exact (floats hold 24-bit samples exactly), so the stored checksum can
// be compared just as with the FLAC library.
func verifyWithApple(ctx context.Context, path string, info audioInfo, report func(int64), lv *levels) (qualityReport, *spectrum, error) {
	spec := newSpectrum(float64(info.SampleRate), info.samples)
	f, err := appleOpen(path, true)
	if err != nil {
		return qualityReport{Damaged: err.Error()}, spec, nil
	}
	defer f.close()
	rep := qualityReport{ReadBy: "macOS"}
	bits := info.Bits
	if bits <= 0 || bits > 32 {
		bits = f.bits
	}
	full := float64(int64(1) << (bits - 1))
	width := (bits + 7) / 8 // bytes per sample in FLAC's checksum
	scale := 1 / full
	sum := md5.New()
	buf := make([]float32, 4096*f.channels)
	out := make([]byte, 0, len(buf)*4)
	mono := make([]float64, 0, 4096)
	var used int64
	var decoded int64
	for {
		if err := ctx.Err(); err != nil {
			return rep, nil, err
		}
		n, err := f.read(unsafe.Pointer(&buf[0]), 4096)
		if err != nil {
			rep.Damaged = err.Error()
			rep.DamageAt = time.Duration(decoded) * time.Second / time.Duration(info.SampleRate)
			break
		}
		if n == 0 {
			break
		}
		out, mono = out[:0], mono[:0]
		for i := range n {
			s := 0.0
			var frame [2]float64
			for c := range f.channels {
				v := int64(math.Round(float64(buf[i*f.channels+c]) * full))
				used |= v
				for b := range width {
					out = append(out, byte(v>>(8*b)))
				}
				x := float64(v) * scale
				s += x
				if c < 2 {
					frame[c] = x
				}
			}
			mono = append(mono, s/float64(f.channels))
			lv.add(frame)
		}
		sum.Write(out)
		spec.add(mono)
		decoded += int64(n)
		report(decoded)
	}
	if rep.Damaged == "" {
		if info.md5 != [md5.Size]byte{} {
			rep.Checksum = checksumBad
			if [md5.Size]byte(sum.Sum(nil)) == info.md5 {
				rep.Checksum = checksumOK
			}
		}
		if info.samples > decoded {
			rep.Missing = time.Duration(info.samples-decoded) * time.Second / time.Duration(info.SampleRate)
		}
	}
	rep.UsedBits = usedBits(used, bits)
	return rep, spec, nil
}

func verifyWAV(ctx context.Context, path string, info audioInfo, report func(int64), lv *levels) (qualityReport, *spectrum, error) {
	var rep qualityReport
	spec := newSpectrum(float64(info.SampleRate), info.samples)
	f, err := os.Open(path)
	if err != nil {
		rep.Damaged = err.Error()
		return rep, spec, nil
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		if avail := st.Size() - info.dataStart; avail < info.dataSize {
			short := (info.dataSize - avail) / int64(info.sampleBytes*info.Channels)
			rep.Missing = time.Duration(short) * time.Second / time.Duration(info.SampleRate)
		}
	}
	if _, err := f.Seek(info.dataStart, io.SeekStart); err != nil {
		rep.Damaged = err.Error()
		return rep, spec, nil
	}

	frame := info.sampleBytes * info.Channels
	r := io.LimitReader(f, info.dataSize)
	buf := make([]byte, frame*4096)
	mono := make([]float64, 0, 4096)
	var decoded int64
	containerBits := info.sampleBytes * 8
	scale := 1 / float64(int64(1)<<(containerBits-1))
	var used int64
	for {
		if err := ctx.Err(); err != nil {
			return rep, nil, err
		}
		n, err := io.ReadFull(r, buf)
		n -= n % frame
		mono = mono[:0]
		for off := 0; off < n; off += frame {
			s := 0.0
			var fr [2]float64
			for c := range info.Channels {
				b := buf[off+c*info.sampleBytes : off+(c+1)*info.sampleBytes]
				var x float64
				if info.Float {
					x = wavFloat(b)
				} else {
					v := wavInt(b)
					used |= v
					x = float64(v) * scale
				}
				s += x
				if c < 2 {
					fr[c] = x
				}
			}
			mono = append(mono, s/float64(info.Channels))
			lv.add(fr)
		}
		spec.add(mono)
		decoded += int64(len(mono))
		report(decoded)
		if err != nil {
			break // io.EOF or io.ErrUnexpectedEOF: the end, short or not, is counted above
		}
	}
	if !info.Float {
		// Samples are stored left-aligned in their container, so bits count
		// from the container's top.
		rep.UsedBits = min(usedBits(used, containerBits), info.Bits)
	}
	return rep, spec, nil
}

// wavInt reads a little-endian sample of 1 to 4 bytes. 8-bit samples are
// unsigned, the rest signed.
func wavInt(b []byte) int64 {
	if len(b) == 1 {
		return int64(b[0]) - 128
	}
	var v int64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | int64(b[i])
	}
	shift := 64 - 8*len(b)
	return v << shift >> shift // sign-extend
}

func wavFloat(b []byte) float64 {
	switch len(b) {
	case 4:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
	case 8:
		return math.Float64frombits(binary.LittleEndian.Uint64(b))
	}
	return 0
}

// usedBits counts how many of a depth-bit sample's bits are in use, given
// every sample ORed together: low bits that are always zero are padding.
func usedBits(or int64, depth int) int {
	if or == 0 {
		return 0 // silence: nothing to tell
	}
	return max(depth-bits.TrailingZeros64(uint64(or)), 0)
}

// ---------------------------------------------------------------------------
// Frequency analysis
// ---------------------------------------------------------------------------

const (
	specSize     = 4096    // FFT length
	specMaxFrame = 3000    // most blocks analysed per file, spread over its length
	quietRMS     = 0.0003  // about -70 dBFS: blocks this quiet are skipped
	bandHz       = 100.0   // width of the bands the spectrum is averaged into
	edgeGap      = 200.0   // compare the level this far below and above an edge…
	edgeSpan     = 500.0   // …over this width
	cutoffDrop   = 30.0    // dB: a drop this sudden is a lossy encoder's cut-off
	minAnalysed  = 20      // blocks needed for a meaningful average
	cutoffFloor  = 5_000.0 // ignore edges below this: no encoder cuts that low
)

// Spectral holes. MP3 and AAC encoders save space by dropping parts of the
// upper spectrum from moment to moment, leaving gaps beside louder sound,
// even when they keep everything up to 20 kHz and so show no cut-off. In a
// real recording a frequency is rarely far below its neighbours, so the
// share of such gaps tells the two apart.
//
// Calibrated on 226 real MP3s (turned into 16-bit, as a fake FLAC holds
// them) and 574 lossless files: genuine files sit at about 0.0008, the
// level noise alone produces; the cut-off and holes tests together caught
// 93% of the MP3s, including most 192 and 320 kbps ones the cut-off test
// misses, and flagged 1.6% of the lossless files, some of them real fakes.
const (
	holesFrom      = 8_000.0  // Hz: below this, encoders keep nearly everything
	holesTo        = 19_500.0 // Hz: above this a cut-off, not holes, would show
	holeDepth      = 0.001    // a bin 30 dB below its 1 kHz neighbourhood's median is a hole
	holesThreshold = 0.0015   // share of hole bins that suggests a lossy source
	strongHoles    = 0.003    // twice that: very likely
)

// spectrum averages the power spectrum of a file's sound.
type spectrum struct {
	rate   float64
	window []float64
	buf    []complex128
	power  []float64
	frames int
	every  int // analyse one block in this many
	block  int // blocks seen
	fill   int // samples in buf

	// columns holds every analysed block's power in specRows frequency
	// rows, quiet ones included, to draw the spectrogram (see picture).
	columns [][]float32

	holes   []int     // per bin: loud blocks in which the bin was a hole
	blk     []float64 // this block's power per bin
	scratch []float64
}

// newSpectrum prepares to analyse audio at rate Hz. samples is the file's
// length if known, so long files can be sampled rather than analysed whole.
func newSpectrum(rate float64, samples int64) *spectrum {
	s := &spectrum{
		rate:   rate,
		window: make([]float64, specSize),
		buf:    make([]complex128, specSize),
		power:  make([]float64, specSize/2),
		every:  max(1, int(samples/specSize/specMaxFrame)),
		holes:  make([]int, specSize/2),
		blk:    make([]float64, specSize/2),
	}
	for i := range s.window {
		s.window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(specSize-1))
	}
	return s
}

// add feeds the next stretch of mono audio, in the range -1 to 1.
func (s *spectrum) add(mono []float64) {
	for len(mono) > 0 {
		n := min(len(mono), specSize-s.fill)
		if s.block%s.every == 0 {
			for i, v := range mono[:n] {
				s.buf[s.fill+i] = complex(v, 0)
			}
		}
		s.fill += n
		mono = mono[n:]
		if s.fill == specSize {
			if s.block%s.every == 0 {
				s.analyse()
			}
			s.block++
			s.fill = 0
		}
	}
}

func (s *spectrum) analyse() {
	sq := 0.0
	for i := range s.buf {
		v := real(s.buf[i])
		sq += v * v
		s.buf[i] = complex(v*s.window[i], 0)
	}
	fft(s.buf)
	col := make([]float32, specRows)
	for k := range s.power {
		re, im := real(s.buf[k]), imag(s.buf[k])
		col[k*specRows/len(s.power)] += float32(re*re + im*im)
	}
	s.columns = append(s.columns, col)
	if math.Sqrt(sq/specSize) < quietRMS {
		return // too quiet to judge the frequency range by
	}
	for k := range s.power {
		re, im := real(s.buf[k]), imag(s.buf[k])
		s.blk[k] = re*re + im*im
		s.power[k] += s.blk[k]
	}
	s.countHoles()
	s.frames++
}

// countHoles marks this block's bins, from holesFrom up, that are far below
// the median of their 1 kHz band.
func (s *spectrum) countHoles() {
	binHz := s.rate / specSize
	per := int(1000 / binHz)
	for lo := int(holesFrom / binHz); lo+per <= len(s.blk); lo += per {
		s.scratch = append(s.scratch[:0], s.blk[lo:lo+per]...)
		slices.Sort(s.scratch)
		limit := s.scratch[per/2] * holeDepth
		for k := lo; k < lo+per; k++ {
			if s.blk[k] < limit {
				s.holes[k]++
			}
		}
	}
}

// holeShare is the share of hole bins between holesFrom and holesTo, or
// 500 Hz below a cut-off, so the cut-off's silence doesn't count as holes.
func (s *spectrum) holeShare(cutoff float64) float64 {
	top := min(holesTo, s.rate/2*0.98)
	if cutoff > 0 {
		top = min(top, cutoff-500)
	}
	binHz := s.rate / specSize
	lo, hi := int(holesFrom/binHz), min(int(top/binHz), len(s.holes))
	if hi <= lo || s.frames == 0 {
		return 0
	}
	n := 0
	for _, h := range s.holes[lo:hi] {
		n += h
	}
	return float64(n) / float64(s.frames*(hi-lo))
}

// spectrumResult is what the frequency analysis found.
type spectrumResult struct {
	Analysed bool    // false if the audio was too short or quiet
	Cutoff   float64 // Hz where the sound stops abruptly, or 0
	Drop     float64 // dB the level falls there: the bigger, the surer
	Holes    float64 // share of the upper spectrum with encoder-like gaps (see holesThreshold)
}

// result looks for a cut-off: the frequency with the biggest sudden drop in
// level, provided the sound never comes back above it.
func (s *spectrum) result() spectrumResult {
	if s.frames < minAnalysed {
		return spectrumResult{}
	}
	binHz := s.rate / specSize
	per := max(1, int(math.Round(bandHz/binHz)))
	var level []float64 // dB per band
	for b := 0; (b+1)*per <= len(s.power); b++ {
		p := 0.0
		for _, v := range s.power[b*per : (b+1)*per] {
			p += v
		}
		level = append(level, 10*math.Log10(p/float64(per*s.frames)+1e-30))
	}
	width := float64(per) * binHz
	at := func(f float64) int { return min(max(int(f/width), 0), len(level)-1) }
	mean := func(lo, hi float64) float64 {
		a, b := at(lo), at(hi)
		sum := 0.0
		for _, v := range level[a : b+1] {
			sum += v
		}
		return sum / float64(b-a+1)
	}
	top := s.rate / 2 * 0.98 // the very top is often rolled off by the converter

	res := spectrumResult{Analysed: true}
	best := 0.0
	for b := at(cutoffFloor); float64(b)*width+edgeGap+edgeSpan <= top; b++ {
		edge := float64(b) * width
		below := mean(edge-edgeGap-edgeSpan, edge-edgeGap)
		above := mean(edge+edgeGap, edge+edgeGap+edgeSpan)
		drop := below - above
		if drop < cutoffDrop || drop <= best {
			continue
		}
		peak := math.Inf(-1) // the loudest the sound gets above the edge
		for _, v := range level[at(edge+edgeGap) : at(top)+1] {
			peak = max(peak, v)
		}
		if peak > above+10 {
			continue // the sound comes back: a gap, not a cut-off
		}
		best = drop
		res.Drop = math.Round(drop)
		// Place the cut-off where the level falls halfway, in dB.
		half := (below + above) / 2
		res.Cutoff = edge
		for c := at(edge - edgeGap - edgeSpan); c <= at(edge+edgeGap+edgeSpan); c++ {
			if level[c] < half {
				res.Cutoff = float64(c) * width
				break
			}
		}
	}
	res.Holes = s.holeShare(res.Cutoff)
	return res
}

// ---------------------------------------------------------------------------
// Verdict
// ---------------------------------------------------------------------------

// qualityLevel is the one-word verdict shown on the badge.
type qualityLevel int

const (
	qualityUnknown  qualityLevel = iota // nothing playing, or unreadable
	qualityLossy                        // MP3, Ogg Vorbis…
	qualityLossless                     // up to CD quality
	qualityHiRes                        // lossless beyond CD: more than 16 bits or 48 kHz
	qualitySuspect                      // lossless format, but probably not lossless audio
	qualityDamaged                      // the audio is corrupt or incomplete
)

type checkKind int

const (
	checkOK checkKind = iota
	checkWarn
	checkBad
	checkInfo
	checkPending
)

// check is one finding, as a sentence for the Now Playing page.
type check struct {
	kind checkKind
	text string
}

type assessment struct {
	level  qualityLevel
	spec   string // e.g. "FLAC · 16-bit · 44.1 kHz · Stereo · 1,011 kbps"
	checks []check
}

// canVerify reports whether verify can check the file's audio.
func canVerify(info audioInfo) bool {
	return info.Lossless && (info.kind == flacFile || info.kind == wavFile)
}

// lossyCutoff is below where a cut-off means a lossy source. Higher cut-offs
// come from high-bitrate MP3 and AAC, but also from some recording gear, so
// they are only mentioned.
const lossyCutoff = 19_500.0

// assess turns what probe and verify found into a verdict. rep is nil if
// the audio hasn't been checked, in which case the verdict comes from the
// headers alone.
func assess(info audioInfo, rep *qualityReport) assessment {
	a := assessment{spec: describe(info)}
	if info.Mismatch != "" {
		a.checks = append(a.checks, check{checkWarn, fmt.Sprintf("Named %s, but holds %s audio", info.Mismatch, info.Codec)})
	}
	if !info.Lossless {
		a.level = qualityLossy
		a.checks = append(a.checks, check{checkInfo, "Lossy: some of the sound was removed to make the file smaller"})
		if rep != nil {
			a.checks = append(a.checks, levelChecks(rep.Levels)...)
		}
		return a
	}
	a.level = qualityLossless
	if (info.Bits > 16 && !info.Float) || info.SampleRate > 48000 {
		a.level = qualityHiRes
	}
	if !canVerify(info) {
		a.checks = append(a.checks, check{checkInfo, "This format's audio can't be checked"})
		return a
	}
	if rep == nil {
		return a // not checked yet; the caller says so
	}

	// Integrity.
	switch {
	case rep.Damaged != "":
		a.checks = append(a.checks, check{checkBad, fmt.Sprintf("Damaged: the audio can't be read from %s on", formatTime(rep.DamageAt))})
	case rep.Checksum == checksumBad:
		a.checks = append(a.checks, check{checkBad, "Damaged: the audio doesn't match the checksum stored in the file"})
	case rep.Missing > 0:
		a.checks = append(a.checks, check{checkBad, fmt.Sprintf("Incomplete: %s of audio is missing from the end", roundedDuration(rep.Missing))})
	case rep.Checksum == checksumOK:
		a.checks = append(a.checks, check{checkOK, "Intact: the audio matches the checksum stored in the file"})
	case info.kind == flacFile:
		a.checks = append(a.checks, check{checkOK, "Reads cleanly (this file has no stored checksum to compare)"})
	default:
		a.checks = append(a.checks, check{checkOK, "Complete and readable (WAV files have no checksum)"})
	}
	switch {
	case rep.ReadBy != "" && rep.Checksum == checksumOK:
		a.checks = append(a.checks, check{checkInfo, fmt.Sprintf(
			"Mugig's decoder stopped at %s, but %s's read the whole file and it matches its checksum. "+
				"If this keeps changing, the disk may be returning bad data", formatTime(rep.OwnStopAt), rep.ReadBy)})
	case rep.ReadBy != "" && rep.Checksum == noChecksum:
		a.checks = append(a.checks, check{checkWarn, fmt.Sprintf(
			"Mugig's decoder stopped at %s, but %s's read the whole file. With no stored checksum, "+
				"damage can't be ruled out", formatTime(rep.OwnStopAt), rep.ReadBy)})
	}
	damaged := rep.Damaged != "" || rep.Checksum == checksumBad || rep.Missing > 0

	// Lossy source or upsampling, from the spectrum.
	// A cut-off at 19.5 kHz or above isn't mentioned: on real files it was
	// only ever the top of a normal recording, never an MP3's.
	suspect := false
	sp := rep.Spectrum
	holey := sp.Holes > holesThreshold
	switch {
	case !sp.Analysed:
		a.checks = append(a.checks, check{checkInfo, "Too short or quiet to check the frequency range"})
	case sp.Cutoff > 0 && sp.Cutoff < lossyCutoff:
		suspect = true
		gaps := ""
		if holey {
			gaps = ", with the gaps lossy encoders leave"
		}
		a.checks = append(a.checks, check{checkWarn, fmt.Sprintf(
			"Sound drops %.0f dB at %s%s, like %s: %s converted from a lossy file",
			sp.Drop, kHz(sp.Cutoff), gaps, lossyGuess(sp.Cutoff), likely(sp.Drop >= strongDrop || holey))})
	case holey:
		suspect = true
		a.checks = append(a.checks, check{checkWarn, fmt.Sprintf(
			"The sound has gaps of the kind MP3 and AAC encoders leave: %s converted from a lossy file",
			likely(sp.Holes >= strongHoles))})
	case info.SampleRate > 48000 && sp.Cutoff > 0 && sp.Cutoff < 24_500:
		suspect = true
		a.checks = append(a.checks, check{checkWarn, fmt.Sprintf(
			"Sound drops %.0f dB at %s: %s upsampled from CD quality, not true hi-res",
			sp.Drop, kHz(sp.Cutoff), likely(sp.Drop >= strongDrop))})
	case info.SampleRate > 48000:
		a.checks = append(a.checks, check{checkOK, "No sign of upsampling or a lossy source"})
	default:
		a.checks = append(a.checks, check{checkOK, "No sign of a lossy source: no cut-off or encoder gaps"})
	}

	// Bit depth.
	hiResBits := info.Bits > 16 && !info.Float
	switch {
	case rep.UsedBits > 0 && rep.UsedBits < info.Bits:
		if rep.UsedBits <= 16 && hiResBits {
			suspect = true
		}
		hiResBits = rep.UsedBits > 16
		a.checks = append(a.checks, check{checkWarn, fmt.Sprintf(
			"Only %d of its %d bits are used: probably made from a %d-bit original", rep.UsedBits, info.Bits, rep.UsedBits)})
	case hiResBits && rep.UsedBits == info.Bits:
		a.checks = append(a.checks, check{checkOK, fmt.Sprintf("Uses all %d bits", info.Bits)})
	}

	a.checks = append(a.checks, levelChecks(rep.Levels)...)

	switch {
	case damaged:
		a.level = qualityDamaged
	case suspect:
		a.level = qualitySuspect
	case a.level == qualityHiRes && !hiResBits && info.SampleRate <= 48000:
		a.level = qualityLossless
	}
	return a
}

// levelChecks describes loudness and peaks: a line with loudness, true
// peak and dynamic range, and one on clipping. These are about how the
// music was mastered, not the file, so they don't change the verdict.
func levelChecks(lv levelsResult) []check {
	if !lv.Measured || math.IsInf(lv.LUFS, -1) {
		return nil
	}
	parts := []string{fmt.Sprintf("Loudness %.1f LUFS", lv.LUFS), fmt.Sprintf("true peak %.1f dBTP", lv.TruePeak)}
	if lv.DR > 0 {
		parts = append(parts, fmt.Sprintf("dynamic range DR%d (%s)", lv.DR, drWords(lv.DR)))
	}
	checks := []check{{checkInfo, strings.Join(parts, " · ")}}
	switch {
	case lv.ClipRuns > 0:
		checks = append(checks, check{checkWarn, fmt.Sprintf(
			"Clipped in %s: the waveform is cut flat at full volume, which distorts", plural(lv.ClipRuns, "place"))})
	case lv.TruePeak > 0.05:
		checks = append(checks, check{checkWarn, fmt.Sprintf(
			"Peaks reach %.1f dB over full scale between samples, which can distort on some players", lv.TruePeak)})
	default:
		checks = append(checks, check{checkOK, "No clipping"})
	}
	return checks
}

// drWords puts a DR figure in words, as the DR database colours them.
func drWords(dr int) string {
	switch {
	case dr <= 7:
		return "heavily compressed"
	case dr <= 13:
		return "moderate"
	}
	return "wide"
}

// strongDrop is how far, in dB, the level must fall at a cut-off to call a
// lossy source very likely rather than probable. Lossy encoders leave
// nothing at all above their cut-off, so the level falls to the noise
// floor, typically 60 dB or more; a natural fall is rarely this sheer.
const strongDrop = 50.0

func likely(strong bool) string {
	if strong {
		return "very likely"
	}
	return "probably"
}

// lossyGuess names the kind of lossy file that cuts off at hz. Encoders
// differ, so this is only a rough guide.
func lossyGuess(hz float64) string {
	switch {
	case hz < 14_000:
		return "a low-bitrate MP3 (96 kbps or less)"
	case hz < 16_500:
		return "an MP3 of about 128 kbps"
	case hz < 18_500:
		return "an MP3 of about 128–160 kbps"
	}
	return "an MP3 of about 192 kbps"
}

// describe sums up the format, e.g. "FLAC · 24-bit · 96 kHz · Stereo ·
// 2,304 kbps".
func describe(info audioInfo) string {
	parts := []string{info.Codec}
	switch {
	case info.Float:
		parts = append(parts, "32-bit float")
	case info.Lossless && info.Bits > 0:
		parts = append(parts, fmt.Sprintf("%d-bit", info.Bits))
	}
	if info.SampleRate > 0 {
		parts = append(parts, kHz(float64(info.SampleRate)))
	}
	switch info.Channels {
	case 0:
	case 1:
		parts = append(parts, "Mono")
	case 2:
		parts = append(parts, "Stereo")
	default:
		parts = append(parts, fmt.Sprintf("%d channels", info.Channels))
	}
	if info.Kbps > 0 {
		parts = append(parts, thousands(info.Kbps)+" kbps")
	}
	return strings.Join(parts, " · ")
}

// kHz formats a frequency, e.g. "44.1 kHz", "48 kHz".
func kHz(hz float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", hz/1000), ".0") + " kHz"
}

// roundedDuration formats d for a sentence, e.g. "2 seconds", "1:05".
func roundedDuration(d time.Duration) string {
	if d < time.Minute {
		return plural(max(int(d.Round(time.Second)/time.Second), 1), "second")
	}
	return formatTime(d)
}
