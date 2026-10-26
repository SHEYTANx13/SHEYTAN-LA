// Package main is the SHEYTAN-Local-Agent entry point.
//
// With no command, the per-platform entrypoint launches the ONE Fyne
// desktop UI in-process over the Go core's secured local IPC channel
// (v1.11.0 one-Go-UI migration — the Qt/QML host and the React web shell
// are retired). Explicit CLI subcommands continue through cmd.
package main

import (
	"os"

	"github.com/Parsaetak/SHEYTAN-local-agent/cmd"
)

func main() {
	os.Exit(cmd.RunWithDefaultFn(runDefault))
}

// runDefault is overridden by the per-platform entrypoint.
// Both Windows and Linux launch the ONE Fyne desktop UI (internal/desktop).
var runDefault = func() int { return 0 }
