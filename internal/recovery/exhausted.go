// Package recovery is SHEYTAN's v1.7.1 answer to a REAL context
// exhaustion: when the active model genuinely reaches its context limit,
// the running work is recovered instead of the task being terminated.
//
// This package is the ONE authoritative owner of the exhaustion-recovery
// concern. It deliberately does not duplicate any existing authority:
//
//   - it does not replace the proactive continuum rollover (continuum
//     stays the pressure-avoidance mechanism); a true exhaustion is a
//     DISTINCT recovery event that happens when avoidance failed;
//   - it does not create a second memory system — the recovery snapshot
//     is captured from the run's existing state (TaskState, session
//     messages, continuum distillation) and persisted as one durable,
//     versioned handoff record;
//   - it does not manage engine processes — restart goes through the
//     existing lifecycle owner via the RecoveryCoordinator seam.
//
// The flow it implements:
//
//	RUNNING → REAL CONTEXT EXHAUSTION (typed condition below)
//	        → FREEZE LOGICAL STATE (Snapshot)
//	        → PERSIST RECOVERY SNAPSHOT (durable Handoff record)
//	        → CREATE WHOLE-CONTEXT SUMMARY (hierarchical, bounded)
//	        → RESTART / RESET MODEL (coordinator, existing lifecycle)
//	        → VERIFY READY (coordinator)
//	        → INJECT RECOVERY HANDOFF (RenderInjection)
//	        → CONTINUE SAME TASK
//
// Loop protection: automatic recovery is allowed at most ONCE per
// exhaustion episode (MaxRecoveryAttempts = 1). A second exhaustion ends
// the automatic loop with a clear diagnostic — never a restart storm.
package recovery

import (
	"errors"
	"fmt"
)

// ErrContextExhausted is the sentinel of the ONE typed context-exhaustion
// condition. Backend-specific errors (llama.cpp HTTP errors, the Native
// Engine's explicit context-bound rejection) unwrap into this sentinel so
// the runtime can detect exhaustion with errors.Is — never by matching raw
// error strings at the point of recovery.
var ErrContextExhausted = errors.New("context exhausted")

// Exhaustion is the typed condition carried by normalized backend
// evidence. One backend boundary produces one Exhaustion value:
//
//   - llama.cpp: HTTP/generation error text classified at the client
//     boundary (llm package) — the only evidence that server exposes;
//   - Native Engine: the structured context-bound rejection
//     (engine.ContextExhaustedError) mapped into this contract.
type Exhaustion struct {
	// Backend is the serving backend that reported exhaustion
	// ("llama.cpp" or "native").
	Backend string `json:"backend,omitempty"`
	// Detail is the backend's own evidence text (bounded).
	Detail string `json:"detail,omitempty"`
	// FinishReason carries a structured finish signal when one exists
	// (e.g. the native engine's context-bound reason). Empty when the
	// evidence is error-text only.
	FinishReason string `json:"finishReason,omitempty"`
}

// Error implements error.
func (e *Exhaustion) Error() string {
	if e == nil {
		return ErrContextExhausted.Error()
	}
	if e.Backend == "" {
		return fmt.Sprintf("%s: %s", ErrContextExhausted.Error(), e.Detail)
	}
	return fmt.Sprintf("%s (%s): %s", ErrContextExhausted.Error(), e.Backend, e.Detail)
}

// Unwrap makes every Exhaustion detectable via errors.Is(err,
// recovery.ErrContextExhausted) — the ONE detection contract.
func (e *Exhaustion) Unwrap() error { return ErrContextExhausted }

// NewExhaustion returns a bounded *Exhaustion for the given backend
// evidence. The detail is clipped so a hostile backend message cannot
// bloat the recovery record.
func NewExhaustion(backend, detail, finishReason string) *Exhaustion {
	return &Exhaustion{
		Backend:      clip(backend, 64),
		Detail:       clip(detail, 500),
		FinishReason: clip(finishReason, 64),
	}
}

// IsContextExhausted reports whether err is (or wraps) the typed
// context-exhaustion condition. This is the ONLY detection predicate the
// recovery path accepts; callers must not grep error text themselves.
func IsContextExhausted(err error) bool {
	return errors.Is(err, ErrContextExhausted)
}

// AsExhaustion returns the typed condition when err carries it.
func AsExhaustion(err error) (*Exhaustion, bool) {
	var ex *Exhaustion
	if errors.As(err, &ex) {
		return ex, true
	}
	return nil, false
}

// MaxRecoveryAttempts bounds the AUTOMATIC recovery loop: one automatic
// recovery per exhaustion episode. If the next attempt immediately
// exhausts again, the loop stops with a clear diagnostic (see
// ErrRecoveryLoopGuard).
const MaxRecoveryAttempts = 1

// ErrRecoveryLoopGuard is returned/reported when automatic recovery
// stopped because the recovered attempt exhausted the context again.
var ErrRecoveryLoopGuard = errors.New(
	"context exhausted again after recovery — automatic recovery stopped after 1 attempt",
)

// clip bounds a string to max runes (byte-safe, no torn runes).
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
