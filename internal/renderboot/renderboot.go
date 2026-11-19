// Package renderboot runs the render service's startup verification OFF
// the first-frame path (run 44457547289 §5 + §13.8: the UI first frame is
// never blocked by device probes): discover in the background, execute
// ONE deterministic verification job through the real gogpu stack,
// publish the observation to the toolkit-neutral channel, and wire the
// verification hook so service-capable builds can re-run it on demand.
//
// This package is imported by builds that LINK the gogpu stack (the
// desktop/core binary); UI-only builds that link go-gui instead read the
// published observations through internal/renderstate.
package renderboot

import (
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/render"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/renderstate"
)

// Start launches the background verification smoke. It returns
// immediately; the job runs on its own goroutine and its outcome lands
// in renderstate (and the log) whenever it completes.
func Start() {
	go func() {
		svc := render.Default()

		snap := svc.Discover()

		logging.Default().Info("render",
			"render service discovered: state=%s backend=%s fallback=%q",
			snap.State, snap.Backend, snap.FallbackReason)

		res, err := svc.RunVerificationJob(1, 240, 120)
		if err != nil {
			logging.Default().Warn("render",
				"render verification job failed (surfaced honestly, never masked): %v", err)

			renderstate.Publish(svc.Snapshot())

			return
		}

		logging.Default().Info("render",
			"render verification job verified: id=%s backend=%s sha256=%.16s… duration=%s",
			res.ID, res.Backend, res.SHA256, res.Duration)

		renderstate.Publish(svc.Snapshot())

		renderstate.SetVerifyFunc(func() (string, error) {
			r, rerr := svc.RunVerificationJob(2, 240, 120)
			if rerr != nil {
				return "", rerr
			}

			return r.ID + " " + r.SHA256, nil
		})
	}()
}
