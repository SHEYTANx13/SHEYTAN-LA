// Package ctxtelemetry implements Phase 7 context-effectiveness telemetry.
//
// The small-model strategy lives or dies on ONE question: which context
// actually improved the task? This package records the measurable answer
// per turn — tokens added and removed, retrieval latency and relevance,
// reused context, compression ratio, pressure, and whether retrieval was
// followed by successful tool work and task success — into a bounded JSONL
// store that diagnostics can aggregate. It observes; it never decides.
//
// v1.2.4 performance rework: the record set is cached in memory and reads
// are served without touching disk. The file is rewritten only when a
// coalescing window closes (flushCount records or flushInterval, whichever
// comes first) or when Flush is called explicitly at run boundaries. This
// removes the per-turn full-file re-read + re-marshal that dominated the
// steady-state cost while keeping the on-disk format byte-compatible.
package ctxtelemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Coalescing bounds: at most flushCount records or flushInterval of
// unflushed telemetry is tolerated before a bounded rewrite happens.
// Telemetry is observational data (never user content), so a hard crash
// may lose at most one coalescing window.
const (
	flushCount    = 64
	flushInterval = 10 * time.Second
)

// TurnRecord is one turn's context-effectiveness measurement.
type TurnRecord struct {
	// Turn identifies the measurement (session + sequence).
	SessionID string    `json:"sessionId"`
	Turn      int       `json:"turn"`
	At        time.Time `json:"at"`

	// Model and budget context.
	Model        string  `json:"model,omitempty"`
	BudgetTokens int     `json:"budgetTokens,omitempty"`
	UsedTokens   int     `json:"usedTokens,omitempty"`
	Pressure     float64 `json:"pressure,omitempty"`

	// 1.1.6 context decision trail — the full context plan per turn so
	// telemetry can answer "what did the planner decide and why".
	ContextRequested int `json:"contextRequested,omitempty"` // policy/global before clamps
	ContextEffective int `json:"contextEffective,omitempty"` // the window the plan used
	ContextModelMax  int `json:"contextModelMax,omitempty"`  // GGUF training limit
	ContextEngineMax int `json:"contextEngineMax,omitempty"` // engine-verified window
	SessionContext   int `json:"sessionContext,omitempty"`   // raw per-session policy (0 = none)

	// 1.1.6 measured section split (token estimates per named section).
	TokensSystem        int `json:"tokensSystem,omitempty"`
	TokensTools         int `json:"tokensTools,omitempty"`
	TokensProject       int `json:"tokensProject,omitempty"`
	TokensRecall        int `json:"tokensRecall,omitempty"` // recall + project card + skills combined (SectionRecall)
	TokensAttachments   int `json:"tokensAttachments,omitempty"`
	TokensHistory       int `json:"tokensHistory,omitempty"`
	TokensCurrentTask   int `json:"tokensCurrentTask,omitempty"`
	TokensOutputReserve int `json:"tokensOutputReserve,omitempty"`
	TokensSafetyReserve int `json:"tokensSafetyReserve,omitempty"`

	// 1.1.6 degradation + protection outcomes.
	TokensElided      int  `json:"tokensElided,omitempty"`      // windowed messages (plan counter)
	TokensCompressed  int  `json:"tokensCompressed,omitempty"`  // history + in-loop tool-result compaction
	RolloverTriggered bool `json:"rolloverTriggered,omitempty"` // Continuum chapter rollover followed
	OverflowPrevented bool `json:"overflowPrevented,omitempty"` // a refusal gate fired instead of an engine call
	RecoveryTriggered bool `json:"recoveryTriggered,omitempty"` // v1.7.1: a REAL context exhaustion was recovered
	RecoveryHandoffID string `json:"recoveryHandoffId,omitempty"` // v1.7.1: durable recovery record id

	// Context deltas.
	TokensAdded   int `json:"tokensAdded,omitempty"`   // injected blocks (recall, attachments, skills)
	TokensRemoved int `json:"tokensRemoved,omitempty"` // windowed/elided history
	ReusedTokens  int `json:"reusedTokens,omitempty"`  // stable prefix served from the engine prompt cache

	// Compression ratio: removed / (added+removed) — 0 when nothing was
	// compressed; approaches 1 when almost everything was elided.
	CompressionRatio float64 `json:"compressionRatio,omitempty"`

	// Retrieval measurements.
	RetrievalLatencyMs int64   `json:"retrievalLatencyMs,omitempty"`
	RetrievalHits      int     `json:"retrievalHits,omitempty"`
	RetrievalRelevance float64 `json:"retrievalRelevance,omitempty"` // mean recall score of injected digests

	// Outcomes observed AFTER the context was assembled.
	ToolCalls       int     `json:"toolCalls,omitempty"`
	ToolSuccesses   int     `json:"toolSuccesses,omitempty"`
	ToolSuccessRate float64 `json:"toolSuccessRate,omitempty"`
	TaskSuccess     bool    `json:"taskSuccess,omitempty"`
	Verified        string  `json:"verified,omitempty"` // objective verification verdict

	// -------------------------------------------------------------------
	// v1.2.5 adaptive-tier + per-request timing fields (measured only).

	// Tier is the context tier the turn STARTED at (FAST/STANDARD/DEEP/MAX).
	Tier string `json:"tier,omitempty"`

	// FinalTier is the tier the turn ended at (differs after escalation).
	FinalTier string `json:"finalTier,omitempty"`

	// Escalations counts tier moves applied during the turn.
	Escalations int `json:"escalations,omitempty"`

	// ThinkingControl records the composer's per-request control
	// (auto/fast/thinking) that actually shaped the backend request.
	ThinkingControl string `json:"thinkingControl,omitempty"`

	// ToolPolicyMode records the per-request tool policy (auto/manual).
	ToolPolicyMode string `json:"toolPolicyMode,omitempty"`

	// NetSearch (v1.3.6, spec §24) records the composer's EXPLICIT
	// per-request Net Search intent — the real setting that drove the
	// run, never an inference from message text.
	NetSearch bool `json:"netSearch,omitempty"`

	// Timing is the measured per-request stage table.
	Timing RequestTiming `json:"timing,omitempty"`
}

