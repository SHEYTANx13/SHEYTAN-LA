// recovery.go — v1.7.1: the run-level context-exhaustion recovery path.
//
// When the LLM call fails with the ONE typed condition
// (recovery.IsContextExhausted), the orchestrator does not terminate the
// task. It executes the recovery flow exactly once per exhaustion
// episode:
//
//	Context limit reached
//	  → Preserving task state (freeze the complete logical snapshot)
//	  → Creating recovery handoff (whole-context summary, durable record)
//	  → Restarting model (via the RecoveryCoordinator lifecycle seam)
//	  → Continuing from handoff (bounded injection + the SAME user turn)
//
// Boundaries honored:
//   - the proactive continuum rollover stays the pressure-avoidance
//     mechanism; this path is the DISTINCT recovery event after a REAL
//     exhaustion (never a timeout, OOM or crash — those surface as other
//     errors and are not recovered here);
//   - engine lifecycle is owned elsewhere: restart happens only through
//     the RecoveryCoordinator (runtime wiring), never by spawning a
//     second unmanaged engine process;
//   - completed work is preserved in the handoff's do-not-redo list and
//     the original user turn is carried exactly once — no duplicate
//     execution, no duplicated user message;
//   - automatic recovery is bounded (recovery.MaxRecoveryAttempts = 1):
//     a second exhaustion ends the loop with a clear diagnostic instead
//     of a restart storm.
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/continuum"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
)

// RecoveryCoordinator is the lifecycle seam between the orchestrator and
// the engine/runtime owners. The orchestrator never touches engine
// processes or persistence directly; the runtime implements this with
// the EXISTING lifecycle owner (LlamaServer.Restart / Engine
// Stop+Start, the recovery handoff store).
type RecoveryCoordinator interface {
	// PrepareRestart resets the active engine through the existing
	// lifecycle owner and verifies ACTUAL readiness (not a sleep).
	// sourceBackend names the backend that exhausted ("llama.cpp" or
	// "native" — an empty value means "the runtime's active backend").
	PrepareRestart(ctx context.Context, sourceBackend string) error
	// Summarize runs ONE bounded LLM summarization request. Failure is
	// non-fatal: the deterministic fallback summary applies.
	Summarize(ctx context.Context, prompt string) (string, error)
	// PersistHandoff writes the durable recovery record (atomic, survives
	// restart). Failure is reported honestly but does not abort the run
	// continuation (the in-memory injection still carries the state).
	PersistHandoff(h *recovery.Handoff) error
}

// SetRecoveryCoordinator installs the runtime recovery seam (nil
// disables automatic context recovery).
func (o *Orchestrator) SetRecoveryCoordinator(rc RecoveryCoordinator) {
	o.mu.Lock()
	o.recoveryCoord = rc
	o.mu.Unlock()
}

// recoveryCoordinator returns the installed seam.
func (o *Orchestrator) recoveryCoordinator() RecoveryCoordinator {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.recoveryCoord
}

// runRecovery carries the per-run recovery inputs the orchestrator
// freezes into the snapshot.
type runRecovery struct {
	sessionID string
	threadID  string
	runID     string
	taskID    string
	runTask   *TaskState
	messages  []llm.Message
	backend   string
	model     string
}

