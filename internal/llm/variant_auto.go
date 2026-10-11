package llm

// variant_auto.go — v1.7.2 (P0, §4/§8/§9): the AUTO GPU candidate probe.
//
// THE v1.7.1 DEADLOCK: the accelerator resolver correctly refuses to claim
// GPU_VULKAN without runtime evidence — but with only the CPU engine
// installed, Vulkan evidence can NEVER appear: ggml-vulkan.dll is not even
// on disk. Selection was circular:
//
//      need Vulkan evidence to select Vulkan
//      while
//      need Vulkan installed to obtain Vulkan evidence.
//
// THE FIX: AUTO may perform ONE bounded Vulkan candidate transaction —
// through the EXISTING transactional engine-variant authority
// (updateEngineVariantTx: stop → stage → closure-validate → install →
// start → health → verify → commit/rollback), extended with the stricter
// EXECUTION-EVIDENCE verification:
//
//      GPU detected
//      → FINAL PREFLIGHT (caller-side eligibility)
//      → Vulkan variant supported?
//      → resolve exact Vulkan asset (existing resolver)
//      → stage candidate (existing installer, package closure validated)
//      → candidate --list-devices (existing enumeration)
//      → usable Vulkan device?
//      → start candidate (existing lifecycle; loads the REAL configured GGUF)
//      → health ready (existing startup state machine)
//      → REAL generation (bounded 1-token probe through the serving engine)
//      → real GPU-offload evidence? ("offloaded N/N layers to GPU")
//      → COMMIT or ROLLBACK
//
// Failure path: the transaction's existing stop → reap → rollback →
// last-known-good restart choreography, plus a TRUTHFUL CPU posture —
// nothing is ever claimed without the evidence layer that proves it.
//
// BOUNDED PROBING (§9): the outcome is persisted in gpu-probe.json, keyed
// by a HARDWARE+ENGINE identity fingerprint (OS/arch + GPU identity +
// driver identity + engine tag + variant). A FAILED probe for the same
// identity is never repeated — no download loop, no probe storm. A
// VERIFIED probe is reused after current-state validation. The state goes
// stale automatically when any identity component changes (new GPU,
// driver update, engine update) or the installed variant changes.
//
// This file creates NO second installer and NO second backend selector:
// it is a caller of the one variant transaction with a stricter verify
// hook plus a small, honest state store.

