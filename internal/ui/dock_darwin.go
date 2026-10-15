//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>

void dockSetIcon(const void *png, int length);
*/
import "C"

// setDockIcon shows the icon in the Dock when Mugig runs outside its app
// bundle, as with `make run`; macOS would otherwise show a generic one. In
// the bundle, the Dock already has the bundle's icon. png is the rounded
// icon, which the Dock is given at Apple's size within the frame.
func setDockIcon(png []byte) {
	p := C.CBytes(png)
	defer C.free(p)
	C.dockSetIcon(p, C.int(len(png)))
}
