package runtime

// runtime_test.go — Phase 5 generation-router tests.
//
// The router (Stack.streamGeneration) is the single seam that decides
// which backend serves each generation request:
//
//   - native when selected AND capable AND plain-text;
//   - llama.cpp for everything else (tools / images / not selected /
//     incapable), with the reason logged;
//   - pre-first-token native failures fall back to llama.cpp;
//   - post-first-token failures surface to the loop.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// fakeStreamBackend records stream calls and produces canned events.
type fakeStreamBackend struct {
	name       string
	capable    bool
	streamErr  error // returned by StreamGenerate
	eventFirst bool  // emit a content event before the error

	streamCalls int
}

func (f *fakeStreamBackend) Name() string { return f.name }

func (f *fakeStreamBackend) Start(ctx context.Context) error { return nil }
func (f *fakeStreamBackend) Stop(ctx context.Context) error  { return nil }

func (f *fakeStreamBackend) Health(ctx context.Context) (llm.HealthReport, error) {
	return llm.HealthReport{Alive: true}, nil
}

func (f *fakeStreamBackend) LoadModel(ctx context.Context, spec llm.ModelSpec) error {
	return nil
}

func (f *fakeStreamBackend) UnloadModel(ctx context.Context) error { return nil }

func (f *fakeStreamBackend) Generate(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, llm.ErrNotImplemented
}

func (f *fakeStreamBackend) StreamGenerate(ctx context.Context, req *llm.ChatRequest,
	onEvent func(llm.StreamEvent) error) (llm.PerfStats, error) {

	f.streamCalls++

	if f.eventFirst {
		_ = onEvent(llm.StreamEvent{Content: "partial "})
	}

	if f.streamErr != nil {
		return llm.PerfStats{}, f.streamErr
	}

	_ = onEvent(llm.StreamEvent{Content: "from " + f.name})
	_ = onEvent(llm.StreamEvent{FinishReason: "stop"})
	return llm.PerfStats{Tokens: 2, TokensPerSec: 10}, nil
}

func (f *fakeStreamBackend) Cancel(ctx context.Context, requestID string) error {
	return llm.ErrCancelContextBased
}

