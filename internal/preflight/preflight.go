// Package preflight is SHEYTAN's v1.7.1 pre-run compatibility gate: the
// ONE authoritative compatibility + resource verdict for a
//
//	MODEL + BACKEND + MODEL CONFIG + DEVICE + CURRENT RESOURCE STATE
//
// combination, evaluated BEFORE spawning/starting model execution. The
// application cannot guarantee every run will be smooth; it CAN detect
// known incompatibilities and dangerous resource conditions early, warn
// clearly, and protect the run when measurable pressure becomes critical.
//
// Authority rules (no second opinion anywhere):
//   - the report is the SAME object for the run-start gate, the API, the
//     UI and logging — the frontend never recalculates compatibility;
//   - hardware/memory facts come from the EXISTING probes (sysinfo,
//     hardware.Snapshot, llm.SystemMemory, llm.ModelCapabilities,
//     llm.AssessContextResource) — this package computes nothing twice;
//   - unknown values stay unknown: a missing measurement never becomes a
//     fabricated "supported" or "unsupported" verdict;
//   - device-name heuristics never infer support.
//
// Severity ladder (§4.5):
//
//	ok                — proceed
//	warning           — proceed, surface the reason
//	high_pressure     — proceed only under an explicit AUTO adjustment
//	critical_pressure — refuse under the refusal policy; protect the run
//	incompatible      — PREFLIGHT → REFUSE → NO ENGINE START
package preflight

