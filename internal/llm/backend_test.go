package llm

// backend_test.go — backend contract tests (v1.1.5 Phase 1).
//
// Coverage required by the Phase 1 plan:
//   - backend selection (native selected + capable / incapable / absent;
//     llama default; malformed config values fail closed to llama)
//   - llama backend: generation delegation, cancel semantics, metrics
//     with measured values only, model info, hardware from sysinfo
//   - existing LLM compatibility (the fake-engine re-exec suite in
//     llama_test.go keeps pinning the underlying paths)

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// fakeBackend is a scriptable llm.Backend for selection tests.
type fakeBackend struct {
	name      string
	capable   bool
	hasCapFn  bool
	fallback  string // reported by GenerationFallbackReason (v1.3.2)
	started   int
	stopped   int
	cancelled []string
}

// GenerationFallbackReason implements the optional v1.3.2 observability
// probe (empty when capable).
func (f *fakeBackend) GenerationFallbackReason() string {
	if f.capable {
		return ""
	}
	if f.fallback != "" {
		return f.fallback
	}
	return "not capable"
}

func (f *fakeBackend) Name() string { return f.name }

func (f *fakeBackend) Start(ctx context.Context) error {
	f.started++
	return nil
}

func (f *fakeBackend) Stop(ctx context.Context) error {
	f.stopped++
	return nil
}

func (f *fakeBackend) Health(ctx context.Context) (HealthReport, error) {
	return HealthReport{State: StateReady, Alive: true}, nil
}

func (f *fakeBackend) LoadModel(ctx context.Context, spec ModelSpec) error {
	return nil
}

func (f *fakeBackend) UnloadModel(ctx context.Context) error { return nil }

func (f *fakeBackend) Generate(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{}, nil
}

func (f *fakeBackend) StreamGenerate(ctx context.Context, req *ChatRequest, onEvent func(StreamEvent) error) (PerfStats, error) {
	return PerfStats{}, nil
}

func (f *fakeBackend) Cancel(ctx context.Context, requestID string) error {
	f.cancelled = append(f.cancelled, requestID)
	return nil
}

func (f *fakeBackend) ModelInfo(ctx context.Context) (ModelInfo, error) {
	return ModelInfo{Backend: f.name}, nil
}

func (f *fakeBackend) HardwareInfo(ctx context.Context) (HardwareInfo, error) {
	return HardwareInfo{Backend: f.name}, nil
}

func (f *fakeBackend) Metrics(ctx context.Context) (Metrics, error) {
	return Metrics{Backend: f.name, EngineState: StateReady}, nil
}

// GenerationCapable implements the optional capability probe.
func (f *fakeBackend) GenerationCapable() bool { return f.capable }

// --- selection tests -----------------------------------------------------

func TestSelectGenerationBackendDefaultsToLlama(t *testing.T) {
	cfg := config.Default()
	cfg.EngineBackend = ""

	native := &fakeBackend{name: "native", capable: true}
	llama := &fakeBackend{name: "llama"}

	got := SelectGenerationBackend(cfg, native, llama)

	if got.Name() != "llama" {
		t.Fatalf("default selection = %q, want llama", got.Name())
	}
}

func TestSelectGenerationBackendFallsBackWhenNotCapable(t *testing.T) {
	// Native selected but generation-incapable → llama.
	cfg := config.Default()
	cfg.EngineBackend = "native"

	native := &fakeBackend{name: "native", capable: false}
	llama := &fakeBackend{name: "llama"}

	got := SelectGenerationBackend(cfg, native, llama)

	if got.Name() != "llama" {
		t.Fatalf("selection = %q, want llama fallback", got.Name())
	}
}

func TestSelectGenerationBackendUsesNativeWhenCapable(t *testing.T) {
	// Phase 2 shape: native selected AND capable → native.
	cfg := config.Default()
	cfg.EngineBackend = "native"

	native := &fakeBackend{name: "native", capable: true}
	llama := &fakeBackend{name: "llama"}

	got := SelectGenerationBackend(cfg, native, llama)

	if got.Name() != "native" {
		t.Fatalf("selection = %q, want native", got.Name())
	}
}

func TestSelectGenerationBackendNativeAbsent(t *testing.T) {
	cfg := config.Default()
	cfg.EngineBackend = "native"

	llama := &fakeBackend{name: "llama"}

	got := SelectGenerationBackend(cfg, nil, llama)

	if got.Name() != "llama" {
		t.Fatalf("selection = %q, want llama when native is nil", got.Name())
	}
}

