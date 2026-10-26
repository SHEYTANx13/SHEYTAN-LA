//go:build !headless

// Ported UI-behavior contracts from the retired TypeScript frontend
// (src/*.ts) — the invariants that survived every migration round:
//
//   - stream-accumulator: activity events carry FULL CUMULATIVE captions;
//     folding REPLACES, never appends (idempotent under reconnect).
//   - session-list-guard: monotonic generation ticket; a stale list
//     response can never resurrect a deleted session or clobber a fresh one.
//   - mode-sessions: chat and agent are separate conversation spaces.
//   - history-ref: references are refs, deduped, no self-attach, capped.
//   - run-phase: the UI never invents a phase the backend did not signal.
package fyneui

import (
	"encoding/json"
	"sort"
	"strings"
)

// StreamKind canonicalizes activity event types (src/run-events.ts).
type StreamKind int

const (
	StreamIgnore StreamKind = iota
	StreamResponse
	StreamReasoning
	StreamStatus
	StreamToolStart
	StreamToolEnd
	StreamPlan
	StreamError
	StreamDone
	StreamComplete
	StreamAborted
	StreamIdle
	StreamEngine
	StreamAttached
	StreamSnapshot
)

// StreamEvent is one decoded /ws/activity frame.
type StreamEvent struct {
	Type      string          `json:"type"`
	Caption   string          `json:"caption,omitempty"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	RunID     string          `json:"runId,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Seq       int64           `json:"seq,omitempty"`
	// run_snapshot fields
	Phase           string `json:"phase,omitempty"`
	Running         bool   `json:"running,omitempty"`
	Revision        int    `json:"revision,omitempty"`
	LatestResponse  string `json:"latestResponse,omitempty"`
	LatestReasoning string `json:"latestReasoning,omitempty"`
	PausedDraft     string `json:"pausedDraft,omitempty"`
	TerminalOutcome string `json:"terminalOutcome,omitempty"`
	StartedAt       int64  `json:"startedAt,omitempty"`
	EndedAt         int64  `json:"endedAt,omitempty"`
	Persisted       bool   `json:"persisted,omitempty"`
}

func canonicalKind(t string) StreamKind {
	switch t {
	case "response", "assistant_delta":
		return StreamResponse
	case "reasoning", "thinking", "thinking_delta":
		return StreamReasoning
	case "status":
		return StreamStatus
	case "tool_start":
		return StreamToolStart
	case "tool_end":
		return StreamToolEnd
	case "plan":
		return StreamPlan
	case "error":
		return StreamError
	case "done":
		return StreamDone
	case "complete":
		return StreamComplete
	case "aborted":
		return StreamAborted
	case "idle":
		return StreamIdle
	case "engine":
		return StreamEngine
	case "attached":
		return StreamAttached
	case "run_snapshot":
		return StreamSnapshot
	default:
		return StreamIgnore
	}
}

// Terminal reports whether the kind settles a run.
func (k StreamKind) Terminal() bool {
	return k == StreamDone || k == StreamComplete || k == StreamAborted || k == StreamError
}

// StreamAccumulator folds the cumulative stream into presentation state.
// Contract: response/reasoning captions are REPLACED, never appended —
// replaying the same events after a reconnect is idempotent.
type StreamAccumulator struct {
	Active     bool
	Response   string
	Reasoning  string
	Status     string
	RunID      string
	Activities []string // bounded display ring (tool/plan/error captions)
	Terminal   StreamKind
}

// maxActivities bounds the display ring (Qt host shipped 50).
const maxActivities = 50

// Fold applies one event. Terminal events set Terminal and Active=false.
func (a *StreamAccumulator) Fold(ev StreamEvent) {
	kind := canonicalKind(ev.Type)
	switch kind {
	case StreamSnapshot:
		// Authoritative replay: adopt the snapshot verbatim.
		a.RunID = ev.RunID
		a.Response = ev.LatestResponse
		a.Reasoning = ev.LatestReasoning
		a.Active = ev.Running && !kind.Terminal()
		if ev.TerminalOutcome != "" {
			a.Terminal = canonicalKind(ev.TerminalOutcome)
			a.Active = false
		}
	case StreamAttached:
		// Wait for snapshot/idle before mutating live state.
	case StreamResponse:
		a.Response = ev.Caption // REPLACE, never append
		a.Active = true
	case StreamReasoning:
		a.Reasoning = ev.Caption // REPLACE, never append
		a.Active = true
	case StreamStatus:
		a.Status = ev.Caption
		a.Active = true
	case StreamToolStart, StreamToolEnd, StreamPlan:
		a.push(ev.Caption)
		a.Active = true
	case StreamError:
		a.push("error: " + ev.Caption)
		a.Active = false
		a.Terminal = StreamError
	case StreamDone, StreamComplete, StreamAborted:
		a.Active = false
		a.Terminal = kind
		if ev.Caption != "" {
			a.push(ev.Caption)
		}
	case StreamIdle:
		a.Active = false
	}
}

func (a *StreamAccumulator) push(caption string) {
	if strings.TrimSpace(caption) == "" {
		return
	}
	a.Activities = append(a.Activities, caption)
	if len(a.Activities) > maxActivities {
		a.Activities = a.Activities[len(a.Activities)-maxActivities:]
	}
}

// Reset clears the accumulator for a fresh attach.
func (a *StreamAccumulator) Reset() {
	*a = StreamAccumulator{}
}

