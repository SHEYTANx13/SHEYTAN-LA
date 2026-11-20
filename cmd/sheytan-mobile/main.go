//go:build !android

// main.go — SHEYTAN-LA mobile entrypoint (gomobile-bindable shape).
//
// gomobile bind requirements honored: package main, a func main, and the
// Go-side lifecycle driven by the platform host. The Android backend of
// go-gui v0.97.0 REQUIRES cgo (gui/backend/android imports "C" for its
// GLES3/EGL surface — verified in the module source), so the real device
// path is guarded by `android && cgo` (mobile_android_main.go — the
// Android ENTRYPOINT added by the run 44663205295 repair; the previous
// build excluded every func main on the android target) and compiles
// only under a real NDK toolchain (gomobile build). The CGO_ENABLED=0
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

	"github.com/go-gui-org/go-gui/gui/backend"
)

// runMobile is the shared boot: theme → window → shell → loop. The
// android&&cgo variant swaps backend.Run for the
// android.SetWindow/Start/Render pattern (see mobile_android_main.go,
// the Android entrypoint that drives the go-gui Android lifecycle from
// the x/mobile app events).
func runMobile(deps MobileDeps) error {
	w, _ := newMobileWindow(deps, 390, 844)
	backend.Run(w)
	return nil
}

func main() {
	endpoint := flag.String("endpoint", os.Getenv("SHEYTAN_IPC_ENDPOINT"), "localipc endpoint")
	key := flag.String("key", os.Getenv("SHEYTAN_IPC_KEY"), "handshake key")
	flag.Parse()

	if err := runMobile(MobileDeps{Endpoint: *endpoint, Key: *key}); err != nil {
		fmt.Fprintln(os.Stderr, "sheytan-mobile:", err)
		log.Fatal(err)
	}
}