// RequestTiming is the v1.2.5 measured per-request timeline: every value
// comes from a real stage timestamp; stages that never fired report 0.
type RequestTiming struct {
	ClassifyMs      int64 `json:"classifyMs,omitempty"`
	ContextMs       int64 `json:"contextMs,omitempty"`
	PromptMs        int64 `json:"promptMs,omitempty"`
	SerializationMs int64 `json:"serializationMs,omitempty"`
	TTFTMs          int64 `json:"ttftMs,omitempty"` // request sent → first token
	GenerationMs    int64 `json:"generationMs,omitempty"`
	ToolMs          int64 `json:"toolMs,omitempty"`
	VerificationMs  int64 `json:"verificationMs,omitempty"`
	TotalMs         int64 `json:"totalMs,omitempty"`

	// FirstPromptTokens is the measured prompt size of the FIRST engine
	// request of the turn (the tier's actual initial cost).
	FirstPromptTokens int `json:"firstPromptTokens,omitempty"`
}

// Stats is the store's I/O telemetry (v1.2.4): it makes the coalescing
// behaviour measurable instead of assumed.
type Stats struct {
	Records      uint64 `json:"records"`      // Record() calls accepted
	Flushes      uint64 `json:"flushes"`      // file rewrites performed
	Coalesced    uint64 `json:"coalesced"`    // records folded into memory without an immediate write
	DirtyMax     int    `json:"dirtyMax"`     // largest observed unflushed window
	InMemory     int    `json:"inMemory"`     // records currently resident
	LastFlushAt  string `json:"lastFlushAt"`  // RFC3339, empty when never flushed
	LastFlushDur int64  `json:"lastFlushDur"` // last rewrite duration, microseconds
}

// Store is the bounded JSONL telemetry store.
type Store struct {
	mu      sync.Mutex
	path    string
	maxRecs int

	// In-memory record cache (v1.2.4). Loaded lazily from disk exactly
	// once; afterwards Record/Recent/Summarize never re-read the file.
	recs    []TurnRecord
	loaded  bool
	dirty   int
	lastFl  time.Time
	reports Stats
}

// Option configures a store.
type Option func(*Store)

// WithMaxRecords overrides the compaction bound (also useful for tests).
func WithMaxRecords(n int) Option {
	return func(s *Store) { s.maxRecs = n }
}

