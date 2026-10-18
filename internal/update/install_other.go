//go:build !darwin && !windows && !linux

package update

import "errors"

// Asset is the name of this system's download: there is none.
func Asset() string { return "" }

// location is where Mugig is installed, which can't be updated on this
// system, as Mugig isn't released for it.
func location() (string, error) { return "", errors.New("Mugig isn't released for this system") }

func cantChange(string) error { return errors.ErrUnsupported }

// Install can't install anything on this system.
func Install(pkg, target string) error { return errors.ErrUnsupported }

// Restart can't start anything on this system.
func Restart(target string) error { return errors.ErrUnsupported }
