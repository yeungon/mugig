//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework MediaPlayer
#include <stdlib.h>

void npStart(void);
void npUpdate(const char *title, const char *artist, const char *album,
	double duration, double elapsed, double rate, int state, int live, const void *art, int artLen);
*/
import "C"

import (
	"time"
	"unsafe"
)

// systemMedia connects playback to the macOS media keys, Control Center and
// lock screen (see nowplaying_darwin.m).
type systemMedia struct{}

// remoteHandler receives media-key commands. Set once by Start.
var remoteHandler func(remoteCommand, time.Duration)

func newSystemMedia() *systemMedia { return &systemMedia{} }

// Start registers for the media keys. Commands are delivered on the UI
// thread. Call it from the main goroutine.
func (*systemMedia) Start(h func(remoteCommand, time.Duration)) {
	remoteHandler = h
	C.npStart()
}

func (*systemMedia) Update(info nowPlayingInfo) {
	title, artist, album := C.CString(info.Title), C.CString(info.Artist), C.CString(info.Album)
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(artist))
	defer C.free(unsafe.Pointer(album))
	var art unsafe.Pointer
	if len(info.Art) > 0 {
		art = unsafe.Pointer(&info.Art[0]) // copied by the C side
	}
	C.npUpdate(title, artist, album,
		C.double(info.Duration.Seconds()), C.double(info.Elapsed.Seconds()),
		C.double(info.Rate), C.int(info.State), C.int(boolInt(info.Live)), art, C.int(len(info.Art)))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

//export goRemoteCommand
func goRemoteCommand(cmd C.int, pos C.double) {
	if h := remoteHandler; h != nil {
		onUI(func() { h(remoteCommand(cmd), seconds(float64(pos))) })
	}
}
