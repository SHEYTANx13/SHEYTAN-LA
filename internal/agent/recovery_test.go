// recovery_test.go — v1.7.1 agent-level integration coverage for the
// context-exhaustion recovery flow:
//
//	small context → REAL exhaustion (typed condition) → recovery
//	snapshot → summary → restart EXACTLY ONCE → handoff delivered →
//	continuation of the SAME task
//
// plus the bounded-loop guarantees (§3.9), the honest-failure paths and
// the no-duplicate-work contract (§3.8).
package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
)

// fakeRecoveryCoordinator records every seam call for assertions.
type fakeRecoveryCoordinator struct {
	restarts    int
	summaries   int
	persisted   []string // handoff ids
	failRestart bool
	failSummary bool
	store       *recovery.Store
}

func (f *fakeRecoveryCoordinator) PrepareRestart(ctx context.Context, sourceBackend string) error {
	f.restarts++
	if f.failRestart {
		return errors.New("engine did not become ready")
	}
	return nil
}

func (f *fakeRecoveryCoordinator) Summarize(ctx context.Context, prompt string) (string, error) {
	f.summaries++
	if f.failSummary {
		return "", errors.New("summarizer offline")
	}
	return "RECOVERED SUMMARY: the task was refactoring the parser", nil
}

func (f *fakeRecoveryCoordinator) PersistHandoff(h *recovery.Handoff) error {
	if err := f.store.Save(h); err != nil {
		return err
	}
	f.persisted = append(f.persisted, h.HandoffID)
	return nil
}

// exhaustionRouter counts streamChat invocations. The first call
// exhausts; subsequent calls succeed and RECORD the request they saw.
type exhaustionRouter struct {
	calls      int
	requests   []*llm.ChatRequest
	exhaustAll bool
}

func (r *exhaustionRouter) stream(ctx context.Context, req *llm.ChatRequest,
	onEvent func(llm.StreamEvent) error) (llm.PerfStats, error) {
	r.calls++
	r.requests = append(r.requests, req)

	if r.exhaustAll || r.calls == 1 {
		// A REAL llama.cpp context-exhaustion refusal (typed at the
		// backend boundary by llm.wrapContextExhaustion in production).
		return llm.PerfStats{}, recovery.NewExhaustion("llama.cpp",
			"LLM HTTP 400: the request exceeds the available context size", "")
	}

	for _, m := range req.Messages {
		if m.Content == "" {
			continue
		}
		_ = onEvent(llm.StreamEvent{Content: m.Content + "\n"})
	}
	_ = onEvent(llm.StreamEvent{Content: "continuation-answer", FinishReason: "stop"})
	return llm.PerfStats{}, nil
}

func newRecoveryTestOrchestrator(t *testing.T) (*Orchestrator, *fakeRecoveryCoordinator, string) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "remote"
	cfg.RemoteBaseURL = "http://127.0.0.1:1/v1" // never dialed: the router seam replaces the transport
	cfg.RemoteAPIKey = "test-key"
	cfg.RemoteModel = "fake-remote-model"
	cfg.MaxIterations = 4

	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	dir := filepath.Join(cfg.DataDir, "recovery")
	coord := &fakeRecoveryCoordinator{store: recovery.NewStore(dir)}
	orch.SetRecoveryCoordinator(coord)
	return orch, coord, dir
}

func recoveryMessages() []llm.Message {
	return []llm.Message{
		{Role: "system", Content: "You are SHEYTAN, the local agent."},
		{Role: "user", Content: "Refactor the parser module and verify"},
	}
}

func TestRunRecoversFromRealContextExhaustion(t *testing.T) {
	orch, coord, dir := newRecoveryTestOrchestrator(t)
	router := &exhaustionRouter{}
	orch.SetGenerationStream(router.stream)

	var captions []string
	result, err := orch.RunDetailed(context.Background(), recoveryMessages(),
		func(a Activity) { captions = append(captions, a.Caption) },
		WithRunIdentity("sess-9", "thread-9", "run-9"),
	)
	if err != nil {
		t.Fatalf("recovered run failed: %v", err)
	}

	// The SAME task continued: exactly one exhaustion + one continuation.
	if router.calls != 2 {
		t.Fatalf("stream calls = %d, want 2 (exhaust + continue)", router.calls)
	}

	// Restart happened EXACTLY ONCE through the lifecycle seam.
	if coord.restarts != 1 {
		t.Fatalf("restarts = %d, want exactly 1", coord.restarts)
	}

	// The durable handoff was persisted and is on disk.
	if len(coord.persisted) != 1 {
		t.Fatalf("persisted handoffs = %d, want 1", len(coord.persisted))
	}
	if _, err := os.Stat(filepath.Join(dir, coord.persisted[0]+".json")); err != nil {
		t.Fatalf("handoff not durable: %v", err)
	}

	// Truthful states reached the activity stream (§3.10).
	joined := strings.Join(captions, "\n")
	for _, want := range []string{
		"Context limit reached",
		"Creating recovery handoff",
		"Restarting model",
		"Continuing from handoff",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing truthful state %q; captions: %v", want, captions)
		}
	}

	// RunResult evidence.
	if !result.RecoveryTriggered || result.RecoveryHandoffID == "" {
		t.Fatalf("result recovery evidence: %+v", result)
	}

	// The continuation request carried the injection + the SAME user turn,
	// NOT a duplicated history (§3.8).
	cont := router.requests[1]
	userTurns := 0
	sawInjection := false
	sawSystem := false
	for _, m := range cont.Messages {
		switch m.Role {
		case "user":
			userTurns++
		case "system":
			if strings.Contains(m.Content, recovery.InjectionHeader) {
				sawInjection = true
			} else {
				sawSystem = true
			}
		}
	}
	if userTurns != 1 {
		t.Fatalf("continuation user turns = %d, want exactly 1 (no duplication)", userTurns)
	}
	if !sawInjection || !sawSystem {
		t.Fatalf("continuation missing injection/system: injection=%v system=%v", sawInjection, sawSystem)
	}

	// The handoff record identity is intact (§3.6).
	store := recovery.NewStore(dir)
	rec := store.Latest("sess-9")
	if rec == nil || rec.HandoffID != result.RecoveryHandoffID ||
		rec.SessionID != "sess-9" || rec.ThreadID != "thread-9" || rec.RunID != "run-9" {
		t.Fatalf("handoff identity: %+v", rec)
	}
	if rec.SummarySource != "hierarchical" {
		t.Fatalf("summary source: %q", rec.SummarySource)
	}
}

