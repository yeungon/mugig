// Mugig is a desktop music player that also checks your files: whether a
// "lossless" FLAC really is lossless, whether it is intact, and how loud
// it is.
//
// The code is in internal/: ui is the window, library finds the songs,
// audio plays them, and quality checks them.
package main

import (
	_ "embed"

	"mugig/internal/ui"
	"mugig/internal/update"
)

// The app's icon, rounded, for the window, the taskbar and the sidebar.
// The macOS app bundle's icon is made from Icon.png instead.
//
//go:embed IconRounded.png
var icon []byte

func main() {
	update.AfterRestart() // after updating itself, wait for the old copy to quit
	ui.Run(icon)
}
