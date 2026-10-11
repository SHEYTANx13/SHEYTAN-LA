// Package accelerator is SHEYTAN's single accelerator abstraction (v1.2.6).
//
// It answers ONE question honestly: which compute backend should serve
// inference on THIS machine, based on MEASURED evidence — never on the
// presence of a DLL.
//
// Kinds (the backend vocabulary):
//
//      CPU          portable scalar compute (always available)
//      GPU_VULKAN   llama.cpp Vulkan backend driving an enumerated GPU device
//      GPU_OPENVINO OpenVINO GPU plugin driving an enumerated GPU device
//      NPU_OPENVINO OpenVINO NPU plugin driving a measured NPU (Intel AI Boost)
//
// Requested profiles (the user-facing resolver input):
//
//      AUTO  evidence-based decision (default)
//      GPU   prefer GPU, fall back per evidence
//      NPU   prefer NPU, fall back per evidence
//      CPU   force CPU
//
// AUTO resolves to one of the runtime profiles:
//
//      INTERACTIVE_GPU  GPU-driven interactive chat/coding (default when a
//                       usable GPU is enumerated)
//      LOW_POWER_NPU    NPU-driven low-power serving (only when every NPU
//                       gate below passes)
//      CPU_SAFE         CPU when no accelerator is usable
//      VISION_GPU       GPU with projector offload for vision workloads
//      MAXIMUM          widest-context posture for heavy workloads
//
// Resolution rules (all evidence-gated):
//
//      NPU_OPENVINO requires ALL of: measured NPU device present, OpenVINO
//      runtime present (measured load, not a name), model architecture +
//      quantization supported, streaming/tool behavior supported, context
//      limitations acceptable, cached compiled model usable, and measured
//      benchmark evidence it is not slower for the workload. Any failed gate
//      falls back to GPU or CPU with the reason recorded. NPU presence alone
//      NEVER selects NPU_OPENVINO.
//
//      GPU_VULKAN requires the installed engine to have ENUMERATED a usable
//      device (--list-devices when supported, or runtime log evidence of
//      real offload). "ggml-vulkan.dll exists" is NOT sufficient evidence.
//      v1.8.6 truth model: enumeration SELECTS (provisioning posture);
//      it never VERIFIES — ExecutionVerified additionally requires a
//      measured offload line or a still-valid ExecutionReceipt.
package accelerator

import (
        "fmt"
        "strings"
)

// Kind is one accelerator backend class.
type Kind string

const (
        KindCPU         Kind = "CPU"
        KindGPUVulkan   Kind = "GPU_VULKAN"
        KindGPUOpenVINO Kind = "GPU_OPENVINO"
        KindNPUOpenVINO Kind = "NPU_OPENVINO"
)

// Requested is the user-facing resolver input.
type Requested string

const (
        RequestAuto Requested = "AUTO"
        RequestGPU  Requested = "GPU"
        RequestNPU  Requested = "NPU"
        RequestCPU  Requested = "CPU"
)

// NormalizeRequested validates a requested profile.
func NormalizeRequested(s string) Requested {
        switch strings.ToUpper(strings.TrimSpace(s)) {
        case "GPU":
                return RequestGPU
        case "NPU":
                return RequestNPU
        case "CPU":
                return RequestCPU
        default:
                return RequestAuto
        }
}

// AutoProfile is the resolved runtime posture under AUTO.
type AutoProfile string

const (
        ProfileInteractiveGPU AutoProfile = "INTERACTIVE_GPU"
        ProfileLowPowerNPU    AutoProfile = "LOW_POWER_NPU"
        ProfileCPUSafe        AutoProfile = "CPU_SAFE"
        ProfileVisionGPU      AutoProfile = "VISION_GPU"
        ProfileMaximum        AutoProfile = "MAXIMUM"
)

// Workload is the coarse workload class the resolution considers.
type Workload string

const (
        WorkloadChat    Workload = "chat"
        WorkloadCoding  Workload = "coding"
        WorkloadVision  Workload = "vision"
        WorkloadLong    Workload = "long-prompt"
        WorkloadMaximum Workload = "maximum"
)

// Device is one accelerator the INSTALLED ENGINE actually enumerated
// (never a registry/WMI guess).
type Device struct {
        Backend string `json:"backend"` // e.g. "Vulkan0", "CUDA0"
        Name    string `json:"name"`    // the device the engine reported
        TotalMB int    `json:"totalMb"` // reported device memory (0 unknown)
        Source  string `json:"source"`  // "engine-enumeration" | "engine-log"
}

