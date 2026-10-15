// Package audiotest makes audio for tests: shaped noise, and FLAC and WAV
// files of exact samples. It also finds the shared sample files.
package audiotest

import (
	"encoding/binary"
	"math"
	"math/cmplx"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// noise synthesises n samples of random-phase noise whose amplitude at
// each frequency is gain(f), scaled so the loudest sample is 0.5. Shaping
// it in the frequency domain gives exact cut-offs, as a lossy encoder would.
func Noise(n int, rate float64, gain func(f float64) float64) []float64 {
	size := 1
	for size < n {
		size <<= 1
	}
	rng := rand.New(rand.NewPCG(1, 2))
	x := make([]complex128, size)
	for k := 1; k < size/2; k++ {
		a := gain(float64(k) * rate / float64(size))
		x[k] = cmplx.Rect(a, rng.Float64()*2*math.Pi)
		x[size-k] = cmplx.Conj(x[k])
	}
	fft(x) // the inverse transform of a real signal's spectrum, up to scale and order
	out := make([]float64, n)
	peak := 0.0
	for i := range out {
		out[i] = real(x[i])
		peak = max(peak, math.Abs(out[i]))
	}
	for i := range out {
		out[i] *= 0.5 / peak
	}
	return out
}

// pink is a rough model of music: level falling 3 dB per octave.
func Pink(f float64) float64 {
	if f < 20 {
		return 0
	}
	return 1 / math.Sqrt(f)
}

// lowpass cuts gain off at hz, falling 60 dB over width Hz as an MP3
// encoder's filter does, then silent.
func Lowpass(gain func(float64) float64, hz, width float64) func(float64) float64 {
	return func(f float64) float64 {
		switch {
		case f <= hz:
			return gain(f)
		case f < hz+width:
			return gain(f) * math.Pow(10, -3*(f-hz)/width)
		}
		return 0
	}
}

// quantize turns s into bits-bit integers with TPDF dither, as converting
// to 16 bits for a CD or FLAC does.
func Quantize(s []float64, bits int) []int32 {
	rng := rand.New(rand.NewPCG(3, 4))
	full := float64(int64(1) << (bits - 1))
	out := make([]int32, len(s))
	for i, v := range s {
		out[i] = int32(math.Round(v*full + rng.Float64() - rng.Float64()))
	}
	return out
}

// writeFLAC encodes channels of samples, which must have equal lengths, as
// a FLAC file with an MD5 checksum.
func WriteFLAC(t testing.TB, path string, rate, bits int, channels ...[]int32) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	n := len(channels[0])
	info := &meta.StreamInfo{
		BlockSizeMin: 4096, BlockSizeMax: 4096,
		SampleRate: uint32(rate), NChannels: uint8(len(channels)), BitsPerSample: uint8(bits),
		NSamples: uint64(n),
	}
	enc, err := flac.NewEncoder(f, info)
	if err != nil {
		t.Fatal(err)
	}
	layout := map[int]frame.Channels{1: frame.ChannelsMono, 2: frame.ChannelsLR}[len(channels)]
	for start := 0; start < n; start += 4096 {
		end := min(start+4096, n)
		fr := &frame.Frame{Header: frame.Header{
			HasFixedBlockSize: true, BlockSize: uint16(end - start),
			SampleRate: uint32(rate), Channels: layout, BitsPerSample: uint8(bits),
		}}
		for _, ch := range channels {
			fr.Subframes = append(fr.Subframes, &frame.Subframe{
				SubHeader: frame.SubHeader{Pred: frame.PredVerbatim},
				Samples:   ch[start:end], NSamples: end - start,
			})
		}
		if err := enc.WriteFrame(fr); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	// The encoder writes STREAMINFO as no real encoder does: its minimum
	// block size is the short last block's, and its maximum frame size is
	// 0 ("unknown"). macOS's decoder refuses such files, so write them as
	// real encoders do: the fixed block size, and a bound on frame size.
	g, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	maxFrame := 16 + 4096*len(channels)*((bits+7)/8)
	g.WriteAt([]byte{0x10, 0x00}, 8) // minimum block size 4096
	g.WriteAt([]byte{byte(maxFrame >> 16), byte(maxFrame >> 8), byte(maxFrame)}, 8+7)
	g.Close()
}

// writeWAV writes integer PCM, each sample stored in sampleBytes bytes with
// the given valid bits (using the extensible header if they differ).
func WriteWAV(t testing.TB, path string, rate, bits, sampleBytes int, channels ...[]int32) {
	t.Helper()
	n, nch := len(channels[0]), len(channels)
	var data []byte
	for i := range n {
		for _, ch := range channels {
			v := uint32(ch[i]) << (8*sampleBytes - bits)
			if sampleBytes == 1 {
				v = uint32(ch[i] + 128)
			}
			for b := range sampleBytes {
				data = append(data, byte(v>>(8*b)))
			}
		}
	}
	le16 := func(v int) []byte { return binary.LittleEndian.AppendUint16(nil, uint16(v)) }
	le32 := func(v int) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(v)) }
	var fmtChunk []byte
	format := 1
	if bits != 8*sampleBytes {
		format = 0xFFFE
	}
	fmtChunk = append(fmtChunk, le16(format)...)
	fmtChunk = append(fmtChunk, le16(nch)...)
	fmtChunk = append(fmtChunk, le32(rate)...)
	fmtChunk = append(fmtChunk, le32(rate*nch*sampleBytes)...)
	fmtChunk = append(fmtChunk, le16(nch*sampleBytes)...)
	fmtChunk = append(fmtChunk, le16(8*sampleBytes)...)
	if format == 0xFFFE {
		fmtChunk = append(fmtChunk, le16(22)...)
		fmtChunk = append(fmtChunk, le16(bits)...)
		fmtChunk = append(fmtChunk, le32(3)...) // front left and right
		fmtChunk = append(fmtChunk, le16(1)...) // PCM sub-format GUID…
		fmtChunk = append(fmtChunk, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xAA, 0, 0x38, 0x9B, 0x71)
	}
	var out []byte
	out = append(out, "RIFF"...)
	out = append(out, le32(4+8+len(fmtChunk)+8+len(data))...)
	out = append(out, "WAVEfmt "...)
	out = append(out, le32(len(fmtChunk))...)
	out = append(out, fmtChunk...)
	out = append(out, "data"...)
	out = append(out, le32(len(data))...)
	out = append(out, data...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func CopyFile(t testing.TB, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(to), 0o755)
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Sample is the path of a file in the project's testdata folder, such as
// "sample.flac", wherever the test runs from.
func Sample(name string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "testdata", name)
}

// fft transforms x in place (radix-2; len(x) must be a power of two).
// Noise uses it, as the inverse transform of a real signal's spectrum is
// the forward one, up to scale and order.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ { // reorder by bit-reversed index
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		step := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			for k := range size / 2 {
				a, b := x[start+k], x[start+k+size/2]*w
				x[start+k], x[start+k+size/2] = a+b, a-b
				w *= step
			}
		}
	}
}
