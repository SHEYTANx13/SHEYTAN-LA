// runregistry.go — v1.2.6 bounded authoritative run-state registry.
//
// PROBLEM (v1.2.5 field report): a simple prompt can complete before the
// activity WebSocket attaches to the run hub. The hub has no replay, so the
// late socket parks in standby and only ever sees the bare `idle` sentinel.
// The frontend's grace window then drops that sentinel (it looks exactly like
// the harmless standby marker emitted right after POST /api/run), and the UI
// stays "running" with no visible answer — forever.
//
// FIX (root cause, not symptom): the server keeps a BOUNDED ring of the most
// recent run OUTCOMES per session. Every idle sentinel now carries the latest
// outcome (run id, end time, result, whether a reply was persisted and its
// measured size). A socket that attaches late therefore receives AUTHORITATIVE
// evidence that the run already ended, and finalises deterministically.
//
// Bounds: lastOutcomesPerSession entries per session (ring), lastOutcomesMax
// sessions total (LRU eviction). Memory is O(sessions × ring), each record a
// few hundred bytes — no content is retained, only measurements.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// lastOutcomesPerSession bounds the per-session outcome ring.
	lastOutcomesPerSession = 4

	// lastOutcomesMax bounds the total number of tracked sessions (LRU).
	lastOutcomesMax = 256
)

// runOutcome is the authoritative terminal state of ONE completed run.
type runOutcome struct {
	RunID       string    `json:"runId"`
	StartedAt   time.Time `json:"startedAt"`
	EndedAt     time.Time `json:"endedAt"`
	Outcome     string    `json:"outcome"` // "done" | "error" | "aborted"
	Caption     string    `json:"caption,omitempty"`
	Persisted   bool      `json:"persisted"` // the assistant reply was appended to the session
	ReplyChars  int       `json:"replyChars,omitempty"`
	ReasonChars int       `json:"reasonChars,omitempty"`
}

// runTimeline measures the API-side stages of one run (v1.2.6). All values
// are wall-clock timestamps taken where the stage actually happens — no
// derived, guessed or reused numbers.
type runTimeline struct {
	runID         string
	sessionID     string
	acceptedAt    time.Time // POST /api/run landed
	registeredAt  time.Time // run registered in the active-runs map
	gateStart     time.Time // engine gate entered
	gateReady     time.Time // engine gate released (engine ready / failed)
	persistedAt   time.Time // assistant reply appended to the session
	donePublished time.Time // done/complete activity published to the hub

	// v1.9.4: aggregate PUBLISHED-EVENT counters (counts only — never
	// captions) so the ordinary run summary distinguishes "nothing
	// streamed" from "streamed but not delivered": events counts
	// everything stamped through the ONE publisher; deltas counts
	// response/reasoning snapshots; firstDeltaAt is the wall-clock of the
	// FIRST content-bearing snapshot (the server-side anchor the opt-in
	// test trace correlates against).
	events       int       // published activity count (aggregate)
	deltas       int       // response/reasoning snapshot count (aggregate)
	firstDeltaAt time.Time // first content-bearing snapshot published
}

// runRegistry is the bounded session → outcome-ring store. Safe for
// concurrent use; never blocks a run (record is O(1) under one mutex).
type runRegistry struct {
	mu       sync.Mutex
	sessions map[string][]runOutcome
	order    []string // session LRU order (front = oldest)
}

func newRunRegistry() *runRegistry {
	return &runRegistry{
		sessions: make(map[string][]runOutcome),
	}
}

// record appends one outcome to the session ring, evicting the oldest entry
// beyond the ring bound and the least-recently-recorded session beyond the
// total bound.
func (r *runRegistry) record(sessionID string, rec runOutcome) {
	if sessionID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	ring := append(r.sessions[sessionID], rec)
	if len(ring) > lastOutcomesPerSession {
		ring = ring[len(ring)-lastOutcomesPerSession:]
	}
	r.sessions[sessionID] = ring

	// LRU touch: move sessionID to the back (most recent).
	for i, id := range r.order {
		if id == sessionID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.order = append(r.order, sessionID)

	// Evict oldest sessions beyond the total bound.
	for len(r.order) > lastOutcomesMax {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.sessions, oldest)
	}
}

// latest returns the most recent outcome for a session (ok=false when none
// was recorded).
func (r *runRegistry) latest(sessionID string) (runOutcome, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ring := r.sessions[sessionID]
	if len(ring) == 0 {
		return runOutcome{}, false
	}

	return ring[len(ring)-1], true
}

// newRunID mints a compact, unique-enough run identifier (16 hex chars from
// the crypto pool — collision-irrelevant at this scale, greppable in logs).
func newRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is effectively impossible on supported
		// platforms; fall back to a time-derived id so a run NEVER loses
		// its identity because id minting failed.
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
