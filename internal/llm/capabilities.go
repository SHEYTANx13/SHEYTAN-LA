// capabilities.go — v1.7.1 (§5.3/§5.7): the shared backend capability
// contract and the per-backend candidate verdicts for selection.
//
// One capability authority split exactly along its seams:
//   - llm.ModelCapabilities (modelcaps.go) is the MODEL-side authority —
//     what the GGUF file is;
//   - BackendCapabilities (here) is the BACKEND-side contract — what each
//     serving alternative can do with it RIGHT NOW.
//
// The contract is an OPTIONAL interface (CapabilityReporter) so existing
// implementations and test fakes stay valid; selection treats a backend
// without the reporter as "capabilities unknown" — never as fabricated
// support.
package llm

import (
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// BackendCapabilities is what one serving alternative reports about
// itself (§5.3). Every field is EVIDENCE-based: unknown stays the zero
// value, never a guess.
type BackendCapabilities struct {
	// Identity names the serving alternative ("llama.cpp-cpu",
	// "llama.cpp-vulkan", "native").
	Identity string

	// GenerationCapable reports whether this backend can serve a
	// generation request at the moment of the call.
	GenerationCapable bool

	// GenerationFallbackReason explains why it cannot (empty when it can).
	GenerationFallbackReason string

	// SupportedArchitectures describes the model architecture families
	// this backend executes ("llama graph family", "llama + native graph
	// family", ...). Empty = unknown.
	SupportedArchitectures string

	// ContextCapability is the engine-verified context window (0 =
	// unknown until the engine has proven one).
	ContextCapability int

	// ChatTemplate/Vision/Streaming/Cancellation report the serving
	// features the backend actually implements.
	Streaming    bool
	Cancellation bool

	// Devices names the devices the backend executes on ("" = unknown).
	Devices string

	// ResourceRequirements summarizes the memory plan when the backend
	// has one loaded ("" = no plan available).
	ResourceRequirements string

	// Readiness is the current lifecycle state ("ready", "stopped", ...).
	Readiness string

	// IncompatibilityReason is the standing reason this backend cannot
	// serve the selected model ("" = compatible / unknown).
	IncompatibilityReason string
}

// CapabilityReporter is the optional interface a Backend implements to
// publish its capability contract.
type CapabilityReporter interface {
	BackendCapabilities() BackendCapabilities
}

// CapabilitiesOf extracts the capability contract from a backend (ok=false
// when the backend does not implement the reporter).
func CapabilitiesOf(b Backend) (BackendCapabilities, bool) {
	if cr, ok := b.(CapabilityReporter); ok {
		return cr.BackendCapabilities(), true
	}
	return BackendCapabilities{Identity: b.Name()}, false
}

// BackendCandidateVerdict is one row of the selection/preflight table
// (§5.7): an explicit serving alternative with its availability and the
// reason behind it.
type BackendCandidateVerdict struct {
	// Name is the stable backend name ("native", "llama").
	Name string `json:"name"`
	// Label is the user-facing serving alternative ("Native Engine",
	// "llama.cpp CPU", "llama.cpp Vulkan").
	Label string `json:"label"`
	// Available reports whether this alternative can serve right now.
	Available bool `json:"available"`
	// Reason carries the honest detail (why not, or how it is chosen).
	Reason string `json:"reason,omitempty"`
	// Selected marks the alternative the selection authority would use.
	Selected bool `json:"selected,omitempty"`
}

// BackendCandidates produces the selection/preflight verdict table for
// the three serving alternatives (§5.7):
//
//	Native Engine      compatible/incompatible + reason
//	llama.cpp CPU      compatible/incompatible + reason
//	llama.cpp Vulkan   compatible/incompatible + reason
//
// It uses ONLY existing evidence: the user's engine-backend preference,
// the native generation probe (GenerationCapable + fallback reason), the
// Vulkan availability probe, and the model-side capability verdicts.
// No invented preference ranking: the ORDER is fixed (Native, CPU,
// Vulkan) and the SELECTED row mirrors SelectGenerationBackendDetailed —
// the two never disagree.
func BackendCandidates(cfg *config.Config, native, fallback Backend, caps *ModelCapabilities) []BackendCandidateVerdict {
	if cfg == nil {
		return nil
	}

	candidates := []BackendCandidateVerdict{
		{
			Name:  config.BackendNative,
			Label: "Native Engine",
		},
		{
			Name:  config.BackendLlama,
			Label: "llama.cpp CPU",
			// The CPU path is the default serving alternative: the
			// managed engine binary is provisioned for it.
			Available: true,
		},
		{
			Name:  "llama-vulkan",
			Label: "llama.cpp Vulkan",
		},
	}

	// Native Engine: enabled AND generation-capable.
	if native != nil {
		capable := true
		reason := ""
		if gc, ok := native.(GenerationCapable); ok {
			capable = gc.GenerationCapable()
		}
		if !capable {
			if fr, ok := native.(GenerationFallbackReporter); ok {
				reason = fr.GenerationFallbackReason()
			}
			if reason == "" {
				reason = "native engine cannot serve generation right now"
			}
		}
		if cfg.NativeBackendEnabled() && caps != nil && !caps.NativeBackend {
			capable = false
			if caps.NativeReason != "" {
				reason = caps.NativeReason
			}
		}
		candidates[0].Available = capable
		candidates[0].Reason = reason
	} else {
		candidates[0].Available = false
		candidates[0].Reason = "native engine is not configured"
	}

	// llama.cpp Vulkan: measured availability (Windows packaging +
	// driver probe). Unknown stays reported as unavailable-with-reason,
	// never as silent support.
	vulkan := VulkanAvailable(cfg)
	candidates[2].Available = vulkan
	if !vulkan {
		candidates[2].Reason = "no usable Vulkan runtime detected for the managed engine"
	}

	// The SELECTED row mirrors the selection authority exactly.
	decision := SelectGenerationBackendDetailed(cfg, native, fallback)
	for i := range candidates {
		if decision.SelectedName == config.BackendNative && candidates[i].Name == config.BackendNative {
			candidates[i].Selected = true
		}
		if decision.SelectedName == config.BackendLlama && candidates[i].Name == config.BackendLlama {
			candidates[i].Selected = true
		}
	}

	return candidates
}
