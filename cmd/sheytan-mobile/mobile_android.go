//go:build android && cgo

// mobile_android.go — the real Android path. COMPILES ONLY under a real
// NDK toolchain (gomobile bind enables cgo; the sandbox CGO_ENABLED=0
// check skips this file by design). The go-gui v0.97.0 Android backend
// is GLES3/EGL over a GLSurfaceView and REQUIRES cgo (module source
// verified: gui/backend/android/*.go carry `import "C"`).
//
// Boot pattern (backend.Run panics on android by design — the host owns
// the GL lifecycle):
//
//	android.SetWindow(w)   // register the window
//	android.Start(w, h, scale) // from the Android surfaceCreated callback
//	android.Render()       // per frame from the Android render callback
package main

import (
	"github.com/go-gui-org/go-gui/gui"
	android "github.com/go-gui-org/go-gui/gui/backend/android"
)

// androidBoot registers the window for the host-driven GL lifecycle.
// The Kotlin/GLSurfaceView host (packaging owns the gradle wiring) calls
// Start/Render from its surface callbacks.
func androidBoot(w *gui.Window) {
	android.SetWindow(w)
}
