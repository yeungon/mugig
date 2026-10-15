//go:build !darwin

package ui

var (
	defaultPicker picker = fynePicker
	defaultSaver  saver  = fyneSaver
)
