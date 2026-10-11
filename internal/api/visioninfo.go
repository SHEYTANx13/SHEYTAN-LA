// v1.2.0 — vision readiness surfaces: per-model vision state on
// /api/models and the runtime vision block on /api/engine. Both derive
// from internal/vision's evidence-based state machine; the UI is never
// allowed to claim vision support from filenames alone.
package api

import (
	"os"
	"path/filepath"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// visionFields extends modelInfo with the ModelPackage vision view: the
// state machine value, the paired projector evidence and its measured
// size. Everything here is computed from real files and GGUF metadata.
type visionFields struct {
	VisionState    vision.State `json:"visionState"`
	VisionReason   string       `json:"visionReason,omitempty"`
	MMProjPath     string       `json:"mmprojPath,omitempty"`
	MMProjName     string       `json:"mmprojName,omitempty"`
	MMProjSize     int64        `json:"mmprojSizeBytes,omitempty"`
	MMProjVerified bool         `json:"mmprojVerified,omitempty"` // engine serving WITH it
}

// visionForModel evaluates the pre-boot vision state for one local model
// file (architecture + projector discovery + override resolution).
func visionForModel(cfgModelsDir, modelPath, override, arch string) visionFields {
	ev := vision.EvaluateModel(cfgModelsDir, modelPath, override, arch)
	vf := visionFields{
		VisionState:  ev.State,
		VisionReason: ev.Reason,
		MMProjPath:   ev.Projector,
	}
	if ev.Projector != "" {
		vf.MMProjName = filepath.Base(ev.Projector)
		if fi, err := os.Stat(ev.Projector); err == nil {
			vf.MMProjSize = fi.Size()
		}
	}
	return vf
}