import (
        "bytes"
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "os"
        "path/filepath"
        "strings"
        "sync/atomic"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// gpuProbeFileName is the AUTO candidate state store, beside the config.
const gpuProbeFileName = "gpu-probe.json"

// generationProbeTimeout bounds the 1-token real-generation probe.
const generationProbeTimeout = 90 * time.Second

// GPU probe outcomes (the persisted status vocabulary).
const (
        GPUProbeStatusVerified = "verified"
        GPUProbeStatusFailed   = "failed"
)

// GPUProbeState is the persisted AUTO candidate outcome. Fields are
// evidence-first: every claim names where it came from.
type GPUProbeState struct {
        // Status is "verified" or "failed" (the only persisted outcomes —
        // deferrals are NOT persisted, so they can never block a later probe).
        Status string `json:"status"`

        // Identity is the fingerprint the outcome is keyed by (caller-built:
        // OS/arch + GPU identity + driver + engine tag + variant).
        Identity string `json:"identity"`

        // EngineTag / Variant identify the candidate that was probed.
        EngineTag string `json:"engineTag,omitempty"`
        Variant   string `json:"variant,omitempty"`

        // Device is the ACTUAL enumerated device identity (e.g.
        // "Vulkan0: Intel(R) Arc(TM) A770M Graphics") — never a hard-coded
        // name. Empty when enumeration was unsupported.
        Device string `json:"device,omitempty"`

        // Evidence is the captured runtime execution evidence line.
        Evidence string `json:"evidence,omitempty"`

        // EvidenceSource names where the evidence came from
        // ("engine-enumeration", "engine-log").
        EvidenceSource string `json:"evidenceSource,omitempty"`

        // Model is the real GGUF the probe loaded.
        Model string `json:"model,omitempty"`

        // Reason is the exact outcome explanation — for failures, the
        // evidence layer that was missing.
        Reason string `json:"reason"`

        // At is the RFC3339 completion time.
        At string `json:"at"`
}

// autoProbeSeq numbers probe attempts in the AUTO-probe diagnostics —
// an ephemeral, identifier-free ordinal (v1.9.5 log contract: never a
// runId/sessionId, never a per-event flood; one line per lifecycle
// transition with counters/ordinals only).
var autoProbeSeq atomic.Int64

// gpuEvent emits one stable AUTO-probe lifecycle diagnostic (§22) with
// the stable fields where available. Normal logs stay readable: one line
// per lifecycle transition, secrets never included.
// v1.9.5 LOG CONTRACT: the probe ordinal (`probe=%d`) is an ephemeral
// per-process counter — never a runId/sessionId and never user content.
// The identifier-shaped field the v1.9.4 line carried is gone.
func gpuEvent(event string, probe int64, engineTag, variant, device, model, evidenceSource, verification, reason string) {
        logging.Default().Info("gpu",
                "%s probe=%d engineTag=%s variant=%s device=%s model=%s evidenceSource=%s verification=%s reason=%s",
                event, probe, engineTag, variant, device, model, evidenceSource, verification, reason)
}

// GPUProbeOutcome is the in-memory result of one AUTO candidate attempt.
type GPUProbeOutcome struct {
        // Skipped is true when NO transaction ran (bounded reuse, stale
        // failure record, or already-verified state).
        Skipped bool
        // State is the resulting persisted state (nil when skipped without
        // a state change).
        State *GPUProbeState
        // Note is the human-readable outcome.
        Note string
        // Err is the transaction error when the candidate failed (never nil
        // for a failed probe — the exact reason travels in the error chain).
        Err error
}

// gpuProbePath resolves the state store location.
func gpuProbePath(cfg *config.Config) string {
        return filepath.Join(cfg.DataDir, gpuProbeFileName)
}

// LoadGPUProbeState reads the persisted AUTO candidate state (ok=false
// when absent or unreadable).
func LoadGPUProbeState(cfg *config.Config) (GPUProbeState, bool) {
        data, err := os.ReadFile(gpuProbePath(cfg))
        if err != nil {
                return GPUProbeState{}, false
        }
        var st GPUProbeState
        if err := json.Unmarshal(data, &st); err != nil {
                return GPUProbeState{}, false
        }
        return st, true
}

// saveGPUProbeState persists the state atomically (tmp + rename).
func saveGPUProbeState(cfg *config.Config, st GPUProbeState) {
        data, err := json.MarshalIndent(st, "", "  ")
        if err != nil {
                return
        }
        path := gpuProbePath(cfg)
        tmp := path + ".tmp"
        if err := os.WriteFile(tmp, data, 0o600); err != nil {
                return
        }
        _ = os.Rename(tmp, path)
}

// GPUProbeIdentity builds the identity fingerprint the probe outcome is
// keyed by: OS/arch + GPU identity (name + driver when measurable) +
// engine tag + variant. Any change (new GPU, driver update, engine
// update) invalidates the persisted outcome automatically.
//
// gpuIdentity is supplied by the caller (the API layer owns the hardware
// probe — internal/hardware imports this package, so the dependency must
// not be inverted).
func GPUProbeIdentity(goos, goarch, engineTag, variant, gpuIdentity string) string {
        return strings.Join([]string{
                goos + "/" + goarch,
                "gpu=" + gpuIdentity,
                "engine=" + engineTag,
                "variant=" + variant,
        }, "|")
}

// AutoGPUProbeSnapshot exposes the persisted AUTO candidate state for the
// API/diagnostics surface (nil when never probed).
func (s *LlamaServer) AutoGPUProbeSnapshot() *GPUProbeState {
        cfg := s.src.Load()
        if st, ok := LoadGPUProbeState(cfg); ok {
                return &st
        }
        return nil
}

// autoProbeEvidence is the structured evidence the verification hook
// collects during the candidate transaction (filled once — no
// re-enumeration after commit: one probe, one enumeration, bounded).
type autoProbeEvidence struct {
        Device         string
        Evidence       string
        EvidenceSource string
}

// autoProbeVerify builds the EXECUTION-EVIDENCE verification extension for
// the Vulkan candidate (§8). It runs after the serving health check and
// the engine's own device enumeration, inside the transaction:
//
//  1. a usable Vulkan DEVICE is required whenever enumeration is
//     supported (the actual enumerated identity — never a hard-coded
//     Vulkan0);
//  2. a REAL bounded generation must succeed on the serving engine;
//  3. REAL GPU-offload evidence ("offloaded N/N layers to GPU", captured
//     from the candidate's own load output) is REQUIRED — a generation
//     that succeeds without it is an UNVERIFIED CPU execution, which for
//     the AUTO candidate means the probe FAILED (its purpose was GPU
//     execution proof) and the transaction rolls back.
//
// Enumeration being unsupported does NOT fail the probe by itself when the
// offload line exists (the offload line is the stronger execution
// evidence); it only means the device identity is attributed to the
// engine log.
func (s *LlamaServer) autoProbeVerify(probe int64, ev *autoProbeEvidence) variantVerifyFunc {
        return func(cfg *config.Config, variant updater.AssetVariant, note string) (string, error) {
                bin := updater.EngineBinaryPath(cfg)

                gpuEvent("gpuDeviceEnumerationStarted", probe, updater.InstalledEngineTag(cfg), string(variant), "", "", "", "", "")

                devices, supported, err := enumerateEngineDevices(bin)
                if err != nil {
                        gpuEvent("gpuDeviceEnumerationFinished", probe, updater.InstalledEngineTag(cfg), string(variant), "", "", "", "failed", err.Error())
                        return "", fmt.Errorf("AUTO candidate device enumeration failed: %w", err)
                }

                var deviceIdentity string
                vulkanDevices := 0
                for _, d := range devices {
                        if strings.HasPrefix(d.Backend, "Vulkan") {
                                vulkanDevices++
                                if deviceIdentity == "" {
                                        deviceIdentity = d.Backend + ": " + d.Name
                                }
                        }
                }

                enumVerdict := fmt.Sprintf("supported=%v vulkanDevices=%d", supported, vulkanDevices)
                gpuEvent("gpuDeviceEnumerationFinished", probe, updater.InstalledEngineTag(cfg), string(variant), deviceIdentity, "", "engine-enumeration", enumVerdict, "")

                if supported && vulkanDevices == 0 {
                        return "", fmt.Errorf(
                                "the Vulkan candidate engine enumerated NO usable Vulkan device on this machine (enumeration supported, none reported) — the AUTO candidate cannot be proven, rolling back to the CPU package")
                }

                // The candidate is serving the real configured GGUF at this point
                // (the transaction's start step loads it and the startup state
                // machine verified health + model).
                model := s.LoadedModel()

                gpuEvent("gpuCandidateStarted", probe, updater.InstalledEngineTag(cfg), string(variant), deviceIdentity, filepath.Base(model), "", "health-ready", note)
                gpuEvent("gpuModelLoaded", probe, updater.InstalledEngineTag(cfg), string(variant), deviceIdentity, filepath.Base(model), "", "startup-state-machine", "")

                // REAL bounded generation through the serving engine.
                if gerr := probeServingGeneration(cfg); gerr != nil {
                        return "", fmt.Errorf("AUTO candidate generation probe failed: %w", gerr)
                }

                // The GPU-offload evidence: captured from the candidate's own
                // model-load output by the line writers (ParseOffloadLine).
                evidence := s.OffloadEvidence()
                if strings.TrimSpace(evidence) == "" {
                        return "", fmt.Errorf(
                                "the Vulkan candidate served a real generation but produced NO GPU-offload evidence (no \"offloaded N/M layers to GPU\" line) — execution is UNVERIFIED; refusing to claim GPU_VULKAN without execution proof")
                }

                source := "engine-enumeration"
                if deviceIdentity == "" {
                        source = "engine-log"
                        deviceIdentity = "unattributed (enumeration unsupported; offload evidence from the engine log)"
                }

                ev.Device = deviceIdentity
                ev.Evidence = evidence
                ev.EvidenceSource = source

                gpuEvent("gpuExecutionEvidence", probe, updater.InstalledEngineTag(cfg), string(variant), deviceIdentity, filepath.Base(model), source, "verified", evidence)

                return fmt.Sprintf(
                        "AUTO candidate execution verified: device %q, real generation succeeded, offload evidence %q",
                        deviceIdentity, evidence), nil
        }
}

// probeServingGeneration runs ONE bounded real generation against the
// serving engine (the raw /completion endpoint — present in every
// llama-server build, independent of chat-template flags). It proves the
// candidate executes, not merely that it listens.
func probeServingGeneration(cfg *config.Config) error {
        base := fmt.Sprintf("http://%s:%d", effectiveHost(cfg), cfg.LlamaPort)

        ctx, cancel := context.WithTimeout(context.Background(), generationProbeTimeout)
        defer cancel()

        body := []byte(`{"prompt":"ping","n_predict":1,"temperature":0.1}`)
        req, err := http.NewRequestWithContext(ctx, http.MethodPost,
                base+"/completion", bytes.NewReader(body))
        if err != nil {
                return err
        }
        req.Header.Set("Content-Type", "application/json")

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return fmt.Errorf("generation probe: %w", err)
        }
        defer resp.Body.Close()
        _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

        if resp.StatusCode != http.StatusOK {
                return fmt.Errorf("generation probe: HTTP %d", resp.StatusCode)
        }
        return nil
}

