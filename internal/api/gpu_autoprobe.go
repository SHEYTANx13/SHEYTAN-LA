package api

// gpu_autoprobe.go — v1.7.2 (P0, §4/§9): the AUTO GPU candidate
// bootstrap — the ELIGIBILITY side.
//
// The llm side (internal/llm/variant_auto.go) owns the bounded transaction
// state; THIS side owns the "may AUTO consider a Vulkan candidate at all
// right now?" decision. Every gate is evidence-based and every skip is
// recorded with its exact reason (deferrals are NOT persisted — only the
// llm side persists terminal outcomes, keyed by hardware+engine identity).
//
// The eligibility chain (all must hold):
//
//      AUTO requested (not CPU/GPU/NPU explicit, not remote)
//      → platform serves a Vulkan package (Windows x64)
//      → a host GPU is DETECTED (hardware probe — OS GPU detection only;
//        it is NOT Vulkan capability, NOT a device, NOT offload proof)
//      → GPU offload is not disabled by config
//      → a model is selected (the candidate must load a REAL GGUF)
//      → the installed engine is not already a verified-usable Vulkan engine
//      → no persisted failed probe for the current identity (bounded)
//      → no active run / no calibration in flight
//      → online
//      → FINAL PREFLIGHT passes (hard incompatibility or severe transient
//        pressure refuses the candidate)
//      → ONE bounded transaction through the existing variant authority
//
// The trigger runs strictly AFTER the startup maintenance gate completes
// (never racing the maintenance transaction or the prewarm release), in
// the background — app startup never blocks on it.

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// autoProbeTimeout bounds the whole AUTO candidate attempt (download,
// swap, start, verification, rollback included).
const autoProbeTimeout = 20 * time.Minute

// maybeAutoProvisionVulkan is the one-shot AUTO GPU candidate bootstrap.
// It waits for the startup maintenance gate, evaluates eligibility, and
// runs at most ONE bounded candidate transaction per server lifetime.
func (s *Server) maybeAutoProvisionVulkan(gateDone <-chan struct{}) {
	// Bound the wait for the maintenance gate: if maintenance hangs, the
	// probe is simply not run this boot (it is an optimization, never a
	// dependency).
	select {
	case <-gateDone:
	case <-time.After(10 * time.Minute):
		logging.Default().Warn("gpu", "AUTO Vulkan bootstrap skipped: the startup maintenance gate did not complete in time")
		return
	case <-s.engineStop:
		return
	}

	select {
	case <-s.engineStop:
		return
	default:
	}

	identity, eligible, reason := s.autoVulkanEligibility()
	if !eligible {
		logging.Default().Info("gpu", "AUTO Vulkan candidate not considered this boot: %s", reason)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), autoProbeTimeout)
	defer cancel()

	select {
	case <-s.engineStop:
		return
	case <-ctx.Done():
		return
	default:
	}

	outcome := s.llama.AutoProvisionVulkanIfWorthy(ctx, identity)
	if outcome.Err != nil {
		// The transaction already rolled back, persisted the bounded
		// failure, and told the exact truth in the logs — this line is
		// the aggregated app-level record.
		logging.Default().Warn("gpu", "AUTO Vulkan candidate transaction failed — CPU posture remains the honest fallback (%v)", outcome.Err)
		return
	}
	if outcome.Skipped {
		logging.Default().Info("gpu", "AUTO Vulkan candidate skipped: %s", outcome.Note)
		return
	}
	logging.Default().Info("gpu", "AUTO Vulkan candidate committed: %s", outcome.Note)
}

