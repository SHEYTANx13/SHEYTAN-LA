package llm

// LlamaBackend adapts the two existing llama.cpp pieces — LlamaServer
// (subprocess lifecycle, authoritative engine state) and Client
// (OpenAI-compatible generation) — to the Backend contract (v1.1.5
// Phase 1).
//
// It is a thin delegation layer: every method forwards to the exact code
// path that already ships in v1.1.4, so streaming, retries, the stall
// watchdog, cancellation, busy reporting, engine events and the config
// snapshot contract are preserved byte-for-byte. No functionality is
// duplicated here — the backend is the seam, not a second engine.

import (
        "context"
        "fmt"
        "path/filepath"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/resources"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
)

// LlamaBackend is the managed llama.cpp engine behind the Backend contract.
type LlamaBackend struct {
        server *LlamaServer
        client *Client
}

// compile-time contract check.
var _ Backend = (*LlamaBackend)(nil)

// NewLlamaBackend wraps an existing LlamaServer + Client pair. Both must
// share the same config.Source (they do in every runtime wiring).
func NewLlamaBackend(server *LlamaServer, client *Client) *LlamaBackend {
        return &LlamaBackend{server: server, client: client}
}

// Server exposes the wrapped LlamaServer for the legacy concrete paths
// (runtime engine gates, API engine snapshot, vision check). New code
// should program against the Backend interface.
func (b *LlamaBackend) Server() *LlamaServer { return b.server }

// Client exposes the wrapped generation client (the orchestrator's
// generation path). New code should program against the Backend interface.
func (b *LlamaBackend) Client() *Client { return b.client }

// Name implements Backend.
func (b *LlamaBackend) Name() string { return config.BackendLlama }

// Start implements Backend. It boots the llama.cpp engine via the
// authoritative LlamaServer.Start (compat ladder, bounded waits, event
// publication). The ctx bounds how long the CALLER is willing to wait —
// the boot itself keeps progressing in the background exactly like
// Stack.EnsureLLMContext does today.
func (b *LlamaBackend) Start(ctx context.Context) error {
        if err := ctx.Err(); err != nil {
                return err
        }

        if b.server.IsRunning() {
                return nil
        }

        errCh := make(chan error, 1)

        go func() {
                errCh <- b.server.Start()
        }()

        select {
        case err := <-errCh:
                return err
        case <-ctx.Done():
                return fmt.Errorf("engine startup still in progress: %w", ctx.Err())
        }
}

// Stop implements Backend (graceful SIGTERM → bounded grace → kill, the
// v1.1.4 path unchanged).
func (b *LlamaBackend) Stop(ctx context.Context) error {
        // Stop is bounded internally (4 s grace); ctx is honored as a
        // pre-check only — a partially-stopped engine must still finish
        // tearing down deterministically.
        _ = ctx
        return b.server.Stop()
}

// Health implements Backend with a REAL probe: LlamaServer.ProbeHealth
// GETs the llama.cpp /health endpoint (bounded 2 s) and only answers
// healthy when the authoritative state machine also says the subprocess
// is alive.
func (b *LlamaBackend) Health(ctx context.Context) (HealthReport, error) {
        err := b.server.ProbeHealth(ctx)

        report := HealthReport{
                State:     b.server.State(),
                Alive:     b.server.IsRunning(),
                Detail:    b.server.Detail(),
                Timestamp: time.Now().UTC(),
        }

        return report, err
}

// LoadModel implements Backend by persisting the model choice through the
// sanctioned config.Source path and relaunching the engine (llama.cpp
// binds the model at process start). A stopped engine just persists the
// choice for the next Start.
func (b *LlamaBackend) LoadModel(ctx context.Context, spec ModelSpec) error {
        if spec.Path == "" {
                return fmt.Errorf("model spec path is empty")
        }

        cfg := b.server.src.Load()

        if _, err := ResolveModelPath(cfg.ModelsDir, spec.Path); err != nil {
                return fmt.Errorf("model not found: %w", err)
        }

        next := b.server.src.Update(func(c *config.Config) {
                c.Model = spec.Path
        })

        if err := config.Save(next.ConfigPath(), next); err != nil {
                return fmt.Errorf("persist model selection: %w", err)
        }

        if b.server.IsRunning() {
                return b.server.Restart()
        }

        return nil
}

// UnloadModel implements Backend. llama.cpp cannot unload a model without
// terminating the server, so this stops the engine.
func (b *LlamaBackend) UnloadModel(ctx context.Context) error {
        return b.server.Stop()
}

// Generate implements Backend via the existing non-streaming client path
// (retries, transient-error classification, logging — unchanged).
func (b *LlamaBackend) Generate(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
        return b.client.Chat(ctx, req)
}

// StreamGenerate implements Backend via StreamChatDetailed (stall watchdog,
// retry-before-first-token, busy hook, perf stats — unchanged).
func (b *LlamaBackend) StreamGenerate(ctx context.Context, req *ChatRequest, onEvent func(StreamEvent) error) (PerfStats, error) {
        return b.client.StreamChatDetailed(ctx, req, onEvent)
}

// Cancel implements Backend. The llama backend cancels generation through
// request contexts (the run registry aborts the run ctx, which closes the
// SSE stream); there is no server-side request id to cancel, so callers
// get ErrCancelContextBased instead of a fake success.
func (b *LlamaBackend) Cancel(ctx context.Context, requestID string) error {
        _ = ctx
        _ = requestID
        return ErrCancelContextBased
}