func TestSelectGenerationBackendMalformedConfigFailsClosed(t *testing.T) {
	for _, raw := range []string{"NATIVE", " native ", "bogus", "llama!!"} {
		cfg := config.Default()
		cfg.EngineBackend = raw

		native := &fakeBackend{name: "native", capable: true}
		llama := &fakeBackend{name: "llama"}

		got := SelectGenerationBackend(cfg, native, llama)

		if got.Name() != "llama" {
			t.Fatalf("EngineBackend=%q selected %q, want llama (fail closed)", raw, got.Name())
		}
	}
}

func TestSelectGenerationBackendNoCapabilityProbe(t *testing.T) {
	// A backend without the optional GenerationCapable probe is treated
	// as capable (the llama backend relies on this). bareBackend embeds
	// the interface (NOT the concrete fakeBackend) so the capability
	// method is not promoted.
	cfg := config.Default()
	cfg.EngineBackend = "native"

	native := &bareBackend{inner: &fakeBackend{name: "native", capable: false}}
	llama := &fakeBackend{name: "llama"}

	got := SelectGenerationBackend(cfg, native, llama)

	if got.Name() != "native" {
		t.Fatalf("selection = %q, want native (no probe = capable)", got.Name())
	}
}

// --- v1.3.2: observable selection (no silent fallback) --------------------

func TestSelectGenerationBackendDetailedExposesFallbackReason(t *testing.T) {
	// The user selected native but it cannot serve: the decision MUST
	// carry the backend's own inspectable reason — the fallback is
	// documented behavior, but never a silent one.
	cfg := config.Default()
	cfg.EngineBackend = "native"

	native := &fakeBackend{
		name:     "native",
		capable:  false,
		fallback: "native engine not running (state failed: host binary not found)",
	}
	llama := &fakeBackend{name: "llama"}

	d := SelectGenerationBackendDetailed(cfg, native, llama)

	if d.Backend.Name() != "llama" || d.SelectedName != "llama" {
		t.Fatalf("decision = %+v, want llama serving", d)
	}
	if d.FallbackReason != native.fallback {
		t.Fatalf("FallbackReason = %q, want the backend's own reason", d.FallbackReason)
	}
}

func TestSelectGenerationBackendDetailedReasonDefaultsWhenReporterAbsent(t *testing.T) {
	// A backend with the capability probe but WITHOUT the optional
	// reporter still gets a non-empty generic reason — the fallback
	// cannot be silent either way. capOnlyBackend exposes
	// GenerationCapable only.
	cfg := config.Default()
	cfg.EngineBackend = "native"

	native := &capOnlyBackend{inner: &fakeBackend{name: "native", capable: false}}
	llama := &fakeBackend{name: "llama"}

	d := SelectGenerationBackendDetailed(cfg, native, llama)

	if d.SelectedName != "llama" {
		t.Fatalf("decision = %+v, want llama serving", d)
	}
	if d.FallbackReason == "" {
		t.Fatal("fallback reason must never be empty when native was selected but fell back")
	}
	if d.FallbackReason == "not capable" {
		t.Fatalf("FallbackReason = %q — the fake's own reason must NOT leak through capOnlyBackend", d.FallbackReason)
	}
}

// capOnlyBackend exposes ONLY the GenerationCapable probe (no fallback
// reporter) — the v1.3.2 default-reason path.
type capOnlyBackend struct {
	inner *fakeBackend
}

func (c *capOnlyBackend) Name() string                    { return c.inner.Name() }
func (c *capOnlyBackend) Start(ctx context.Context) error { return c.inner.Start(ctx) }
func (c *capOnlyBackend) Stop(ctx context.Context) error  { return c.inner.Stop(ctx) }
func (c *capOnlyBackend) Health(ctx context.Context) (HealthReport, error) {
	return c.inner.Health(ctx)
}
func (c *capOnlyBackend) LoadModel(ctx context.Context, s ModelSpec) error {
	return c.inner.LoadModel(ctx, s)
}
func (c *capOnlyBackend) UnloadModel(ctx context.Context) error { return c.inner.UnloadModel(ctx) }
func (c *capOnlyBackend) Generate(ctx context.Context, r *ChatRequest) (*ChatResponse, error) {
	return c.inner.Generate(ctx, r)
}
func (c *capOnlyBackend) StreamGenerate(ctx context.Context, r *ChatRequest, fn func(StreamEvent) error) (PerfStats, error) {
	return c.inner.StreamGenerate(ctx, r, fn)
}
func (c *capOnlyBackend) Cancel(ctx context.Context, id string) error { return c.inner.Cancel(ctx, id) }
func (c *capOnlyBackend) ModelInfo(ctx context.Context) (ModelInfo, error) {
	return c.inner.ModelInfo(ctx)
}
func (c *capOnlyBackend) HardwareInfo(ctx context.Context) (HardwareInfo, error) {
	return c.inner.HardwareInfo(ctx)
}
func (c *capOnlyBackend) Metrics(ctx context.Context) (Metrics, error) { return c.inner.Metrics(ctx) }
func (c *capOnlyBackend) GenerationCapable() bool                      { return c.inner.capable }

