package api

// perf.go — v1.1.7 compact live performance surface.
//
// GET /api/perf — ONE endpoint that reports only REAL, measured values for
// the Settings → Performance view:
//
//   - CPU / RAM sampled live from the OS (cheap, bounded probes);
//   - GPU / VRAM via nvidia-smi when that tool exists (N/A otherwise);
//   - prompt tok/s, generation tok/s and TTFT from the streaming client's
//     own per-request measurements (llm.SnapshotEnginePerf — no sample yet
//     means the UI renders N/A, never a fabricated number);
//   - context usage from the last measured generation vs the engine's
//     verified window;
//   - the ACTIVE engine profile (compat level, recorded reason, removed
//     options) — the "why is my engine in compatibility mode" answer;
//   - recommended settings derived from detected hardware + model +
//     engine capability (never silently applied — the UI asks first).
//
// This is deliberately NOT a new telemetry architecture: every value comes
// from infrastructure that already existed (sysinfo probe, streaming
// tokenTimer, capability profile, model card reader).

import (
        "context"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "runtime"
        "strconv"
        "strings"
        "sync"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/contextcache"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/ctxtelemetry"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// gpuSampleInterval bounds how often nvidia-smi may run.
const gpuSampleInterval = 5 * time.Second

// perfHandler caches slightly-expensive samples so concurrent UI polls stay
// lightweight (CPU needs two delta samples; GPU shells out to nvidia-smi).
type perfHandler struct {
        mu          sync.Mutex
        cpuKnown    bool
        lastIdle    float64
        lastBusy    float64
        gpuMu       sync.Mutex
        lastGPUAt   time.Time
        gpuKnown    bool
        gpuPercent  float64
        gpuMemUsed  float64
        gpuMemTotal float64
}

var perfState = &perfHandler{}

// sampleCPU returns the average CPU busy percentage since the previous
// call (a real delta over the poll interval, not an invented snapshot).
// The first call only primes the baseline and reports not-known.
func (p *perfHandler) sampleCPU() (float64, bool) {
        p.mu.Lock()
        defer p.mu.Unlock()

        idle, busy, ok := cpuTimesSnapshot()
        if !ok {
                return 0, false
        }

        if !p.cpuKnown {
                p.lastIdle, p.lastBusy, p.cpuKnown = idle, busy, true
                return 0, false
        }

        dIdle := idle - p.lastIdle
        dBusy := busy - p.lastBusy
        p.lastIdle, p.lastBusy = idle, busy

        total := dIdle + dBusy
        if total <= 0 {
                return 0, false
        }

        return 100 * dBusy / total, true
}

// sampleGPU runs nvidia-smi (when present) for GPU utilisation and memory.
// Any failure leaves the cache empty and the UI shows N/A — never a guess.
func (p *perfHandler) sampleGPU() (pct, memUsed, memTotal float64, ok bool) {
        p.gpuMu.Lock()
        defer p.gpuMu.Unlock()

        if p.gpuKnown && time.Since(p.lastGPUAt) < gpuSampleInterval {
                return p.gpuPercent, p.gpuMemUsed, p.gpuMemTotal, true
        }

        bin, err := exec.LookPath("nvidia-smi")
        if err != nil {
                return 0, 0, 0, false
        }

        ctx, cancel := context.WithTimeout(context.Background(), gpuSampleInterval)
        defer cancel()

        out, err := exec.CommandContext(ctx, bin,
                "--query-gpu=utilization.gpu,memory.used,memory.total",
                "--format=csv,noheader,nounits",
        ).Output()
        if err != nil || len(out) == 0 {
                return 0, 0, 0, false
        }

        // First GPU line: "34, 5120, 8192"
        fields := strings.Split(strings.SplitN(string(out), "\n", 2)[0], ",")
        if len(fields) < 3 {
                return 0, 0, 0, false
        }

        pct, err1 := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
        used, err2 := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
        total, err3 := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
        if err1 != nil || err2 != nil || err3 != nil {
                return 0, 0, 0, false
        }

        p.gpuPercent, p.gpuMemUsed, p.gpuMemTotal = pct, used, total
        p.gpuKnown, p.lastGPUAt = true, time.Now()
        return pct, used, total, true
}

// perfPayload is the /api/perf response. Pointer fields are nil when the
// value is genuinely not measurable — the UI renders "N/A" for those.
type perfPayload struct {
        GeneratedAt string `json:"generatedAt"`

        // Hardware (live where measurable).
        CPUPercent  *float64       `json:"cpuPercent"`
        GPUPercent  *float64       `json:"gpuPercent"`
        RAM         *ramSample     `json:"ram"`
        VRAM        *vramSample    `json:"vram"`
        Backend     string         `json:"backend"`
        EngineState string         `json:"engineState"`
        Model       string         `json:"model,omitempty"`
        Compat      llm.CompatInfo `json:"compat"`

        // Accelerator (v1.2.6): the measured, evidence-based resolution —
        // requested profile, resolved backend/device, offload state and the
        // WHY. Never a claim without runtime evidence.
        Accelerator *accelerator.Resolution `json:"accelerator,omitempty"`

        // Generation telemetry (real measurements from the streaming client).
        Gen EnginePerfAPI `json:"generation"`

        // Context usage of the last measured generation.
        Context *contextUsage `json:"context,omitempty"`

        // v1.2.4: runtime memory-policy telemetry — allocations/reclaims made
        // measurable (heap before/after last cleanup, bytes freed, cache trims,
        // bounded-capture counters, cache hit ratios).
        Memory *memoryStatsAPI `json:"memory,omitempty"`

        // EngineMemory (v1.8.6 §8): the serving engine's REAL memory evidence
        // — the MEASURED engine process RSS (from the ONE monitor cadence,
        // folded by the Governor), the model FILE facts (explicitly labeled —
        // a GGUF file size is never presented as "RAM used"), and the runtime
        // offload evidence line. Every value carries its provenance; unknown
        // stays unknown.
        EngineMemory *engineMemoryAPI `json:"engineMemory,omitempty"`

        // Recommended settings (apply is always explicit, never automatic).
        Recommended *recommendedSettings `json:"recommended,omitempty"`

        // v1.2.5: per-request timing + tier telemetry for the Advanced/System
        // diagnostics view — the measured timeline of the most recent turns.
        Requests []requestTimingAPI `json:"requests,omitempty"`

        // v1.2.5: the data-ownership ladder (ACTIVE/SESSION/HOT/COLD/EXPIRED/
        // RELEASE) with live counters.
        Ownership []ownershipLevelAPI `json:"ownership,omitempty"`

        // v1.2.5: the tool result/spec cache counters.
        ToolCache *toolCacheAPI `json:"toolCache,omitempty"`
}

// requestTimingAPI is one recent turn's measured timeline (wire form).
type requestTimingAPI struct {
        At                time.Time `json:"at"`
        SessionID         string    `json:"sessionId,omitempty"`
        Tier              string    `json:"tier,omitempty"`
        FinalTier         string    `json:"finalTier,omitempty"`
        Escalations       int       `json:"escalations,omitempty"`
        ThinkingControl   string    `json:"thinkingControl,omitempty"`
        ToolPolicyMode    string    `json:"toolPolicyMode,omitempty"`
        FirstPromptTokens int       `json:"firstPromptTokens,omitempty"`
        ClassifyMs        int64     `json:"classifyMs,omitempty"`
        ContextMs         int64     `json:"contextMs,omitempty"`
        PromptMs          int64     `json:"promptMs,omitempty"`
        SerializationMs   int64     `json:"serializationMs,omitempty"`
        TTFTMs            int64     `json:"ttftMs,omitempty"`
        GenerationMs      int64     `json:"generationMs,omitempty"`
        ToolMs            int64     `json:"toolMs,omitempty"`
        VerificationMs    int64     `json:"verificationMs,omitempty"`
        TotalMs           int64     `json:"totalMs,omitempty"`
        ToolCalls         int       `json:"toolCalls,omitempty"`
        Verified          string    `json:"verified,omitempty"`
}

// ownershipLevelAPI is one ownership-ladder row (wire form).
type ownershipLevelAPI struct {
        Level       string   `json:"level"`
        Owners      int      `json:"owners,omitempty"`
        Names       []string `json:"names,omitempty"`
        Bytes       int64    `json:"bytes,omitempty"`
        At          string   `json:"at,omitempty"`
        Description string   `json:"description"`
}

// toolCacheAPI carries the measured tool cache counters.
type toolCacheAPI struct {
        ResultHits    int `json:"resultHits"`
        ResultMisses  int `json:"resultMisses"`
        ResultEntries int `json:"resultEntries"`
        ResultBytes   int `json:"resultBytes"`
}

// memoryStatsAPI is the wire form of the coordinated memory telemetry.
type memoryStatsAPI struct {
        HeapAllocBytes    uint64 `json:"heapAllocBytes"`
        HeapInuseBytes    uint64 `json:"heapInuseBytes"`
        SysBytes          uint64 `json:"sysBytes"`
        ActiveRuns        int64  `json:"activeRuns"`
        CleanupsRun       uint64 `json:"cleanupsRun"`
        PressureCleanups  uint64 `json:"pressureCleanups"`
        LastCleanupAt     string `json:"lastCleanupAt,omitempty"`
        LastCleanupReason string `json:"lastCleanupReason,omitempty"`
        LastCleanupDurMs  int64  `json:"lastCleanupDurMs"`
        LastFreedBytes    int64  `json:"lastFreedBytes"`
        FreedBytesTotal   int64  `json:"freedBytesTotal"`
        HeapBeforeLast    uint64 `json:"heapBeforeLast,omitempty"`
        HeapAfterLast     uint64 `json:"heapAfterLast,omitempty"`

        // Data-path telemetry (v1.2.4): bytes moved vs bytes avoided.
        ContextCache *contextcache.Stats `json:"contextCache,omitempty"`
        ToolCapture  *tools.CaptureStats `json:"toolCapture,omitempty"`
        CtxTelemetry *ctxtelemetry.Stats `json:"ctxTelemetry,omitempty"`
        MemoryDedup  uint64              `json:"memoryDuplicatesSkipped,omitempty"`
}

// EnginePerfAPI mirrors llm.EnginePerfSnapshot for the wire (keeps the llm
// package free of API naming concerns).
type EnginePerfAPI struct {
        HasSample        bool    `json:"hasSample"`
        GenTokPerSec     float64 `json:"genTokPerSec,omitempty"`
        PromptTokPerSec  float64 `json:"promptTokPerSec,omitempty"`
        TTFTSeconds      float64 `json:"ttftSeconds,omitempty"`
        PromptTokens     int     `json:"promptTokens,omitempty"`
        Tokens           int     `json:"tokens,omitempty"`
        SamplesAvailable int     `json:"samplesAvailable"`
}

type ramSample struct {
        TotalBytes     uint64  `json:"totalBytes"`
        AvailableBytes uint64  `json:"availableBytes"`
        UsedBytes      uint64  `json:"usedBytes"`
        UsedPercent    float64 `json:"usedPercent"`
}

// engineMemoryAPI is the v1.8.6 serving-engine memory evidence block:
// MEASURED facts, FILE facts and runtime evidence, each labeled — never
// a file size dressed up as RAM used, never a guessed KV figure.
type engineMemoryAPI struct {
        // Backend / Model name the facts belong to.
        Backend string `json:"backend"`
        Model   string `json:"model,omitempty"`

        // ProcessRSSBytes is the ENGINE process's measured resident set
        // (from the ONE monitor cadence through the Governor's folded state).
        // RSSMeasured=false means unmeasured — the value stays zero.
        ProcessRSSBytes int64  `json:"processRssBytes,omitempty"`
        RSSMeasured     bool   `json:"rssMeasured"`

        // ModelFileBytes is the loaded model's FILE size — a file fact,
        // explicitly NOT resident memory (mapped weights are paged lazily;
        // the measured residency is ProcessRSSBytes above).
        ModelFileBytes int64 `json:"modelFileBytes,omitempty"`
        ModelFileKnown bool  `json:"modelFileKnown"`

        // OffloadEvidence is the measured runtime offload line ("" unknown).
        OffloadEvidence string `json:"offloadEvidence,omitempty"`

        // Notes carry the measured-vs-file-fact-vs-unknown provenance.
        Notes []string `json:"notes,omitempty"`
}

type vramSample struct {
        TotalMB float64 `json:"totalMb"`
        UsedMB  float64 `json:"usedMb"`
        Percent float64 `json:"percent,omitempty"`
        Source  string  `json:"source"`
}

type contextUsage struct {
        UsedTokens int     `json:"usedTokens"`
        Total      int     `json:"total"`
        Percent    float64 `json:"percent,omitempty"`
        Source     string  `json:"source,omitempty"`
}

type recommendedSettings struct {
        Threads        int      `json:"threads"`
        ThreadsBatch   int      `json:"threadsBatch"`
        GPULayers      int      `json:"gpuLayers"`
        GPUAutoOffload bool     `json:"gpuAutoOffload"`
        FlashAttention bool     `json:"flashAttention"`
        FlashAttnState string   `json:"flashAttnState"` // Supported | Unsupported
        UBatchSize     int      `json:"ubatchSize"`
        CacheReuse     int      `json:"cacheReuse"`
        Context        int      `json:"context"`
        Notes          []string `json:"notes,omitempty"`
}

// handlePerf serves GET /api/perf.
func (s *Server) handlePerf(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                return
        }

        writeJSON(w, s.buildPerfPayload(llm.SnapshotEnginePerf()))
}

