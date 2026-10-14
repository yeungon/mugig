//go:build !darwin

package main

import (
	"errors"
	"unsafe"

	"github.com/gopxl/beep/v2"
)

// Other systems have no second FLAC decoder to fall back on.
const appleDecoding = false

type appleFile struct {
	rate     float64
	channels int
	bits     int
	frames   int64
}

var errNoApple = errors.New("no other decoder on this system")

func appleOpen(string, bool) (*appleFile, error)           { return nil, errNoApple }
func (f *appleFile) read(unsafe.Pointer, int) (int, error) { return 0, errNoApple }
func (f *appleFile) close()                                {}

func appleDecode(string) (beep.StreamSeekCloser, beep.Format, error) {
	return nil, beep.Format{}, errNoApple
}
