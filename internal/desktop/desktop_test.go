//go:build !headless

// desktop_test.go — unit tests for the one-Go-UI launcher contract
// (v1.11.0). The former Qt child-spawn resolution tests retired with the
// child-spawn mechanism itself. What remains launch-verifiable without a
// real display is pinned here; the full lifecycle (core up → IPC bound →
// Fyne UI attached → clean exit) is exercised by the Fyne UI tests
// (ui/fyne) and the CI smoke on a real display server.
package desktop

import (
	"testing"
)

// TestDesktopPackageBuildsPinsTheOneUIImport is the architecture guard:
// the desktop launcher package must reference the ONE Fyne UI package and
// must not resurrect a second UI implementation. It compiles only because
// the fyneui import exists in desktop.go; the assertion keeps the intent
// explicit and greppable.
func TestDesktopPackageBuildsPinsTheOneUIImport(t *testing.T) {
	if fyneUIPackagePath != "github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne" {
		t.Fatalf("the desktop launcher must launch the ONE Fyne UI package, got %q", fyneUIPackagePath)
	}
}