// buildPerfPayload assembles the measured-only performance payload.
func (s *Server) buildPerfPayload(perf llm.EnginePerfSnapshot) perfPayload {
        payload := perfPayload{
                GeneratedAt: time.Now().UTC().Format(time.RFC3339),
        }

        // Backend + state + model (honest: from the authoritative snapshot).
        engineSnap := s.engineSnapshot()
        payload.Backend = engineSnap.Backend
        payload.EngineState = engineSnap.State
        payload.Model = engineSnap.Model
        payload.Compat = s.llama.CompatSnapshot()

        // CPU: live delta sample (nil until two samples exist).
        if pct, ok := perfState.sampleCPU(); ok {
                v := pct
                payload.CPUPercent = &v
        }

        // RAM: cheap OS-native read (no CIM round-trip).
        if ram, ok := sampleRAM(); ok {
                payload.RAM = &ram
        }

        // GPU / VRAM: nvidia-smi when present.
        if pct, used, total, ok := perfState.sampleGPU(); ok {
                v := pct
                payload.GPUPercent = &v
                payload.VRAM = &vramSample{
                        TotalMB: total, UsedMB: used, Source: "nvidia-smi",
                }
                if total > 0 {
                        payload.VRAM.Percent = 100 * used / total
                }
        }

        // v1.2.6→v1.8.6: the accelerator resolution — built from MEASURED
        // evidence only, through the ONE accessor shared with /api/engine
        // (engine device enumeration, runtime offload line, hardware probe,
        // OpenVINO load probe, model facts, the persisted transaction
        // receipt).
        payload.Accelerator = s.currentAcceleratorResolution(engineSnap.LoadedPath)

        // Generation telemetry — pass through the real measurements.
        payload.Gen = EnginePerfAPI{
                HasSample:        perf.HasSample,
                GenTokPerSec:     perf.GenTokPerSec,
                PromptTokPerSec:  perf.PromptTokPerSec,
                TTFTSeconds:      perf.TTFTSeconds,
                PromptTokens:     perf.PromptTokens,
                Tokens:           perf.Tokens,
                SamplesAvailable: perf.SamplesAvailable,
        }

        // Context usage: last measured prompt vs the engine's verified window.
        if perf.HasSample && perf.PromptTokens > 0 {
                total := s.llama.VerifiedContext()
                if total <= 0 {
                        total = s.src.Load().LLM.NumCtx
                }
                if total > 0 {
                        payload.Context = &contextUsage{
                                UsedTokens: perf.PromptTokens,
                                Total:      total,
                                Source:     "last generation",
                        }
                        payload.Context.Percent = 100 * float64(perf.PromptTokens) / float64(total)
                }
        }

        // Recommended settings from detected hardware + model + capability.
        payload.Recommended = s.buildRecommended(engineSnap.LoadedPath)

        // v1.2.4: coordinated memory telemetry (nil-safe for tests).
        payload.Memory = s.buildMemoryStats()

        // v1.8.6 §8: the serving engine's real memory evidence.
        payload.EngineMemory = s.buildEngineMemory(engineSnap)

        payload.Requests = s.buildRequestTimings()
        payload.Ownership = s.buildOwnershipLevels()
        hits, misses, entries, bytes := agent.ToolResultCacheStats()
        payload.ToolCache = &toolCacheAPI{
                ResultHits:    hits,
                ResultMisses:  misses,
                ResultEntries: entries,
                ResultBytes:   bytes,
        }

        return payload
}