// OpenVINOStatus is the MEASURED OpenVINO runtime availability.
type OpenVINOStatus struct {
        RuntimePresent bool   `json:"runtimePresent"`
        Detail         string `json:"detail,omitempty"` // where it was found / why not
}

// ModelFacts are the selected model's measured properties relevant to
// accelerator compatibility.
type ModelFacts struct {
        Architecture string `json:"architecture,omitempty"`
        Quantization string `json:"quantization,omitempty"`
        ContextLimit int    `json:"contextLimit,omitempty"`
}

// Evidence is every measured fact the resolver may use. Callers fill it
// from real probes; a zero Evidence resolves to CPU with the recorded
// reason (fail-closed).
type Evidence struct {
        // EngineDevices: devices the installed engine enumerated itself
        // (--list-devices or runtime logs). Empty = none or unsupported.
        EngineDevices []Device `json:"engineDevices"`

        // EnumerationSupported: false when the installed engine build does
        // not support --list-devices (the fallback evidence rules apply).
        EnumerationSupported bool `json:"enumerationSupported"`

        // RuntimeOffloadEvidence: the engine log measured real layer offload
        // (e.g. "offloaded 33/33 layers to GPU") DURING THE CURRENT BOOT.
        // This is the strongest per-boot execution proof.
        RuntimeOffloadEvidence string `json:"runtimeOffloadEvidence,omitempty"`

        // GPUExecutionReceipt (v1.8.6): the structured runtime execution
        // receipt — a committed, identity-carrying proof that a serving
        // execution really offloaded to the GPU (the persisted bounded
        // transaction outcome). NIL when none exists or when the caller's
        // identity validation already rejected it. Enumeration is NEVER a
        // receipt.
        GPUExecutionReceipt *ExecutionReceipt `json:"gpuExecutionReceipt,omitempty"`

        // EngineTag / EngineVariant identify the CURRENT installed engine —
        // the identity a receipt must still be valid FOR (stale-evidence
        // invalidation; empty = unknown, identity checks are skipped).
        EngineTag     string `json:"engineTag,omitempty"`
        EngineVariant string `json:"engineVariant,omitempty"`

        // VulkanBackendPresent: a Vulkan backend sits beside the engine
        // binary. NOT sufficient for GPU by itself (documented fallback only).
        VulkanBackendPresent bool `json:"vulkanBackendPresent"`

        // NPUPresent + NPU measured identity (hardware presence only).
        NPUPresent bool   `json:"npuPresent"`
        NPUName    string `json:"npuName,omitempty"`

        // OpenVINO measured status.
        OpenVINO OpenVINOStatus `json:"openvino"`

        // NPUBenchmarkTokPerSec: MEASURED generation tok/s on the NPU for a
        // comparable workload (0 = never measured — NPU never wins without it).
        NPUBenchmarkTokPerSec float64 `json:"npuBenchmarkTokPerSec,omitempty"`

        // GPUBenchmarkTokPerSec: measured GPU tok/s for the same workload.
        GPUBenchmarkTokPerSec float64 `json:"gpuBenchmarkTokPerSec,omitempty"`

        // Model facts (arch/quant gates for the NPU path).
        Model ModelFacts `json:"model"`

        // AvailableRAMMB: measured available system memory.
        AvailableRAMMB int `json:"availableRamMb,omitempty"`

        // Workload class.
        Workload Workload `json:"workload"`
}

