// preflight.go — v1.7.1: the Stack-side preflight gate + live resource
// protection wiring.
//
// Stack.PreflightReport is the run-start gate's ONE evaluation entry: it
// resolves the SAME inputs the existing authorities own — the backend
// selection (SelectGenerationBackendDetailed), model capabilities
// (ResolveModelCapabilities), the engine-verified window
// (LlamaServer.EngineContextLimit), machine memory (hardware.Snapshot /
// sysinfo) — and hands them to the ONE report (preflight.Evaluate). It
// starts nothing and stops nothing.
package runtime

import (
        "context"
        "os"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/resources"
)

// PreflightReport evaluates the current model/backend/device/resource
// combination BEFORE any engine start. requestedContext <= 0 uses the
// configured context.
func (s *Stack) PreflightReport(requestedContext int) preflight.Report {
        cfg := s.Src.Load()

        if cfg.IsRemote() {
                // Remote inference runs on someone else's machine: the local
                // resource gate does not apply.
                return preflight.Report{
                        Backend:    "remote",
                        Compatible: true,
                        Severity:   preflight.SeverityOK,
                }
        }

        modelPath := cfg.Model
        if modelPath == "" {
                return preflight.Report{
                        Model:             "",
                        Backend:           "llama.cpp-cpu",
                        Compatible:        false,
                        Severity:          preflight.SeverityIncompatible,
                        Reasons:           []string{"no model is selected"},
                        RecommendedAction: "choose a model in the Model Selector first",
                }
        }

        // Backend selection: the EXISTING authority, unchanged.
        decision := llm.SelectGenerationBackendDetailed(cfg, s.nativeBackend, s.llamaBackend)
        backendLabel := decision.SelectedName
        if backendLabel != "native" {
                backendLabel = "llama.cpp-cpu"
                if llm.VulkanAvailable(cfg) {
                        backendLabel = "llama.cpp-vulkan"
                }
        }

        backendAvailable := true
        unavailableReason := ""
        if decision.SelectedName == "" || decision.Backend == nil {
                backendAvailable = false
                unavailableReason = decision.FallbackReason
        }

        // Model capabilities: the ONE capability authority.
        caps := llm.ResolveModelCapabilities(cfg, modelPath)

        // Engine-verified window (0 = unknown until the engine has run).
        engineLimit := 0
        if s.Llama != nil {
                engineLimit = s.Llama.EngineContextLimit()
        }

        // Context: the configured window is the run's default plan.
        if requestedContext <= 0 {
                requestedContext = cfg.LLM.NumCtx
        }

        // Machine memory: the EXISTING hardware snapshot (never blocks on the
        // deep probe; unknown stays unknown).
        hw := hardware.Snapshot(cfg)
        sys := llm.SystemMemory{
                TotalRAMBytes:     int64(hw.RAM.TotalBytes),
                AvailableRAMBytes: int64(hw.RAM.AvailableBytes),
                TotalVRAMBytes:    hw.VRAMBytes(),
        }

        report := preflight.Evaluate(preflight.Input{
                ModelPath:                modelPath,
                Card:                     nil, // card facts already folded into caps
                Caps:                     caps,
                Backend:                  backendLabel,
                Device:                   deviceLabel(hw, backendLabel),
                BackendAvailable:         backendAvailable,
                BackendUnavailableReason: unavailableReason,
                RequestedContext:         requestedContext,
                EngineContextLimit:       engineLimit,
                Sys:                      sys,
                NativeEnabled:            cfg.NativeBackendEnabled(),
                KVQuant:                  cfg.EffectiveKVCacheQuant(),
                StreamingRequired:        true,
        })

        // Truthful logging: the gate decision is always on the record.
        logging.Default().Info("preflight",
                "model=%s backend=%s severity=%s compatible=%v reasons=%v",
                modelPath, backendLabel, report.Severity, report.Compatible, report.Reasons)
        return report
}

// deviceLabel renders the device the selected backend will use. It
// labels; it never infers capability from the name.
func deviceLabel(hw hardware.Profile, backend string) string {
        if backend == "native" {
                return "cpu (native engine host)"
        }
        if gpu := hw.PrimaryGPU(); gpu != nil && backend == "llama.cpp-vulkan" {
                return "vulkan:" + gpu.Name
        }
        return "cpu"
}

// --- live monitor wiring (§4.6/§4.7) ------------------------------------------

// liveMonitorPollInterval is the bounded polling cadence. 15 s is slow
// enough to be invisible and fast enough to protect a run.
const liveMonitorPollInterval = 15 * time.Second

