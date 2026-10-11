// runcontrol.go — v1.7.4 backend-neutral PAUSE / EDIT / RESUME control.
//
// One RunControl per run, created by the API layer and handed to the
// orchestrator through WithRunControl. It is the ONLY channel through
// which a pause request reaches a live generation: the orchestrator polls
// it at SAFE BOUNDARIES (stream deltas, tool-round edges) and stops
// generation cooperatively — never mid-mutation, never by killing the
// parent process, never by faking a successful result.
//
// Semantics (the one authoritative contract):
//
//	RUNNING → PAUSING → PAUSED → RESUMING → RUNNING → DONE/ERROR/ABORTED
//
// Pause is SEMANTIC CONTINUATION, honestly labeled: the partial answer is
// persisted as a durable checkpoint, the backend stream is stopped at the
// next boundary, and resume rebuilds the request from the persisted
// transcript + accepted draft. This is not a claimed byte-identical KV
// continuation; backend capability probing may upgrade specific backends
// later without changing this contract.
package agent

import (
	"sync/atomic"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// ToolPauseState records where the run was when a pause took effect, so a
// resume can distinguish tool work that never started from work that is
// already durably committed (spec §9: never replay a side-effecting tool
// call blindly).
type ToolPauseState string

const (
	// ToolStateNone — no tool calls were assembled when the pause fired.
	ToolStateNone ToolPauseState = ""

	// ToolStatePending — tool calls were assembled by the model but NOT
	// executed. A resume re-offers them through the normal loop; nothing
	// was side-effected.
	ToolStatePending ToolPauseState = "pending"

	// ToolStateCommitted — the last assembled tool round fully completed
	// (results captured into the conversation). A resume continues AFTER
	// it; results are never duplicated.
	ToolStateCommitted ToolPauseState = "committed"
)

// RunControl is the per-run pause control shared between the API layer
// and the orchestrator. Safe for concurrent use.
type RunControl struct {
	requested atomic.Bool // a pause has been requested, not yet effective
}

// NewRunControl creates a fresh control (fresh runs and resumed runs each
// get one; the API may reuse the resumed run's control so a SECOND pause
// after a resume still works).
func NewRunControl() *RunControl { return &RunControl{} }

// RequestPause asks the run to stop at the next safe boundary. Idempotent:
// duplicate requests are cheap no-ops.
func (c *RunControl) RequestPause() {
	if c == nil {
		return
	}
	c.requested.Store(true)
}

// PauseRequested reports whether a pause is pending.
func (c *RunControl) PauseRequested() bool {
	return c != nil && c.requested.Load()
}

// Clear resets the flag (used when a pause is delivered and the run later
// resumes with the SAME control object).
func (c *RunControl) Clear() {
	if c == nil {
		return
	}
	c.requested.Store(false)
}

// resumeContinuation carries the persisted partial answer a resumed run
// must continue from (semantic continuation mode).
type resumeContinuation struct {
	draft     string // accepted generated assistant prefix
	reasoning string // reasoning prefix (only what the product already streams)

	// pendingToolCalls are tool calls the model assembled but never
	// executed when the pause fired. They ride the checkpoint for
	// diagnostics and honest state; a resumed run does NOT blindly replay
	// them — the continuation request carries them so the loop re-offers
	// them through the normal, idempotent execution path.
	pendingToolCalls []llm.ToolCall
}

// WithRunControl hands the run its pause control. Nil means the run can
// never be paused (CLI paths keep their existing behavior).
func WithRunControl(ctrl *RunControl) RunOption {
	return func(ro *runOptions) {
		ro.runControl = ctrl
	}
}

// WithResumeContinuation marks this RunDetailed call as a RESUME of a
// paused run: the model receives the accepted draft as its own partial
// answer and is instructed to continue exactly where it left off. The
// caller joins draft + continuation into ONE authoritative assistant
// revision — the draft is never persisted twice.
func WithResumeContinuation(draft, reasoning string) RunOption {
	return func(ro *runOptions) {
		ro.resume = &resumeContinuation{draft: draft, reasoning: reasoning}
	}
}
