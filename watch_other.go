//go:build !darwin

package main

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// watchFolder reports changes under root using fsnotify, which watches one
// folder at a time, so every folder in the tree is added, including new
// ones as they appear. changed is called on another goroutine.
func watchFolder(root string, changed func(paths []string)) (stop func(), err error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	addTree := func(dir string) {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				w.Add(p)
			}
			return nil
		})
	}
	addTree(root)
	go func() {
		for {
			select {
			case e, ok := <-w.Events:
				if !ok {
					return
				}
				if e.Has(fsnotify.Create) {
					if fi, err := os.Stat(e.Name); err == nil && fi.IsDir() {
						addTree(e.Name)
					}
				}
				changed([]string{e.Name})
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() { w.Close() }, nil
}
