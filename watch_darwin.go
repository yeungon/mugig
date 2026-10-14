//go:build darwin

package main

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsevents"
)

// watchFolder reports changes under root using macOS's FSEvents, which
// follows a whole folder tree as one stream. (fsnotify's kqueue backend
// needs a file handle for every file and folder, which a big library can
// run out of.) changed is called on another goroutine with the paths that
// changed; a folder means "look through this folder again".
func watchFolder(root string, changed func(paths []string)) (stop func(), err error) {
	// FSEvents reports real paths, e.g. /private/var for /var, so map them
	// back onto root as the library knows it.
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	es := &fsevents.EventStream{
		Paths:   []string{root},
		Latency: time.Second, // FSEvents coalesces what happens within this
		Flags:   fsevents.FileEvents,
	}
	if err := es.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case events, ok := <-es.Events:
				if !ok {
					return
				}
				paths := make([]string, 0, len(events))
				for _, e := range events {
					p := "/" + strings.TrimPrefix(e.Path, "/")
					if rest, ok := strings.CutPrefix(p, real); ok && real != root {
						p = root + rest
					}
					paths = append(paths, p)
				}
				changed(paths)
			case <-done:
				return
			}
		}
	}()
	return func() {
		es.Stop()
		close(done)
	}, nil
}
