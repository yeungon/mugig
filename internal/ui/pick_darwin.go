//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

void pickShow(uintptr_t window, int id, bool files, bool folders, bool multiple,
	const char *message, const char *start);
*/
import "C"

import (
	"strings"
	"sync"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// The macOS Open panel (see pick_darwin.m) lists disks, USB drives and
// network shares under Locations, as in every Mac app. Fyne's own dialog
// only offers "Computer", so disks are hard to find there.

var (
	pickMu   sync.Mutex
	pickNext C.int
	pickDone = map[C.int]func([]string){}
)

// nativePicker shows the Open panel as a sheet on w.
func nativePicker(w fyne.Window, opts pickOptions, done func([]string)) {
	var window uintptr
	if nw, ok := w.(driver.NativeWindow); ok {
		nw.RunNative(func(ctx any) {
			if mac, ok := ctx.(driver.MacWindowContext); ok {
				window = mac.NSWindow
			}
		})
	}
	pickMu.Lock()
	pickNext++
	id := pickNext
	pickDone[id] = done
	pickMu.Unlock()

	message, start := C.CString(opts.title), C.CString(opts.start)
	defer C.free(unsafe.Pointer(message))
	defer C.free(unsafe.Pointer(start))
	C.pickShow(C.uintptr_t(window), id, C.bool(opts.files), C.bool(opts.folders), C.bool(opts.multiple), message, start)
}

//export goPickDone
func goPickDone(id C.int, paths *C.char) {
	pickMu.Lock()
	done := pickDone[id]
	delete(pickDone, id)
	pickMu.Unlock()
	var chosen []string
	if paths != nil {
		chosen = strings.Split(C.GoString(paths), "\n")
	}
	if done != nil {
		onUI(func() { done(chosen) })
	}
}

var defaultPicker picker = nativePicker
