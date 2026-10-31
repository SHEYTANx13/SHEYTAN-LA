// runstate.go — v1.2.6 continuation: the AUTHORITATIVE live per-run state.
//
// PROBLEM (v1.2.6 continuation brief): the v1.2.6 terminal replay fixed the
// run-that-finished-before-attach race, but the activity hub STILL has no
// replay for a LIVE run: a WebSocket that attaches mid-generation receives
// only FUTURE hub events. A reconnect during generation therefore renders an
// empty stream until the next coalesced delta, and a socket attaching in a
// gap between emits (tool execution, engine gate, emit throttle window) sees
// nothing at all. The UI discovers the run's state only when a future token
// happens to arrive.
//
// FIX (root cause, not symptom): every run now owns a BOUNDED authoritative
// state object updated as the server publishes each activity:
//
//	runId / sessionId / phase / running
//	latestResponseSnapshot   (cumulative — response captions are full
//	latestReasoningSnapshot   snapshots, so "latest" IS the state)
//	latestStatus
//	sequence                  (monotonic, stamped on every event)
//	startedAt / endedAt
//	terminalOutcome / persisted / error
//	recent events ring        (bounded, diagnostics only)
//
// On every new WebSocket attachment the server sends a `run_snapshot` frame
// built from this state BEFORE forwarding live events, and the client
// continues from the snapshot's sequence — a reconnect never waits for a
// future token to discover the current state. Response/reasoning captions
// are cumulative by contract (orchestrator emitProgress), so replay is
// naturally idempotent on the client.
//
// Bounds: one live state per active run (the runs map, removed when the run
// settles), snapshots capped at snapshotCapBytes, the recent-events ring at
// recentEventCap entries, terminal states in the bounded outcome registry
// (runregistry.go). Memory is O(active runs × snapshotCapBytes).
package api

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/agent"
)

const (
	// snapshotCapBytes bounds one cumulative response/reasoning snapshot.
	// Local-model answers stay far below this; the cap exists so a
	// pathological stream can never grow the retained state unboundedly.
	snapshotCapBytes = 256 << 10

	// recentEventCap bounds the per-run recent-events ring (diagnostics).
	recentEventCap = 32
)

// runLive is the authoritative live state of ONE run. Safe for concurrent
// use: the publisher goroutine stamps/observes, WebSocket attachers read
// snapshots. All fields are either atomic or guarded by mu.
type runLive struct {
	runID     string
	sessionID string

	// settled is the SETTLEMENT EDGE (v1.3.7): a channel closed exactly
	// once, at the top of settleTerminal, i.e. strictly AFTER settle() has
	// recorded the terminal outcome in the bounded registry (the v1.2.7
	// ordering) and therefore at the moment the outcome becomes
	// recoverable AND the authoritative state settles terminal. The
	// idle/lastRun reader path waits on this edge instead of racing the
	// settlement window — see server.go's idleSentinel barrier.
	settled     chan struct{}
	settledOnce sync.Once

	// seq is the monotonic per-run event sequence. Every published event
	// of this run carries the value returned by next() — stamped BEFORE
	// the event reaches any subscriber, so (snapshot at seq N) + (all
	// events with seq > N) is a gapless, duplicate-free replay contract.
	seq atomic.Int64

	startedAt time.Time

	mu              sync.Mutex
	running         bool
	phase           string // "preparing" | "thinking" | "generating" | "pausing" | "paused" | "resuming" | terminal outcome
	latestResponse  string // cumulative snapshot (bounded)
	latestReasoning string // cumulative snapshot (bounded)
	latestStatus    string
	latestTask      map[string]any // v1.2.8 bounded task-state snapshot
	endedAt         time.Time
	terminalOutcome string // "" while running; "done" | "error" | "aborted"
	persisted       bool
	errMsg          string
	recent          []agent.Activity
	persistedReply  string // terminal reply snapshot (bounded) for late WS replay
	persistedReason string

	// v1.7.4 run-control state machine: RUNNING → PAUSING → PAUSED →
	// RESUMING → RUNNING → terminal. revision is monotonic per run and
	// bumped by every accepted edit; stale-revision mutations are
	// rejected with the current value so a client can resync. The
	// paused draft is retained here (bounded) so a reconnecting client
	// re-renders the partial answer without re-reading the checkpoint.
	revision    int64
	pausedDraft string
	pausedWhy   string
}

