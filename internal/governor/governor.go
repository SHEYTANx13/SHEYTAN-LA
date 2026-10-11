// governor.go — v1.8.0 ADAPTIVE RUNTIME INTELLIGENCE: the ONE runtime
// policy authority.
//
// v1.9.3 CPU POLICY CONTRACT (pinned, tested in BOTH directions):
//
//   - the CPU-reduction branch of the envelope fires only on a SUSTAINED
//     rolling signal: >= cpuWarmupSamples folded samples (warm-up floor)
//     AND a rolling average >= CPUReduceAbove. A lone high sample —
//     including the very first sample, whose EWMA equals the raw value —
//     NEVER flips the envelope. This keeps the documented "one noisy
//     sample never drives policy" promise true in code and keeps
//     integration tests independent of the host's real load (the
//     v1.9.2 race-gate failure was exactly a runner's first high CPU
//     sample leaking into an OK-pressure wire contract).
//   - a real sustained high load still reduces background/concurrency
//     (>= 3 consecutive measured samples), matching the sustained-
//     pressure philosophy of the RAM branches.
//
// THE R&D DIRECTION (2026-09): SHEYTAN continuously understands its machine
// and adapts runtime behavior while preserving host responsiveness. v1.8
// turns the existing measurement/protection infrastructure into a real
// closed-loop policy system:
//
//      Telemetry → Resource State → Pressure Model → Resource Envelope
//                → Runtime Decision → Existing subsystem actions → Measure
//
// ONE-AUTHORITY RULES (non-negotiable):
//
//   - The Governor owns POLICY ONLY. Execution stays with the existing
//     authorities: the live monitor keeps the protection path (cooperative
//     cancellation), the engine lifecycle owner keeps engine processes, the
//     scheduler keeps automation, the memory manager keeps trimming. The
//     Governor never kills processes, never restarts engines, never forces
//     accelerators, never duplicates a subsystem.
//   - The Governor samples NOTHING itself: it is fed the EXISTING
//     preflight.LiveMonitor samples (one sampler, one poll cadence, one
//     hysteresis). A CPU-load seam is injected by the wiring layer; where
//     the platform cannot measure, the fact stays unknown.
//   - Unknown facts never become fake certainty: an unmeasurable value is
//     reported as unknown and admission behaves CONSERVATIVELY (refuse)
//     rather than inventing confidence.
//
// PRESSURE VOCABULARY: the R&D proposes GREEN/YELLOW/ORANGE/RED/EMERGENCY.
// This codebase already ships an evidence-backed four-level vocabulary in
// internal/preflight (ok / warning / high_pressure / critical_pressure)
// with measured thresholds and hysteresis. The Governor REUSES it — the
// mapping is GREEN=ok, YELLOW=warning, ORANGE+RED=high_pressure,
// EMERGENCY=critical_pressure — instead of inventing a second scale. The
// critical-protection path (cooperative cancel of active runs) remains
// owned by the monitor wiring exactly as shipped in v1.7.1.
//
// ADJUSTMENT CLASSES: an envelope decision names HOW a recommendation can
// honestly be applied:
//
//      live      — the control point exists and accepts changes while running
//                  (background work, tool concurrency, admission)
//      next-run  — the next generation can pick the value up (context plan)
//      reload    — the engine must be reloaded/restarted; pretending otherwise
//                  would be a lie about llama.cpp's contract
package governor

