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
void saveShow(uintptr_t window, int id, const char *name, const char *type);
*/
import "C"

import (
	"path/filepath"
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

// nsWindow is w's NSWindow, for a panel to show as a sheet on, or 0.
func nsWindow(w fyne.Window) uintptr {
	var window uintptr
	if nw, ok := w.(driver.NativeWindow); ok {
		nw.RunNative(func(ctx any) {
			if mac, ok := ctx.(driver.MacWindowContext); ok {
				window = mac.NSWindow
			}
		})
	}
	return window
}

// awaitPanel files done under a new ID, for goPickDone to call.
func awaitPanel(done func([]string)) C.int {
	pickMu.Lock()
	defer pickMu.Unlock()
	pickNext++
	pickDone[pickNext] = done
	return pickNext
}

// nativePicker shows the Open panel as a sheet on w.
func nativePicker(w fyne.Window, opts pickOptions, done func([]string)) {
	window, id := nsWindow(w), awaitPanel(done)

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

// nativeSaver shows the Save panel as a sheet on w, for a file named
// name, of the type its extension gives.
func nativeSaver(w fyne.Window, name string, done func(string)) {
	window := nsWindow(w)
	id := awaitPanel(func(paths []string) {
		if len(paths) == 0 {
			done("")
			return
		}
		done(paths[0])
	})
	cname := C.CString(name)
	ctype := C.CString(strings.TrimPrefix(filepath.Ext(name), "."))
	defer C.free(unsafe.Pointer(cname))
	defer C.free(unsafe.Pointer(ctype))
	C.saveShow(C.uintptr_t(window), id, cname, ctype)
}

var (
	defaultPicker picker = nativePicker
	defaultSaver  saver  = nativeSaver
)
