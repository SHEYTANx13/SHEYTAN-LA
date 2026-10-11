// stress-main — headless entry point for the stress suite.
//
// The root main package wires runDefault to the Wails desktop shell
// (internal/desktop), which on Linux drags in GTK/WebKit cgo headers. This
// tiny main wires a no-op default instead, so the stress binary builds and
// runs on any host:
//
//	go build -o /tmp/sheytan-stress ./scripts/stress-main
//	SHEYTAN_DATA_DIR=/tmp/sheytan-stress-root /tmp/sheytan-stress stress
package main

import (
	"os"

	"github.com/Parsaetak/SHEYTAN-local-agent/cmd"
)

func main() {
	os.Exit(cmd.RunWithDefaultFn(func() int { return 0 }))
}
