//go:build nogogpu

package desktop

// nogogpu render-verification wiring (run 44547031698 §3.2): this build
// profile EXCLUDES the gogpu/goffi stack because the pinned goffi v0.6.8
// documentation documents an unresolvable-at-our-layer link failure on
// Linux when goffi is combined with other cgo code (fyne/go-gl, purego)
// in one binary - "unhandled relocation ... (SDYNIMPORT ...)", goffi
// issue #72, with the CGO_ENABLED=0 nofakecgo route reserved for
// cgo-free targets. The Linux desktop lane builds with -tags nogogpu and
// the CPU/Go-Gui rendering path; the combined gogpu link regression proof
// stays on the macOS/Windows lanes, where upstream documents the
// combination as supported.
//
// Honesty contract: the observation channel carries the typed not-wired
// state - the UIs show the real availability reason instead of pretending
// a GPU verification ran (work order §10.1: GoGPU is optional and must
// never block shell/core startup).

import "github.com/SHEYTANx13/SHEYTAN-LA/internal/renderstate"

// startRenderVerification publishes the honest not-wired observation.
// No render service exists in this build profile; nothing blocks boot.
func startRenderVerification() {
	renderstate.Publish(renderstate.Snapshot{
		State:          "unavailable",
		Backend:        "cpu",
		FallbackReason: "gogpu adapter not linked in this build profile (-tags nogogpu)",
		BuildNote: "goffi v0.6.8 issue 72: the gogpu/goffi stack cannot link beside other cgo code on linux; " +
			"the desktop build profile uses the CPU/Go-Gui path and the combined-link proof stays on the macOS/Windows lanes",
	})
}
