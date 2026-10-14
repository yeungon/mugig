//go:build !darwin && !windows

package main

import (
	"net/url"
	"path/filepath"

	"fyne.io/fyne/v2"
)

const revealLabel = "Open Containing Folder"

// reveal opens the file's folder in the file manager. Selecting the file
// too has no standard way across Linux desktops.
func reveal(path string) error {
	return fyne.CurrentApp().OpenURL(&url.URL{Scheme: "file", Path: filepath.Dir(path)})
}
