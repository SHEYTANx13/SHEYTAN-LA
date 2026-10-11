package engine

// backend.go — the native engine adapter for the llm.Backend contract
// (v1.1.5 Phase 5: REAL generation).
//
// The lifecycle, health, hardware, metrics, cancel and MODEL surfaces
// (LoadModel / UnloadModel / ModelInfo — native GGUF loading with
// metadata + memory plan + the generation-capability verdict) are REAL,
// backed by the supervised host subprocess and the IPC protocol.
//
// Phase 5: Generate / StreamGenerate are REAL — the native engine runs
// the actual transformer forward pass (llama architecture) and streams
// coarse chunks back. GenerationCapable() reports true only when the
// engine is alive AND the loaded model validated natively executable
// (llama graph + supported tensor types + tokenizer). Requests the native
// path cannot serve (tools, images — documented Phase 5 limits) return
// llm.ErrNotImplemented, which the selection layer maps to the llama.cpp
// fallback.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// ABIVersionExpected is the C ABI version the Go core expects from the
// native engine (must match include/shtn/version.h).
const ABIVersionExpected uint32 = 4

// Backend adapts an *Engine to the llm.Backend contract.
type Backend struct {
	eng *Engine
}

// compile-time contract check.
var _ llm.Backend = (*Backend)(nil)

// NewBackend wraps an Engine.
func NewBackend(eng *Engine) *Backend { return &Backend{eng: eng} }

// Engine exposes the supervised engine.
func (b *Backend) Engine() *Engine { return b.eng }

// Name implements Backend.
func (b *Backend) Name() string { return "native" }

// Start implements Backend: spawn + handshake + health + ready.
func (b *Backend) Start(ctx context.Context) error { return b.eng.Start(ctx) }

// Stop implements Backend: graceful ask, bounded grace, kill.
func (b *Backend) Stop(ctx context.Context) error { return b.eng.Stop(ctx) }

// Health implements Backend with a real IPC round-trip.
func (b *Backend) Health(ctx context.Context) (llm.HealthReport, error) {
	return b.eng.Health(ctx)
}

// LoadModel implements Backend: the engine validates the file, loads it
// natively (GGUF validate → memory-map → metadata → memory plan → llama
// graph validation) and reports real model state.
//
// v1.2.9: a successful load ALSO initializes the model's tokenizer and,
// when the tokenizer is supported, installs the EXACT token-counting tier
// used by the context planner (chunking.SetExactTokenCounter). This is
// the "exact model/engine tokenizer accounting when available" tier of
// the context-budget chain; the counter is memoized (bounded) so inner
// planning loops do not pay an IPC round-trip per call, and any encode
// failure degrades to the family/heuristic estimator instead of trusting
// a broken number.
func (b *Backend) LoadModel(ctx context.Context, spec llm.ModelSpec) error {
	if spec.Path == "" {
		return fmt.Errorf("model spec path is empty")
	}

	// Plain file validation (the backend does not know the models
	// root; a path jail can be applied by callers through
	// engine.ValidateModelSpec).
	if _, err := statRegularFile(spec.Path); err != nil {
		return err
	}

	if err := b.eng.LoadModel(ctx, ModelSpec{Path: spec.Path}); err != nil {
		return err
	}

	b.wireExactTokenizer(ctx)
	return nil
}

// tokenCountMemo bounds the exact-counter memo so repeated planning
// measurements of the same strings stay IPC-free. Guarded by its own
// mutex; capped, evict-all-on-full (simple and predictable).
type tokenCountMemo struct {
	mu    sync.Mutex
	cap   int
	count map[string]int
}

var tokenizerMemo = &tokenCountMemo{cap: 512, count: map[string]int{}}

func (m *tokenCountMemo) get(s string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.count[s]
	return n, ok
}

func (m *tokenCountMemo) put(s string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.count) >= m.cap {
		m.count = map[string]int{} // bounded: reset rather than grow
	}
	m.count[s] = n
}

// wireExactTokenizer initializes the tokenizer for the loaded model and
// installs the exact counting tier when it is actually available.
// Unsupported tokenizer kinds keep the estimator chain on its lower
// tiers (family/heuristic) — never a silent wrong number.
func (b *Backend) wireExactTokenizer(ctx context.Context) {
	res, err := b.eng.InitTokenizer(ctx)
	if err != nil || !res.Initialized {
		// ErrUnsupportedTokenizer and any init failure: leave the
		// estimator chain on its lower tiers.
		chunking.ResetTokenEstimator()
		return
	}

	eng := b.eng
	counter := func(s string) int {
		if s == "" {
			return 0
		}
		if n, ok := tokenizerMemo.get(s); ok {
			return n
		}
		// Count without BOS/EOS (the planner counts content tokens) and
		// without an output cap that could silently truncate.
		enc, err := eng.TokenizerEncode(context.Background(), s, EncodeOptions{MaxTokens: 1 << 20})
		if err != nil {
			return -1 // fall through to the estimate tiers
		}
		if enc.Truncated {
			return -1 // a truncated count is a lower bound, not a count
		}
		n := int(enc.Count)
		tokenizerMemo.put(s, n)
		return n
	}

	chunking.SetExactTokenCounter(counter)
}

