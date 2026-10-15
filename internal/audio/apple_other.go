//go:build !darwin

package audio

import (
	"errors"
	"unsafe"

	"github.com/gopxl/beep/v2"
)

// Other systems have no second FLAC decoder to fall back on, and nothing
// here plays M4A files there.
const AppleDecoding = false

type AppleFile struct {
	Rate     float64
	Channels int
	Bits     int
	Frames   int64
	Codec    string
}

var errNoApple = errors.New("no other decoder on this system")

func OpenApple(string, bool) (*AppleFile, error)           { return nil, errNoApple }
func (f *AppleFile) Read(unsafe.Pointer, int) (int, error) { return 0, errNoApple }
func (f *AppleFile) Close()                                {}

func appleDecode(string) (beep.StreamSeekCloser, beep.Format, error) {
	return nil, beep.Format{}, errNoApple
}