// NewStore creates the store under dir (file: ctxtelemetry.jsonl).
func NewStore(dir string, opts ...Option) *Store {
	s := &Store{
		path:    filepath.Join(dir, "ctxtelemetry.jsonl"),
		maxRecs: 4096,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Record appends one turn measurement; oversized stores are compacted to
// the newest half. The record lands in the in-memory cache immediately and
// is persisted when the coalescing window closes. Errors are swallowed by
// design: telemetry must never break a turn.
func (s *Store) Record(rec TurnRecord) {
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.reports.Records++
	s.ensureLoadedLocked()

	s.recs = append(s.recs, rec)
	if len(s.recs) > s.maxRecs {
		// Compaction happens in memory; the next flush persists it.
		s.recs = append([]TurnRecord(nil), s.recs[len(s.recs)-s.maxRecs/2:]...)
	}
	s.dirty++
	if s.dirty > s.reports.DirtyMax {
		s.reports.DirtyMax = s.dirty
	}
	s.reports.Coalesced++

	// Coalesced persistence: rewrite only when the window closes.
	if s.dirty >= flushCount || (!s.lastFl.IsZero() && time.Since(s.lastFl) >= flushInterval) {
		s.flushLocked()
	}
}

// Flush persists any unflushed records. Called at run boundaries and at
// shutdown so a crash loses at most one coalescing window.
func (s *Store) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirty == 0 {
		return
	}
	s.flushLocked()
}

// Stats snapshots the store's I/O telemetry.
func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.reports
	out.InMemory = len(s.recs)
	if !s.lastFl.IsZero() {
		out.LastFlushAt = s.lastFl.UTC().Format(time.RFC3339)
	}
	return out
}

// flushLocked rewrites the file from the in-memory cache. Caller holds mu.
func (s *Store) flushLocked() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	start := time.Now()

	var b []byte
	for _, r := range s.recs {
		line, err := json.Marshal(r)
		if err != nil {
			continue
		}
		b = append(b, line...)
		b = append(b, '\n')
	}

	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}

	s.dirty = 0
	s.lastFl = time.Now()
	s.reports.Flushes++
	s.reports.LastFlushDur = time.Since(start).Microseconds()
}

// ensureLoadedLocked lazily loads the file exactly once per process.
// Caller holds mu.
func (s *Store) ensureLoadedLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.recs = s.readFileLocked()
}

// readFileLocked parses the on-disk JSONL. Best-effort: corrupt lines are
// skipped (self-healing, same as before).
func (s *Store) readFileLocked() []TurnRecord {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	var out []TurnRecord
	for _, line := range splitLines(data) {
		var r TurnRecord
		if json.Unmarshal(line, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// Recent returns the newest n records. Served from the in-memory cache.
func (s *Store) Recent(n int) []TurnRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	if n <= 0 || len(s.recs) <= n {
		return append([]TurnRecord(nil), s.recs...)
	}
	return append([]TurnRecord(nil), s.recs[len(s.recs)-n:]...)
}

// Summary aggregates the store: means over the recorded window.
type Summary struct {
	Turns             int     `json:"turns"`
	AvgPressure       float64 `json:"avgPressure"`
	AvgCompression    float64 `json:"avgCompression"`
	AvgRetrievalMs    float64 `json:"avgRetrievalMs"`
	AvgRelevance      float64 `json:"avgRelevance"`
	ToolSuccessRate   float64 `json:"toolSuccessRate"`
	RetrievedTurnRate float64 `json:"retrievedTurnRate"` // share of turns that injected recall
}

// Summarize aggregates all records (from the in-memory cache).
func (s *Store) Summarize() Summary {
	s.mu.Lock()
	s.ensureLoadedLocked()
	recs := s.recs
	s.mu.Unlock()

	var sum Summary
	sum.Turns = len(recs)
	if sum.Turns == 0 {
		return sum
	}

	var press, comp, retMs, rel, tRate float64
	var retrieved, toolTurns int
	for _, r := range recs {
		press += r.Pressure
		comp += r.CompressionRatio
		retMs += float64(r.RetrievalLatencyMs)
		rel += r.RetrievalRelevance
		if r.RetrievalHits > 0 {
			retrieved++
		}
		if r.ToolCalls > 0 {
			toolTurns++
			tRate += float64(r.ToolSuccesses) / float64(r.ToolCalls)
		}
	}

	n := float64(sum.Turns)
	sum.AvgPressure = press / n
	sum.AvgCompression = comp / n
	sum.AvgRetrievalMs = retMs / n
	sum.AvgRelevance = rel / n
	if toolTurns > 0 {
		sum.ToolSuccessRate = tRate / float64(toolTurns)
	}
	sum.RetrievedTurnRate = float64(retrieved) / n
	return sum
}

func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range data {
		if c == '\n' {
			if i > start {
				out = append(out, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}