// buildAcceleratorResolution assembles the MEASURED evidence and resolves
// the accelerator posture (v1.2.6). Every input is a real probe:
//   - the installed engine's --list-devices enumeration (cached per binary);
//   - the runtime offload line the engine log actually printed (per boot);
//   - the persisted bounded GPU transaction receipt (identity-validated);
//   - the hardware probe's measured adapters + NPU identity;
//   - the OpenVINO runtime LOAD probe;
//   - the loaded model's GGUF facts (arch/quant).
//
// v1.8.6: this is the ONE gatherer behind BOTH /api/perf and /api/engine
// (through currentAcceleratorResolution — see below): one authority, one
// evidence path, two consuming surfaces that can never disagree.
func (s *Server) buildAcceleratorResolution(loadedModelPath string) *accelerator.Resolution {
        cfg := s.src.Load()

        ev := s.gatherAcceleratorEvidence(cfg, loadedModelPath)

        resolution := accelerator.Resolve(accelerator.NormalizeRequested(cfg.Accelerator), ev)

        s.logAcceleratorResolution(resolution.Describe())

        // v1.8.6: memo the authority's output WITH its input signature —
        // a read-through cache the engine surface validates before use.
        s.resolutionMu.Lock()
        cached := resolution
        s.lastResolution = &cached
        s.resolutionSig = s.acceleratorInputSignature(cfg, loadedModelPath)
        s.resolutionMu.Unlock()

        return &resolution
}

