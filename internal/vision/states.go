package vision

import (
	"os"
	"path/filepath"
	"strings"
)

// State is the vision-readiness state machine (v1.2.0). The UI must never
// claim vision support from a filename alone — every state below is derived
// from real evidence (GGUF architecture, projector discovery, or the
// engine's own boot outcome):
//
//	unsupported        the model architecture carries no vision encoder
//	supported          architecture is vision-capable, no projector scanned yet
//	projector-missing  vision-capable, and mmproj files exist but none pair
//	projector-found    a compatible projector was discovered on disk
//	projector-verified the engine booted WITH the projector and verified it
//	loading            the engine is booting with the projector
//	ready              projector verified AND serving (the only green state)
//	degraded           the projector failed with every profile; text-only retry
//	failed             an explicit override could not be resolved / boot failed
type State string

const (
	StateUnsupported State = "unsupported"
	StateSupported   State = "supported"
	StateMissing     State = "projector-missing"
	StateFound       State = "projector-found"
	StateVerified    State = "projector-verified"
	StateLoading     State = "loading"
	StateReady       State = "ready"
	StateDegraded    State = "degraded"
	StateFailed      State = "failed"
)

// Healthy reports whether the state represents usable vision.
func (s State) Healthy() bool {
	return s == StateReady || s == StateVerified
}

// Known reports whether the value is part of the state machine.
func (s State) Known() bool {
	switch s {
	case StateUnsupported, StateSupported, StateMissing, StateFound,
		StateVerified, StateLoading, StateReady, StateDegraded, StateFailed:
		return true
	}
	return false
}

// visionArchs lists GGUF general.architecture values whose models ship a
// vision encoder consumable by llama.cpp's mtmd runtime. This is a
// CONSERVATIVE allow-list used only when no compatible projector exists on
// disk — the presence of a pairing mmproj is always the stronger evidence
// and short-circuits this set (see EvaluateModel).
var visionArchs = map[string]bool{
	"llava":     true,
	"mllama":    true,
	"minicpmv":  true,
	"gemma3":    true,
	"gemma3n":   true,
	"qwen2vl":   true,
	"qwen2p5vl": true,
	"qwen3vl":   true,
	"pixtral":   true,
	"internvl":  true,
	"smolvlm":   true,
	"idefics3":  true,
	"mistral3":  true,
}

// VisionCapableArch reports whether a GGUF architecture value is known to
// carry a vision encoder. Unknown architectures are honestly reported as
// NOT capable unless a compatible projector exists (the pairing check is
// architecture-agnostic by design).
func VisionCapableArch(arch string) bool {
	return visionArchs[strings.ToLower(strings.TrimSpace(arch))]
}

// Evaluation is the pre-boot vision assessment for one model file.
type Evaluation struct {
	// State is the evidence-backed state machine value (never guessed).
	State State `json:"state"`
	// Projector is the discovered/override projector path ("" when none).
	Projector string `json:"projector,omitempty"`
	// Reason is the human explanation consumed by Details panels.
	Reason string `json:"reason,omitempty"`
}

// EvaluateModel assesses what vision support the model file CAN have on
// this machine right now, before the engine runs:
//
//   - an existing explicit override wins (it is validated — a missing
//     override reports failed, never a silent substitute);
//   - a discovered compatible projector reports projector-found regardless
//     of architecture (an mmproj that pairs is itself the evidence);
//   - otherwise the architecture allow-list decides supported vs
//     unsupported; mmproj files present in dir that do NOT pair upgrade
//     supported → projector-missing.
func EvaluateModel(dir, modelFile, override, arch string) Evaluation {
	if override != "" {
		candidate := override
		if !filepath.IsAbs(override) {
			candidate = filepath.Join(dir, override)
		}
		if _, err := os.Stat(candidate); err == nil {
			return Evaluation{
				State:     StateFound,
				Projector: candidate,
				Reason:    "explicit projector override resolved: " + filepath.Base(candidate),
			}
		}
		return Evaluation{
			State: StateFailed,
			Reason: "explicit projector override not found: " +
				filepath.Base(override),
		}
	}

	paired := FindProjector(dir, modelFile, "")
	if paired != "" {
		return Evaluation{
			State:     StateFound,
			Projector: paired,
			Reason:    "compatible projector discovered and paired: " + filepath.Base(paired),
		}
	}

	if !VisionCapableArch(arch) {
		return Evaluation{
			State:  StateUnsupported,
			Reason: "architecture " + displayArch(arch) + " carries no vision encoder",
		}
	}

	if len(ListProjectors(dir)) > 0 {
		return Evaluation{
			State:  StateMissing,
			Reason: "vision-capable architecture, but no mmproj in the models folder pairs with this model",
		}
	}

	return Evaluation{
		State:  StateSupported,
		Reason: "vision-capable architecture; no projector file present in the models folder",
	}
}

func displayArch(arch string) string {
	if strings.TrimSpace(arch) == "" {
		return "unknown"
	}
	return arch
}

// ProjectorInfo summarizes one projector file for Details panels. Every
// field is measured from the file; nothing is inferred from the name
// except the family label used for pairing.
type ProjectorInfo struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	// Format reports the detection evidence: "gguf" (GGUF magic + clip
	// metadata) or "name-convention" (mmproj-*.gguf).
	Format string `json:"format,omitempty"`
}

// InspectProjector measures a projector file: size, existence and the
// detection evidence behind it. Missing files report ok=false.
func InspectProjector(path string) (ProjectorInfo, bool) {
	info := ProjectorInfo{Path: path, Name: filepath.Base(path)}
	fi, err := os.Stat(path)
	if err != nil {
		return info, false
	}
	info.SizeBytes = fi.Size()
	if ggufHasClipKeys(path) {
		info.Format = "gguf"
	} else {
		info.Format = "name-convention"
	}
	return info, true
}