func TestSecondExhaustionStopsAutomaticRecovery(t *testing.T) {
	orch, coord, _ := newRecoveryTestOrchestrator(t)
	router := &exhaustionRouter{exhaustAll: true}
	orch.SetGenerationStream(router.stream)

	_, err := orch.RunDetailed(context.Background(), recoveryMessages(),
		func(a Activity) {}, WithRunIdentity("sess-x", "thread-x", "run-x"))
	if err == nil {
		t.Fatal("double exhaustion must surface an error honestly")
	}
	if !errors.Is(err, recovery.ErrRecoveryLoopGuard) {
		t.Fatalf("err = %v, want the loop-guard diagnostic", err)
	}

	// §3.9: the automatic loop ran AT MOST once.
	if coord.restarts != 1 {
		t.Fatalf("restarts = %d, want exactly 1 (bounded loop)", coord.restarts)
	}
	if router.calls != 2 {
		t.Fatalf("stream calls = %d, want exactly 2 (original + one recovery)", router.calls)
	}
}

func TestNonExhaustionErrorsNeverRecover(t *testing.T) {
	orch, coord, _ := newRecoveryTestOrchestrator(t)

	// A transport failure is NOT exhaustion: no recovery, honest error.
	orch.SetGenerationStream(func(ctx context.Context, req *llm.ChatRequest,
		onEvent func(llm.StreamEvent) error) (llm.PerfStats, error) {
		return llm.PerfStats{}, errors.New("connection reset by peer")
	})

	_, err := orch.RunDetailed(context.Background(), recoveryMessages(), func(a Activity) {})
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("transport error must surface honestly: %v", err)
	}
	if coord.restarts != 0 {
		t.Fatalf("restarts = %d, want 0 for a non-exhaustion error", coord.restarts)
	}
}

func TestRecoveryUsesDeterministicFallbackWhenSummarizerFails(t *testing.T) {
	orch, coord, dir := newRecoveryTestOrchestrator(t)
	coord.failSummary = true
	router := &exhaustionRouter{}
	orch.SetGenerationStream(router.stream)

	result, err := orch.RunDetailed(context.Background(), recoveryMessages(), func(a Activity) {})
	if err != nil {
		t.Fatalf("recovered run failed: %v", err)
	}
	if router.calls != 2 || coord.restarts != 1 {
		t.Fatalf("recovery flow broken: calls=%d restarts=%d", router.calls, coord.restarts)
	}

	rec := recovery.NewStore(dir).Latest("sess-x")
	if rec != nil {
		t.Fatalf("no identity passed — record must not be misfiled: %+v", rec)
	}

	// The fallback summary still carried the task forward.
	cont := router.requests[1]
	foundSummary := false
	for _, m := range cont.Messages {
		if strings.Contains(m.Content, recovery.InjectionHeader) &&
			strings.Contains(m.Content, "Refactor the parser module") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatal("fallback handoff missing the task state")
	}
	_ = result
}

func TestPrepareRestartFailureSurfacesHonestly(t *testing.T) {
	orch, coord, _ := newRecoveryTestOrchestrator(t)
	coord.failRestart = true
	router := &exhaustionRouter{}
	orch.SetGenerationStream(router.stream)

	_, err := orch.RunDetailed(context.Background(), recoveryMessages(), func(a Activity) {})
	if err == nil {
		t.Fatal("a failed restart must surface the error honestly")
	}
	if router.calls != 1 {
		t.Fatalf("stream calls = %d, want 1 (no continuation after failed restart)", router.calls)
	}
}

func TestRecoveryDisabledWithoutCoordinator(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "remote"
	cfg.RemoteBaseURL = "http://127.0.0.1:1/v1"
	cfg.RemoteAPIKey = "k"
	cfg.RemoteModel = "m"
	cfg.MaxIterations = 4

	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client) // NO coordinator installed

	router := &exhaustionRouter{}
	orch.SetGenerationStream(router.stream)

	_, err := orch.RunDetailed(context.Background(), recoveryMessages(), func(a Activity) {})
	if err == nil || !recovery.IsContextExhausted(err) {
		t.Fatalf("without the seam the typed exhaustion must surface: %v", err)
	}
	if router.calls != 1 {
		t.Fatalf("stream calls = %d, want 1 (no recovery attempted)", router.calls)
	}
}
