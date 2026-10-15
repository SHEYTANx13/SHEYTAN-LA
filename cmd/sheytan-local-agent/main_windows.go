//go:build windows && !headless

package main

import (
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/desktop"
)

func init() {
	// On Windows with no subcommand, launch the native Qt desktop host.
	runDefault = func() int {
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			return 1
		}

		return desktop.Run(cfg)
	}
}
