//go:build darwin

package main

import "os/exec"

const revealLabel = "Show in Finder"

// reveal opens a Finder window on the file's folder with the file selected.
func reveal(path string) error {
	return exec.Command("open", "-R", path).Run()
}