// UnloadModel implements Backend: releases the natively loaded model
// (mapping, handles, cached metadata, generation binding). Idempotent.
// v1.2.9: it also drops the exact tokenizer tier — the memo belongs to a
// model that is no longer loaded.
func (b *Backend) UnloadModel(ctx context.Context) error {
	chunking.ResetTokenEstimator()
	tokenizerMemo.mu.Lock()
	tokenizerMemo.count = map[string]int{}
	tokenizerMemo.mu.Unlock()
	return b.eng.UnloadModel(ctx)
}

// buildNativePrompt renders a ChatRequest's messages into the plain
// prompt text the native engine tokenizes.
//
// KNOWN LIMITATION (documented, not hidden): the native path uses a
// simple deterministic role-labeled format — it does NOT interpret the
// model's chat template (no Jinja engine exists in the native backend).
// llama.cpp retains full chat-template fidelity on its path; instruct
// models generate usable-but-not-template-perfect continuations here.
func buildNativePrompt(req *llm.ChatRequest) string {
	var sb strings.Builder
	for _, m := range req.Messages {
		content := m.Content
		if content == "" {
			continue
		}
		switch m.Role {
		case "system":
			sb.WriteString("System: ")
			sb.WriteString(content)
			sb.WriteString("\n\n")
		case "user":
			sb.WriteString("User: ")
			sb.WriteString(content)
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("Assistant: ")
			sb.WriteString(content)
			sb.WriteString("\n\n")
		case "tool":
			sb.WriteString("Tool: ")
			sb.WriteString(content)
			sb.WriteString("\n\n")
		default:
			sb.WriteString(content)
			sb.WriteString("\n\n")
		}
	}
	sb.WriteString("Assistant:")
	return sb.String()
}

// nativeSupportedRequest reports whether the native generation path can
// serve this request shape (Phase 5: plain text only — tool schemas and
// multimodal content stay on llama.cpp, returned as the explicit
// ErrNotImplemented fallback signal).
func nativeSupportedRequest(req *llm.ChatRequest) (bool, string) {
	if len(req.Tools) > 0 {
		return false, "request carries tool schemas (native tool-call formatting is a later phase)"
	}
	for _, m := range req.Messages {
		if len(m.Images) > 0 {
			return false, "request carries images (native vision is a later phase)"
		}
	}
	if strings.TrimSpace(req.Model) != "" {
		// A model alias is fine (the engine serves the loaded model);
		// remote-provider-only fields are ignored by design.
		_ = req.Model
	}
	return true, ""
}

// Generate implements Backend (Phase 5: REAL native generation — full
// forward pass, sampler over real logits, EOS/max-token/context stops).
func (b *Backend) Generate(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if ok, reason := nativeSupportedRequest(req); !ok {
		return nil, fmt.Errorf("%w: native engine generation: %s", llm.ErrNotImplemented, reason)
	}

	var text strings.Builder
	result, err := b.eng.StreamGeneration(ctx, b.generationRequest(req), func(chunk GenerationChunk) error {
		text.WriteString(chunk.Text)
		return nil
	})
	if err != nil {
		return nil, err
	}

	finish := mapFinish(result.FinishReason)

	resp := &llm.ChatResponse{}
	resp.Choices = append(resp.Choices, struct {
		Message      llm.Message `json:"message"`
		FinishReason string      `json:"finish_reason"`
	}{Message: llm.Message{Role: "assistant", Content: text.String()},
		FinishReason: finish})
	resp.Usage.PromptTokens = int(result.PromptTokens)
	resp.Usage.CompletionTokens = int(result.GeneratedTokens)
	resp.Usage.TotalTokens = int(result.PromptTokens + result.GeneratedTokens)
	return resp, nil
}

