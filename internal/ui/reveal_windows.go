//go:build windows

package ui

import (
	"os/exec"
	"syscall"
)

const revealLabel = "Show in Explorer"

// reveal opens an Explorer window on the file's folder with the file
// selected (see explorerCmdLine).
func reveal(path string) error {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: explorerCmdLine(path)}
	cmd.Run() // Explorer exits with status 1 even when it worked
	return nil
}
