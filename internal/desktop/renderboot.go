//go:build !nogogpu

package desktop

// Default render-verification wiring (run 44547031698 §3.2): the build
// links the gogpu/goffi stack, so the render service starts in the
// background and publishes its observations to the toolkit-neutral
// channel (internal/renderstate). This is the DEFAULT profile - macOS and
// Windows desktop binaries link goffi beside other cgo code without the
// documented Linux-only link failure (goffi v0.6.8 README, issue #72:
// macOS and Windows are not affected).

import "github.com/SHEYTANx13/SHEYTAN-LA/internal/renderboot"

// startRenderVerification launches the background render-service
// verification (never on the first-frame path).
func startRenderVerification() {
	renderboot.Start()
}