func newRunLive(runID, sessionID string, startedAt time.Time) *runLive {
	return &runLive{
		runID:     runID,
		sessionID: sessionID,
		startedAt: startedAt,
		running:   true,
		phase:     "preparing",
		settled:   make(chan struct{}),
	}
}

// nextSeq reserves the next monotonic sequence number for this run.
func (l *runLive) nextSeq() int64 {
	return l.seq.Add(1)
}

// currentSeq reports the last issued sequence number.
func (l *runLive) currentSeq() int64 {
	return l.seq.Load()
}

// observe folds one stamped activity into the authoritative state. Called
// exactly once per published event, on the publisher's goroutine.
func (l *runLive) observe(a agent.Activity) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Bounded recent-events ring (diagnostics only).
	if l.recent == nil {
		l.recent = make([]agent.Activity, 0, recentEventCap)
	}

	// v1.7.4: never retain a caller-owned map by reference — the
	// publisher goroutine may still hold and mutate it while a WS
	// reader marshals the retained ring/snapshot. Clone map details on
	// the way in; the retained ring stays iteration-safe.
	if m, ok := a.Detail.(map[string]any); ok {
		a.Detail = cloneStringAnyMap(m)
	}

	l.recent = append(l.recent, a)
	if len(l.recent) > recentEventCap {
		l.recent = l.recent[len(l.recent)-recentEventCap:]
	}

	// Cumulative snapshots: response/reasoning captions replace; status is
	// a rolling caption. phase derives ONLY from what was observed.
	switch a.Type {
	case "response", "assistant_delta":
		l.latestResponse = capSnapshot(a.Caption)
		if l.phase == "preparing" || l.phase == "thinking" {
			l.phase = "generating"
		}
	case "reasoning", "thinking_delta":
		l.latestReasoning = capSnapshot(a.Caption)
		if l.phase == "preparing" {
			l.phase = "thinking"
		}
	case "status":
		l.latestStatus = capSnapshot(a.Caption)
	case "task":
		// v1.2.8: the bounded task-state snapshot rides the run
		// snapshot — a reconnecting client restores the task view
		// (goal/plan/step/files/tests/verification) along with the
		// cumulative text, without the run restarting.
		// v1.7.4: stored as a PRIVATE clone — snapshot() hands the
		// map to a concurrent JSON marshaler; sharing the caller's
		// instance was a fatal concurrent map read/write hazard.
		if m, ok := a.Detail.(map[string]any); ok {
			l.latestTask = cloneStringAnyMap(m)
		} else if a.Detail != nil {
			// Non-map detail (e.g. a typed struct in-process):
			// re-marshal through JSON once, bounded.
			if data, err := json.Marshal(a.Detail); err == nil {
				var m map[string]any
				if json.Unmarshal(data, &m) == nil {
					l.latestTask = m
				}
			}
		}
	case "tool_start", "tool_end":
		// Tool activity keeps the current phase — the run keeps
		// generating around tool calls; the phase never regresses.
	case "done", "complete":
		if l.terminalOutcome == "" {
			l.terminalOutcome = "done"
		}
		l.running = false
		l.endedAt = time.Now()
		if l.phase != "error" {
			l.phase = "done"
		}
	case "aborted":
		// v1.8.0: the orchestrator's HONEST abort marker. A canceled
		// generation is not a completed one: publishing "done" here
		// flipped the early-visible terminal outcome to "done" while
		// the caller's settle() recorded "aborted" - the registry and
		// the live state then disagreed forever (settleTerminal never
		// overrides an existing outcome). The abort paths now publish
		// "aborted" and every terminal observer agrees.
		if l.terminalOutcome == "" {
			l.terminalOutcome = "aborted"
		}
		l.running = false
		l.endedAt = time.Now()
		l.phase = "aborted"
	case "error":
		if l.terminalOutcome == "" {
			l.terminalOutcome = "error"
		}
		l.running = false
		l.endedAt = time.Now()
		l.phase = "error"
		l.errMsg = capSnapshot(a.Caption)
	}
}

