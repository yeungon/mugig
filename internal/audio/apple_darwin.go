//go:build darwin

package audio

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AudioToolbox -framework Foundation
#include <stdlib.h>

void *appleOpen(const char *path, int asFloat, double *rate, int *channels, int *bits, long long *frames);
int appleRead(void *h, void *buf, int frames, int channels);
int appleSeek(void *h, long long frame);
void appleClose(void *h);
*/
import "C"

import (
	"errors"
	"unsafe"

	"github.com/gopxl/beep/v2"
)

// macOS's own decoders read the occasional FLAC file that Mugig's FLAC
// library rejects (see verifyFLAC), so those still play and can be checked.
const AppleDecoding = true

// AppleFile is a file open in macOS's decoder.
type AppleFile struct {
	h        unsafe.Pointer
	Rate     float64
	Channels int
	Bits     int
	Frames   int64
}

func OpenApple(path string, asFloat bool) (*AppleFile, error) {
	c := C.CString(path)
	defer C.free(unsafe.Pointer(c))
	var rate C.double
	var channels, bits C.int
	var frames C.longlong
	fl := C.int(0)
	if asFloat {
		fl = 1
	}
	h := C.appleOpen(c, fl, &rate, &channels, &bits, &frames)
	if h == nil {
		return nil, errors.New("macOS can't decode it either")
	}
	return &AppleFile{h: h, Rate: float64(rate), Channels: int(channels), Bits: int(bits), Frames: int64(frames)}, nil
}

// Read fills buf with interleaved samples (float32 or int32, as opened)
// and returns the frames read, 0 at the end.
func (f *AppleFile) Read(buf unsafe.Pointer, frames int) (int, error) {
	n := int(C.appleRead(f.h, buf, C.int(frames), C.int(f.Channels)))
	if n < 0 {
		return 0, errors.New("macOS's decoder failed partway")
	}
	return n, nil
}

func (f *AppleFile) Close() { C.appleClose(f.h) }

// appleStream plays a file through macOS's decoder.
type appleStream struct {
	f   *AppleFile
	buf []float32
	pos int
	err error
}

func appleDecode(path string) (beep.StreamSeekCloser, beep.Format, error) {
	f, err := OpenApple(path, true)
	if err != nil {
		return nil, beep.Format{}, err
	}
	format := beep.Format{SampleRate: beep.SampleRate(f.Rate), NumChannels: f.Channels, Precision: 4}
	return &appleStream{f: f}, format, nil
}

func (s *appleStream) Stream(samples [][2]float64) (int, bool) {
	ch := s.f.Channels
	if cap(s.buf) < len(samples)*ch {
		s.buf = make([]float32, len(samples)*ch)
	}
	n, err := s.f.Read(unsafe.Pointer(&s.buf[0]), len(samples))
	if err != nil {
		s.err = err
		return 0, false
	}
	for i := range n {
		l := float64(s.buf[i*ch])
		r := l
		if ch > 1 {
			r = float64(s.buf[i*ch+1])
		}
		samples[i] = [2]float64{l, r}
	}
	s.pos += n
	return n, n > 0
}

func (s *appleStream) Err() error    { return s.err }
func (s *appleStream) Len() int      { return int(s.f.Frames) }
func (s *appleStream) Position() int { return s.pos }
func (s *appleStream) Close() error  { s.f.Close(); return nil }

func (s *appleStream) Seek(p int) error {
	if C.appleSeek(s.f.h, C.longlong(p)) != 0 {
		return errors.New("seek failed")
	}
	s.pos = p
	return nil
}
