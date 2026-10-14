//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework CoreAudio -framework Foundation
#include <stdlib.h>

double caNominalRate(const char *uid);
int caSetNominalRate(const char *uid, double rate);
void caWatchDevices(void);
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

// devicesChanged is called on the UI thread when an output device is added
// or removed, or the default output changes. Set by watchDevices.
var devicesChanged func()

func watchDevices(f func()) {
	devicesChanged = f
	C.caWatchDevices()
}

//export goAudioDevicesChanged
func goAudioDevicesChanged() {
	if f := devicesChanged; f != nil {
		onUI(f)
	}
}
