//go:build windows

package trash

import (
	"fmt"
	"syscall"
	"unsafe"
)

// shFileOpStruct is SHFILEOPSTRUCTW as laid out on 64-bit Windows (32-bit
// Windows packs it differently).
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 0x3
	fofSilent         = 0x4
	fofNoConfirmation = 0x10
	fofAllowUndo      = 0x40 // to the Recycle Bin, not deleted
	fofNoErrorUI      = 0x400
)

var shFileOperation = syscall.NewLazyDLL("shell32.dll").NewProc("SHFileOperationW")

// move uses the shell's delete with undo, which sends the file to the
// Recycle Bin.
func move(path string) error {
	// pFrom is a list of names, each ended by a NUL, and the list by another.
	from, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0)
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}
	r, _, _ := shFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	switch {
	case r != 0:
		return fmt.Errorf("the Recycle Bin refused it (error %#x)", r)
	case op.fAnyOperationsAborted != 0:
		return fmt.Errorf("moving it to the Recycle Bin was cancelled")
	}
	return nil
}
