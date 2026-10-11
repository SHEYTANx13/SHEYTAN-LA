package llm

// execution.go — v1.8.5 Phase 1: the ONE shared execution/evidence ladder
// for the Go control plane ↔ C++/serving execution-plane boundary.
//
// WHY THIS EXISTS (Phase 2 foundation, not a new authority): v1.8.x
// already measures every rung of the ladder through its own authorities —
// device enumeration (devices.go), backend health (LlamaServer state +
// ProbeHealth), the selection machinery (accelerator.Resolution), verified
// model loading (engine verification), real generation telemetry
// (perftracker.go) and runtime offload lines (ObserveEngineLine). What
// Phase 2 needs — backend selection driven by PROVEN runtime evidence —
// is ONE shared, honest composition of those rungs, so that every
// consumer (engine snapshot, future backend selection, GPU verification
// transactions) reads the SAME ladder instead of re-deriving it.
//
// THE NON-NEGOTIABLE RULE this structure enforces by construction:
//
//      device enumeration  ≠  execution verified
//
// The stage is the highest rung for which EVERY lower rung also holds; a
// gap anywhere stops the ladder and is reported as the reason. Unknown
// values stay unknown (""); nothing is fabricated, inferred or
// hard-coded — no Intel/Arc/Vulkan/SYCL/OpenVINO execution claim is made
// merely because a backend or device exists.
//
// This file adds NO new sampler, NO new policy engine, NO new manager: it
// is a pure composition over the existing execution-plane authorities.

// ExecutionStage is one rung of the evidence ladder. The order is the
// contract: each stage implies all stages below it.
type ExecutionStage string

const (
	// StageNone: nothing is proven yet (engine absent, nothing detected).
	StageNone ExecutionStage = "none"

	// StageDetected: a device/backend was DETECTED (enumeration or
	// presence). Detection only proves existence — never execution.
	StageDetected ExecutionStage = "detected"

	// StageBackendAvailable: the serving backend booted and is healthy
	// (authoritative engine state + real health probe).
	StageBackendAvailable ExecutionStage = "backend-available"

	// StageDeviceSelected: a device/backend variant is COMMITTED for
	// serving through the ONE selection authority (the accelerator
	// resolution — not mere presence in a list).
	StageDeviceSelected ExecutionStage = "device-selected"

	// StageModelLoaded: a model is VERIFIED loaded and serving (the
	// engine's verified-model proof, not a spawn or a config value).
	StageModelLoaded ExecutionStage = "model-loaded"

	// StageGenerationExecuted: at least one REAL generation completed
	// through this serving path (measured perf samples exist — the
	// streaming path's own token timer).
	StageGenerationExecuted ExecutionStage = "generation-executed"

	// StageExecutionEvidence: RUNTIME execution/offload evidence was
	// observed for the SELECTED device (a measured offload line, or the
	// selection's own execution-verification contract).
	StageExecutionEvidence ExecutionStage = "execution-evidence"

	// StageVerified: the full transaction-level verified state — the
	// selection is execution-verified AND a model is loaded AND real
	// generations ran through it. This is the ONLY stage that may back
	// the word "verified" in any surface.
	StageVerified ExecutionStage = "verified"
)

// executionStageOrder is the ladder's total order (index = height).
var executionStageOrder = []ExecutionStage{
	StageNone,
	StageDetected,
	StageBackendAvailable,
	StageDeviceSelected,
	StageModelLoaded,
	StageGenerationExecuted,
	StageExecutionEvidence,
	StageVerified,
}

// ExecutionStageRank returns the ladder position of a stage (unknown →
// StageNone's position). Public for validation and tests.
func ExecutionStageRank(s ExecutionStage) int {
	for i, stage := range executionStageOrder {
		if stage == s {
			return i
		}
	}

	return 0
}

// ExecutionReportInputs are the MEASURED inputs the ladder composes —
// every field comes from an existing authority; zero values are honest
// unknowns, never defaults.
type ExecutionReportInputs struct {
	// DevicesKnown: device enumeration (or accelerator detection) knows
	// at least one device exists. Detection only.
	DevicesKnown bool

	// BackendHealthy: the serving backend's authoritative state is
	// ready/running/busy AND its health probe passes.
	BackendHealthy bool

	// BackendName: the serving backend ("llama"/"native"), "" unknown.
	BackendName string

	// EngineTag: the EXACT engine identity in service ("" unknown).
	// Preserving engine identity is part of the contract: never verify
	// one binary and claim another.
	EngineTag string

	// DeviceSelected: the device/backend committed by the selection
	// authority ("" when nothing is committed).
	DeviceSelected string

	// SelectionExecutionVerified: the selection authority's own
	// execution-verification contract (accelerator.Resolution.
	// ExecutionVerified — true ONLY for runtime execution evidence, and
	// for CPU by definition of execution).
	SelectionExecutionVerified bool

	// Verification: how the selection was verified, or the verification
	// plan when it was not ("" unknown).
	Verification string

	// VerifiedModel: the engine's VERIFIED loaded model ("" when no
	// model is verified serving).
	VerifiedModel string

	// GenerationSamples: count of measured real generations through the
	// serving path (perf ring). 0 = none yet — an honest unknown.
	GenerationSamples int

	// OffloadEvidence: the measured runtime offload line ("" when none
	// was observed). The strongest per-boot execution proof.
	OffloadEvidence string
}