// settledSignal returns the settlement edge of this run: a channel that is
// closed exactly once, after the terminal outcome has been recorded in the
// bounded registry and the authoritative state has settled terminal. A
// reader released by this close observes BOTH the recorded outcome and the
// terminal state — the deterministic happens-before edge the idle/lastRun
// path synchronizes on (v1.3.7). A receive on the still-open channel simply
// blocks, which is how a reader inside the settlement window parks until the
// outcome is recoverable.
func (l *runLive) settledSignal() <-chan struct{} {
	return l.settled
}

// settleTerminal records the authoritative terminal outcome exactly once
// (the run goroutine calls it on every exit path; outcome from REAL
// signals only). persisted carries the reply/reasoning actually saved to
// the session store so a late socket can render the final answer without
// waiting for a session reload.
func (l *runLive) settleTerminal(outcome, caption string, persisted bool, reply, reasoning string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// v1.3.7 SETTLEMENT EDGE. settleTerminal is called by settle() strictly
	// AFTER the terminal outcome has been recorded in the bounded registry
	// (the v1.2.7 ordering), so closing the signal HERE means "the outcome
	// is recoverable and the state is authoritatively terminal". The close
	// must happen on EVERY call path — including the already-terminal early
	// return below — because observe() may have folded the terminal
	// activity first (the early-visibility flip that created the
	// settlement window); the registry record still happened before this
	// call, so the edge is honest on that path too. This is the missing
	// synchronization edge of Actions run 35749698189: previously a socket
	// attaching between the observe() flip and the settle() record read an
	// empty registry and emitted a lastRun-less idle sentinel.
	l.settledOnce.Do(func() { close(l.settled) })

	if l.terminalOutcome != "" {
		return
	}

	l.terminalOutcome = outcome
	l.running = false
	l.endedAt = time.Now()
	l.phase = outcome
	l.persisted = persisted
	l.persistedReply = capSnapshot(reply)
	l.persistedReason = capSnapshot(reasoning)

	if outcome == "error" && caption != "" {
		l.errMsg = capSnapshot(caption)
	}
}

// runSnapshot is the wire shape of the authoritative replay frame sent on
// every WebSocket attachment (and mirrored inside the idle sentinel's
// lastRun block for terminal runs).
type runSnapshot struct {
	RunID           string    `json:"runId"`
	SessionID       string    `json:"sessionId"`
	Phase           string    `json:"phase"`
	Running         bool      `json:"running"`
	Sequence        int64     `json:"sequence"`
	StartedAt       time.Time `json:"startedAt"`
	EndedAt         time.Time `json:"endedAt,omitzero"` // Go 1.24+ omitzero; zero stays out on the wire
	LatestResponse  string    `json:"latestResponse,omitempty"`
	LatestReasoning string    `json:"latestReasoning,omitempty"`
	LatestStatus    string    `json:"latestStatus,omitempty"`
	TerminalOutcome string    `json:"terminalOutcome,omitempty"`
	Persisted       bool      `json:"persisted,omitempty"`
	Error           string    `json:"error,omitempty"`

	// Task (v1.2.8) is the latest bounded task-state snapshot folded from
	// the run's `task` activities — reconnect recovery carries the whole
	// working state, not just the streamed text.
	Task map[string]any `json:"task,omitempty"`

	// v1.7.4 run-control surface: the monotonic checkpoint revision and
	// the accepted partial answer while the run is paused. A reconnect
	// during PAUSED re-renders the draft and resumes against the SAME
	// revision — stale clients are rejected server-side.
	Revision    int64  `json:"revision"`
	PausedDraft string `json:"pausedDraft,omitempty"`
}

