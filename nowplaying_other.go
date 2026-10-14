//go:build !darwin

package main

import "time"

// systemMedia does nothing outside macOS for now. Linux could use MPRIS
// over D-Bus and Windows the SystemMediaTransportControls.
type systemMedia struct{}

func newSystemMedia() *systemMedia { return &systemMedia{} }

func (*systemMedia) Start(func(remoteCommand, time.Duration)) {}

func (*systemMedia) Update(nowPlayingInfo) {}