// gatherAcceleratorEvidence collects every measured fact the resolver
// consumes (the ONE evidence path — no surface re-derives it).
func (s *Server) gatherAcceleratorEvidence(cfg *config.Config, loadedModelPath string) accelerator.Evidence {
        bin := cfg.LlamaBinPath
        if bin == "" {
                bin = filepath.Join(cfg.DataDir, "bin", "llama-server")
                if runtime.GOOS == "windows" {
                        bin += ".exe"
                }
        }

        devices, enumerationSupported, enumErr := llm.EnumerateEngineDevices(bin)

        // v1.6.1 LOG DISCIPLINE: the enumeration warning is part of the
        // per-poll chatter. An UNCHANGED warning is demoted to DEBUG
        // (still visible in Advanced diagnostics); only a CHANGED one
        // warns at INFO/WARN level once.
        if enumErr != nil {
                s.logAcceleratorEnumWarn(enumErr.Error())
        }

        hw := hardware.Snapshot(cfg)

        ev := accelerator.Evidence{
                EngineDevices:          devices,
                EnumerationSupported:   enumerationSupported,
                RuntimeOffloadEvidence: s.llama.OffloadEvidence(),
                VulkanBackendPresent:   llm.VulkanAvailable(cfg),
                NPUPresent:             hw.NPU != nil,
                OpenVINO:               accelerator.DetectOpenVINO(filepath.Dir(bin)),
                AvailableRAMMB:         int(hw.RAM.AvailableBytes >> 20),
                Workload:               accelerator.WorkloadChat,

                // v1.8.6: the CURRENT engine identity (receipt
                // stale-evidence invalidation) and the structured
                // execution receipt from the bounded transaction store.
                EngineTag:     updater.InstalledEngineTag(cfg),
                EngineVariant: string(updater.InstalledEngineVariant(cfg)),
        }

        if st := s.llama.AutoGPUProbeSnapshot(); st != nil {
                ev.GPUExecutionReceipt = &accelerator.ExecutionReceipt{
                        Kind:      accelerator.ReceiptKindVerifiedProbe,
                        Line:      st.Evidence,
                        EngineTag: st.EngineTag,
                        Variant:   st.Variant,
                        Device:    st.Device,
                        Model:     st.Model,
                        Status:    st.Status,
                        At:        st.At,
                }
        }

        if hw.NPU != nil {
                ev.NPUName = hw.NPU.Name
        }

        if loadedModelPath != "" {
                if card, err := llm.ReadModelCard(loadedModelPath); err == nil && card != nil {
                        ev.Model = accelerator.ModelFacts{
                                Architecture: card.Arch,
                                Quantization: card.Quant,
                                ContextLimit: card.ContextLength,
                        }
                }
        }

        // The generation telemetry's measured throughput feeds the NPU/GPU
        // benchmark gates when samples exist.
        perf := llm.SnapshotEnginePerf()
        if perf.HasSample && perf.GenTokPerSec > 0 {
                ev.GPUBenchmarkTokPerSec = perf.GenTokPerSec
        }

        return ev
}

