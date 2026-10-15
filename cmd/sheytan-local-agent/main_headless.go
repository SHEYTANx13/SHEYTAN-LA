//go:build headless

package main

import (
	"fmt"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/desktop"
)

func init() {
	// v1.1.3: headless mode (build tag `headless`) runs the SAME runtime
	// stack and HTTP/WebSocket API as the desktop shell, minus the native
	// window. This is what CI, containers, and servers use — and what makes
	// `go build -tags headless ./...` and the headless test suite possible
	// on machines without GTK/WebKit development libraries.
	//
	// Desktop builds (no headless tag) route through main_other.go /
	// main_windows.go and launch the native Qt desktop host instead.
	runDefault = func() int {
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			fmt.Fprintln(stderrWriter, "headless: load config:", err)
			return 1
		}

		return desktop.ServeHeadless(cfg)
	}
}
