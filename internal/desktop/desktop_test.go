//go:build !headless

// desktop_test.go — unit tests for the desktop launcher contract. The
// Fyne-era one-UI import guard retired with the Fyne shell itself; what
// remains launch-verifiable without a real display is pinned here: the
// shell selection contract and the ONE shell package pin. The full
// lifecycle (core up → IPC bound → web host window → clean exit) is
// exercised by the CI smoke on a real display server.
package desktop

import (
	"errors"
	"strings"
	"testing"
)

// TestDesktopPinsTheOneShellPackage is the architecture guard: the
// desktop launcher renders the ONE adaptive web shell; no second shell
// implementation may return.
func TestDesktopPinsTheOneShellPackage(t *testing.T) {
	if webShellPackagePath != "github.com/SHEYTANx13/SHEYTAN-LA/internal/webshell" {
		t.Fatalf("the desktop launcher must render the ONE web shell package, got %q", webShellPackagePath)
	}
}

// TestResolveUIMode pins the explicit, observable selection contract:
// auto and web resolve to the web host, an unknown value is a typed
// error, and the retired fyne name fails LOUDLY with the retirement
// error instead of silently picking another shell.
func TestResolveUIMode(t *testing.T) {
	for _, accepted := range []string{"", "auto", "AUTO", " web "} {
		mode, err := resolveUIModeEnv(accepted)
		if err != nil {
			t.Fatalf("resolveUIMode(%q) = %q, %v — want the web mode", accepted, mode, err)
		}
		if mode != uiModeAuto && mode != uiModeWeb {
			t.Fatalf("resolveUIMode(%q) = %q, want auto or web", accepted, mode)
		}
	}

	if _, err := resolveUIModeEnv("definitely-not-a-shell"); err == nil {
		t.Fatalf("an unknown SHEYTAN_UI value must be a typed error")
	}

	_, err := resolveUIModeEnv("fyne")
	if !errors.Is(err, ErrFyneRetired) {
		t.Fatalf("SHEYTAN_UI=fyne must answer the typed retirement error, got %v", err)
	}
	if !strings.Contains(err.Error(), "docs/retired.md") {
		t.Fatalf("the retirement error must point at docs/retired.md, got %q", err.Error())
	}
}