import (
        "fmt"
        "sync"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

// AdjustmentClass names how an envelope recommendation can be applied.
type AdjustmentClass string

const (
        // AdjustNone: no adjustment required at the current pressure.
        AdjustNone AdjustmentClass = "none"
        // AdjustLive: the adjustment can be applied by live authorities.
        AdjustLive AdjustmentClass = "live"
        // AdjustNextRun: the adjustment lands with the next generation.
        AdjustNextRun AdjustmentClass = "next-run"
        // AdjustReload: the adjustment requires an engine reload/restart.
        AdjustReload AdjustmentClass = "reload"
)

// ResourceState is ONE coherent snapshot of measurable current reality.
// Every field is either measured by an existing authority or explicitly
// unknown — never interpolated, never fabricated.
type ResourceState struct {
        At time.Time `json:"at"`

        // RAM facts (bytes; AvailableKnown=false means the platform could not
        // measure available RAM — the value MUST NOT be trusted).
        RAMTotalBytes     int64 `json:"ramTotalBytes,omitempty"`
        RAMAvailableBytes int64 `json:"ramAvailableBytes,omitempty"`
        AvailableKnown    bool  `json:"availableKnown"`

        // ProcRSSBytes is THIS process's resident set (0 = unmeasurable).
        ProcRSSBytes int64 `json:"procRssBytes,omitempty"`

        // EngineRSSBytes is the engine process's resident set where the
        // wiring can measure it (0 = no engine or unmeasurable).
        EngineRSSBytes int64  `json:"engineRssBytes,omitempty"`
        EngineRSSKnown bool   `json:"engineRssKnown,omitempty"`
        EngineRunning  bool   `json:"engineRunning,omitempty"`
        EngineModel    string `json:"engineModel,omitempty"`

        // Inference footprint (v1.8.6): the CURRENT inference workload's
        // memory model from the existing model-card/context authorities —
        // model weights (file size, a FILE FACT — explicitly not "RAM used")
        // and the planned KV-cache at the serving window. InferenceKnown is
        // false whenever the card/window facts could not be measured: the
        // values then stay zero and policy treats them as unknown.
        InferenceModelBytes int64 `json:"inferenceModelBytes,omitempty"`
        InferenceKVBytes    int64 `json:"inferenceKvBytes,omitempty"`
        InferenceKnown      bool  `json:"inferenceKnown"`

        // ActiveRuns is the number of active generations known by the
        // existing run authority.
        ActiveRuns int `json:"activeRuns"`

        // CPULoadPercent is the platform load signal (0..100). Unknown when
        // the platform cannot measure it; CPURollingAvg is what policy reads.
        CPULoadPercent float64 `json:"cpuLoadPercent,omitempty"`
        CPULoadKnown   bool    `json:"cpuLoadKnown,omitempty"`
        CPURollingAvg  float64 `json:"cpuRollingAvg,omitempty"`

        // CPUSamples is how many CPU samples the rolling series has folded.
        // Policy reads it as the warm-up floor: until the series holds
        // cpuWarmupSamples observations, the rolling average is not a rolling
        // signal yet (a lone sample IS the average) and the CPU branch of the
        // envelope must not fire. This is the code-level guarantee behind the
        // "one noisy sample never drives policy" contract — including the
        // first sample ever folded (the v1.9.3 race-gate fix: a CI runner's
        // real load must not leak into an OK-pressure contract).
        CPUSamples int `json:"cpuSamples,omitempty"`

        // Level is the accepted pressure level (the monitor's hysteresis-
        // protected classification — the Governor never re-classifies).
        Level preflight.PressureLevel `json:"level"`

        // Sustained is how long the CURRENT level has held. Policy reads
        // this instead of reacting to one sample.
        Sustained time.Duration `json:"sustained"`

        // Unknowns names the facts that could not be measured this cycle.
        Unknowns []string `json:"unknowns,omitempty"`
}

// Envelope is the current execution envelope: what the runtime may admit,
// what should be reduced, and the honest adjustment class. Reasons are
// deterministic and explainable — "policy changed because X".
type Envelope struct {
        Level     preflight.PressureLevel `json:"level"`
        Sustained time.Duration           `json:"sustained"`

        // AdmitHeavyweight: another heavyweight workload (a model load, a
        // repository index, a research batch) may start now.
        AdmitHeavyweight bool `json:"admitHeavyweight"`

        // ReduceBackground: nonessential background work (indexing,
        // maintenance, summarization) should be reduced or paused.
        ReduceBackground bool `json:"reduceBackground"`

        // ReduceContextWork: optional retrieval/context expansion should
        // stay minimal for the next plan (next-run class).
        ReduceContextWork bool `json:"reduceContextWork"`

        // ReduceToolConcurrency: tool/background concurrency should drop.
        ReduceToolConcurrency bool `json:"reduceToolConcurrency"`

        // ResidentMemoryBudgetBytes is the resident budget a NEW workload
        // may plan against (available RAM minus the protection headroom,
        // minus measured process RSS). 0 = no admission at this level.
        ResidentMemoryBudgetBytes int64 `json:"residentMemoryBudgetBytes,omitempty"`

        // AdjustmentClass is the strongest class this envelope requires.
        AdjustmentClass AdjustmentClass `json:"adjustmentClass"`

        // Reasons explain the envelope deterministically.
        Reasons []string `json:"reasons,omitempty"`
}

// MemoryPlan is a workload's resident-memory requirement supplied by the
// caller from the EXISTING model-card/context-planning authorities. The
// Governor never reads model files and never treats a mapped file's size
// as resident memory: the caller must plan weights + KV + runtime
// overhead as the RESIDENT estimate.
type MemoryPlan struct {
        // Workload names the admission candidate ("model load: qwen2-7b-q4").
        Workload string
        // ResidentBytes is the planned RESIDENT footprint (weights + KV at
        // the planned context + runtime overhead).
        ResidentBytes int64
}

// Decision is an admission verdict with an explainable reason.
type Decision struct {
        Admitted bool     `json:"admitted"`
        Why      string   `json:"why"`
        Reasons  []string `json:"reasons,omitempty"`
}

// Thresholds are the Governor's policy boundaries. The RAM fractions
// mirror the shipped preflight pressure thresholds so the envelope and
// the protection path can never disagree about what a level MEANS.
type Thresholds struct {
        // HeadroomFraction of total RAM is reserved for the host when
        // computing the resident admission budget (default 0.10).
        HeadroomFraction float64
        // SustainedWarning is how long a non-ok level must hold before
        // background reduction is recommended (default 30s).
        SustainedWarning time.Duration
        // SustainedHeavy is how long high pressure must hold before new
        // heavyweight admission is refused outright (default 60s).
        SustainedHeavy time.Duration
        // CPUReduceAbove is the rolling CPU load percentage above which
        // background reduction is recommended (default 85; unknown CPU
        // never triggers CPU policy).
        CPUReduceAbove float64
}

// DefaultThresholds are the v1.8.0 defaults. They mirror the preflight
// pressure vocabulary and stay conservative.
func DefaultThresholds() Thresholds {
        return Thresholds{
                HeadroomFraction: 0.10,
                SustainedWarning: 30 * time.Second,
                SustainedHeavy:   60 * time.Second,
                CPUReduceAbove:   85,
        }
}

// CPUSampler returns the platform CPU load percentage (0..100) and
// whether it was measurable. Injected by the wiring layer; a nil sampler
// means CPU facts stay unknown on this platform.
type CPUSampler func() (percent float64, ok bool)

// EngineFacts carries what the wiring layer knows about the live engine.
// The Governor never touches engine processes — it only reads facts the
// engine lifecycle owner publishes.
type EngineFacts struct {
        Running  bool
        Model    string
        RSSBytes int64 // 0 = unmeasurable
        Known    bool  // RSS measurability (separate from Running)
}

// EngineSource supplies the current engine facts. Injected by the wiring
// layer from the EXISTING engine lifecycle owner.
type EngineSource func() EngineFacts

// InferenceFootprint (v1.8.6) is the current inference workload's memory
// model, supplied by the wiring layer FROM THE EXISTING model-card and
// context-planning authorities (the Governor never reads model files).
// ModelBytes is the model FILE size (a file fact — never presented as
// "RAM used"); KVBytes is the planned KV-cache at the serving window.
// Known=false means the facts could not be measured — the values stay
// zero and policy treats the footprint as unknown.
type InferenceFootprint struct {
        ModelBytes int64
        KVBytes    int64
        Known      bool
}

// InferenceSource supplies the current inference footprint. Injected by
// the wiring layer; nil/unknown keeps the facts unknown — never guessed.
type InferenceSource func() InferenceFootprint

// Governor is the ONE runtime policy authority. Safe for concurrent use.
type Governor struct {
        mu sync.Mutex

        thresholds Thresholds
        now        func() time.Time // injected clock (deterministic tests)

        cpu     CPUSampler
        engine  EngineSource
        infSrc  InferenceSource

        // state is the latest folded resource state.
        state ResourceState

        // levelSince tracks when the accepted level became current.
        levelSince   time.Time
        levelStarted bool

        // rolling keeps the bounded rolling signals for RAM and CPU.
        ramRolling rollingSeries
        cpuRolling rollingSeries

        // runs supplies the active-generation count from the EXISTING run
        // authority (injected by the wiring; nil = 0).
        runs func() int

        // observed counts folded samples (diagnostics).
        observed int64
}

// SetActiveRunsSource injects the existing run authority's counter. The
// Governor never counts runs itself — one authority per concern.
func (g *Governor) SetActiveRunsSource(fn func() int) {
        g.mu.Lock()
        g.runs = fn
        g.mu.Unlock()
}

// SetInferenceSource injects the inference-footprint source (the wiring
// reads the existing model-card/context authorities; the Governor owns
// policy only — it never reads model files, never maps GGUF bytes to
// "RAM used", and never invents a footprint).
func (g *Governor) SetInferenceSource(fn InferenceSource) {
        g.mu.Lock()
        g.infSrc = fn
        g.mu.Unlock()
}

// SetCPUSampler replaces the injected CPU seam (v1.9.3). The production
// wiring (runtime.StartGovernor) keeps governor.CPULoadPlatform — the ONE
// platform sampler. The seam exists so integration tests can pin a
// deterministic CPU fact (or an honest unknown) instead of the host's real
// load — a CI runner's measured load must never leak into a policy
// contract that is not about the host (the exact run 38035650428 /
// job 114165397477 failure mode). Guarded by the same mutex as Observe.
func (g *Governor) SetCPUSampler(fn CPUSampler) {
        g.mu.Lock()
        g.cpu = fn
        g.mu.Unlock()
}

// cpuWarmupSamples is the v1.9.3 CPU warm-up floor: the rolling series
// must hold at least this many folded observations before the CPU branch
// of the envelope may fire. Three samples at the shipped 15s monitor
// cadence ≈ 45s of measured load — the same sustained-pressure philosophy
// as the RAM branches (SustainedWarning), and the code-level guarantee
// that one noisy sample (including the first sample ever folded, whose
// EWMA equals the raw value) never flips the envelope.
const cpuWarmupSamples = 3

// New builds a Governor on top of the EXISTING live monitor's samples.
// cpu and engine are injected seams (nil = facts stay unknown — never
// guessed). now may be nil for the real clock.
func New(thresholds Thresholds, cpu CPUSampler, engine EngineSource, now func() time.Time) *Governor {
        if thresholds.HeadroomFraction <= 0 || thresholds.HeadroomFraction >= 1 {
                thresholds.HeadroomFraction = 0.10
        }
        if thresholds.SustainedWarning <= 0 {
                thresholds.SustainedWarning = 30 * time.Second
        }
        if thresholds.SustainedHeavy <= 0 {
                thresholds.SustainedHeavy = 60 * time.Second
        }
        if thresholds.CPUReduceAbove <= 0 {
                thresholds.CPUReduceAbove = 85
        }
        if now == nil {
                now = time.Now
        }
        return &Governor{
                thresholds: thresholds,
                now:        now,
                cpu:        cpu,
                engine:     engine,
        }
}

// Observe folds one sample of the EXISTING monitor into the policy state.
// The wiring layer calls this from the monitor's poll path — the Governor
// owns no polling, no sampler, no thread.
func (g *Governor) Observe(s preflight.Sample) ResourceState {
        now := g.now().UTC()

        g.mu.Lock()
        defer g.mu.Unlock()

        g.observed++

        st := ResourceState{
                At:                now,
                RAMTotalBytes:     s.RAMTotalBytes,
                RAMAvailableBytes: s.RAMAvailableBytes,
                AvailableKnown:    s.RAMTotalBytes > 0 && s.RAMAvailableBytes > 0,
                ProcRSSBytes:      s.ProcRSSBytes,
                Level:             s.Level,
        }

        if !st.AvailableKnown {
                st.Unknowns = append(st.Unknowns, "available RAM not measurable on this platform")
        }
        if s.ProcRSSBytes == 0 {
                st.Unknowns = append(st.Unknowns, "process RSS not measurable")
        }

        // CPU load: measured by the injected seam, folded into a bounded
        // rolling signal. One noisy sample never drives policy.
        if g.cpu != nil {
                if pct, ok := g.cpu(); ok && pct >= 0 {
                        st.CPULoadKnown = true
                        st.CPULoadPercent = pct
                        g.cpuRolling.observe(pct)
                } else {
                        st.Unknowns = append(st.Unknowns, "CPU load not measurable on this platform")
                }
        } else {
                st.Unknowns = append(st.Unknowns, "CPU load not measurable on this platform")
        }
        st.CPURollingAvg = g.cpuRolling.average()
        st.CPUSamples = g.cpuRolling.len()

        // Engine facts: read from the injected source (the engine lifecycle
        // owner), never probed by the Governor.
        if g.engine != nil {
                ef := g.engine()
                st.EngineRunning = ef.Running
                st.EngineModel = ef.Model
                st.EngineRSSBytes = ef.RSSBytes
                st.EngineRSSKnown = ef.Known
                if ef.Running && !ef.Known {
                        st.Unknowns = append(st.Unknowns, "engine RSS not measurable")
                }
        }

        // Inference footprint (v1.8.6): read from the injected source (the
        // model-card/context authorities via the wiring), never derived
        // here. Unknown stays unknown — no footprint policy fires.
        if g.infSrc != nil {
                if inf := g.infSrc(); inf.Known {
                        st.InferenceModelBytes = inf.ModelBytes
                        st.InferenceKVBytes = inf.KVBytes
                        st.InferenceKnown = true
                }
        }
        if !st.InferenceKnown {
                st.Unknowns = append(st.Unknowns, "inference footprint (model+KV) not measurable")
        }

        // Active runs: read from the injected run authority (never counted
        // by the Governor itself).
        if g.runs != nil {
                st.ActiveRuns = g.runs()
        }

        // Sustained-level tracking: how long has the accepted level held?
        // This is the time-dimension hysteresis — the reasons can always
        // state the duration, and admission reads it before acting.
        if !g.levelStarted || g.state.Level != st.Level {
                g.levelSince = now
                g.levelStarted = true
        }
        st.Sustained = now.Sub(g.levelSince)

        g.ramRolling.observe(availableFraction(st))
        g.state = st
        return st
}

// State returns the latest folded resource state (zero value before the
// first Observe — honest: nothing has been measured yet).
func (g *Governor) State() ResourceState {
        g.mu.Lock()
        defer g.mu.Unlock()
        return g.state
}

// SustainedFor reports how long the current level has held.
func (g *Governor) SustainedFor() time.Duration {
        g.mu.Lock()
        defer g.mu.Unlock()
        if !g.levelStarted {
                return 0
        }
        return g.now().Sub(g.levelSince)
}

// Envelope computes the CURRENT execution envelope from the folded state.
// Pure policy over measured facts — deterministic, explainable.
func (g *Governor) Envelope() Envelope {
        g.mu.Lock()
        defer g.mu.Unlock()
        return envelopeLocked(g.state, g.thresholds)
}

// AdmitHeavyweight decides whether a NEW heavyweight workload may start
// NOW. This is the coarse gate: model loads use AdmitMemory, which adds
// the caller's resident plan.
func (g *Governor) AdmitHeavyweight(kind string) Decision {
        g.mu.Lock()
        defer g.mu.Unlock()

        st := g.state
        env := envelopeLocked(st, g.thresholds)

        if !st.AvailableKnown {
                return Decision{
                        Admitted: false,
                        Why:      fmt.Sprintf("%s deferred: available RAM unknown - refusing to guess", kind),
                        Reasons:  env.Reasons,
                }
        }

        if !env.AdmitHeavyweight {
                return Decision{
                        Admitted: false,
                        Why: fmt.Sprintf("%s deferred: %s pressure sustained for %s",
                                kind, st.Level, roundedSustained(st.Sustained)),
                        Reasons: env.Reasons,
                }
        }

        return Decision{
                Admitted: true,
                Why: fmt.Sprintf("%s admitted: %s pressure (sustained %s), resident budget %d bytes",
                        kind, st.Level, roundedSustained(st.Sustained), env.ResidentMemoryBudgetBytes),
                Reasons: env.Reasons,
        }
}

// AdmitMemory decides whether a workload with the given RESIDENT memory
// plan may start now. The plan comes from the existing model-card and
// context-planning authorities; mapped file size is NOT resident size and
// must never be passed as ResidentBytes.
func (g *Governor) AdmitMemory(plan MemoryPlan) Decision {
        g.mu.Lock()
        defer g.mu.Unlock()

        st := g.state

        if plan.ResidentBytes <= 0 {
                // A caller that cannot produce a resident estimate gets the
                // conservative verdict, not a free pass.
                return Decision{
                        Admitted: false,
                        Why:      fmt.Sprintf("%s deferred: no resident memory estimate - refusing to admit on unknown requirements", plan.Workload),
                        Reasons:  []string{"the admission authority requires a resident estimate (weights + KV + overhead)"},
                }
        }

        if !st.AvailableKnown {
                return Decision{
                        Admitted: false,
                        Why:      fmt.Sprintf("%s deferred: available RAM unknown - refusing to guess", plan.Workload),
                        Reasons:  []string{"available RAM not measurable on this platform"},
                }
        }

        // The pressure gate applies to memory admission too: a plan that
        // would arithmetically fit is still deferred while the envelope
        // refuses heavyweight work (high/critical pressure). Protection
        // semantics stay in ONE policy.
        env := envelopeLocked(st, g.thresholds)
        if !env.AdmitHeavyweight {
                return Decision{
                        Admitted: false,
                        Why: fmt.Sprintf("%s deferred: %s pressure sustained for %s",
                                plan.Workload, st.Level, roundedSustained(st.Sustained)),
                        Reasons: env.Reasons,
                }
        }

        budget := residentBudget(st, g.thresholds.HeadroomFraction)

        if plan.ResidentBytes > budget {
                return Decision{
                        Admitted: false,
                        Why: fmt.Sprintf("%s deferred: planned resident %d bytes exceeds the current resident budget %d bytes (available %d, process RSS %d, headroom %.0f%%)",
                                plan.Workload, plan.ResidentBytes, budget, st.RAMAvailableBytes, st.ProcRSSBytes, g.thresholds.HeadroomFraction*100),
                        Reasons: []string{
                                "the resident budget is available RAM minus host headroom minus measured process RSS",
                                "mapped file size is never treated as resident memory",
                        },
                }
        }

        return Decision{
                Admitted: true,
                Why: fmt.Sprintf("%s admitted: planned resident %d bytes fits the resident budget %d bytes (available %d, process RSS %d)",
                        plan.Workload, plan.ResidentBytes, budget, st.RAMAvailableBytes, st.ProcRSSBytes),
        }
}

// SelfModel returns the Governor's contribution to the runtime self-model:
// verified current facts with provenance (measured at, sustained for,
// what is unknown). The API layer composes this with the existing
// hardware/capability authorities — the Governor adds no duplication.
func (g *Governor) SelfModel() map[string]any {
        g.mu.Lock()
        defer g.mu.Unlock()

        st := g.state

        sm := map[string]any{
                "at":              st.At,
                "level":           string(st.Level),
                "observedSamples": g.observed,
        }

        if st.AvailableKnown {
                sm["ram"] = map[string]any{
                        "totalBytes":     st.RAMTotalBytes,
                        "availableBytes": st.RAMAvailableBytes,
                        "measured":       true,
                }
        } else {
                sm["ram"] = map[string]any{"measured": false}
        }

        if st.ProcRSSBytes > 0 {
                sm["process"] = map[string]any{"rssBytes": st.ProcRSSBytes, "measured": true}
        } else {
                sm["process"] = map[string]any{"measured": false}
        }

        if st.EngineRunning {
                engine := map[string]any{"running": true, "model": st.EngineModel}
                if st.EngineRSSKnown {
                        engine["rssBytes"] = st.EngineRSSBytes
                        engine["rssMeasured"] = true
                } else {
                        engine["rssMeasured"] = false
                }
                sm["engine"] = engine
        } else {
                sm["engine"] = map[string]any{"running": false}
        }

        if st.CPULoadKnown {
                sm["cpu"] = map[string]any{
                        "loadPercent": st.CPULoadPercent,
                        "rollingAvg":  st.CPURollingAvg,
                        "measured":    true,
                }
        } else {
                sm["cpu"] = map[string]any{"measured": false}
        }

        // v1.8.6: the inference workload's memory model (file-fact weights
        // + planned KV) with the same provenance discipline.
        if st.InferenceKnown {
                sm["inference"] = map[string]any{
                        "modelBytes": st.InferenceModelBytes,
                        "kvBytes":    st.InferenceKVBytes,
                        "measured":   true,
                }
        } else {
                sm["inference"] = map[string]any{"measured": false}
        }

        sm["activeRuns"] = st.ActiveRuns
        sm["sustained"] = st.Sustained.String()
        if len(st.Unknowns) > 0 {
                sm["unknowns"] = st.Unknowns
        }

        return sm
}

// envelopeLocked is the pure envelope computation over a state (also used
// by the admission paths so policy can never diverge between surfaces).
func envelopeLocked(st ResourceState, th Thresholds) Envelope {
        env := Envelope{
                Level:            st.Level,
                Sustained:        st.Sustained,
                AdmitHeavyweight: true,
                AdjustmentClass:  AdjustNone,
        }

        if !st.AvailableKnown {
                env.AdmitHeavyweight = false
                env.AdjustmentClass = AdjustNextRun
                env.Reasons = append(env.Reasons,
                        "available RAM unknown - heavyweight admission refused until a measurement exists")
                return env
        }

        env.ResidentMemoryBudgetBytes = residentBudget(st, th.HeadroomFraction)

        switch st.Level {
        case preflight.PressureOK:
                env.Reasons = append(env.Reasons,
                        fmt.Sprintf("pressure ok for %s - no adjustment", roundedSustained(st.Sustained)))
        case preflight.PressureWarning:
                if st.Sustained >= th.SustainedWarning {
                        env.ReduceBackground = true
                        env.ReduceContextWork = true
                        env.AdjustmentClass = AdjustLive
                        env.Reasons = append(env.Reasons,
                                fmt.Sprintf("warning pressure sustained for %s - background and optional context work reduced", roundedSustained(st.Sustained)))
                } else {
                        env.Reasons = append(env.Reasons,
                                fmt.Sprintf("warning pressure for %s - watching for a sustained state before reducing work", roundedSustained(st.Sustained)))
                }
        case preflight.PressureHigh:
                env.ReduceBackground = true
                env.ReduceContextWork = true
                env.ReduceToolConcurrency = true
                env.AdjustmentClass = AdjustLive
                env.AdmitHeavyweight = false
                env.Reasons = append(env.Reasons,
                        fmt.Sprintf("high pressure sustained for %s - new heavyweight workloads deferred, background reduced", roundedSustained(st.Sustained)))
        case preflight.PressureCritical:
                env.ReduceBackground = true
                env.ReduceContextWork = true
                env.ReduceToolConcurrency = true
                env.AdjustmentClass = AdjustLive
                env.AdmitHeavyweight = false
                env.ResidentMemoryBudgetBytes = 0
                env.Reasons = append(env.Reasons,
                        fmt.Sprintf("critical pressure sustained for %s - protection path owns active runs (cooperative cancellation); all new heavyweight work refused", roundedSustained(st.Sustained)))
        }

        // v1.9.3 policy pin (the CPU contract): the reduction fires only on
        // a SUSTAINED rolling signal — the series must hold at least
        // cpuWarmupSamples folded observations AND its average must sit
        // above the threshold. A lone spike (or a high FIRST sample, whose
        // EWMA equals the raw value) never flips the envelope by itself; a
        // genuinely sustained high load (>= 3 samples, ~45s at the shipped
        // 15s cadence) still reduces background/concurrency.
        if st.CPULoadKnown && st.CPUSamples >= cpuWarmupSamples &&
                st.CPURollingAvg >= th.CPUReduceAbove {
                env.ReduceBackground = true
                env.ReduceToolConcurrency = true
                if env.AdjustmentClass == AdjustNone {
                        env.AdjustmentClass = AdjustLive
                }
                env.Reasons = append(env.Reasons,
                        fmt.Sprintf("rolling CPU load %.0f%% above %.0f%% sustained over %d samples - background concurrency reduced", st.CPURollingAvg, th.CPUReduceAbove, st.CPUSamples))
        }

        // v1.8.6 — the CURRENT inference workload is accounted for: when
        // the measured inference footprint (weights + planned KV) plus
        // the process RSS already exceed the resident budget, the machine
        // is effectively running at its inference capacity — background
        // work is reduced (the existing honest adjustment class) with the
        // reason stated, and no NEW heavyweight admission fits. RAM is
        // memory capacity here, never an accelerator; unknown footprint
        // never triggers this branch.
        if st.InferenceKnown {
                footprint := st.InferenceModelBytes + st.InferenceKVBytes
                committed := footprint
                if st.ProcRSSBytes > 0 {
                        committed += st.ProcRSSBytes
                }
                if budget := residentBudget(st, th.HeadroomFraction); budget > 0 && committed >= budget {
                        env.ReduceBackground = true
                        if env.AdjustmentClass == AdjustNone || env.AdjustmentClass == AdjustNextRun {
                                env.AdjustmentClass = AdjustLive
                        }
                        env.Reasons = append(env.Reasons,
                                fmt.Sprintf("inference footprint %d bytes (model %d + KV %d) plus process RSS %d consumes the resident budget %d - background work reduced",
                                        footprint, st.InferenceModelBytes, st.InferenceKVBytes, st.ProcRSSBytes, budget))
                }
        }

        if env.ReduceContextWork && env.AdjustmentClass == AdjustNone {
                env.AdjustmentClass = AdjustNextRun
        }

        return env
}

// residentBudget computes available RAM minus host headroom minus the
// measured process RSS (floor 0). The engine RSS is NOT subtracted again:
// it already shrank the host's available RAM.
func residentBudget(st ResourceState, headroom float64) int64 {
        budget := float64(st.RAMAvailableBytes) * (1 - headroom)
        if st.ProcRSSBytes > 0 {
                budget -= float64(st.ProcRSSBytes)
        }
        if budget < 0 {
                return 0
        }
        return int64(budget)
}

func availableFraction(st ResourceState) float64 {
        if st.RAMTotalBytes <= 0 || st.RAMAvailableBytes <= 0 {
                return 0
        }
        return float64(st.RAMAvailableBytes) / float64(st.RAMTotalBytes)
}

func roundedSustained(d time.Duration) string {
        if d < time.Second {
                return "<1s"
        }
        return d.Round(time.Second).String()
}
