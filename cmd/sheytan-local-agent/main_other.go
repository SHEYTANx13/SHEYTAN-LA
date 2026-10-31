//go:build !windows && !headless

package main

import (
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/desktop"
)

func init() {
	// On Linux and other non-Windows platforms with no subcommand,
	// launch the ONE Fyne desktop UI over the Go core's secured local
	// IPC channel (v1.11.0 one-Go-UI migration — internal/desktop.Run
	// owns the core → IPC → Fyne attach order).
	runDefault = func() int {
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			return 1
		}

		return desktop.Run(cfg)
	}
}