func (f *fakeStreamBackend) ModelInfo(ctx context.Context) (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

func (f *fakeStreamBackend) HardwareInfo(ctx context.Context) (llm.HardwareInfo, error) {
	return llm.HardwareInfo{}, nil
}

func (f *fakeStreamBackend) Metrics(ctx context.Context) (llm.Metrics, error) {
	return llm.Metrics{}, nil
}

func (f *fakeStreamBackend) GenerationCapable() bool { return f.capable }

// routerStack builds a minimal Stack with the two fake backends wired
// the same way NewStack wires them (selection policy reads through Src).
func routerStack(t *testing.T, native, llama *fakeStreamBackend, selected bool) *Stack {
	t.Helper()

	cfg := config.Default()
	if selected {
		cfg.EngineBackend = "native"
	}

	s := &Stack{
		Cfg: cfg,
	}
	s.Src = config.NewSource(cfg)
	s.nativeBackend = native
	s.llamaBackend = llama

	// The llama.cpp client path (stubbed: records the call and returns
	// the canned llama stream).
	s.clientStream = func(ctx context.Context, req *llm.ChatRequest,
		onEvent func(llm.StreamEvent) error) (llm.PerfStats, error) {
		llama.streamCalls++
		_ = onEvent(llm.StreamEvent{Content: "via-client"})
		return llm.PerfStats{Tokens: 2}, nil
	}

	return s
}

func TestStreamGenerationRoutesToNativeWhenCapable(t *testing.T) {
	native := &fakeStreamBackend{name: "native", capable: true}
	llama := &fakeStreamBackend{name: "llama"}

	s := routerStack(t, native, llama, true)

	// The client path is the llama branch — give the stack a client
	// whose behavior mirrors the llama backend (the router calls
	// s.Client.StreamChatDetailed; for selection tests we assert the
	// NATIVE arm, which needs no client).

	var text strings.Builder
	perf, err := s.streamGeneration(context.Background(), &llm.ChatRequest{
		MaxTokens: 4,
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
	}, func(ev llm.StreamEvent) error {
		text.WriteString(ev.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("stream generation: %v", err)
	}

	if native.streamCalls != 1 {
		t.Fatalf("native stream calls = %d, want 1", native.streamCalls)
	}
	if text.String() != "from native" {
		t.Fatalf("text = %q, want %q", text.String(), "from native")
	}
	if perf.Tokens != 2 {
		t.Fatalf("perf tokens = %d, want 2 (native perf)", perf.Tokens)
	}
}

func TestStreamGenerationToolsStayOnClient(t *testing.T) {
	native := &fakeStreamBackend{name: "native", capable: true}
	llama := &fakeStreamBackend{name: "llama"}

	s := routerStack(t, native, llama, true)

	dummy := llm.ToolSpec{Type: "function"}
	dummy.Function.Name = "x"

	_, err := s.streamGeneration(context.Background(), &llm.ChatRequest{
		MaxTokens: 4,
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
		Tools:     []llm.ToolSpec{dummy},
	}, func(ev llm.StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("tool request routing: %v", err)
	}

	if native.streamCalls != 0 {
		t.Fatalf("native stream calls = %d, want 0 (tools stay on llama)", native.streamCalls)
	}
	if llama.streamCalls != 1 {
		t.Fatalf("client stream calls = %d, want 1", llama.streamCalls)
	}
}

func TestStreamGenerationImagesStayOnClient(t *testing.T) {
	native := &fakeStreamBackend{name: "native", capable: true}
	llama := &fakeStreamBackend{name: "llama"}

	s := routerStack(t, native, llama, true)

	_, _ = s.streamGeneration(context.Background(), &llm.ChatRequest{
		MaxTokens: 4,
		Messages:  []llm.Message{{Role: "user", Content: "hi", Images: []string{"/tmp/a.png"}}},
	}, func(ev llm.StreamEvent) error { return nil })

	if native.streamCalls != 0 {
		t.Fatalf("native stream calls = %d, want 0 (images stay on llama)", native.streamCalls)
	}
}

func TestStreamGenerationIncapableFallsBackToClient(t *testing.T) {
	native := &fakeStreamBackend{name: "native", capable: false}
	llama := &fakeStreamBackend{name: "llama"}

	s := routerStack(t, native, llama, true)

	_, err := s.streamGeneration(context.Background(), &llm.ChatRequest{
		MaxTokens: 4,
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
	}, func(ev llm.StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("routing: %v", err)
	}

	if native.streamCalls != 0 {
		t.Fatalf("native stream calls = %d, want 0 (incapable)", native.streamCalls)
	}
	if llama.streamCalls != 1 {
		t.Fatalf("client stream calls = %d, want 1", llama.streamCalls)
	}
}

func TestStreamGenerationNativeFailureBeforeFirstTokenFallsBack(t *testing.T) {
	native := &fakeStreamBackend{name: "native", capable: true, streamErr: errors.New("native exploded")}
	llama := &fakeStreamBackend{name: "llama"}

	s := routerStack(t, native, llama, true)

	var text strings.Builder
	_, err := s.streamGeneration(context.Background(), &llm.ChatRequest{
		MaxTokens: 4,
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
	}, func(ev llm.StreamEvent) error {
		text.WriteString(ev.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("fallback routing: %v", err)
	}

	if native.streamCalls != 1 {
		t.Fatalf("native stream calls = %d, want 1 (tried first)", native.streamCalls)
	}
	if llama.streamCalls != 1 {
		t.Fatalf("client stream calls = %d, want 1 (fallback)", llama.streamCalls)
	}
	if text.String() != "via-client" {
		t.Fatalf("text = %q, want the fallback content", text.String())
	}
}

func TestStreamGenerationNativeFailureAfterFirstTokenSurfaces(t *testing.T) {
	native := &fakeStreamBackend{
		name:       "native",
		capable:    true,
		streamErr:  errors.New("mid-flight failure"),
		eventFirst: true,
	}
	llama := &fakeStreamBackend{name: "llama"}

	s := routerStack(t, native, llama, true)

	_, err := s.streamGeneration(context.Background(), &llm.ChatRequest{
		MaxTokens: 4,
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
	}, func(ev llm.StreamEvent) error { return nil })
	if err == nil {
		t.Fatal("mid-flight failure must surface, not silently fall back")
	}
	if !strings.Contains(err.Error(), "mid-flight failure") {
		t.Fatalf("error = %v", err)
	}
	if llama.streamCalls != 0 {
		t.Fatalf("client stream calls = %d, want 0 (no double generation)", llama.streamCalls)
	}
}
