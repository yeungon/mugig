//go:build !darwin && !windows

package main

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
)

const revealLabel = "Open Containing Folder"

// reveal asks the desktop's file manager to show the file selected (see
// showItemsArgs). Without one that answers, or without dbus-send, it opens
// the folder instead, through xdg-open.
func reveal(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if exec.CommandContext(ctx, "dbus-send", showItemsArgs(path)...).Run() == nil {
		return nil
	}
	return fyne.CurrentApp().OpenURL(&url.URL{Scheme: "file", Path: filepath.Dir(path)})
}
