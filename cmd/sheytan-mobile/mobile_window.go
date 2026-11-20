//go:build !ios

// mobile_window.go — the mobile window construction shared by the host
// entrypoint (main.go, !android) and the Android device entrypoint
// (mobile_android_main.go, android && cgo). ONE source of truth for the
// mobile boot wiring: platform adapters, headless core client, and the
// go-gui window rooted on the shell at first frame.
//
// Build-constraint map of this package (run 44663205295 repair):
//
//	main.go                  !android        func main (host/desktop shape)
//	mobile_android_main.go   android && cgo  func main (device shape)
//	mobile_android_nocgo.go  android && !cgo func main (typed refusal)
//	mobile_android.go        android && cgo  androidBoot (SetWindow seam)
//	mobile_window.go         !ios            shared wiring (this file)
//
// Exactly ONE func main applies per GOOS/build-tag intersection — no
// duplicate-main build conflicts — and the iOS targets keep the
// gomobile-bind library shape (no func main of this package: the iOS
// lanes bind or package the agent app, never this shell).
package main

import (
	"github.com/go-gui-org/go-gui/gui"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/platform"
	goguiui "github.com/SHEYTANx13/SHEYTAN-LA/ui/gogui"
)

// MobileDeps carries the platform wiring the host supplies.
type MobileDeps struct {
	Endpoint string // localipc endpoint string (core side)
	Key      string // ephemeral handshake key
}

// providers returns the mobile platform adapter bundle. Desktop-grade
// capabilities that have no mobile implementation yet report the typed
// ErrUnsupported (honest), the clipboard/power seams mirror the go-gui
// window, and lifecycle starts as the storing no-op.
func providers() platform.Providers {
	return platform.DefaultProviders().WithDefaults()
}

// apWindowState is the small seam the shell exposes for host-shaped
// boots (the desktop App.Run owns the same wiring inline).
func apWindowState(ap *goguiui.App) any { return ap.ShellState() }

// newMobileWindow constructs the shared mobile boot: platform adapters,
// headless CoreClient, DesktopApp shell, and the go-gui window whose
// OnInit roots the shell view before the first frame. The caller owns
// the run loop (backend.Run on the host; the android && cgo entrypoint
// drives the go-gui Android lifecycle from the x/mobile app events).
func newMobileWindow(deps MobileDeps, width, height int) (*gui.Window, *goguiui.App) {
	providers() // adapters are constructed for the host wiring; typed no-ops until bound

	client := goguiui.NewCoreClient(deps.Endpoint, deps.Key)
	ap := goguiui.NewDesktopApp(client)

	w := gui.NewWindow(gui.WindowCfg{
		State:  apWindowState(ap),
		Title:  "SHEYTAN-LA",
		Width:  width,
		Height: height,
		OnInit: func(w *gui.Window) { w.SetView(ap.ShellRoot()) },
	})

	return w, ap
}