// snapshot copies the authoritative state for one wire frame. The returned
// sequence is the LAST event already folded into the snapshots — the
// subscriber forwards only events with a strictly greater sequence.
func (l *runLive) snapshot() runSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	snap := runSnapshot{
		RunID:           l.runID,
		SessionID:       l.sessionID,
		Phase:           l.phase,
		Running:         l.running,
		Sequence:        l.seq.Load(),
		StartedAt:       l.startedAt,
		EndedAt:         l.endedAt,
		LatestResponse:  l.latestResponse,
		LatestReasoning: l.latestReasoning,
		LatestStatus:    l.latestStatus,
		TerminalOutcome: l.terminalOutcome,
		Persisted:       l.persisted,
		Error:           l.errMsg,
		Revision:        l.revision,
		PausedDraft:     l.pausedDraft,
		Task:            cloneStringAnyMap(l.latestTask),
	}

	// A finished run replays the PERSISTED reply (authoritative final
	// answer) even when the streamed snapshot was empty (e.g. the run
	// completed before any delta was observed by a socket).
	if !l.running && l.persistedReply != "" {
		snap.LatestResponse = l.persistedReply
		snap.LatestReasoning = l.persistedReason
	}

	return snap
}

// capSnapshot bounds one retained cumulative snapshot.
func capSnapshot(s string) string {
	if len(s) <= snapshotCapBytes {
		return s
	}
	// Keep the TAIL: the newest bytes are the live edge of a cumulative
	// snapshot; the head is history already persisted per-milestone.
	return "…[truncated]…" + s[len(s)-snapshotCapBytes:]
}

// ---------------------------------------------------------------------------
// v1.7.4 run-control state machine
//
// RUNNING → PAUSING → PAUSED → RESUMING → RUNNING → DONE/ERROR/ABORTED
//
// The ONE authoritative per-run state (runLive) owns the transitions; the
// HTTP handlers and the run goroutine call these methods and nothing else
// mutates the pause fields. Transitions are idempotent and honestly
// ordered: the durable checkpoint is persisted by the CALLER before
// confirmPaused is invoked, so the phase that promises durability is only
// ever published after the promise is kept.
// ---------------------------------------------------------------------------

// requestPause marks a running run as PAUSE_REQUESTED ("pausing"). It is
// idempotent: a duplicate request (double-click, retry, reconnect) is a
// no-op that still returns the current state. Terminal and already-paused
// runs reject the request so a stale client cannot resurrect a finished
// run.
func (l *runLive) requestPause() (int64, string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// v1.7.5: a RESUMING run rejects the pause too. Its generation has not
	// started yet and the resume path resets the shared control flag
	// (RunControl.Clear) — accepting the request here would silently
	// swallow it. The client gets an explicit conflict and retries once the
	// run is generating again.
	if l.terminalOutcome != "" || l.phase == "paused" || l.phase == "pausing" || l.phase == "resuming" {
		return l.revision, l.phase, false
	}

	l.phase = "pausing"
	return l.revision, l.phase, true
}

// confirmPaused records the PAUSED state. Call it ONLY after the durable
// checkpoint is on disk. Not terminal: the run stays registered, its hub
// stays open, and a reconnect replays the paused draft.
func (l *runLive) confirmPaused(draft, why string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.terminalOutcome != "" {
		return
	}

	l.phase = "paused"
	l.pausedDraft = capSnapshot(draft)
	l.pausedWhy = why
}

// restoreRevision pins a RESTART-RECOVERED run's revision to the durable
// checkpoint value (v1.7.6, §7/§12): the paused state must claim exactly
// the revision the checkpoint records. The v1.7.5 recovery restored the
// draft but left the revision at 0 — a run that was EDITED before its
// process died came back unable to resume at its own recorded revision
// (every resume attempt 409'd against the very checkpoint it recovered
// from). Valid only while paused; never moves a revision backwards.
func (l *runLive) restoreRevision(revision int64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.terminalOutcome != "" || l.phase != "paused" || revision < l.revision {
		return
	}

	l.revision = revision
}

