//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const revealLabel = "Show in Explorer"

// reveal opens an Explorer window on the file's folder with the file
// selected. Explorer reads its own command line, which needs /select, and
// the path as one argument.
func reveal(path string) error {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + path + `"`}
	cmd.Run() // Explorer reports failure even when it worked
	return nil
}
