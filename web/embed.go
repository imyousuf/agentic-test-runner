//go:build !noweb

// Package web holds the live view assets that the binary serves.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the built web application.
func Assets() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