// attemptRecovery executes ONE bounded recovery attempt. It returns the
// rebuilt message list on success; (nil, "") on failure — the caller
// then falls through to the honest error return.
func (o *Orchestrator) attemptRecovery(
	ctx context.Context,
	rr *runRecovery,
	exhaustErr error,
	onActivity func(Activity),
) ([]llm.Message, string) {
	coord := o.recoveryCoordinator()
	if coord == nil {
		return nil, ""
	}

	detail := exhaustErr.Error()
	emit := func(caption string) {
		onActivity(Activity{
			Type:      "status",
			Caption:   caption,
			Timestamp: time.Now(),
		})
	}

	logging.Default().Warn("agent", "context exhaustion detected: %s — starting recovery", detail)

	// 1. Context limit reached (truthful state, no hidden reasoning).
	emit("Context limit reached — preserving task state")

	// 2. FREEZE the complete logical state.
	detected := recovery.Exhaustion{Detail: detail}
	if ex, ok := recovery.AsExhaustion(exhaustErr); ok {
		detected = *ex
	}

	snap := recovery.NewSnapshot(rr.sessionID, rr.threadID, rr.taskID, rr.runID, 1, detected)
	snap.Mission = rr.runTask.Goal
	snap.CurrentState = rr.runTask.CurrentStep
	snap.Constraints = rr.runTask.Constraints
	snap.FilesInspected = rr.runTask.FilesInspected
	snap.FilesChanged = rr.runTask.FilesChanged
	snap.TestsAndBuilds = rr.runTask.TestsRun
	snap.Failures = rr.runTask.Failures
	snap.Repairs = rr.runTask.Repairs
	snap.Artifacts = rr.runTask.Artifacts
	if rr.runTask.NextStep != "" {
		snap.NextActions = []string{rr.runTask.NextStep}
	}
	snap.SourceBackend = firstNonEmpty(detected.Backend, rr.backend)
	snap.SourceModel = rr.model
	snap.Messages = toRecoveryMessages(rr.messages)

	// Distilled knowledge comes from the EXISTING continuum authority.
	fw := continuum.Distill(continuum.NewFramework(), rr.messages)
	snap.Facts = fw.Facts
	snap.Decisions = fw.Decisions
	snap.OpenThreads = fw.OpenThreads
	snap.Preferences = fw.Preferences
	snap.Bound()

	// 3. WHOLE-CONTEXT SUMMARY (hierarchical; deterministic fallback).
	emit("Creating recovery handoff")
	summary, source := "", "fallback"
	out, err := recovery.HierarchicalSummary(ctx, snap, summarizerFunc(coord.Summarize))
	if err != nil || strings.TrimSpace(out) == "" {
		if err != nil {
			logging.Default().Warn("agent", "hierarchical recovery summary failed (%v) — deterministic fallback applies", err)
		}
		summary = recovery.FallbackSummary(snap)
	} else {
		summary, source = out, "hierarchical"
	}

	handoff := recovery.NewHandoff(snap, summary, source)
	if err := coord.PersistHandoff(handoff); err != nil {
		// Honest, non-fatal: the continuation still carries the state.
		logging.Default().Warn("agent", "recovery handoff persistence failed: %v", err)
		onActivity(Activity{
			Type:      "error",
			Caption:   "Recovery handoff could not be persisted: " + err.Error(),
			Timestamp: time.Now(),
		})
	}

	// 4. SAFE RESTART through the existing lifecycle owner.
	emit("Restarting model")
	if err := coord.PrepareRestart(ctx, snap.SourceBackend); err != nil {
		onActivity(Activity{
			Type:      "error",
			Caption:   "Recovery failed: model restart did not become ready (" + err.Error() + ")",
			Timestamp: time.Now(),
		})
		return nil, ""
	}

	// 5. INJECT the handoff and CONTINUE THE SAME TASK.
	injection := recovery.RenderInjection(handoff)
	if injection == "" {
		// Nothing to inject (empty snapshot) — recovery is meaningless.
		return nil, ""
	}
	emit("Continuing from handoff")
	logging.Default().Info("agent",
		"recovery handoff %s ready (summary source: %s, snapshot ~%d tokens) — continuing same task",
		handoff.HandoffID, source, snap.TokenCount())

	rebuilt := rebuildAfterExhaustion(rr.messages, injection)
	if len(rebuilt) == 0 {
		return nil, ""
	}
	return rebuilt, handoff.HandoffID
}

// rebuildAfterExhaustion builds the continuation message list:
// the run's system prompts + the bounded recovery injection + the
// ORIGINAL last user message (carried exactly once — never duplicated,
// never re-quoted inside the injection).
func rebuildAfterExhaustion(messages []llm.Message, injection string) []llm.Message {
	if injection == "" {
		return nil
	}

	var out []llm.Message
	lastUser := -1
	for i := range messages {
		switch messages[i].Role {
		case "system":
			// Old continuum briefings are superseded state, not prompts.
			if continuum.IsBriefing(messages[i].Content) {
				continue
			}
			out = append(out, messages[i])
		case "user":
			lastUser = i
		}
	}
	if lastUser < 0 {
		// No user turn to continue — nothing meaningful to recover.
		return nil
	}

	out = append(out, llm.Message{
		Role:    "system",
		Content: injection,
	})
	out = append(out, messages[lastUser])
	return out
}

// toRecoveryMessages converts the live conversation to the neutral form.
func toRecoveryMessages(msgs []llm.Message) []recovery.Message {
	out := make([]recovery.Message, 0, len(msgs))
	for _, m := range msgs {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, recovery.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// summarizerFunc adapts the coordinator's Summarize to the recovery seam.
type summarizerFunc func(ctx context.Context, prompt string) (string, error)

func (f summarizerFunc) Summarize(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// recoveryAbortDiagnostic is the honest terminal message when the
// recovered attempt exhausted the context AGAIN: the automatic loop
// stops here (recovery.MaxRecoveryAttempts = 1).
func recoveryAbortDiagnostic() string {
	return fmt.Sprintf(
		"Context limit reached again after recovery. Automatic recovery stopped after %d attempt to protect the task — start a new session, reduce the working set, or increase the model context.",
		recovery.MaxRecoveryAttempts,
	)
}
