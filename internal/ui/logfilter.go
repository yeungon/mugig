package ui

// The FLAC library writes notes to the standard logger while it decodes,
// such as one for every frame of a 176.4 kHz file saying its own tests
// don't cover that rate. They are for its developers, not a sign of a
// problem: decoding errors come back as errors, and the quality check
// verifies the result. Thousands of them per song would bury anything that
// matters, so they are dropped. Everything else, such as Fyne's errors,
// still shows.

import (
	"bytes"
	"io"
	"log"
	"os"
)

// flacNote marks the FLAC library's messages: it starts them with the
// function's name, and all of them are in its frame package.
var flacNote = []byte("frame.Frame.")

type quietLog struct{ out io.Writer }

// Write takes one log entry, which the logger passes whole.
func (q quietLog) Write(p []byte) (int, error) {
	if bytes.Contains(p, flacNote) {
		return len(p), nil
	}
	return q.out.Write(p)
}

func quietFLACNotes() { log.SetOutput(quietLog{os.Stderr}) }
