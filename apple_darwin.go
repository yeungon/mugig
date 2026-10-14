//go:build darwin

package main

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
const appleDecoding = true

// appleFile is a file open in macOS's decoder.
type appleFile struct {
	h        unsafe.Pointer
	rate     float64
	channels int
	bits     int
	frames   int64
}

func appleOpen(path string, asFloat bool) (*appleFile, error) {
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
	return &appleFile{h: h, rate: float64(rate), channels: int(channels), bits: int(bits), frames: int64(frames)}, nil
}

// read fills buf with interleaved samples (float32 or int32, as opened)
// and returns the frames read, 0 at the end.
func (f *appleFile) read(buf unsafe.Pointer, frames int) (int, error) {
	n := int(C.appleRead(f.h, buf, C.int(frames), C.int(f.channels)))
	if n < 0 {
		return 0, errors.New("macOS's decoder failed partway")
	}
	return n, nil
}

func (f *appleFile) close() { C.appleClose(f.h) }

// appleStream plays a file through macOS's decoder.
type appleStream struct {
	f   *appleFile
	buf []float32
	pos int
	err error
}

func appleDecode(path string) (beep.StreamSeekCloser, beep.Format, error) {
	f, err := appleOpen(path, true)
	if err != nil {
		return nil, beep.Format{}, err
	}
	format := beep.Format{SampleRate: beep.SampleRate(f.rate), NumChannels: f.channels, Precision: 4}
	return &appleStream{f: f}, format, nil
}

func (s *appleStream) Stream(samples [][2]float64) (int, bool) {
	ch := s.f.channels
	if cap(s.buf) < len(samples)*ch {
		s.buf = make([]float32, len(samples)*ch)
	}
	n, err := s.f.read(unsafe.Pointer(&s.buf[0]), len(samples))
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
func (s *appleStream) Len() int      { return int(s.f.frames) }
func (s *appleStream) Position() int { return s.pos }
func (s *appleStream) Close() error  { s.f.close(); return nil }

func (s *appleStream) Seek(p int) error {
	if C.appleSeek(s.f.h, C.longlong(p)) != 0 {
		return errors.New("seek failed")
	}
	s.pos = p
	return nil
}