// StreamGenerate implements Backend (Phase 5: REAL native streaming —
// coarse event chunks from the engine, converted to llm.StreamEvent).
func (b *Backend) StreamGenerate(ctx context.Context, req *llm.ChatRequest, onEvent func(llm.StreamEvent) error) (llm.PerfStats, error) {
	if ok, reason := nativeSupportedRequest(req); !ok {
		return llm.PerfStats{}, fmt.Errorf("%w: native engine streaming generation: %s", llm.ErrNotImplemented, reason)
	}

	start := time.Now()
	var ttft time.Time

	perf := llm.PerfStats{}

	result, err := b.eng.StreamGeneration(ctx, b.generationRequest(req), func(chunk GenerationChunk) error {
		if chunk.Text == "" && !chunk.Final {
			return nil
		}
		if ttft.IsZero() && chunk.Text != "" {
			ttft = time.Now()
		}
		return onEvent(llm.StreamEvent{
			Content: chunk.Text,
		})
	})
	if err != nil {
		return perf, err
	}

	// Terminal event: carries finish reason + measured usage.
	_ = onEvent(llm.StreamEvent{
		FinishReason: mapFinish(result.FinishReason),
		Usage: &struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     int(result.PromptTokens),
			CompletionTokens: int(result.GeneratedTokens),
			TotalTokens:      int(result.PromptTokens + result.GeneratedTokens),
		},
	})

	wall := time.Since(start)
	perf.Tokens = int(result.GeneratedTokens)
	perf.PromptTokens = int(result.PromptTokens)
	perf.WallMs = int(wall.Milliseconds())
	if !ttft.IsZero() {
		perf.TTFTMs = int(ttft.Sub(start).Milliseconds())
	}
	if result.GeneratedTokens > 0 && result.Metrics.DecodeSeconds > 0 {
		perf.TokensPerSec = float64(result.GeneratedTokens) / result.Metrics.DecodeSeconds
	} else if wall.Seconds() > 0 {
		perf.TokensPerSec = float64(result.GeneratedTokens) / wall.Seconds()
	}

	return perf, nil
}

// generationRequest maps the llm request onto the native payload.
func (b *Backend) generationRequest(req *llm.ChatRequest) GenerationRequest {
	g := GenerationRequest{
		RequestID:         newRequestID("shtn"),
		Prompt:            buildNativePrompt(req),
		MaxTokens:         uint32(req.MaxTokens),
		Temperature:       req.Temperature,
		TopK:              int32(req.TopK),
		TopP:              req.TopP,
		RepetitionPenalty: 1.0,
		Seed:              uint64(req.Seed),
	}
	if g.MaxTokens == 0 {
		g.MaxTokens = 256
	}
	// Repetition penalty: the shared contract exposes frequency/presence
	// penalties; the native sampler takes the classic repetition penalty.
	// Use frequency penalty as the repetition multiplier when set (same
	// scale, 1.0 = disabled), otherwise presence penalty, else 1.0.
	switch {
	case req.FrequencyPenalty > 0:
		g.RepetitionPenalty = 1.0 + req.FrequencyPenalty
	case req.PresencePenalty > 0:
		g.RepetitionPenalty = 1.0 + req.PresencePenalty
	default:
		g.RepetitionPenalty = 1.0
	}
	if req.RepeatLastN > 0 {
		g.RepeatLastN = uint32(req.RepeatLastN)
	} else {
		g.RepeatLastN = 64
	}
	return g
}

// mapFinish converts the native finish reason onto the shared contract
// vocabulary.
func mapFinish(reason string) string {
	switch reason {
	case FinishEOS:
		return "stop"
	case FinishCancelled:
		return "cancelled"
	case FinishLength:
		return "length"
	default:
		return "stop"
	}
}

// Cancel implements Backend: REAL cooperative cancellation of the
// in-flight native generation (observed by the native loop per token).
func (b *Backend) Cancel(ctx context.Context, requestID string) error {
	return b.eng.Cancel(ctx, requestID)
}

// ModelInfo implements Backend: a real round-trip to the engine's model
// concern, mapped onto the shared llm.ModelInfo contract.
func (b *Backend) ModelInfo(ctx context.Context) (llm.ModelInfo, error) {
	info := llm.ModelInfo{Backend: b.Name()}

	if b.eng == nil || !b.eng.IsAlive() {
		// Engine down: nothing can be resident.
		info.Loaded = false
		return info, nil
	}

	result, err := b.eng.ModelInfo(ctx)
	if err != nil {
		return info, err
	}

	info.Loaded = result.Loaded
	info.State = result.State

	if result.Model != nil {
		m := result.Model

		info.ModelPath = m.Path
		info.Architecture = m.Architecture
		info.Quantization = m.Quantization
		info.ContextLength = int(m.ContextLength)
		info.Parameters = llm.FormatParameterCount(m.ParameterCount)
		info.FileSizeBytes = m.FileSizeBytes
		info.TensorCount = int(m.TensorCount)
		info.VocabSize = int(m.VocabularySize)
		info.EmbeddingLength = int(m.EmbeddingLength)
		info.LayerCount = int(m.LayerCount)
		info.GGUFVersion = int(m.GGUFVersion)

		if result.Memory != nil {
			info.KVCacheEstimateBytes = result.Memory.KVCacheBytes
			info.WorkspaceEstimateBytes = result.Memory.WorkspaceBytes
			info.TotalMemoryEstimateBytes = result.Memory.TotalBytes
		}
	}

	return info, nil
}