// Resolution is the stored, inspectable result of one resolution.
type Resolution struct {
        Requested   Requested   `json:"requested"`
        Backend     Kind        `json:"backend"`
        Device      string      `json:"device,omitempty"`
        AutoProfile AutoProfile `json:"autoProfile,omitempty"`
        GPULayers   int         `json:"gpuLayers"` // 0 = CPU, -1 = all layers
        Context     int         `json:"context,omitempty"`
        Batch       int         `json:"batch,omitempty"`
        KVCache     string      `json:"kvCache,omitempty"`
        Reason      string      `json:"reason"`              // ALWAYS set — the why
        Fallbacks   []string    `json:"fallbacks,omitempty"` // failed gates, in order

        // --- v1.2.6 continuation: EXPLICIT EVIDENCE STATE ------------------
        // The explainable block. "Vulkan DLL exists" and "GPU execution
        // verified" are DIFFERENT claims and must never be conflated:
        // ExecutionVerified is true ONLY when RUNTIME EXECUTION evidence
        // exists (a measured offload line or a still-valid execution
        // receipt); the weaker enumeration/DLL-presence postures select
        // GPU_VULKAN with ExecutionVerified FALSE and the verification plan
        // stated in Verification.

        // Selected is the selected backend (mirror of Backend, spelled out
        // for the explainable contract).
        Selected Kind `json:"selected"`

        // Available lists every backend the evidence shows is AVAILABLE on
        // this machine (selected included; CPU always — it is definitionally
        // available). Presence in this list ≠ selected and ≠ verified.
        Available []Kind `json:"available"`

        // ExecutionVerified reports whether SELECTED has RUNTIME EXECUTION
        // evidence (v1.8.6: a MEASURED offload line from the current boot, or
        // a still-valid execution receipt produced by a completed serving
        // execution — never device enumeration, never a DLL — or, for CPU,
        // execution by definition). FALSE means the selection is a documented
        // fallback pending verification.
        ExecutionVerified bool `json:"executionVerified"`

        // Verification states HOW the selection was verified, or the
        // verification plan when it was not ("pending — device enumerated;
        // real GPU execution evidence required").
        Verification string `json:"verification,omitempty"`

        // Fallback is the safe posture if the selected-but-unverified
        // backend fails to execute: "none" when the selection is verified,
        // "safe" when the CPU path is the guaranteed fallback.
        Fallback string `json:"fallback"`
}

// ExecutionReceipt (v1.8.6) is REAL runtime execution evidence for a
// non-CPU backend — the structured object the whole pipeline shares
// instead of scattered booleans. A receipt exists ONLY when a serving
// execution actually produced it:
//
//   - the engine's own runtime log printed a measured offload line
//     ("offloaded 33/33 layers to GPU") during the CURRENT boot, or
//   - the bounded GPU candidate transaction COMPLETED: staged → started
//     → health → model load → real generation → measured offload
//     evidence → commit (the persisted gpu-probe outcome).
//
// A receipt carries the IDENTITY it was produced under. Staleness is a
// correctness property: a receipt for another engine tag, another
// variant or a failed probe can NEVER verify the current selection.
type ExecutionReceipt struct {
        // Kind names the receipt's origin: "offload-line" (a current-boot
        // measured line) or "verified-probe" (a committed transaction).
        Kind string `json:"kind"`

        // Line is the measured evidence line itself ("" when the persisted
        // receipt only summarizes — the reason then carries it).
        Line string `json:"line,omitempty"`

        // EngineTag / Variant identify the EXACT engine binary+variant the
        // evidence was produced by. Never verify one binary and serve
        // another.
        EngineTag string `json:"engineTag,omitempty"`
        Variant   string `json:"variant,omitempty"`

        // Device is the device identity the evidence attributed ("" when
        // unattributed — e.g. offload line from a build without enumeration).
        Device string `json:"device,omitempty"`

        // Model is the model that was serving when the evidence was
        // produced ("" unknown).
        Model string `json:"model,omitempty"`

        // Status is the receipt's outcome ("verified"; a "failed" persisted
        // probe is a NEGATIVE receipt — it must never verify anything).
        Status string `json:"status,omitempty"`

        // At is the RFC3339 production time ("" unknown).
        At string `json:"at,omitempty"`
}

// Receipt kinds (the origin vocabulary).
const (
        ReceiptKindOffloadLine   = "offload-line"
        ReceiptKindVerifiedProbe = "verified-probe"
)

// ReceiptStatus values for persisted probe receipts.
const (
        ReceiptStatusVerified = "verified"
        ReceiptStatusFailed   = "failed"
)

// ValidFor reports whether this receipt may verify the CURRENT engine
// identity (engineTag + variant). A receipt produced by another engine
// build or variant is stale by construction — "never verify one binary
// and serve another". Failed receipts never verify anything. The
// hardware/device dimension of staleness is owned by the caller side
// (the transaction identity fingerprint); this check enforces the
// engine-identity dimension the resolution can see.
func (r *ExecutionReceipt) ValidFor(engineTag, variant string) bool {
        if r == nil {
                return false
        }

        if r.Status != "" && r.Status != ReceiptStatusVerified {
                return false
        }

        if engineTag != "" && r.EngineTag != "" && r.EngineTag != engineTag {
                return false
        }

        if variant != "" && r.Variant != "" && r.Variant != variant {
                return false
        }

        return true
}

