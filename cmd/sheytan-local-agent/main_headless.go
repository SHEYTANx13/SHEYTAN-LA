//go:build headless

package main

import (
	"fmt"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/desktop"
)

func init() {
	// v1.1.3: headless mode (build tag `headless`) runs the SAME runtime
	// stack and HTTP/WebSocket API as the desktop shell, minus the native
	// window. This is what CI, containers, and servers use — and what makes
	// `go build -tags headless ./...` and the headless test suite possible
	// on machines without GPU/windowing development libraries.
	//
	// Desktop builds (no headless tag) route through main_other.go /
	// main_windows.go and launch the ONE Fyne UI over the core's secured
	// local IPC channel (v1.11.0 one-Go-UI migration).
	runDefault = func() int {
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			fmt.Fprintln(stderrWriter, "headless: load config:", err)
			return 1
		}

		return desktop.ServeHeadless(cfg)
	}
}