// requestResume moves a paused run to RESUMING. Only a paused run resumes;
// the caller supplies the revision it acted on and a stale request is
// rejected with the current revision (actionable conflict, never silence).
func (l *runLive) requestResume(revision int64) (int64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.terminalOutcome != "" || l.phase != "paused" || revision != l.revision {
		return l.revision, false
	}

	l.phase = "resuming"
	return l.revision, true
}

// confirmResumed returns a resumed run to the live generating state
// (deltas re-derive the exact streaming phase).
func (l *runLive) confirmResumed() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.terminalOutcome != "" || l.phase != "resuming" {
		return
	}

	l.phase = "generating"
	l.pausedDraft = ""
	l.pausedWhy = ""
}

// revertResumingLocked (v1.10.1) returns a run whose resume was REFUSED
// before the generation started (server shutting down) to the settled
// paused state — the durable checkpoint still exists, so the run must stay
// honestly paused and resumable. The caller holds ctrlMu; the live mutex
// is taken here (same nesting as the other confirm* callers).
func (l *runLive) revertResumingLocked() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.terminalOutcome != "" || l.phase != "resuming" {
		return
	}

	l.phase = "paused"
}

// isPaused reports the authoritative paused/pausing state.
func (l *runLive) isPaused() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.phase == "paused" || l.phase == "pausing"
}

// isPausedSettled reports a CONFIRMED paused run (the checkpoint exists).
func (l *runLive) isPausedSettled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.phase == "paused"
}

// currentRevision reports the checkpoint revision (0 while running).
func (l *runLive) currentRevision() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.revision
}

// bumpRevision advances the checkpoint revision after an accepted edit and
// returns the new value. Editing is only valid while paused.
//
// v1.7.5: the edit handler no longer calls this BEFORE the durable work —
// an in-memory revision that runs ahead of the durable checkpoint is
// exactly the inconsistency §10 forbids. Use canEdit (validation only)
// plus confirmEdited (publication after durability) instead. Kept for the
// state-machine unit tests that exercise the transition in isolation.
func (l *runLive) bumpRevision(revision int64) (int64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.terminalOutcome != "" || l.phase != "paused" || revision != l.revision {
		return l.revision, false
	}

	l.revision++
	return l.revision, true
}

// canEdit reports whether an edit acting on the given revision would be
// accepted — WITHOUT mutating anything (v1.7.5). The edit handler uses
// this CAS-peek before its durable work; the revision actually becomes
// authoritative only in confirmEdited, after the checkpoint is safely on
// disk, so a failed save can never leave the live state ahead of the
// durable record.
func (l *runLive) canEdit(revision int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.terminalOutcome == "" && l.phase == "paused" && revision == l.revision
}

// cloneStringAnyMap returns a shallow-value deep copy of a JSON-shaped
// map: keys/values are copied into a fresh map so the returned instance
// shares no mutable structure with the caller. Values are JSON-shaped
// (strings, numbers, bools, nested maps/slices); nested containers are
// cloned recursively — a run snapshot may be marshaled on ANY goroutine
// while the publisher still mutates the source map.
func cloneStringAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneJSONValue(v)
	}
	return out
}

func cloneJSONValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneStringAnyMap(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneJSONValue(e)
		}
		return out
	default:
		return v
	}
}

// phaseLocked reports the current phase (diagnostics + conflict messages).
func (l *runLive) phaseLocked() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.phase
}

// confirmEdited publishes an accepted EDIT on the authoritative state
// AFTER the checkpoint is durable: the draft shown to reconnecting clients
// and the revision advance together, atomically under the state lock.
func (l *runLive) confirmEdited(draft string, revision int64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.terminalOutcome != "" {
		return
	}

	l.phase = "paused"
	l.pausedDraft = capSnapshot(draft)
	l.revision = revision
}