func TestSelectGenerationBackendDetailedNativeServingHasNoFallback(t *testing.T) {
	cfg := config.Default()
	cfg.EngineBackend = "native"

	native := &fakeBackend{name: "native", capable: true}
	llama := &fakeBackend{name: "llama"}

	d := SelectGenerationBackendDetailed(cfg, native, llama)

	if d.SelectedName != "native" {
		t.Fatalf("decision = %+v, want native serving", d)
	}
	if d.FallbackReason != "" {
		t.Fatalf("FallbackReason = %q, want empty when native serves", d.FallbackReason)
	}
}

func TestSelectGenerationBackendDetailedLlamaDefaultIsNotAFallback(t *testing.T) {
	// The user never selected the native engine: llama serving is the
	// DEFAULT, not a fallback — the reason must stay empty.
	cfg := config.Default()
	cfg.EngineBackend = ""

	native := &fakeBackend{name: "native", capable: false}
	llama := &fakeBackend{name: "llama"}

	d := SelectGenerationBackendDetailed(cfg, native, llama)

	if d.SelectedName != "llama" {
		t.Fatalf("decision = %+v, want llama default", d)
	}
	if d.FallbackReason != "" {
		t.Fatalf("FallbackReason = %q, want empty (default is not a fallback)", d.FallbackReason)
	}
}

// bareBackend hides every optional capability method behind the plain
// interface.
type bareBackend struct {
	inner Backend
}

func (b *bareBackend) Name() string                                     { return b.inner.Name() }
func (b *bareBackend) Start(ctx context.Context) error                  { return b.inner.Start(ctx) }
func (b *bareBackend) Stop(ctx context.Context) error                   { return b.inner.Stop(ctx) }
func (b *bareBackend) Health(ctx context.Context) (HealthReport, error) { return b.inner.Health(ctx) }
func (b *bareBackend) LoadModel(ctx context.Context, s ModelSpec) error {
	return b.inner.LoadModel(ctx, s)
}
func (b *bareBackend) UnloadModel(ctx context.Context) error { return b.inner.UnloadModel(ctx) }
func (b *bareBackend) Generate(ctx context.Context, r *ChatRequest) (*ChatResponse, error) {
	return b.inner.Generate(ctx, r)
}
func (b *bareBackend) StreamGenerate(ctx context.Context, r *ChatRequest, fn func(StreamEvent) error) (PerfStats, error) {
	return b.inner.StreamGenerate(ctx, r, fn)
}
func (b *bareBackend) Cancel(ctx context.Context, id string) error { return b.inner.Cancel(ctx, id) }
func (b *bareBackend) ModelInfo(ctx context.Context) (ModelInfo, error) {
	return b.inner.ModelInfo(ctx)
}
func (b *bareBackend) HardwareInfo(ctx context.Context) (HardwareInfo, error) {
	return b.inner.HardwareInfo(ctx)
}
func (b *bareBackend) Metrics(ctx context.Context) (Metrics, error) {
	return b.inner.Metrics(ctx)
}

// --- llama backend tests ---------------------------------------------------

// startFakeOpenAI serves one chat completion response.
func startFakeOpenAI(t *testing.T) (*httptest.Server, *config.Source) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test-model",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": "hello from the fake engine",
					},
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     5,
				"completion_tokens": 7,
				"total_tokens":      12,
			},
		})
	}))

	t.Cleanup(srv.Close)

	cfg := config.Default()
	cfg.LLMBaseURL = srv.URL + "/v1"
	cfg.Model = "test-model"

	return srv, config.NewSource(cfg)
}

