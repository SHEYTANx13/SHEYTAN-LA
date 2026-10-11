// Package web holds the embedded React frontend assets that ship inside
// the SHEYTAN Local Agent executable.
package web

import "embed"

// StaticFS is the embedded production frontend.
//
// The frontend build is copied into web/static before the Go desktop binary
// is compiled. Vite's generated index.html, JavaScript, CSS, and assets are
// therefore packaged directly into the executable.
//
//go:embed all:static
var StaticFS embed.FS
