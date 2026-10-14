//go:build darwin

package trash

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>

char *trashMove(const char *path);
*/
import "C"

import (
	"errors"
	"unsafe"
)

// move uses NSFileManager, as Finder does.
func move(path string) error {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	if msg := C.trashMove(p); msg != nil {
		defer C.free(unsafe.Pointer(msg))
		return errors.New(C.GoString(msg))
	}
	return nil
}
