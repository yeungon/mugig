//go:build !darwin

package main

// Other systems leave the device's rate alone; miniaudio converts when the
// device runs at another rate.

func nominalRate(string) float64          { return 0 }
func setNominalRate(string, float64) bool { return false }
func watchDevices(func())                 {}
