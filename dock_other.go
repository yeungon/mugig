//go:build !darwin

package main

// setDockIcon is only needed on macOS: elsewhere the window's icon is the
// taskbar's.
func setDockIcon([]byte) {}