// Verification evidence strings (the measured how).
//
// v1.8.6 SEMANTICS: --list-devices enumeration is SELECTION/PROVISIONING
// evidence, never execution proof (VerificationEngineEnum names the
// device-identity attribution only). Execution verification requires a
// measured offload line (VerificationOffloadLog) or a still-valid
// execution receipt (VerificationReceipt).
const (
        VerificationEngineEnum = "engine enumerated the device via --list-devices (selection evidence — not execution proof)"

        // VerificationPendingExecution is the verification plan whenever a
        // GPU was DETECTED/SELECTED but never EXECUTED: the ladder's
        // detected ≠ executed gap, stated.
        VerificationPendingExecution = "pending — device enumerated but not execution-verified: real GPU offload evidence (a measured offload line or a committed execution receipt) is required"

        // VerificationPendingLog covers the enumeration-unsupported build.
        VerificationPendingLog = "pending — device enumeration unsupported by this engine build; offload to be verified from the runtime log"

        VerificationOffloadLog   = "runtime engine log measured real GPU offload"
        VerificationReceipt      = "committed GPU execution receipt (bounded transaction: real generation + measured offload evidence)"
        VerificationCPUExecution = "CPU execution is the definitionally available fallback"
        VerificationNPUBench     = "measured NPU benchmark with OpenVINO runtime"
)

// NPU model-architecture support gate (extend ONLY with measured evidence
// that the runtime actually executes the arch).
var npuSupportedArchs = map[string]bool{
        "llama":  true,
        "qwen2":  true,
        "phi3":   true,
        "gemma":  true,
        "gemma2": true,
}

var npuSupportedQuants = map[string]bool{
        "Q4_0": true, "Q4_1": true, "Q8_0": true, "F16": true, "INT4": true,
}

// Resolve maps (requested profile, measured evidence) to the concrete
// backend decision. Pure function — deterministic, unit-testable, no I/O.
// It NEVER claims an accelerator the evidence does not prove.
func Resolve(requested Requested, ev Evidence) Resolution {
        res := Resolution{
                Requested: NormalizeRequested(string(requested)),
                Backend:   KindCPU,
                Selected:  KindCPU,
                Available: availableBackends(ev),
                Fallback:  "none",
        }

        gpuDevice := bestGPUDevice(ev)

        // --- forced CPU ------------------------------------------------------
        if res.Requested == RequestCPU {
                res.AutoProfile = ProfileCPUSafe
                res.Reason = "requested CPU"
                res.ExecutionVerified = true
                res.Verification = VerificationCPUExecution
                return res
        }

        // --- the NPU gate chain (reached for AUTO/NPU requests) ---------------
        if res.Requested == RequestNPU || res.Requested == RequestAuto {
                if backend, device, reason, ok := resolveNPU(ev); ok {
                        if res.Requested == RequestNPU || npuBeatsGPU(ev) {
                                res.Backend = backend
                                res.Selected = backend
                                res.Device = device
                                res.AutoProfile = ProfileLowPowerNPU
                                res.Reason = reason
                                // NPU selection requires the FULL gate chain
                                // INCLUDING a measured benchmark — the only
                                // path here that can be verified-selected.
                                res.ExecutionVerified = true
                                res.Verification = VerificationNPUBench
                                return res
                        }
                        res.Fallbacks = append(res.Fallbacks, "NPU usable but measured GPU throughput is higher")
                } else if res.Requested == RequestNPU || ev.NPUPresent {
                        res.Fallbacks = append(res.Fallbacks, reason)
                }
        }

        // --- the GPU gate chain ------------------------------------------------
        if res.Requested == RequestGPU || res.Requested == RequestAuto {
                gpuDevice, gpuReason, gpuOK, verified, how := resolveGPU(ev, gpuDevice)

                if gpuOK {
                        res.Backend = KindGPUVulkan
                        res.Selected = KindGPUVulkan
                        res.Device = gpuDevice.Name
                        res.GPULayers = -1 // all layers (llama.cpp 99 semantics)
                        res.AutoProfile = autoProfileFor(ev.Workload)
                        res.Reason = gpuReason
                        res.ExecutionVerified = verified
                        res.Verification = how

                        if verified {
                                res.Fallback = "none"
                        } else {
                                // Selected but UNVERIFIED: the CPU path is the guaranteed
                                // fallback — GPU detection is not GPU execution.
                                res.Fallback = "safe"
                        }

                        applyWorkloadTuning(&res, ev)
                        return res
                }

                res.Fallbacks = append(res.Fallbacks, gpuReason)
        }

        // --- fall back to CPU ---------------------------------------------------
        res.AutoProfile = ProfileCPUSafe
        res.Selected = KindCPU
        res.Reason = "no accelerator proven usable by measured evidence — CPU"
        res.ExecutionVerified = true
        res.Verification = VerificationCPUExecution
        res.Fallback = "none"
        return res
}