// ModelInfo implements Backend from measured values: the resolved loaded
// model path, the engine's own /v1/models listing when alive, and the GGUF
// card of the loaded file when parseable.
func (b *LlamaBackend) ModelInfo(ctx context.Context) (ModelInfo, error) {
        _ = ctx

        info := ModelInfo{
                Backend: b.Name(),
                Loaded:  b.server.IsRunning(),
        }

        loaded := b.server.LoadedModel()

        if loaded != "" {
                info.ModelPath = loaded

                if card, err := ReadModelCard(loaded); err == nil && card != nil {
                        info.Architecture = card.Arch
                        info.Quantization = card.Quant
                        info.ContextLength = card.ContextLength
                        info.Parameters = card.FormatParams()
                }
        } else if cfg := b.server.src.Load(); cfg.Model != "" {
                if resolved, err := ResolveModelPath(cfg.ModelsDir, cfg.Model); err == nil {
                        info.ModelPath = resolved
                } else {
                        info.ModelPath = cfg.Model
                }
        }

        if b.server.IsRunning() {
                if ids, err := b.server.ListLoadedModels(); err == nil {
                        info.LoadedIDs = ids
                }
        }

        return info, nil
}

// HardwareInfo implements Backend from the sysinfo probe (CIM-first on
// Windows, nvidia-smi/CL on Linux — real detected values only).
func (b *LlamaBackend) HardwareInfo(ctx context.Context) (HardwareInfo, error) {
        _ = ctx
        return hardwareFromSysInfo(sysinfo.Probe(), b.Name()), nil
}

// Metrics implements Backend from measured lifecycle facts only: engine
// state, model, pid, uptime (since last successful boot) and the restart
// count of the current episode. Generation-time metrics are per-request
// PerfStats on the client path, not engine lifetime values, so they are
// omitted here rather than faked.
func (b *LlamaBackend) Metrics(ctx context.Context) (Metrics, error) {
        _ = ctx

        m := Metrics{
                Backend:     b.Name(),
                EngineState: b.server.State(),
                Pid:         b.server.Pid(),
                Restarts:    b.server.Restarts(),
        }

        if loaded := b.server.LoadedModel(); loaded != "" {
                m.Model = filepath.Base(loaded)
        }

        if started := b.server.StartedAt(); !started.IsZero() {
                m.UptimeSeconds = time.Since(started).Seconds()
        }

        // v1.8.6 (§8 real engine memory evidence): the ENGINE process's
        // resident set, MEASURED through the existing OS sampling path
        // (resources.ProcRAM — the same authority the live monitor uses for
        // the Go process). A failed read leaves the field unset (an honest
        // unknown — the Metrics contract never fakes a measurement). This is
        // what feeds the Governor's engine-RSS fact through the Stack wiring.
        if pid := m.Pid; pid > 0 {
                if rss, err := resources.ProcRAM(pid); err == nil && rss > 0 {
                        m.ProcessRSSBytes = uint64(rss)
                }
        }

        return m, nil
}

// hardwareFromSysInfo converts the sysinfo probe result into the
// platform-neutral hardware profile. Only fields sysinfo actually detects
// are populated; GPUs without VRAM detection keep zero; accelerators stay
// empty until a detector exists.
func hardwareFromSysInfo(si *sysinfo.SysInfo, backend string) HardwareInfo {
        if si == nil {
                return HardwareInfo{Backend: backend}
        }

        hw := HardwareInfo{
                Backend:      backend,
                Architecture: si.Arch,
                OS:           si.OS,
                CPU: CPUHardware{
                        Name:          si.CPU.Name,
                        PhysicalCores: si.CPU.PhysicalCores,
                        LogicalCores:  si.CPU.LogicalCores,
                        FrequencyMHz:  si.CPU.FrequencyMHz,
                },
                RAM: RAMHardware{
                        TotalBytes:     si.RAM.TotalBytes,
                        AvailableBytes: si.RAM.Available,
                },
                DetectedBy: []string{"sysinfo"},
        }

        for _, gpu := range si.GPU {
                hw.GPUs = append(hw.GPUs, GPUHardware{
                        Vendor:        gpu.Vendor,
                        Name:          gpu.Name,
                        VRAMBytes:     gpu.VRAMBytes,
                        DriverVersion: gpu.DriverVer,
                })
        }

        return hw
}

// BackendCapabilities implements llm.CapabilityReporter (v1.7.1 §5.3):
// the llama.cpp serving alternative reports its contract from measured
// state — streaming/cancellation are real; the context window is the
// ENGINE-VERIFIED value (0 until the engine has proven one); the Vulkan
// variant distinguishes itself through the managed binary's runtime.
func (b *LlamaBackend) BackendCapabilities() BackendCapabilities {
        caps := BackendCapabilities{
                Identity:               "llama.cpp-cpu",
                GenerationCapable:      true,
                SupportedArchitectures: "llama graph family (llama.cpp)",
                Streaming:              true,
                Cancellation:           true,
                Readiness:              b.server.State(),
        }
        if b.server != nil {
                if b.server.hasVulkanBackend(b.server.src.Load()) {
                        caps.Identity = "llama.cpp-vulkan"
                        caps.Devices = "vulkan"
                } else {
                        caps.Devices = "cpu"
                }
                if limit := b.server.EngineContextLimit(); limit > 0 {
                        caps.ContextCapability = limit
                }
        }
        return caps
}
