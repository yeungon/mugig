//go:build !darwin

package main

// Other systems leave the device's rate alone; miniaudio converts when the
// device runs at another rate. Exclusive access is macOS only.

func nominalRate(string) float64          { return 0 }
func setNominalRate(string, float64) bool { return false }
func watchDevices(func())                 {}

const canHogDevice = false

func hogDevice(string) uint32 { return 0 }
func unhogDevice(uint32)      {}
func hogOwner(uint32) int     { return -1 }