// HardwareInfo implements Backend: the native probe merged with sysinfo.
func (b *Backend) HardwareInfo(ctx context.Context) (llm.HardwareInfo, error) {
	return b.eng.Hardware(ctx)
}

// Metrics implements Backend: measured lifecycle facts + the engine's own
// metrics reading (real generation stats since Phase 5).
func (b *Backend) Metrics(ctx context.Context) (llm.Metrics, error) {
	return b.eng.MetricsSnapshot(ctx)
}

// GenerationCapable implements llm.GenerationCapable (Phase 5: REAL).
// True only when the engine is ALIVE and the loaded model validated as
// natively executable at load time (llama graph + supported tensor
// types). A host restart or model unload flips it back to false —
// capability tracks reality, not hope. Mid-flight failures still fall
// back through the router's pre-first-token retry policy.
func (b *Backend) GenerationCapable() bool {
	// v1.7.1: nil-receiver safe — a nil-typed Backend surfaced through an
	// interface is an honest "not capable", never a panic.
	if b == nil || b.eng == nil || !b.eng.IsAlive() {
		return false
	}
	return b.eng.NativeGenerationCapable()
}

// GenerationFallbackReason implements llm.GenerationFallbackReporter: the
// inspectable reason the native engine cannot serve generation RIGHT NOW
// (empty when it can). It distinguishes the infrastructure classes the
// selection layer reports to the user instead of collapsing them into a
// generic "not capable":
//
//   - engine not running (never started, stopped, or failed)
//   - engine running but no model loaded
//   - model loaded but not natively executable (the engine's own verdict)
func (b *Backend) GenerationFallbackReason() string {
	if b == nil || b.eng == nil {
		return "native engine is not configured"
	}
	if !b.eng.IsAlive() {
		detail := b.eng.Detail()
		if detail != "" {
			return fmt.Sprintf("native engine not running (state %s: %s)", b.eng.State(), detail)
		}
		return fmt.Sprintf("native engine not running (state %s)", b.eng.State())
	}
	if b.eng.NativeModelState() != ModelStateLoaded {
		return "native engine is running but no model is loaded"
	}
	if reason := b.eng.NativeGenerationReason(); reason != "" {
		return "loaded model is not natively executable: " + reason
	}
	return ""
}

// statRegularFile validates that path exists and is a regular file
// (no directory, no dangling symlink).
func statRegularFile(path string) (os.FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("model file: %w", err)
	}

	if fi.IsDir() {
		return nil, fmt.Errorf("model path %q is a directory", path)
	}

	return fi, nil
}

// BackendCapabilities implements llm.CapabilityReporter (v1.7.1 §5.3):
// the Native Engine reports its serving contract from real state —
// generation capability and fallback reasons come from the existing
// probes; the context window and memory plan come from the loaded
// model's own metadata (0/empty when unknown, never guessed).
func (b *Backend) BackendCapabilities() llm.BackendCapabilities {
	if b == nil {
		return llm.BackendCapabilities{
			Identity:                 "native",
			GenerationFallbackReason: "native engine is not configured",
			IncompatibilityReason:    "native engine is not configured",
		}
	}
	caps := llm.BackendCapabilities{
		Identity:               "native",
		SupportedArchitectures: "llama graph family (native engine)",
		Streaming:              true, // protocol v4 streamed events
		Cancellation:           true, // cooperative cancel op
		Readiness:              string(b.eng.State()),
	}

	// Generation capability: the existing selection probes.
	caps.GenerationCapable = b.GenerationCapable()
	if !caps.GenerationCapable {
		caps.GenerationFallbackReason = b.GenerationFallbackReason()
		caps.IncompatibilityReason = caps.GenerationFallbackReason
	}

	// Model facts from the loaded model (unknown stays zero).
	if info := b.eng.ModelInfoCached(); info != nil {
		if info.ContextLength > 0 {
			caps.ContextCapability = int(info.ContextLength)
		}
		if plan := b.eng.MemoryPlanCached(); plan != nil && plan.TotalBytes > 0 {
			caps.ResourceRequirements = fmt.Sprintf(
				"weights+KV+workspace plan: %d bytes total (available RAM %d)",
				plan.TotalBytes, plan.AvailableRAMBytes)
		}
	}

	return caps
}
