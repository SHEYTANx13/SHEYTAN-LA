//go:build android && cgo

// mobile_android_main.go — the Android device ENTRYPOINT (run
// 44663205295 repair: the Android build previously excluded the only
// func main of the package — main.go carries `//go:build !android` and
// mobile_android.go only defined androidBoot — so the pinned
//
//	gomobile build -target=android/arm64 ... ./cmd/sheytan-mobile
//
// failed with "runtime.main_main·f: function main is undeclared in the
// main package" and no APK could exist).
//
// Lifecycle wiring, verified against the pinned go-gui v0.97.0 module
// source: gui/backend/android is Pattern B ONLY ("Pattern B only (no
// Pattern A / Run)" — backend.go): the HOST drives
// SetWindow / SurfaceCreated / Start / Render, and backend.Run panics on
// android by design. Under `gomobile build` the host is the
// NativeActivity glue of the pinned x/mobile runtime, so THIS entrypoint
// is the bridge: the golang.org/x/mobile app event loop (same exact pin
// recorded in scripts/build/android.sh) drives the go-gui Android
// lifecycle calls:
//
//	lifecycle CrossOn  -> androidBoot (SetWindow) before the loop, then
//	                      android.SurfaceCreated on every RE-created
//	                      context (the GLSurfaceView onSurfaceCreated
//	                      contract: GL resources die with each EGL
//	                      context, the backend rebuilds them);
//	size.Event         -> android.Start(WidthPx, HeightPx, PixelsPerPt);
//	paint.Event        -> android.Render() then a.Publish().
//
// The window content is the SAME Go-Gui shell as the host entrypoint
// (shared newMobileWindow wiring in mobile_window.go) over the headless
// core client — no second UI implementation.
//
// The go.mod require for golang.org/x/mobile is the EXACT pinned
// v0.0.0-20261117202452-47699113848a recorded by the Android lane; the
// runtime inside a gomobile-built APK is built from that same pin.
package main

import (
	"os"

	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"

	android "github.com/go-gui-org/go-gui/gui/backend/android"
)

// mobilePortrait is the logical boot size the window is created with;
// the first real size.Event replaces it with the surface's physical
// pixel size and density before the first frame renders.
const mobilePortrait = 390 // paired with 844 — the go-gui compact class

func main() {
	// The gomobile-build APK has no command line: the endpoint/key come
	// from the environment (the embedded-core wiring exports them on
	// the activity process). No flag machinery is duplicated here — the
	// host entrypoint (main.go) keeps its own.
	deps := MobileDeps{
		Endpoint: os.Getenv("SHEYTAN_IPC_ENDPOINT"),
		Key:      os.Getenv("SHEYTAN_IPC_KEY"),
	}

	w, _ := newMobileWindow(deps, mobilePortrait, 844)
	androidBoot(w) // android.SetWindow — must precede the first Start

	app.Main(func(a app.App) {
		var (
			started bool // android.Start ran at least once
			hasCtx  bool // a live EGL context is current (CrossOn)
		)
		for e := range a.Events() {
			switch e := a.Filter(e).(type) {
			case lifecycle.Event:
				switch e.Crosses(lifecycle.StageVisible) {
				case lifecycle.CrossOn:
					// A live EGL context is current. After the first
					// Start this is a RE-created context: the backend
					// rebuilds every GL resource that died with the
					// old one (SurfaceCreated contract; the first call
					// before Start does nothing by design).
					hasCtx = true
					if started {
						android.SurfaceCreated()
					}
					a.Send(paint.Event{})
				case lifecycle.CrossOff:
					// The context is gone; stop painting until the
					// next CrossOn rebuilds it.
					hasCtx = false
				}
			case size.Event:
				// The surface exists with new dimensions: (re)start the
				// backend at the physical pixel size and density (Start
				// builds the backend on first call, Resize on later
				// ones — backend.go).
				android.Start(e.WidthPx, e.HeightPx, e.PixelsPerPt)
				started = true
			case paint.Event:
				if !hasCtx {
					continue
				}
				android.Render() // one frame: layout + draw + present
				a.Publish()      // swap the buffers
			}
		}
	})
}