func TestLlamaBackendGenerateDelegates(t *testing.T) {
	_, src := startFakeOpenAI(t)

	client := NewClient(src)
	server := NewLlamaServer(src)
	backend := NewLlamaBackend(server, client)

	resp, err := backend.Generate(context.Background(), &ChatRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestLlamaBackendCancelSemantics(t *testing.T) {
	_, src := startFakeOpenAI(t)

	backend := NewLlamaBackend(NewLlamaServer(src), NewClient(src))

	err := backend.Cancel(context.Background(), "run-1")
	if err == nil {
		t.Fatal("llama cancel must report context-based cancellation")
	}
}

func TestLlamaBackendMetricsMeasuredValuesOnly(t *testing.T) {
	_, src := startFakeOpenAI(t)

	backend := NewLlamaBackend(NewLlamaServer(src), NewClient(src))

	m, err := backend.Metrics(context.Background())
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}

	if m.Backend != "llama" {
		t.Fatalf("backend = %q", m.Backend)
	}

	if m.EngineState != StateIdle {
		t.Fatalf("engineState = %q, want idle before any start", m.EngineState)
	}

	// Values that cannot exist before a boot must stay absent.
	if m.UptimeSeconds != 0 {
		t.Fatalf("uptime measured without a boot: %v", m.UptimeSeconds)
	}

	if m.ProcessRSSBytes != 0 {
		t.Fatal("RSS must not be invented for the llama backend")
	}

	if m.DecodeTokensPerSecond != 0 {
		t.Fatal("decode speed must not be invented without generation")
	}
}

func TestLlamaBackendHardwareFromSysInfo(t *testing.T) {
	_, src := startFakeOpenAI(t)

	backend := NewLlamaBackend(NewLlamaServer(src), NewClient(src))

	hw, err := backend.HardwareInfo(context.Background())
	if err != nil {
		t.Fatalf("hardware: %v", err)
	}

	if hw.Backend != "llama" {
		t.Fatalf("backend = %q", hw.Backend)
	}

	if hw.Architecture == "" {
		t.Fatal("architecture missing")
	}

	if hw.CPU.LogicalCores <= 0 {
		t.Fatalf("logical cores = %d, want > 0", hw.CPU.LogicalCores)
	}

	if hw.RAM.TotalBytes == 0 {
		t.Fatal("RAM total missing")
	}

	found := false

	for _, src := range hw.DetectedBy {
		if src == "sysinfo" {
			found = true
		}
	}

	if !found {
		t.Fatalf("detectedBy = %v, want sysinfo", hw.DetectedBy)
	}
}

func TestLlamaBackendHealthHonestWhenDown(t *testing.T) {
	_, src := startFakeOpenAI(t)

	backend := NewLlamaBackend(NewLlamaServer(src), NewClient(src))

	report, err := backend.Health(context.Background())
	if err == nil {
		t.Fatal("health must fail when the engine is down")
	}

	if report.Alive {
		t.Fatalf("report = %+v, want alive=false", report)
	}

	if report.State != StateIdle {
		t.Fatalf("state = %q, want idle", report.State)
	}
}

func TestLlamaBackendLoadModelValidates(t *testing.T) {
	_, src := startFakeOpenAI(t)

	backend := NewLlamaBackend(NewLlamaServer(src), NewClient(src))

	// A model that does not resolve must be rejected before any restart.
	err := backend.LoadModel(context.Background(), ModelSpec{Path: "definitely-not-here.gguf"})
	if err == nil {
		t.Fatal("expected error for missing model")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty spec is a contract violation.
	if err := backend.LoadModel(context.Background(), ModelSpec{}); err == nil {
		t.Fatal("expected error for empty spec")
	}
}

func TestLlamaBackendModelInfoIdle(t *testing.T) {
	_, src := startFakeOpenAI(t)

	cfg := src.Load()
	cfg.Model = "test-model"

	backend := NewLlamaBackend(NewLlamaServer(src), NewClient(src))

	info, err := backend.ModelInfo(context.Background())
	if err != nil {
		t.Fatalf("model info: %v", err)
	}

	if info.Backend != "llama" {
		t.Fatalf("backend = %q", info.Backend)
	}

	if info.Loaded {
		t.Fatal("engine is down; loaded must be false")
	}

	if !strings.Contains(info.ModelPath, "test-model") {
		t.Fatalf("modelPath = %q", info.ModelPath)
	}
}

func TestMetricsStringCompact(t *testing.T) {
	m := Metrics{
		Backend:     "native",
		EngineState: "ready",
		Pid:         42,
	}

	s := m.String()

	for _, want := range []string{"native", "ready", "pid=42"} {
		if !strings.Contains(s, want) {
			t.Fatalf("String() = %q, want to contain %q", s, want)
		}
	}

	if strings.Contains(s, "decode=") {
		t.Fatalf("String() must not print absent metrics: %q", s)
	}
}

func TestBackendContractConstants(t *testing.T) {
	// The backend names must match the config constants so selection,
	// snapshot payloads and documentation agree.
	if config.BackendLlama != "llama" {
		t.Fatalf("BackendLlama = %q", config.BackendLlama)
	}

	if config.BackendNative != "native" {
		t.Fatalf("BackendNative = %q", config.BackendNative)
	}

	// ErrNotImplemented must be detectable with errors.Is at call sites.
	if ErrNotImplemented == nil {
		t.Fatal("ErrNotImplemented must exist")
	}

	if ErrCancelContextBased == nil {
		t.Fatal("ErrCancelContextBased must exist")
	}

	// Sanity: the interface method set the native backend must satisfy.
	var _ interface {
		GenerationCapable() bool
	} = (*fakeBackend)(nil)

	// Compile-time interface conformance for the llama backend.
	var _ Backend = (*LlamaBackend)(nil)
}

// fake for fmt import retention
var _ = fmt.Sprintf