// ExecutionReport is the composed ladder — the ONE shared structure
// Phase 2's deep engine work consumes. JSON tags serve the /api/engine
// surface; every omitted field is an explicit unknown.
type ExecutionReport struct {
	// Stage is the highest rung whose predecessors ALL hold.
	Stage ExecutionStage `json:"stage"`

	// Backend is the serving backend name ("" unknown).
	Backend string `json:"backend,omitempty"`

	// EngineTag is the exact engine identity in service ("" unknown).
	EngineTag string `json:"engineTag,omitempty"`

	// Device is the committed device/backend from the selection
	// authority ("" unknown).
	Device string `json:"device,omitempty"`

	// Model is the verified loaded model ("" unknown).
	Model string `json:"model,omitempty"`

	// GenerationExecuted reports whether real generations ran (measured
	// samples exist).
	GenerationExecuted bool `json:"generationExecuted"`

	// OffloadEvidence is the measured runtime offload line ("" unknown —
	// never a guess, never a device-enumeration echo).
	OffloadEvidence string `json:"offloadEvidence,omitempty"`

	// ExecutionVerified mirrors the selection authority's contract for
	// the SELECTED device only. It is TRUE only at StageExecutionEvidence
	// or above BY CONSTRUCTION — detection alone can never set it.
	ExecutionVerified bool `json:"executionVerified"`

	// Verification states HOW the selection was verified or the pending
	// plan ("" unknown).
	Verification string `json:"verification,omitempty"`

	// Gaps lists the rung-by-rung reasons the ladder stopped where it
	// did (one per unproven rung, lowest first). Honest diagnostics —
	// never empty while Stage < StageVerified.
	Gaps []string `json:"gaps,omitempty"`
}

// ComposeExecutionReport derives the ladder from the measured inputs. A
// PURE function over explicit inputs: deterministic, unit-testable, and
// the only derivation path — consumers never re-derive the stage.
//
// Ladder rules (all by construction):
//
//   - detected requires DevicesKnown;
//   - backend-available requires detected AND BackendHealthy;
//   - device-selected requires backend-available AND a committed device;
//   - model-loaded requires device-selected AND a verified model;
//   - generation-executed requires model-loaded AND measured samples;
//   - execution-evidence requires generation-executed AND (a measured
//     offload line OR the selection's execution-verified contract);
//   - verified requires execution-evidence AND the selection's
//     execution-verified contract AND a verified model AND generations.
//
// Any failed rung is recorded in Gaps with its reason; the ladder stops
// at the last proven rung. A detection-only report can therefore never
// carry ExecutionVerified or a stage above StageDetected.
func ComposeExecutionReport(in ExecutionReportInputs) ExecutionReport {
	rep := ExecutionReport{
		Backend:         in.BackendName,
		EngineTag:       in.EngineTag,
		Device:          in.DeviceSelected,
		Model:           in.VerifiedModel,
		OffloadEvidence: in.OffloadEvidence,
		Verification:    in.Verification,
	}

	stage := StageNone

	// Rung 1 — detected.
	if !in.DevicesKnown {
		rep.Gaps = append(rep.Gaps, "detected: no device enumeration evidence yet")
		rep.Stage = stage

		return rep
	}

	stage = StageDetected

	// Rung 2 — backend available.
	if !in.BackendHealthy {
		rep.Gaps = append(rep.Gaps, "backend-available: the serving backend is not healthy")
		rep.Stage = stage

		return rep
	}

	stage = StageBackendAvailable

	// Rung 3 — device selected (a COMMITTED selection, not presence).
	if in.DeviceSelected == "" {
		rep.Gaps = append(rep.Gaps, "device-selected: no device committed by the selection authority")
		rep.Stage = stage

		return rep
	}

	stage = StageDeviceSelected

	// Rung 4 — model loaded (VERIFIED serving, not a spawn).
	if in.VerifiedModel == "" {
		rep.Gaps = append(rep.Gaps, "model-loaded: no verified serving model")
		rep.Stage = stage

		return rep
	}

	stage = StageModelLoaded

	// Rung 5 — generation executed (MEASURED samples).
	if in.GenerationSamples <= 0 {
		rep.Gaps = append(rep.Gaps, "generation-executed: no measured generation through this serving path yet")
		rep.Stage = stage

		return rep
	}

	rep.GenerationExecuted = true
	stage = StageGenerationExecuted

	// Rung 6 — execution/offload evidence for the SELECTED device.
	hasOffload := in.OffloadEvidence != ""

	if !hasOffload && !in.SelectionExecutionVerified {
		rep.Gaps = append(rep.Gaps,
			"execution-evidence: no runtime offload/execution evidence for the selected device (detection is not execution)")
		rep.Stage = stage

		return rep
	}

	stage = StageExecutionEvidence

	// Rung 7 — verified: the full contract.
	if !in.SelectionExecutionVerified || in.VerifiedModel == "" || in.GenerationSamples <= 0 {
		rep.Gaps = append(rep.Gaps,
			"verified: the selection's execution-verification contract is not satisfied end-to-end")
		rep.Stage = stage

		return rep
	}

	rep.ExecutionVerified = true
	rep.Stage = StageVerified

	return rep
}
