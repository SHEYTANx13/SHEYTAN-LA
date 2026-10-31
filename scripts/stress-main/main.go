// stress-main — headless entry point for the stress suite.
//
// The root main package wires runDefault to the native Qt desktop shell
// (internal/desktop), which links the desktop launch path. This tiny main
// wires a no-op default instead, so the stress binary builds and runs on
// any host without the desktop surface:
//
//	go build -o /tmp/sheytan-stress ./scripts/stress-main
//	SHEYTAN_DATA_DIR=/tmp/sheytan-stress-root /tmp/sheytan-stress stress
package main

import (
	"os"

	"github.com/SHEYTANx13/SHEYTAN-LA/cmd"
)

func main() {
	os.Exit(cmd.RunWithDefaultFn(func() int { return 0 }))
}