// effectiveHost resolves the engine host for the generation probe (the
// loopback literal when bound to all interfaces).
func effectiveHost(cfg *config.Config) string {
        h := cfg.LlamaHost
        if h == "0.0.0.0" || h == "" {
                return "127.0.0.1"
        }
        if h == "::" || h == "[::]" {
                return "[::1]"
        }
        return h
}

// AutoProvisionVulkanIfWorthy runs the bounded AUTO Vulkan candidate
// transaction when the caller-side eligibility already passed. identity
// is the hardware+engine fingerprint (GPUProbeIdentity); the outcome is
// persisted keyed by it.
//
// The caller owns the eligibility decision (AUTO requested, platform
// support, GPU detected, final preflight, no active runs, a model
// selected); this side owns the bounded-state logic:
//
//      verified state for the same identity → reuse (no transaction)
//      failed  state for the same identity → bounded skip (no transaction)
//      anything else                        → ONE transaction
func (s *LlamaServer) AutoProvisionVulkanIfWorthy(ctx context.Context, identity string) GPUProbeOutcome {
        probe := autoProbeSeq.Add(1)

        cfg := s.src.Load()
        tag := updater.InstalledEngineTag(cfg)

        // Defensive: an already-Vulkan installation never re-probes through
        // this path (the caller-side eligibility owns that decision, but the
        // bounded state must hold here too — a no-op transaction returns
        // success WITHOUT any new evidence, which must never be recorded as
        // a verified probe).
        if updater.InstalledEngineVariant(cfg) == updater.VariantVulkan {
                return GPUProbeOutcome{Skipped: true, Note: "the installed engine already carries the vulkan variant — reuse the existing evidence, never re-probe"}
        }

        gpuEvent("gpuCandidateRequested", probe, tag, string(updater.VariantVulkan), "", filepath.Base(cfg.Model), "", "", "identity="+identity)

        // Bounded-state gates.
        if st, ok := LoadGPUProbeState(cfg); ok && st.Identity == identity {
                switch st.Status {
                case GPUProbeStatusVerified:
                        gpuEvent("gpuCommit", probe, st.EngineTag, st.Variant, st.Device, st.Model, st.EvidenceSource, "reused", "verified state for the current hardware+engine identity — no new candidate transaction")
                        return GPUProbeOutcome{Skipped: true, State: &st, Note: "verified AUTO Vulkan state reused for the current identity"}
                case GPUProbeStatusFailed:
                        gpuEvent("gpuRollback", probe, st.EngineTag, st.Variant, "", "", "", "bounded-skip", "failed probe for the current hardware+engine identity: "+st.Reason)
                        return GPUProbeOutcome{Skipped: true, State: &st, Note: "previous AUTO Vulkan candidate failed for this identity — bounded, not retried: " + st.Reason}
                }
        }

        gpuEvent("gpuAssetResolved", probe, tag, string(updater.VariantVulkan), "", "", "", "", "resolving through the variant-aware release resolver (current tag preferred)")

        // The evidence collector: filled ONCE by the verification hook inside
        // the transaction (one enumeration per probe — bounded).
        var evidence autoProbeEvidence

        outcome, err := s.updateEngineVariantTx(ctx, updater.VariantVulkan, nil, s.autoProbeVerify(probe, &evidence))

        state := GPUProbeState{
                Status:    GPUProbeStatusFailed,
                Identity:  identity,
                EngineTag: updater.InstalledEngineTag(cfg),
                Variant:   string(updater.VariantVulkan),
                Model:     cfg.Model,
                At:        time.Now().UTC().Format(time.RFC3339),
        }

        if err != nil {
                // v1.9.4: a run-coordination refusal is a TEMPORARY
                // condition (a run became active inside the candidate
                // window), never an execution failure of the Vulkan
                // candidate on this hardware — it must NOT be persisted as
                // a bounded failed probe (that would permanently disable
                // the AUTO candidate for this identity). The probe is
                // skipped for this boot; a later boot with no active run
                // may reconsider.
                if errors.Is(err, ErrEngineBusyWithRun) {
                        gpuEvent("gpuRollback", probe, tag,
                                string(updater.VariantVulkan), "", "",
                                "", "deferred",
                                "a run became active during the candidate window — deferred for this boot, not a bounded failure")

                        return GPUProbeOutcome{
                                Skipped: true,
                                Note: "AUTO Vulkan candidate deferred: " +
                                        err.Error(),
                        }
                }

                state.Reason = err.Error()
                saveGPUProbeState(cfg, state)
                gpuEvent("gpuRollback", probe, state.EngineTag, state.Variant, "", filepath.Base(state.Model), "", "failed", state.Reason)
                logging.Default().Warn("gpu",
                        "AUTO Vulkan candidate probe failed (bounded — will not retry for this hardware+engine identity): %v", err)
                return GPUProbeOutcome{State: &state, Note: "AUTO Vulkan candidate failed: " + err.Error(), Err: err}
        }

        // Committed: the verification hook collected the structured evidence.
        state.Status = GPUProbeStatusVerified
        state.Device = evidence.Device
        state.Evidence = evidence.Evidence
        state.EvidenceSource = evidence.EvidenceSource
        state.Reason = outcome
        if state.EvidenceSource == "" {
                state.EvidenceSource = "engine-log"
        }
        if state.Device == "" {
                state.Device = "unattributed (see /api/perf accelerator resolution)"
        }
        saveGPUProbeState(cfg, state)

        gpuEvent("gpuCommit", probe, state.EngineTag, state.Variant, state.Device, filepath.Base(state.Model), state.EvidenceSource, "verified", state.Reason)

        logging.Default().Info("gpu",
                "AUTO Vulkan candidate committed: device %q, offload evidence %q — GPU_VULKAN is now backed by runtime execution evidence",
                state.Device, state.Evidence)

        return GPUProbeOutcome{State: &state, Note: outcome}
}