// availableBackends lists the backends the evidence shows are AVAILABLE
// (never claiming verified execution for any of them — availability and
// verification are different claims).
func availableBackends(ev Evidence) []Kind {
        out := []Kind{KindCPU}

        gpu := bestGPUDevice(ev)
        if (gpu != nil && gpu.Source == "engine-enumeration") ||
                strings.TrimSpace(ev.RuntimeOffloadEvidence) != "" ||
                (!ev.EnumerationSupported && ev.VulkanBackendPresent) {
                out = append(out, KindGPUVulkan)
        }

        if ev.NPUPresent {
                out = append(out, KindNPUOpenVINO)
        }

        return out
}

// bestGPUDevice picks the engine-enumerated GPU with the most memory.
func bestGPUDevice(ev Evidence) *Device {
        var best *Device

        for i := range ev.EngineDevices {
                d := &ev.EngineDevices[i]
                if best == nil || d.TotalMB > best.TotalMB {
                        best = d
                }
        }

        return best
}

// resolveGPU applies the GPU evidence gates. The v1.8.6 truth model
// (GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU
// verified) maps onto the returns:
//
//      Gate 1 — the engine enumerated a device: SELECTS GPU_VULKAN with the
//                device identity attributed, but verified=FALSE — enumeration
//                is selection/provisioning evidence, NOT execution proof
//                (--list-devices never offloads a layer).
//      Gate 2 — the engine LOG measured real offload during the CURRENT
//                boot: SELECTS and VERIFIES (runtime execution evidence).
//      Gate 2b — a still-valid EXECUTION RECEIPT (committed bounded
//                transaction: real generation + measured offload evidence,
//                identity-checked against the current engine tag/variant):
//                SELECTS and VERIFIES.
//      Gate 3 — the documented weaker fallback (Vulkan DLL present,
//                enumeration unsupported): SELECTS, verified=FALSE.
func resolveGPU(ev Evidence, device *Device) (dev Device, reason string, ok bool, verified bool, how string) {
        // Gate 2 (strongest — a measured offload line from the current boot).
        if strings.TrimSpace(ev.RuntimeOffloadEvidence) != "" {
                d := Device{}
                if device != nil {
                        d = *device
                }
                return d, "runtime engine log measured real GPU offload: " + ev.RuntimeOffloadEvidence,
                        true, true, VerificationOffloadLog
        }

        // Gate 2b — a committed, still-valid execution receipt (stale-evidence
        // invalidation is the identity check inside ValidFor).
        if ev.GPUExecutionReceipt != nil &&
                ev.GPUExecutionReceipt.ValidFor(ev.EngineTag, ev.EngineVariant) {
                d := Device{}
                if device != nil {
                        d = *device
                }
                if ev.GPUExecutionReceipt.Device != "" && d.Name == "" {
                        d.Name = ev.GPUExecutionReceipt.Device
                }
                return d, fmt.Sprintf(
                                "committed GPU execution receipt for engine %s/%s (device %q, model %q): real generation and measured offload evidence",
                                ev.GPUExecutionReceipt.EngineTag, ev.GPUExecutionReceipt.Variant,
                                ev.GPUExecutionReceipt.Device, ev.GPUExecutionReceipt.Model,
                        ), true, true, VerificationReceipt
        }

        // Gate 1 — the engine itself enumerated a device: SELECTION evidence.
        // It may select/provision GPU_VULKAN (with the CPU safety net) but it
        // can NEVER verify execution: --list-devices lists devices; it does
        // not offload a single layer.
        if device != nil && device.Source == "engine-enumeration" {
                return *device, fmt.Sprintf(
                        "engine enumerated %s device %q (%d MB) via --list-devices — selected; execution verification pending real GPU-offload evidence",
                        device.Backend, device.Name, device.TotalMB,
                        ), true, false, VerificationPendingExecution
        }

        // Gate 3 (documented fallback — WEAKER): the engine build does not
        // support enumeration AND no execution evidence exists, but a Vulkan
        // backend is installed. Selected and explicitly UNVERIFIED.
        if !ev.EnumerationSupported && ev.VulkanBackendPresent {
                return Device{}, "device enumeration unsupported by this engine build; Vulkan backend present — offload to be verified from the runtime log", true, false, VerificationPendingLog
        }

        return Device{}, "no enumerated GPU device and no runtime execution evidence", false, false, ""
}