// acceleratorInputSignature renders the memo's input identity: the exact
// engine (tag+variant), the requested profile, the loaded model, the
// current-boot offload evidence and the persisted receipt state. ANY
// change invalidates the memo — a stale poll-only snapshot can never
// claim a current backend, and engine/model/device identity stays
// aligned with the resolution being served.
func (s *Server) acceleratorInputSignature(cfg *config.Config, loadedModelPath string) string {
        st := s.llama.AutoGPUProbeSnapshot()

        receipt := ""
        if st != nil {
                receipt = strings.Join([]string{
                        st.Status, st.Identity, st.EngineTag, st.Variant, st.Device, st.Evidence,
                }, "\x1f")
        }

        return strings.Join([]string{
                updater.InstalledEngineTag(cfg),
                string(updater.InstalledEngineVariant(cfg)),
                string(accelerator.NormalizeRequested(cfg.Accelerator)),
                loadedModelPath,
                s.llama.OffloadEvidence(),
                receipt,
        }, "\x1e")
}

// currentAcceleratorResolution is the ONE accessor BOTH surfaces use
// (/api/perf and /api/engine). It returns the memo when its input
// signature still matches the current reality — the read-through cache —
// and recomputes through the ONE gatherer when the memo is absent or
// stale. Consequent guarantees:
//
//   - /api/engine.execution reflects the current serving reality WITHOUT
//     depending on /api/perf having polled first;
//   - /api/perf.accelerator and /api/engine.execution read the SAME
//     authority (one resolution, one evidence path) and therefore agree;
//   - a stale memo (engine swap, variant change, new receipt, first
//     offload line, model change, profile change) is refreshed before
//     any consumer sees it — no stale poll-only snapshot can claim a
//     current backend.
//
// Concurrent first-polls may both recompute (the same benign, bounded
// race the perf path always had); both compute the SAME answer from the
// SAME inputs, and the underlying enumeration is per-binary cached.
func (s *Server) currentAcceleratorResolution(loadedModelPath string) *accelerator.Resolution {
        cfg := s.src.Load()
        sig := s.acceleratorInputSignature(cfg, loadedModelPath)

        s.resolutionMu.Lock()
        if s.lastResolution != nil && s.resolutionSig == sig {
                cached := *s.lastResolution
                s.resolutionMu.Unlock()
                return &cached
        }
        s.resolutionMu.Unlock()

        // Absent or stale: recompute through the ONE gatherer (which also
        // rewrites the memo + signature under the same lock).
        return s.buildAcceleratorResolution(loadedModelPath)
}

