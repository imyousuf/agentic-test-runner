//go:build noweb

// Package web holds the live view assets that the binary serves.
package web

import (
	"io/fs"
	"testing/fstest"
)

// Assets returns a placeholder. Build without the noweb tag, after "make web",
// to embed the real application.
func Assets() (fs.FS, error) {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!doctype html><title>ATR</title>" +
				"<p>This binary was built with the noweb tag. Run \"make web\" and rebuild."),
		},
	}, nil
}