// resolveNPU applies the FULL NPU gate chain. Every gate must pass.
func resolveNPU(ev Evidence) (backend Kind, device string, reason string, ok bool) {
        if !ev.NPUPresent {
                return "", "", "no NPU measured on this machine", false
        }

        if !ev.OpenVINO.RuntimePresent {
                return "", "", "OpenVINO runtime not measured as available", false
        }

        if !npuSupportedArchs[strings.ToLower(strings.TrimSpace(ev.Model.Architecture))] {
                return "", "", fmt.Sprintf(
                        "model architecture %q not in the measured NPU-supported set",
                        ev.Model.Architecture,
                ), false
        }

        if !npuSupportedQuants[strings.ToUpper(strings.TrimSpace(ev.Model.Quantization))] {
                return "", "", fmt.Sprintf(
                        "model quantization %q not in the measured NPU-supported set",
                        ev.Model.Quantization,
                ), false
        }

        // The benchmark gate: NPU never wins on PRESENCE — only measured
        // throughput for a comparable workload justifies the low-power posture.
        if ev.NPUBenchmarkTokPerSec <= 0 {
                return "", "", "NPU never benchmarked for this workload — refusing to select it on presence alone", false
        }

        return KindNPUOpenVINO, ev.NPUName,
                fmt.Sprintf(
                        "NPU %q measured usable (OpenVINO runtime present, model supported, %.1f tok/s measured)",
                        ev.NPUName, ev.NPUBenchmarkTokPerSec,
                ), true
}

// npuBeatsGPU reports whether the MEASURED NPU throughput beats the
// measured GPU throughput (both must exist).
func npuBeatsGPU(ev Evidence) bool {
        return ev.NPUBenchmarkTokPerSec > 0 &&
                ev.GPUBenchmarkTokPerSec > 0 &&
                ev.NPUBenchmarkTokPerSec > ev.GPUBenchmarkTokPerSec
}

// autoProfileFor picks the AUTO runtime posture for a workload.
func autoProfileFor(w Workload) AutoProfile {
        switch w {
        case WorkloadVision:
                return ProfileVisionGPU
        case WorkloadLong, WorkloadMaximum:
                return ProfileMaximum
        default:
                return ProfileInteractiveGPU
        }
}

// applyWorkloadTuning fills the context/batch posture fields from the
// measured memory budget (bounded, honest — no invented "optimal" values).
func applyWorkloadTuning(res *Resolution, ev Evidence) {
        switch res.AutoProfile {
        case ProfileMaximum:
                if ev.AvailableRAMMB >= 8192 {
                        res.Context = 16384
                } else if ev.AvailableRAMMB >= 4096 {
                        res.Context = 8192
                }
                res.Batch = 1024
        case ProfileInteractiveGPU:
                if ev.AvailableRAMMB >= 4096 {
                        res.Context = 8192
                }
                res.Batch = 512
        }
}

// Describe renders the one-line resolution summary for logs and the UI.
// The explainable form carries the evidence state explicitly:
// selected/executionVerified/reason/fallback — never a bare backend name.
func (r Resolution) Describe() string {
        s := fmt.Sprintf("requested=%s selected=%s executionVerified=%t",
                r.Requested, r.Backend, r.ExecutionVerified)

        if r.Device != "" {
                s += " device=" + r.Device
        }

        if r.AutoProfile != "" {
                s += " profile=" + string(r.AutoProfile)
        }

        s += " reason=" + r.Reason

        s += " fallback=" + r.Fallback

        return s
}
