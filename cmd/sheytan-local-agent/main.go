// Package main is the SHEYTAN-Local-Agent entry point.
//
// With no command, the per-platform entrypoint launches the native Qt
// desktop host (Go core + secured local IPC; v1.10.1). Explicit CLI
// subcommands continue through cmd.
package main

import (
	"os"

	"github.com/Parsaetak/SHEYTAN-local-agent/cmd"
)

func main() {
	os.Exit(cmd.RunWithDefaultFn(runDefault))
}

// runDefault is overridden by the per-platform entrypoint.
// Both Windows and Linux launch the native Qt desktop host.
var runDefault = func() int { return 0 }
