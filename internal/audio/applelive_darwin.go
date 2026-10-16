//go:build darwin

package audio

/*
#cgo LDFLAGS: -framework AudioToolbox
void *liveOpen(unsigned int hint);
int liveFeed(void *h, const void *data, int n);
int liveFormat(void *h, double *rate, int *channels);
int liveDecode(void *h, float *out, int frames);
void liveClose(void *h);
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"unsafe"

	"mugig/internal/radio"

	"github.com/gopxl/beep/v2"
)

// The kinds of stream macOS's parser is told to expect: its
// kAudioFileAAC_ADTSType and kAudioFileMP3Type.
const (
	hintADTS = 'a'<<24 | 'd'<<16 | 't'<<8 | 's'
	hintMP3  = 'M'<<24 | 'P'<<16 | 'G'<<8 | '3'
)

// maxProbe is how much of a stream is read looking for its format.
const maxProbe = 1 << 20

// appleLive decodes a stream of MP3 or ADTS AAC as it arrives, with
// macOS's decoders, which also play HE-AAC as many stations send it.
type appleLive struct {
	h   unsafe.Pointer
	src io.Reader
	ch  int
	in  []byte
	buf []float32
	pos int
	err error
}

func appleLiveDecode(src io.Reader, codec radio.Codec) (beep.StreamSeekCloser, beep.Format, error) {
	hint := hintMP3
	if codec == radio.AAC {
		hint = hintADTS
	}
	h := C.liveOpen(C.uint(hint))
	if h == nil {
		return nil, beep.Format{}, errors.New("macOS's decoder couldn't start")
	}
	a := &appleLive{h: h, src: src, in: make([]byte, 8192)}
	var rate C.double
	var ch C.int
	for read := 0; C.liveFormat(h, &rate, &ch) == 0; {
		if read > maxProbe {
			a.Close()
			return nil, beep.Format{}, errors.New("no sound was found in the station's stream")
		}
		n, err := src.Read(a.in)
		read += n
		if n > 0 && C.liveFeed(h, unsafe.Pointer(&a.in[0]), C.int(n)) != 0 {
			a.Close()
			return nil, beep.Format{}, fmt.Errorf("macOS can't decode the station's %s", codec)
		}
		if err != nil {
			a.Close()
			return nil, beep.Format{}, err
		}
	}
	a.ch = int(ch)
	return a, beep.Format{SampleRate: beep.SampleRate(rate), NumChannels: a.ch, Precision: 4}, nil
}

// Stream decodes, reading more of the stream whenever the decoder runs
// out, until samples is full or the stream fails or ends.
func (a *appleLive) Stream(samples [][2]float64) (int, bool) {
	if cap(a.buf) < len(samples)*a.ch {
		a.buf = make([]float32, len(samples)*a.ch)
	}
	filled := 0
	for filled < len(samples) && a.err == nil {
		n := int(C.liveDecode(a.h, (*C.float)(unsafe.Pointer(&a.buf[0])), C.int(len(samples)-filled)))
		switch {
		case n < 0:
			a.err = errors.New("macOS's decoder couldn't decode the station's sound")
		case n > 0:
			for i := range n {
				l := float64(a.buf[i*a.ch])
				r := l
				if a.ch > 1 {
					r = float64(a.buf[i*a.ch+1])
				}
				samples[filled+i] = [2]float64{l, r}
			}
			filled += n
		default:
			k, err := a.src.Read(a.in)
			if k > 0 && C.liveFeed(a.h, unsafe.Pointer(&a.in[0]), C.int(k)) != 0 {
				err = errors.New("the station's stream is damaged")
			}
			if err != nil {
				a.err = err
			}
		}
	}
	a.pos += filled
	return filled, filled > 0 || a.err == nil
}

func (a *appleLive) Err() error {
	if a.err == io.EOF {
		return nil
	}
	return a.err
}

func (a *appleLive) Len() int       { return 0 }
func (a *appleLive) Position() int  { return a.pos }
func (a *appleLive) Seek(int) error { return errLiveSeek }
func (a *appleLive) Close() error {
	if a.h != nil {
		C.liveClose(a.h)
		a.h = nil
	}
	return nil
}