// buildEngineMemory assembles the serving engine's memory evidence
// (v1.8.6 §8). Every fact keeps its provenance:
//
//   - the engine process RSS is MEASURED (resources.ProcRAM through the
//     backend Metrics on the ONE monitor cadence, folded by the Governor
//     — no second sampler on the poll path);
//   - the model FILE size is a file fact, explicitly NOT resident memory
//     (a mapped GGUF is paged lazily — presenting it as "RAM used" would
//     be a fabrication);
//   - the offload evidence is the runtime line the engine printed this
//     boot;
//   - everything the current engine cannot expose (KV-cache allocation
//     on builds without a metrics surface) stays UNKNOWN with a note —
//     never a guessed figure.
func (s *Server) buildEngineMemory(snap engineSnapshot) *engineMemoryAPI {
        out := &engineMemoryAPI{
                Backend:         snap.Backend,
                Model:           snap.Model,
                OffloadEvidence: s.llama.OffloadEvidence(),
        }

        // Measured engine RSS: the Governor's folded fact (the monitor's
        // engine-facts seam reads backend Metrics on the 15s cadence).
        if s.stack != nil {
                if g := s.stack.Governor(); g != nil {
                        st := g.State()
                        if st.EngineRSSKnown && st.EngineRSSBytes > 0 {
                                out.ProcessRSSBytes = st.EngineRSSBytes
                                out.RSSMeasured = true
                        }
                        if st.EngineRunning && st.EngineModel != "" && out.Model == "" {
                                out.Model = st.EngineModel
                        }
                }
        }

        if !out.RSSMeasured {
                out.Notes = append(out.Notes,
                        "engine process RSS not measured yet (the monitor cadence folds it once the engine is up)")
        }

        // Model file facts (a file size, never residency).
        if snap.LoadedPath != "" {
                if fi, err := os.Stat(snap.LoadedPath); err == nil {
                        out.ModelFileBytes = fi.Size()
                        out.ModelFileKnown = true
                        out.Notes = append(out.Notes,
                                "modelFileBytes is the FILE size — a file fact, not RAM used; measured residency is processRssBytes")
                }
        }

        // What the current engine cannot tell us stays explicitly unknown.
        out.Notes = append(out.Notes,
                "KV-cache allocation is unknown on this engine build (no measured surface) — reported as unknown, never estimated here")

        if out.Backend == "native" {
                // The native backend's own facts flow through the same contract;
                // its estimates (when it serves) carry their own provenance in
                // its ModelInfo — never re-derived here.
                out.Notes = append(out.Notes,
                        "native engine: CPU execution only — no GPU offload is claimed or reported")
        }

        return out
}