// --- session-list-guard (src/session-list-guard.ts) ---

// SessionListGuard makes stale list responses harmless: every mutation
// invalidates in-flight tickets; a response whose ticket is no longer
// current is rejected by IsCurrent before it can touch UI state.
type SessionListGuard struct {
	generation uint64
}

// Ticket issues a fresh generation for an in-flight list request.
func (g *SessionListGuard) Ticket() uint64 {
	g.generation++
	return g.generation
}

// Invalidate bumps the generation so every outstanding ticket goes stale.
func (g *SessionListGuard) Invalidate() { g.generation++ }

// IsCurrent reports whether a ticket may still publish.
func (g *SessionListGuard) IsCurrent(t uint64) bool { return t == g.generation }

// --- mode-sessions (src/mode-sessions.ts) ---

// Session modes. Legacy sessions without a mode default to "agent"
// (the historical default surface) — same normalization the React client
// shipped.
const (
	ModeChat  = "chat"
	ModeAgent = "agent"
)

// NormalizeMode maps "" to ModeAgent and validates known modes.
func NormalizeMode(m string) string {
	switch m {
	case ModeChat:
		return ModeChat
	case ModeAgent, "":
		return ModeAgent
	default:
		return ModeAgent
	}
}

// FilterSessions returns only sessions of the given mode.
func FilterSessions(all []Session, mode string) []Session {
	mode = NormalizeMode(mode)
	out := make([]Session, 0, len(all))
	for _, s := range all {
		if NormalizeMode(s.Mode) == mode {
			out = append(out, s)
		}
	}
	return out
}

// --- history-ref (src/history-ref.ts) ---

// MaxHistoryRefs caps cross-session context references (shipped cap 4).
const MaxHistoryRefs = 4

// NormalizeHistoryRefs dedups, drops self-attachments and enforces the cap
// preserving first-seen order. The core validates again server-side; this
// keeps the UI honest about what it will send.
func NormalizeHistoryRefs(refs []string, activeSessionID string) []string {
	seen := make(map[string]bool, len(refs))
	out := make([]string, 0, MaxHistoryRefs)
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" || r == activeSessionID || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
		if len(out) == MaxHistoryRefs {
			break
		}
	}
	return out
}

// --- run-phase (src/run-phase.ts) ---

// RunPhase is the presentation lifecycle. The UI never invents a phase the
// backend did not signal: phases advance ONLY on observed events.
type RunPhase int

const (
	PhaseIdle RunPhase = iota
	PhasePreparing
	PhaseStreaming
	PhaseFinalising
	PhasePaused
	PhaseComplete
	PhaseError
	PhaseAborted
)

// RunPhaseMachine derives phases from observed events only; duplicate
// pause is a no-op (backend contract mirrored client-side).
type RunPhaseMachine struct {
	Phase RunPhase
}

// Observe transitions on stream events.
func (m *RunPhaseMachine) Observe(kind StreamKind) RunPhase {
	switch kind {
	case StreamSnapshot:
		if m.Phase != PhasePaused {
			m.Phase = PhaseStreaming
		}
	case StreamResponse, StreamReasoning, StreamStatus, StreamToolStart, StreamToolEnd, StreamPlan, StreamEngine:
		if m.Phase != PhasePaused {
			m.Phase = PhaseStreaming
		}
	case StreamDone, StreamComplete:
		m.Phase = PhaseComplete
	case StreamError:
		m.Phase = PhaseError
	case StreamAborted:
		m.Phase = PhaseAborted
	case StreamIdle:
		if m.Phase != PhaseComplete && m.Phase != PhaseError && m.Phase != PhaseAborted {
			m.Phase = PhaseIdle
		}
	}
	return m.Phase
}

// Pause marks paused (no-op when already paused or terminal).
func (m *RunPhaseMachine) Pause() RunPhase {
	switch m.Phase {
	case PhasePaused, PhaseComplete, PhaseError, PhaseAborted:
		return m.Phase
	default:
		m.Phase = PhasePaused
		return m.Phase
	}
}

// Reset returns to idle.
func (m *RunPhaseMachine) Reset() { m.Phase = PhaseIdle }

// --- thinking ladder (src ComposerControls reasoning-controls) ---

// ThinkingLevels is the shipped ladder (legacy names normalized in core).
var ThinkingLevels = []string{"low", "mid", "high", "ultra"}

// NormalizeThinking maps legacy names and validates.
func NormalizeThinking(s string) string {
	switch s {
	case "low", "mid", "high", "ultra":
		return s
	case "auto", "":
		return "mid" // v1.11 UI default; core normalizes legacy values too
	case "fast":
		return "low"
	case "thinking":
		return "high"
	default:
		return "mid"
	}
}

// --- zero-session send contract (src/store.ts) ---
// SendText with no active session MUST first create one in the current
// mode and complete the SAME send; failure to create surfaces a typed
// error instead of wedging the composer. Enforced in runtime.Send (state.go)
// and pinned by TestZeroSessionSend.

// SortSessionsByUpdated orders newest-first for display lists.
func SortSessionsByUpdated(s []Session) {
	sort.SliceStable(s, func(i, j int) bool {
		ti, tj := int64(0), int64(0)
		if s[i].UpdatedAt != nil {
			ti = s[i].UpdatedAt.Unix()
		}
		if s[j].UpdatedAt != nil {
			tj = s[j].UpdatedAt.Unix()
		}
		return ti > tj
	})
}
