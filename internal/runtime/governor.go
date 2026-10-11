// governor.go — v1.8.0 Runtime Governor wiring on the Stack.
//
// ONE closed loop, built entirely on existing authorities:
//
//   - the EXISTING preflight.LiveMonitor keeps its sampler, cadence,
//     hysteresis and the critical-protection path (cooperative
//     cancellation of active runs) — untouched;
//   - the SAME poll loop now also feeds the accepted sample to the
//     Governor, which folds it into the resource state and recomputes
//     the envelope;
//   - the Governor exposes POLICY (envelope, admission, self-model);
//     it executes nothing.
//
// Seams injected here:
//
//   - CPU load: governor.CPULoadPlatform (measured where the platform
//     can measure; unknown elsewhere — never guessed);
//   - engine facts: the EXISTING engine lifecycle owner (llm.Backend
//     Metrics — pid/model/RSS), read-only;
//   - active runs: the EXISTING Stack run counter.
package runtime

import (
        "context"
        "fmt"
        "os"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/governor"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

// StartGovernor builds the ONE runtime policy authority over the existing
// live monitor's poll path. It is idempotent and safe to call before or
// after StartLiveMonitor — without a monitor the Governor still runs, fed
// at whatever cadence the wiring observes.
func (s *Stack) StartGovernor() {
        if s.governor != nil {
                return // already started
        }

        g := governor.New(
                governor.DefaultThresholds(),
                governor.CPULoadPlatform,
                s.governorEngineFacts,
                nil,
        )
        g.SetActiveRunsSource(s.ActiveRuns)

        // v1.8.6: the CURRENT inference workload's memory model (model
        // weights as a file fact + the planned KV-cache at the serving
        // window), read from the EXISTING model-card/context authorities
        // — the Governor's resource state now accounts for what the
        // machine is actually serving (policy only, no probing here).
        g.SetInferenceSource(s.inferenceFootprint)

        s.governorMu.Lock()
        if s.governor != nil { // lost the race — keep the existing one
                s.governorMu.Unlock()
                return
        }
        s.governor = g
        s.governorMu.Unlock()

        logging.Default().Info("resources", "runtime governor active: policy authority wired to the live monitor (inference footprint accounting on)")
}

// inferenceFootprint reads the current inference workload's memory model
// from the EXISTING authorities — the loaded/selected model's GGUF card
// (file facts) and the planned KV-cache at the serving window. It is a
// READ of existing facts, not a new planner:
//
//   - ModelBytes is the model FILE size (a file fact — never presented
//     as "RAM used"; the engine's RSS is the measured residency, folded
//     separately from the engine facts);
//   - KVBytes is llm.KVCacheBytes (the existing planner) at the serving
//     window (the engine's verified window when it is smaller than the
//     configured one);
//   - unknown stays unknown (no card, no model, remote) — the Governor
//     never invents a footprint.
func (s *Stack) inferenceFootprint() governor.InferenceFootprint {
        cfg := s.Src.Load()

        if cfg.IsRemote() || cfg.Model == "" {
                return governor.InferenceFootprint{}
        }

        path, err := llm.ResolveModelPath(cfg.ModelsDir, cfg.Model)
        if err != nil {
                return governor.InferenceFootprint{}
        }

        card, err := llm.ReadModelCard(path)
        if err != nil || card == nil {
                return governor.InferenceFootprint{}
        }

        var modelBytes int64
        if fi, err := os.Stat(path); err == nil {
                modelBytes = fi.Size()
        }

        window := cfg.LLM.NumCtx
        if s.Llama != nil {
                if vc := s.Llama.VerifiedContext(); vc > 0 && (window <= 0 || vc < window) {
                        window = vc
                }
        }

        kv := llm.KVCacheBytes(card, window, cfg.EffectiveKVCacheQuant())
        if modelBytes <= 0 && kv <= 0 {
                return governor.InferenceFootprint{}
        }

        return governor.InferenceFootprint{
                ModelBytes: modelBytes,
                KVBytes:    kv,
                Known:      true,
        }
}

// GovernorAdmitsModelLoad is the v1.8.6 resource-aware run gate (§7):
// BEFORE an expensive model load/generation boot, the ONE policy
// authority decides whether the current resource envelope admits it.
//
//   - The resident plan comes from the EXISTING preflight resource
//     assessment (llm.AssessContextResource through PreflightReport —
//     weights + KV at the window + runtime overhead, the same numbers
//     /api/preflight reports);
//   - The Governor is consulted ONLY when it has folded a MEASURED
//     resource state (monitor samples with known available RAM): its
//     pressure envelope + resident budget policy then applies (unknown
//     pressure never blocks — the preflight gate's own measured RAM
//     evaluation remains the authority in that case, exactly as before);
//   - A deferral is TRUTHFUL: the error carries the Governor's explainable
//     reason (pressure level, sustained duration, budget arithmetic).
//
// This is not a second scheduler: it is the existing AdjustNextRun
// adjustment class made effective at the one seam where heavyweight
// work starts.
func (s *Stack) GovernorAdmitsModelLoad() error {
        g := s.Governor()
        if g == nil {
                return nil
        }

        st := g.State()
        if !st.AvailableKnown {
                // No folded measurement yet: the preflight gate (measured
                // RAM snapshot) already evaluated this load — the Governor
                // adds no policy on top of an unmeasured state.
                return nil
        }

        plan := s.modelLoadMemoryPlan()
        if plan == nil {
                // No computable resident plan (no card facts): the
                // preflight gate's conservative assessment owns it.
                return nil
        }

        decision := g.AdmitMemory(*plan)
        if !decision.Admitted {
                logging.Default().Warn("resources", "model load deferred by the runtime governor: %s", decision.Why)
                return fmt.Errorf("runtime governor deferred the model load: %s", decision.Why)
        }

        logging.Default().Info("resources", "model load admitted by the runtime governor: %s", decision.Why)
        return nil
}

// modelLoadMemoryPlan derives the resident memory plan for the upcoming
// model load from the ONE resource assessment authority — the same
// preflight evaluation the run gate and /api/preflight already use.
// nil when no plan is computable (no model/remote/unknown facts).
func (s *Stack) modelLoadMemoryPlan() *governor.MemoryPlan {
        cfg := s.Src.Load()

        if cfg.IsRemote() || cfg.Model == "" {
                return nil
        }

        pre := s.PreflightReport(0)
        if pre.Requirements.TotalBytes <= 0 {
                return nil
        }

        return &governor.MemoryPlan{
                Workload:      "model load: " + cfg.Model,
                ResidentBytes: pre.Requirements.TotalBytes,
        }
}

// Governor exposes the ONE runtime policy authority (nil before
// StartGovernor — callers must be nil-safe).
func (s *Stack) Governor() *governor.Governor {
        s.governorMu.Lock()
        defer s.governorMu.Unlock()
        return s.governor
}

// governorEngineFacts reads the engine lifecycle owner's published facts.
// Read-only: the Governor never starts, stops or probes engine processes.
func (s *Stack) governorEngineFacts() governor.EngineFacts {
        eng := s.Engine()
        if eng == nil {
                return governor.EngineFacts{}
        }

        m, err := eng.Metrics(context.Background())
        if err != nil {
                // A metrics read failure is an honest unknown, not a fake
                // "engine stopped": the engine may be perfectly alive.
                return governor.EngineFacts{}
        }

        facts := governor.EngineFacts{
                Running: m.Pid != 0,
                Model:   m.Model,
        }

        if m.ProcessRSSBytes > 0 {
                facts.RSSBytes = int64(m.ProcessRSSBytes)
                facts.Known = true
        }

        return facts
}

// observeGovernor folds one accepted monitor sample into the Governor.
// Called from the SAME poll loop that drives the monitor — one cadence,
// one thread, no second scheduler.
func (s *Stack) observeGovernor(sample preflight.Sample) {
        g := s.Governor()
        if g == nil {
                return
        }

        g.Observe(sample)
}