// logAcceleratorResolution applies the v1.6.1 log discipline: INFO only
// when the resolution CHANGED (the evidence, the requested profile or the
// verdict moved); DEBUG for the unchanged per-poll recomputation. The
// normal log stays compact and every transition stays visible.
func (s *Server) logAcceleratorResolution(desc string) {
        s.accelMu.Lock()
        unchanged := s.lastAccelDesc == desc
        s.lastAccelDesc = desc
        s.accelMu.Unlock()

        if unchanged {
                logging.Default().Debug("perf", "accelerator resolution (unchanged): %s", desc)
                return
        }

        logging.Default().Info("perf", "accelerator resolution: %s", desc)
}

// logAcceleratorEnumWarn demotes an unchanged per-poll enumeration warning
// to DEBUG; a new or changed warning surfaces once at WARN.
func (s *Server) logAcceleratorEnumWarn(msg string) {
        s.accelMu.Lock()
        unchanged := s.lastAccelEnumWarn == msg
        s.lastAccelEnumWarn = msg
        s.accelMu.Unlock()

        if unchanged {
                logging.Default().Debug("perf", "engine device enumeration (unchanged): %s", msg)
                return
        }

        logging.Default().Warn("perf", "engine device enumeration: %s", msg)
}

// buildMemoryStats assembles the runtime memory-policy snapshot plus the
// data-path counters (bytes avoided by cache hits and deduplication) from
// the components that own them.
func (s *Server) buildMemoryStats() *memoryStatsAPI {
        if s.stack == nil || s.stack.MemMgr == nil {
                return nil
        }
        ms := s.stack.MemMgr.Stats()
        out := &memoryStatsAPI{
                HeapAllocBytes:    ms.HeapAllocBytes,
                HeapInuseBytes:    ms.HeapInuseBytes,
                SysBytes:          ms.SysBytes,
                ActiveRuns:        ms.ActiveRuns,
                CleanupsRun:       ms.CleanupsRun,
                PressureCleanups:  ms.PressureCleanups,
                LastCleanupAt:     ms.LastCleanupAt,
                LastCleanupReason: ms.LastCleanupReason,
                LastCleanupDurMs:  ms.LastCleanupDurMs,
                LastFreedBytes:    ms.LastFreedBytes,
                FreedBytesTotal:   ms.FreedBytesTotal,
                HeapBeforeLast:    ms.HeapBeforeLast,
                HeapAfterLast:     ms.HeapAfterLast,
        }
        if s.stack.Cache != nil {
                cs := s.stack.Cache.Stats()
                out.ContextCache = &cs
        }
        tc := tools.GetCaptureStats()
        out.ToolCapture = &tc
        if s.stack.Telemetry != nil {
                ts := s.stack.Telemetry.Stats()
                out.CtxTelemetry = &ts
        }
        if s.stack.Mem != nil {
                out.MemoryDedup = s.stack.Mem.DuplicatesSkipped()
        }
        return out
}

