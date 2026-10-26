//go:build windows && !headless

package main

import (
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/desktop"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

func init() {
	// On Windows with no subcommand, launch the ONE Fyne desktop UI
	// over the Go core's secured local IPC channel (v1.11.0 one-Go-UI
	// migration — internal/desktop.Run owns core → IPC → Fyne order).
	runDefault = func() int {
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			// v1.10.14 (run #36 class): this failure used to return 1
			// with NO logging at all — one of the invisible boot-exit
			// classes. By the time the default path runs, the canonical
			// manager is installed (cmd/root.go opens the sinks before
			// dispatch), so the failure now reaches app.log as a named
			// ERROR line; under -H=windowsgui nothing else would.
			// Control flow unchanged: still returns 1.
			logging.Default().Error("boot", "config load failed: %v", err)
			return 1
		}

		return desktop.Run(cfg)
	}
}
