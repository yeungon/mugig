//go:build unix

package update

import (
	"errors"
	"syscall"
)

// running reports whether the process numbered pid is still running.
func running(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
