//go:build !android

// main.go — SHEYTAN-LA mobile entrypoint (gomobile-bindable shape).
//
// gomobile bind requirements honored: package main, a func main, and the
// Go-side lifecycle driven by the platform host. The Android backend of
// go-gui v0.97.0 REQUIRES cgo (gui/backend/android imports "C" for its
// GLES3/EGL surface — verified in the module source), so the real device
// path is guarded by `android && cgo` (mobile_android.go) and compiles
// only under a real NDK toolchain (gomobile bind). The CGO_ENABLED=0
// type-check build (sandbox verification) compiles THIS file plus the
// headless core client and the platform adapters, and skips the cgo
// backend — documented in docs/architecture/GO_GUI_MIGRATION.md.
//
// What runs here: the go-gui window + the goguiui shell over a HEADLESS
// core client (the caller supplies the endpoint+key, e.g. the embedded
// core bound through gomobile on device, or a loopback test server).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend"

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

// runMobile is the shared boot: theme → window → shell → loop. The
// android&&cgo variant swaps backend.Run for the
// android.SetWindow/Start/Render pattern (see mobile_android.go).
func runMobile(deps MobileDeps) error {
	providers() // adapters are constructed for the host wiring; typed no-ops until bound

	client := goguiui.NewCoreClient(deps.Endpoint, deps.Key)
	ap := goguiui.NewDesktopApp(client)

	w := gui.NewWindow(gui.WindowCfg{
		State:  apWindowState(ap),
		Title:  "SHEYTAN-LA",
		Width:  390,
		Height: 844,
		OnInit: func(w *gui.Window) { w.SetView(ap.ShellRoot()) },
	})
	backend.Run(w)
	return nil
}

// apWindowState is the small seam the shell exposes for host-shaped
// boots (the desktop App.Run owns the same wiring inline).
func apWindowState(ap *goguiui.App) any { return ap.ShellState() }

func main() {
	endpoint := flag.String("endpoint", os.Getenv("SHEYTAN_IPC_ENDPOINT"), "localipc endpoint")
	key := flag.String("key", os.Getenv("SHEYTAN_IPC_KEY"), "handshake key")
	flag.Parse()

	if err := runMobile(MobileDeps{Endpoint: *endpoint, Key: *key}); err != nil {
		fmt.Fprintln(os.Stderr, "sheytan-mobile:", err)
		log.Fatal(err)
	}
}