// liveMonitorHysteresis requires this many consecutive agreeing samples
// before a level change is accepted (no oscillation on tiny fluctuations).
const liveMonitorHysteresis = 2

// StartLiveMonitor begins the bounded live pressure monitor on the Stack
// lifecycle. While at least one generation is active the monitor polls;
// when pressure reaches CRITICAL it triggers the protection path through
// the EXISTING cancellation ownership — active run contexts are canceled
// cooperatively (they settle, persist and recover; nothing is killed).
func (s *Stack) StartLiveMonitor() {
        if s.liveMonitor != nil {
                return // already started
        }

        sampler := func() preflight.Sample {
                hw := hardware.Snapshot(s.Src.Load())
                s := preflight.Sample{
                        At:                time.Now().UTC(),
                        RAMTotalBytes:     int64(hw.RAM.TotalBytes),
                        RAMAvailableBytes: int64(hw.RAM.AvailableBytes),
                }
                if pid := os.Getpid(); pid > 0 {
                        if bytes, rerr := resources.ProcRAM(pid); rerr == nil {
                                s.ProcRSSBytes = bytes
                        }
                }
                return s
        }

        monitor := preflight.NewLiveMonitor(sampler, preflight.DefaultPressureThresholds(), liveMonitorHysteresis)
        monitor.OnCritical(func(sample preflight.Sample) {
                if n := s.activeRuns.Load(); n > 0 {
                        logging.Default().Warn("resources",
                                "CRITICAL memory pressure during active runs (available %d / total %d bytes) — protecting %d active run(s) through cooperative cancellation",
                                sample.RAMAvailableBytes, sample.RAMTotalBytes, n)
                        for _, cancel := range s.activeRunCancels() {
                                cancel()
                        }
                } else {
                        logging.Default().Warn("resources",
                                "critical memory pressure detected with no active runs — monitoring only",
                        )
                }
        })
        monitor.OnChange(func(sample preflight.Sample) {
                logging.Default().Info("resources",
                        "live pressure level changed to %s (available %d bytes)",
                        sample.Level, sample.RAMAvailableBytes)
        })

        s.liveMonitor = monitor

        s.lifeWG.Add(1)
        go func() {
                defer s.lifeWG.Done()
                ticker := time.NewTicker(liveMonitorPollInterval)
                defer ticker.Stop()
                for {
                        select {
                        case <-s.lifeCtx.Done():
                                monitor.Stop()
                                return
                        case <-ticker.C:
                                // Bounded polling only while runs are active; idle
                                // machines are sampled at the same slow cadence for the
                                // UI but never trigger protection (no active runs).
                                sample := monitor.Poll()

                                // v1.8.0: the SAME accepted sample feeds the Runtime
                                // Governor's policy state — one sampler, one cadence,
                                // one protection path; the Governor adds policy only.
                                s.observeGovernor(sample)
                        }
                }
        }()
}

// registerActiveRun records one active generation's cancel ownership and
// returns its registry id.
func (s *Stack) registerActiveRun(cancel context.CancelFunc) int64 {
        s.activeRunMu.Lock()
        // Stack literals (tests, partial wirings) bypass NewStack: the
        // registry initializes lazily so protection never nil-panics.
        if s.activeRunCancelFns == nil {
                s.activeRunCancelFns = map[int64]context.CancelFunc{}
        }
        s.activeRunSeq++
        id := s.activeRunSeq
        s.activeRunCancelFns[id] = cancel
        s.activeRunMu.Unlock()
        s.activeRuns.Add(1)
        return id
}

// unregisterActiveRun removes a settled generation by id.
func (s *Stack) unregisterActiveRun(id int64) {
        s.activeRunMu.Lock()
        delete(s.activeRunCancelFns, id)
        s.activeRunMu.Unlock()
        if s.activeRuns.Load() > 0 {
                s.activeRuns.Add(-1)
        }
}

// activeRunCancels snapshots the cancel ownership map.
func (s *Stack) activeRunCancels() []context.CancelFunc {
        s.activeRunMu.Lock()
        defer s.activeRunMu.Unlock()
        out := make([]context.CancelFunc, 0, len(s.activeRunCancelFns))
        for _, cancel := range s.activeRunCancelFns {
                out = append(out, cancel)
        }
        return out
}

// LiveMonitor exposes the monitor for diagnostics/tests.
func (s *Stack) LiveMonitor() *preflight.LiveMonitor { return s.liveMonitor }

// ActiveRuns reports the number of active generations.
func (s *Stack) ActiveRuns() int { return int(s.activeRuns.Load()) }