// autoVulkanEligibility evaluates every gate and returns the probe
// identity with a distinct skip reason per failed gate.
func (s *Server) autoVulkanEligibility() (identity string, eligible bool, reason string) {
	cfg := s.src.Load()

	// Requested profile must be AUTO (the explicit GPU profile goes
	// through the user-facing provisioning surface; CPU/NPU never
	// bootstraps a Vulkan candidate).
	if accelerator.NormalizeRequested(cfg.Accelerator) != accelerator.RequestAuto {
		return "", false, "requested accelerator profile is not AUTO"
	}

	if cfg.IsRemote() {
		return "", false, "a remote inference provider is active"
	}

	// Platform support matrix (Windows x64 serves the Vulkan prebuilt).
	if !updater.VariantSupported(updater.VariantVulkan) {
		return "", false, "no prebuilt Vulkan engine package for " + runtime.GOOS + "/" + runtime.GOARCH
	}

	// A REAL model must be loadable by the candidate.
	if strings.TrimSpace(cfg.Model) == "" {
		return "", false, "no model selected — the candidate verification needs a real GGUF to load"
	}
	if _, err := llm.ResolveModelPath(cfg.ModelsDir, cfg.Model); err != nil {
		return "", false, "the selected model cannot be resolved: " + err.Error()
	}

	// GPU offload must not be disabled by configuration — a candidate
	// launched with zero offload can never produce offload evidence.
	//
	// v1.8.4 (P0-B): this gate now sees only an EXPLICIT OFF. The
	// pre-1.8.4 recommendation pipeline wrote gpuAutoOffload=false as a
	// derived posture ("CPU-only until a Vulkan engine build is
	// provisioned") that then landed here indistinguishable from a
	// user decision — the circularity that kept AUTO CPU-only forever.
	// config.Load repairs that derived state once (repairDerivedGPUPosture)
	// and every recommendation path stopped writing it, so a remaining
	// false posture under AUTO is a genuine user OFF and is respected
	// exactly as the explicit-OFF contract requires.
	if !cfg.GPUAutoOffload && cfg.LLM.NumGPU <= 0 {
		return "", false, "GPU offload is explicitly disabled in settings (numGPU=0, auto-offload off) — AUTO respects the user's explicit OFF and does not provision a GPU candidate"
	}

	// No run may be interrupted by the package swap.
	if s.anyRunActive() {
		return "", false, "a run is active"
	}
	if s.calibrating.Load() {
		return "", false, "an automatic calibration is in flight"
	}

	if netcheck.IsOffline() {
		return "", false, "offline"
	}

	// Host GPU detection — the hardware probe (deep facts when needed).
	// NOTE: a detected OS GPU is ONLY the entry ticket; it is not Vulkan
	// capability, not an enumerated device, and not offload proof.
	hw := hardware.Snapshot(cfg)
	if !hw.DeepReady {
		hw = hardware.Collect(cfg)
	}
	if !hw.HasGPU() {
		return "", false, "no host GPU detected by the hardware probe"
	}

	// Identity fingerprint for the bounded state (OS/arch + GPU identity
	// + driver + engine tag + variant).
	identity = llm.GPUProbeIdentity(
		runtime.GOOS, runtime.GOARCH,
		updater.InstalledEngineTag(cfg),
		string(updater.VariantVulkan),
		hardwareGPUFingerprint(hw),
	)

	// Already a verified-usable Vulkan engine? Reuse, never re-probe:
	// the installed variant IS vulkan and the engine's own enumeration
	// still reports a usable Vulkan device.
	if updater.InstalledEngineVariant(cfg) == updater.VariantVulkan {
		bin := updater.EngineBinaryPath(cfg)
		devices, supported, err := llm.EnumerateEngineDevices(bin)
		if err == nil && supported && hasVulkanDevice(devices) {
			return identity, false,
				"the installed Vulkan engine still enumerates a usable Vulkan device — verified state reused, no candidate needed"
		}
		// Vulcan variant installed but no usable device: the CPU posture
		// continues to serve (the Vulkan package is additive); a probe
		// cannot improve on it and is not repeated for this identity.
		if st := s.llama.AutoGPUProbeSnapshot(); st != nil && st.Identity == identity {
			return identity, false, "bounded: " + st.Status + " probe already recorded for this identity (" + st.Reason + ")"
		}
		return identity, false,
			"the installed Vulkan engine currently enumerates no usable Vulkan device (driver/device state) — CPU fallback continues"
	}

	// Persisted failed probe for the CURRENT identity → bounded skip
	// (the llm side re-checks too; this avoids even logging a request).
	if st := s.llama.AutoGPUProbeSnapshot(); st != nil &&
		st.Status == llm.GPUProbeStatusFailed && st.Identity == identity {
		return identity, false, "previous AUTO Vulkan candidate failed for the current hardware+engine identity — bounded, not retried: " + st.Reason
	}

	// FINAL PREFLIGHT — the existing authority evaluates the CURRENT
	// model/backend/resource combination. Hard incompatibility or severe
	// transient pressure refuses the candidate before any download.
	if s.stack != nil {
		report := s.stack.PreflightReport(0)
		switch report.Severity {
		case preflight.SeverityIncompatible:
			return identity, false, "final preflight: hard incompatibility (" + strings.Join(report.Reasons, "; ") + ")"
		case preflight.SeverityCriticalPressure:
			return identity, false, "final preflight: severe resource pressure (" + strings.Join(report.Reasons, "; ") + ") — candidate deferred"
		}
	}

	return identity, true, ""
}

// hasVulkanDevice reports whether the engine's enumeration carries a
// Vulkan device.
func hasVulkanDevice(devices []accelerator.Device) bool {
	for _, d := range devices {
		if strings.HasPrefix(d.Backend, "Vulkan") {
			return true
		}
	}
	return false
}

// hardwareGPUFingerprint renders the GPU identity (name + driver when
// measurable) for the probe state key.
func hardwareGPUFingerprint(hw hardware.Profile) string {
	parts := make([]string, 0, len(hw.GPUs))
	for _, g := range hw.GPUs {
		p := g.Name
		if g.DriverVer != "" {
			p += " (driver " + g.DriverVer + ")"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "; ")
}