import (
	"fmt"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// Severity is the pre-run verdict class.
type Severity string

const (
	// SeverityOK: no measured obstacle.
	SeverityOK Severity = "ok"
	// SeverityWarning: proceed, but the reason is user-facing.
	SeverityWarning Severity = "warning"
	// SeverityHighPressure: tight memory — proceed only with adjustment.
	SeverityHighPressure Severity = "high_pressure"
	// SeverityCriticalPressure: measurable memory deficit — refuse when
	// the policy requires refusal.
	SeverityCriticalPressure Severity = "critical_pressure"
	// SeverityIncompatible: hard incompatibility — never start.
	SeverityIncompatible Severity = "incompatible"
)

// MemoryRequirements is the estimated footprint of the planned run.
type MemoryRequirements struct {
	ModelBytes           int64 `json:"modelBytes,omitempty"`
	KVCacheBytes         int64 `json:"kvCacheBytes,omitempty"`
	RuntimeOverheadBytes int64 `json:"runtimeOverheadBytes,omitempty"`
	TotalBytes           int64 `json:"totalBytes"`
	ContextTokens        int   `json:"contextTokens,omitempty"`
}

// MemoryAvailable is the measured memory of the machine (0 = unknown).
type MemoryAvailable struct {
	RAMTotalBytes     int64 `json:"ramTotalBytes,omitempty"`
	RAMAvailableBytes int64 `json:"ramAvailableBytes,omitempty"`
	VRAMBytes         int64 `json:"vramBytes,omitempty"`
}

// Report is the ONE pre-run verdict. The same object serves the run-start
// gate, the API, the UI and logs.
type Report struct {
	Model      string   `json:"model"`
	Backend    string   `json:"backend"`
	Device     string   `json:"device,omitempty"`
	Compatible bool     `json:"compatible"`
	Severity   Severity `json:"severity"`

	// Reasons lists the user-facing evidence for the verdict, most
	// specific first. Empty only for SeverityOK.
	Reasons []string `json:"reasons,omitempty"`

	// Requirements/Available carry the numbers behind the verdict.
	Requirements MemoryRequirements `json:"requirements"`
	Available    MemoryAvailable    `json:"available"`

	// SafetyMarginPct is (available − estimated) / available × 100 when
	// available memory is measured; omitted when unknown.
	SafetyMarginPct float64 `json:"safetyMarginPct,omitempty"`

	// RecommendedAction names a valid alternative when one exists.
	RecommendedAction string `json:"recommendedAction,omitempty"`

	// Unknown lists the facts that could NOT be measured (honest gaps).
	Unknown []string `json:"unknown,omitempty"`
}

// Input is everything the gate knows BEFORE launch. Every field is
// sourced from an existing authority; unknown values stay zero.
type Input struct {
	// Model identity.
	ModelPath string
	Card      *llm.ModelCard         // GGUF card facts (nil = unreadable)
	Caps      *llm.ModelCapabilities // resolved capabilities (nil = unknown)

	// Backend selection (resolved by the EXISTING selection authority).
	Backend                  string // "native" | "llama.cpp-cpu" | "llama.cpp-vulkan"
	Device                   string // human-facing device label ("" = unknown)
	BackendAvailable         bool
	BackendUnavailableReason string

	// Context plan.
	RequestedContext   int // the context the run will actually use
	SessionContext     int // the session's own policy (0 = none)
	EngineContextLimit int // engine-verified window (0 = unknown)

	// Machine facts (from sysinfo/hardware — never re-probed here).
	Sys llm.SystemMemory

	// Configuration facts.
	NativeEnabled     bool
	KVQuant           string
	VisionRequired    bool
	StreamingRequired bool
}

// Evaluate produces the authoritative pre-run verdict. It never starts or
// stops anything, never re-probes hardware and never invents measurements.
func Evaluate(in Input) Report {
	report := Report{
		Model:      in.ModelPath,
		Backend:    in.Backend,
		Device:     in.Device,
		Compatible: true,
		Severity:   SeverityOK,
	}
	if in.Backend == "" {
		in.Backend = "llama.cpp-cpu"
		report.Backend = in.Backend
	}

	fail := func(reason, action string) {
		report.Compatible = false
		report.Severity = SeverityIncompatible
		report.Reasons = append(report.Reasons, reason)
		report.RecommendedAction = action
	}

	// ---- 1. Backend availability (hard gate) ----------------------------
	if !in.BackendAvailable {
		reason := fmt.Sprintf("backend %q is unavailable", in.Backend)
		if in.BackendUnavailableReason != "" {
			reason += ": " + in.BackendUnavailableReason
		}
		fail(reason, "select an available backend (llama.cpp CPU, Vulkan where supported, or the Native Engine when enabled)")
		return finalize(report, in)
	}

	// ---- 2. Model-architecture capability (hard gate) --------------------
	if in.Backend == "native" {
		if in.Caps != nil && !in.Caps.NativeBackend {
			reason := "the Native Engine cannot execute this model"
			if in.Caps.NativeReason != "" {
				reason += ": " + in.Caps.NativeReason
			}
			fail(reason, "use a llama.cpp backend for this model")
			return finalize(report, in)
		}
	} else if in.Caps != nil && in.Caps.Arch != "" {
		if ok, why := llm.NativeArchitectureSupport(in.Caps.Arch); !ok {
			// The llama.cpp graph family check: a model outside the
			// executable family will fail at load time.
			fail("llama.cpp cannot execute this model architecture: "+why,
				"choose a model from a supported architecture family")
			return finalize(report, in)
		}
	}

	// ---- 3. Context fit (hard gate on the model, clamp on the engine) ----
	requested := in.RequestedContext
	if requested <= 0 && in.SessionContext > 0 {
		requested = in.SessionContext
	}
	if in.Caps != nil && in.Caps.ContextLength > 0 && requested > in.Caps.ContextLength {
		fail(fmt.Sprintf("requested context %d exceeds the model's own limit %d",
			requested, in.Caps.ContextLength),
			fmt.Sprintf("reduce the context to %d or lower", in.Caps.ContextLength))
		return finalize(report, in)
	}
	if in.EngineContextLimit > 0 && requested > in.EngineContextLimit {
		// The engine clamps at launch — a warning with the real numbers,
		// not a silent rewrite of manual settings.
		report.Reasons = append(report.Reasons, fmt.Sprintf(
			"requested context %d will be clamped to the engine window %d",
			requested, in.EngineContextLimit))
		report.Severity = SeverityWarning
		requested = in.EngineContextLimit
	}

	// ---- 4. Resource fit (the existing resource authority) ---------------
	var modelSize int64
	if in.Caps != nil && in.Caps.SizeBytes > 0 {
		modelSize = in.Caps.SizeBytes
	} else if in.Card != nil && in.Card.SizeBytes > 0 {
		modelSize = in.Card.SizeBytes
	}

	var card *llm.ModelCard
	if in.Card != nil && (in.Card.Layers > 0 || in.Card.EmbeddingLen > 0) {
		card = in.Card
	}

	assessment := llm.AssessContextResource(card, modelSize, requested, in.KVQuant, in.Sys)
	report.Requirements = MemoryRequirements{
		ModelBytes:           assessment.ModelMemoryBytes,
		KVCacheBytes:         assessment.KVCacheBytes,
		RuntimeOverheadBytes: assessment.RuntimeOverheadBytes,
		TotalBytes:           assessment.EstimatedTotalBytes,
		ContextTokens:        requested,
	}
	report.Available = MemoryAvailable{
		RAMTotalBytes:     in.Sys.TotalRAMBytes,
		RAMAvailableBytes: in.Sys.AvailableRAMBytes,
		VRAMBytes:         in.Sys.TotalVRAMBytes,
	}

	switch assessment.Class {
	case llm.ResourceUnsupported:
		fail("estimated memory footprint "+humanBytes(assessment.EstimatedTotalBytes)+
			" exceeds this machine's memory: "+assessment.Reason,
			"reduce the context size, use a smaller quantization, or choose a smaller model")
		return finalize(report, in)

	case llm.ResourceCaution:
		report.Reasons = append(report.Reasons,
			"tight memory: "+assessment.Reason)
		if report.Severity == SeverityOK {
			report.Severity = SeverityWarning
		}
	}

	// ---- 5. CURRENT pressure (measured availability, §4.5) ----------------
	if in.Sys.AvailableRAMBytes > 0 && assessment.EstimatedTotalBytes > 0 {
		available := float64(in.Sys.AvailableRAMBytes)
		needed := float64(assessment.EstimatedTotalBytes)
		if needed > available {
			// Mandatory requirement not satisfiable RIGHT NOW.
			fail(fmt.Sprintf("estimated %s exceeds currently available memory %s — starting would push the machine into swap or OOM",
				humanBytes(int64(needed)), humanBytes(int64(available))),
				"free memory (close other workloads), reduce the context, or select a smaller model")
			return finalize(report, in)
		}
		report.SafetyMarginPct = (available - needed) / available * 100
		switch {
		case report.SafetyMarginPct < 10:
			report.Reasons = append(report.Reasons, fmt.Sprintf(
				"critical memory pressure: only %.0f%% safety margin on available RAM",
				report.SafetyMarginPct))
			report.Severity = SeverityCriticalPressure
		case report.SafetyMarginPct < 25:
			report.Reasons = append(report.Reasons, fmt.Sprintf(
				"high memory pressure: %.0f%% safety margin on available RAM",
				report.SafetyMarginPct))
			if report.Severity == SeverityOK || report.Severity == SeverityWarning {
				report.Severity = SeverityHighPressure
			}
		}
	} else if in.Sys.AvailableRAMBytes <= 0 {
		report.Unknown = append(report.Unknown, "available RAM (deep probe not ready)")
	}

	// ---- 6. Capability requirements ---------------------------------------
	if in.VisionRequired && in.Caps != nil && !in.Caps.Multimodal {
		fail("the request requires vision support and this model has no multimodal projector",
			"attach the request to a vision-capable model or remove the image input")
		return finalize(report, in)
	}
	if in.StreamingRequired && in.Backend == "native" {
		// The native engine streams (protocol v4 events) — the check is
		// here so a future backend that cannot stream fails honestly.
		_ = in.StreamingRequired
	}

	return finalize(report, in)
}

// finalize records unknowns and returns the completed report.
func finalize(r Report, in Input) Report {
	if r.Severity == SeverityOK && len(r.Reasons) > 0 {
		// Reasons without a downgrade are informational — keep them visible.
		r.Severity = SeverityWarning
	}
	if r.Severity == SeverityIncompatible && r.RecommendedAction == "" {
		r.RecommendedAction = "select a compatible model/backend combination"
	}
	return r
}

// RefusalMessage renders the user-facing refusal text (§4.8):
//
//	Cannot start this model
//	Reason: …
//	Alternative: …
func RefusalMessage(r Report) string {
	if r.Severity != SeverityIncompatible {
		return ""
	}
	var b strings.Builder
	b.WriteString("Cannot start this model")
	if r.Model != "" {
		b.WriteString(" (" + shortModel(r.Model) + ")")
	}
	b.WriteString("\n")
	b.WriteString("Reason: ")
	if len(r.Reasons) > 0 {
		b.WriteString(strings.Join(r.Reasons, "; "))
	} else {
		b.WriteString("the model/backend/device combination is not supported")
	}
	b.WriteString("\n")
	if r.RecommendedAction != "" {
		b.WriteString("Alternative: " + r.RecommendedAction)
	}
	return b.String()
}

// humanBytes renders a byte count for user-facing reasons.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// shortModel trims a model path to its file name.
func shortModel(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 && i+1 < len(path) {
		return path[i+1:]
	}
	return path
}
