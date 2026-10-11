// recovery.go — v1.7.1: the runtime implementation of the agent
// RecoveryCoordinator seam.
//
// The orchestrator owns the DECISION to recover; the runtime owns the
// mechanics through the EXISTING lifecycle authorities:
//
//   - PrepareRestart resets the backend that exhausted through its own
//     lifecycle owner (LlamaServer.Restart for llama.cpp — which already
//     implements stop → port-free → start; Engine Stop+Start for the
//     Native Engine) and then verifies ACTUAL readiness with the same
//     health contract the backend selection uses. No second engine
//     process is ever spawned here.
//   - Summarize runs ONE bounded generation request through the existing
//     backend-aware generation router (Stack.streamGeneration) — the
//     same selection authority, the same fallback discipline.
//   - PersistHandoff writes the durable recovery record through the one
//     recovery store under <DataDir>/recovery.
package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
)

// restartReadinessTimeout bounds the post-restart readiness verification.
const restartReadinessTimeout = 60 * time.Second

// stackRecoveryCoordinator adapts the Stack to agent.RecoveryCoordinator.
type stackRecoveryCoordinator struct {
	s *Stack
}

// newRecoveryCoordinator returns the Stack's recovery seam.
func newRecoveryCoordinator(s *Stack) agent.RecoveryCoordinator {
	return stackRecoveryCoordinator{s: s}
}

// PrepareRestart resets the source backend through its existing lifecycle
// owner and verifies readiness (health probe, bounded retries — never a
// bare sleep). An empty sourceBackend means "the runtime's llama.cpp
// engine" (the historical default path).
func (c stackRecoveryCoordinator) PrepareRestart(ctx context.Context, sourceBackend string) error {
	backend := strings.ToLower(strings.TrimSpace(sourceBackend))

	if backend == "native" {
		return c.restartNative(ctx)
	}
	return c.restartLlama(ctx)
}

// restartLlama restarts the llama.cpp engine through LlamaServer.Restart
// (the existing lifecycle owner: stop → port-free verification → start)
// and verifies readiness through the backend health contract.
func (c stackRecoveryCoordinator) restartLlama(ctx context.Context) error {
	if c.s.Llama == nil {
		return context.DeadlineExceeded
	}
	if err := c.s.Llama.Restart(); err != nil {
		return err
	}
	return c.awaitReady(ctx, c.s.llamaBackend)
}

// restartNative stops and re-starts the Native Engine host through the
// engine's own lifecycle (Stop → Start → handshake), then verifies
// readiness through the backend health contract.
func (c stackRecoveryCoordinator) restartNative(ctx context.Context) error {
	if c.s.Native == nil {
		return context.DeadlineExceeded
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.s.Native.Stop(stopCtx); err != nil {
		// A failed stop still proceeds: Start re-establishes the host
		// process through the supervised lifecycle or reports honestly.
	}
	if err := c.s.Native.Start(ctx); err != nil {
		return err
	}
	return c.awaitReady(ctx, c.s.nativeBackend)
}

// awaitReady polls the backend health contract until the engine reports
// alive (bounded) — actual readiness, never a sleep-and-hope.
func (c stackRecoveryCoordinator) awaitReady(ctx context.Context, backend llm.Backend) error {
	if backend == nil {
		return context.DeadlineExceeded
	}

	deadline := time.Now().Add(restartReadinessTimeout)
	for {
		report, err := backend.Health(ctx)
		if err == nil && report.Alive {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Summarize runs ONE bounded summarization request through the existing
// generation router (backend selection authority included).
func (c stackRecoveryCoordinator) Summarize(ctx context.Context, prompt string) (string, error) {
	cfg := c.s.Src.Load()
	req := c.s.Client.BuildChatRequestWithOptions(
		cfg.EffectiveModel(),
		[]llm.Message{{Role: "user", Content: prompt}},
		nil,  // no tools: plain summarization
		8192, // bounded window for the summary request
	)
	req.MaxTokens = 900
	req.Temperature = 0.2

	var out strings.Builder
	_, err := c.s.streamGeneration(ctx, req, func(ev llm.StreamEvent) error {
		if ev.TimingMark != "" {
			return nil
		}
		out.WriteString(ev.Content)
		return nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// PersistHandoff writes the durable recovery record through the one store.
func (c stackRecoveryCoordinator) PersistHandoff(h *recovery.Handoff) error {
	return c.s.recoveryStore.Save(h)
}

// recoveryDir returns the durable recovery directory for the config:
// <DataDir>/recovery.
func recoveryDir(cfg *config.Config) string {
	if cfg == nil || cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(cfg.DataDir, "recovery")
}