// buildRecommended derives settings advice from measurable inputs only:
// the sysinfo hardware probe, the loaded model's GGUF card and the
// verified engine capability profile. Nothing here mutates configuration —
// the UI applies values explicitly.
func (s *Server) buildRecommended(loadedModelPath string) *recommendedSettings {
        // v1.2.6 continuation: /api/perf is an INTERACTIVE surface — it must
        // never block on the deep hardware probe. The previous code called
        // sysinfo.Probe() (deep, single-flight): the FIRST perf request of a
        // cold start could wait seconds behind the CIM probe. ProbeFast
        // serves the fast facts instantly and merges the measured deep facts
        // once the background probe lands (WarmDeep runs at startup).
        si := sysinfo.ProbeFast()
        if si == nil {
                si = sysinfo.Probe()
        }
        if si == nil {
                return nil
        }

        cfg := s.src.Load()

        rec := &recommendedSettings{
                Threads:        si.Recommended.NumThread,
                GPUAutoOffload: cfg.GPUAutoOffload,
                Context:        si.Recommended.NumCtx,
        }

        if si.Recommended.NumBatch > 0 {
                rec.UBatchSize = si.Recommended.NumBatch
        }

        if gen, batch := sysinfo.RecommendThreads(); gen > 0 {
                if rec.Threads == 0 {
                        rec.Threads = gen
                }
                if rec.ThreadsBatch == 0 {
                        rec.ThreadsBatch = batch
                }
        }

        // GPU advice: auto-offload is the right posture when a GPU with
        // measurable VRAM exists; CPU-only machines should keep it off.
        if len(si.GPU) > 0 && si.GPU[0].VRAMBytes > 0 {
                rec.GPUAutoOffload = true
                if si.Recommended.NumGPU > 0 {
                        rec.GPULayers = si.Recommended.NumGPU
                }
        } else if len(si.GPU) == 0 {
                rec.GPUAutoOffload = false
                rec.GPULayers = 0
                rec.Notes = append(rec.Notes,
                        "No dedicated GPU detected — run on CPU with GPU offload disabled.")
        }

        // Flash attention: capability-aware, never assumed.
        caps := s.llama.CapsSnapshot()
        if caps != nil && caps.FlashAttnEnabled {
                rec.FlashAttention = true
                rec.FlashAttnState = "Supported"
        } else {
                rec.FlashAttention = false
                rec.FlashAttnState = "Unsupported"
                rec.Notes = append(rec.Notes,
                        "This engine build does not support --flash-attn — keep it off.")
        }

        // Cache reuse: keep the effective engine default when supported.
        if caps != nil && caps.CacheReuse {
                rec.CacheReuse = cfg.EffectiveCacheReuse()
        }

        // Context: never exceed the loaded model's own training limit.
        if loadedModelPath != "" {
                if card, err := llm.ReadModelCard(loadedModelPath); err == nil &&
                        card != nil && card.ContextLength > 0 && rec.Context > card.ContextLength {
                        rec.Context = card.ContextLength
                        rec.Notes = append(rec.Notes,
                                "Context clamped to the loaded model's training limit.")
                }
        }

        return rec
}

// buildRequestTimings renders the recent per-request measured timelines
// (v1.2.5) — the Advanced/System diagnostics view's core table.
func (s *Server) buildRequestTimings() []requestTimingAPI {
        var out []requestTimingAPI

        if s.stack.Telemetry == nil {
                return nil
        }

        for _, rec := range s.stack.Telemetry.Recent(10) {
                out = append(out, requestTimingAPI{
                        At:                rec.At,
                        SessionID:         rec.SessionID,
                        Tier:              rec.Tier,
                        FinalTier:         rec.FinalTier,
                        Escalations:       rec.Escalations,
                        ThinkingControl:   rec.ThinkingControl,
                        ToolPolicyMode:    rec.ToolPolicyMode,
                        FirstPromptTokens: rec.Timing.FirstPromptTokens,
                        ClassifyMs:        rec.Timing.ClassifyMs,
                        ContextMs:         rec.Timing.ContextMs,
                        PromptMs:          rec.Timing.PromptMs,
                        SerializationMs:   rec.Timing.SerializationMs,
                        TTFTMs:            rec.Timing.TTFTMs,
                        GenerationMs:      rec.Timing.GenerationMs,
                        ToolMs:            rec.Timing.ToolMs,
                        VerificationMs:    rec.Timing.VerificationMs,
                        TotalMs:           rec.Timing.TotalMs,
                        ToolCalls:         rec.ToolCalls,
                        Verified:          rec.Verified,
                })
        }

        return out
}

// buildOwnershipLevels renders the data-ownership ladder (v1.2.5).
func (s *Server) buildOwnershipLevels() []ownershipLevelAPI {
        if s.stack.MemMgr == nil {
                return nil
        }

        var out []ownershipLevelAPI

        for _, lv := range s.stack.MemMgr.OwnershipSnapshot() {
                out = append(out, ownershipLevelAPI{
                        Level:       string(lv.Level),
                        Owners:      lv.Owners,
                        Names:       lv.Names,
                        Bytes:       lv.Bytes,
                        At:          lv.At,
                        Description: lv.Description,
                })
        }

        return out
}
