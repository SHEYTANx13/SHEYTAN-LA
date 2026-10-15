//go:build !windows && !headless

package main

import (
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/desktop"
)

func init() {
	// On Linux and other non-Windows platforms, launch the same native
	// Qt desktop host instead of falling back to CLI-only mode.
	runDefault = func() int {
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			return 1
		}

		return desktop.Run(cfg)
	}
}
