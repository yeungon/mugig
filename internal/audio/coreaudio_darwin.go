//go:build darwin

package audio

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework CoreAudio -framework Foundation
#include <stdlib.h>

double caNominalRate(const char *uid);
int caSetNominalRate(const char *uid, double rate);
void caWatchDevices(void);
unsigned int caHog(const char *uid);
void caUnhog(unsigned int dev);
int caHogOwner(unsigned int dev);
*/
import "C"

import (
	"unsafe"
)

// The output device's own sample rate ("nominal" rate, as shown in Audio
// MIDI Setup). Switching it to the track's rate means nothing between the
// file and the device converts the audio.

// nominalRate returns the rate of the device with uid, or the default
// output device for "". 0 if unknown.
func nominalRate(uid string) float64 {
	c := C.CString(uid)
	defer C.free(unsafe.Pointer(c))
	return float64(C.caNominalRate(c))
}

// setNominalRate switches the device to rate, if it supports it, and
// reports whether it now runs at rate.
func setNominalRate(uid string, rate float64) bool {
	c := C.CString(uid)
	defer C.free(unsafe.Pointer(c))
	return C.caSetNominalRate(c, C.double(rate)) == 0
}

// Exclusive access ("hog mode"): while Mugig has the device to itself, no
// other app's sound, nor the system's alerts, can mix into the music or
// change the device's rate. The system gives the device back if Mugig
// quits or crashes.

const CanHogDevice = true

// hogDevice takes the device with uid, or the default output for "", for
// Mugig alone. It returns the device's ID, or 0 if another app has it or it
// can't be had.
func hogDevice(uid string) uint32 {
	c := C.CString(uid)
	defer C.free(unsafe.Pointer(c))
	return uint32(C.caHog(c))
}

// unhogDevice gives the device back to every app.
func unhogDevice(dev uint32) { C.caUnhog(C.uint(dev)) }

// hogOwner is the process ID of the app with dev to itself, or -1.
func hogOwner(dev uint32) int { return int(C.caHogOwner(C.uint(dev))) }

// devicesChanged is called, on a CoreAudio thread, when an output device is
// added or removed, or the default output changes. Set by WatchDevices.
var devicesChanged func()

func WatchDevices(f func()) {
	devicesChanged = f
	C.caWatchDevices()
}

//export goAudioDevicesChanged
func goAudioDevicesChanged() {
	if f := devicesChanged; f != nil {
		f()
	}
}
