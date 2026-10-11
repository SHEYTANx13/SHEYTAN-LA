// pertiming.go — v1.2.5 per-request performance timing.
//
// One RunClock per agent turn records the REAL timestamps of every stage
// on the first-response critical path:
//
//	received → classified → context_start/end → prompt_start/end →
//	serialized → request_sent → first_byte → first_token →
//	generation_end → tool_start/end → verification_start/end → done
//
// and derives the stage durations the diagnostics surface shows:
// classify_ms, context_ms, prompt_ms, serialization_ms, TTFT,
// generation_ms, tool_ms, verification_ms, total_ms.
//
// Every value is MEASURED — marks that never fired report 0, never an
// invented number. The clock is goroutine-safe; the orchestrator marks
// from the run goroutine and the streaming callback marks first-token.
package agent

import (
	"sync"
	"time"
)

// Stage names, stable for the diagnostics contract.
//
// v1.2.6 continuation semantics (the transport ladder is MEASURED by the
// HTTP streaming client itself, never derived from content deltas):
//
//	request_sent      — the HTTP request was handed to the transport
//	response_headers  — the server's response headers arrived
//	first_byte        — the FIRST network byte of the body was READ
//	first_token       — the first generated content/reasoning token
//
// The client emits these as llm.StreamEvent timing marks at the exact
// moment each happens (llm client.go); the orchestrator folds them into
// the clock. A stage that never fired reports 0 — never an invented value.
const (
	StageReceived          = "received"
	StageClassified        = "classified"
	StageContextStart      = "context_start"
	StageContextEnd        = "context_end"
	StagePromptStart       = "prompt_start"
	StagePromptEnd         = "prompt_end"
	StageSerialized        = "serialized"
	StageRequestSent       = "request_sent"
	StageResponseHeaders   = "response_headers"
	StageFirstByte         = "first_byte"
	StageFirstToken        = "first_token"
	StageGenerationEnd     = "generation_end"
	StageToolStart         = "tool_start"
	StageToolEnd           = "tool_end"
	StageVerificationStart = "verification_start"
	StageVerificationEnd   = "verification_end"
	StageDone              = "done"
)

// RunClock is the measured timeline of one agent turn.
type RunClock struct {
	mu     sync.Mutex
	stages map[string]time.Time

	// toolMs accumulates ALL tool execution time of the turn.
	toolMs int64

	// verificationMs accumulates verification reporting time.
	verificationMs int64
}

// NewRunClock starts a clock with the received mark set to now.
func NewRunClock() *RunClock {
	c := &RunClock{stages: map[string]time.Time{}}
	c.stages[StageReceived] = time.Now()
	return c
}

// Mark records one stage timestamp (first write wins — the earliest
// occurrence of a stage is the honest one).
func (c *RunClock) Mark(stage string) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.stages[stage]; !ok {
		c.stages[stage] = time.Now()
	}
}

// SetReceived re-anchors the received mark to the API layer's timestamp
// (earlier than clock construction when the request waited in flight).
func (c *RunClock) SetReceived(t time.Time) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.stages[StageReceived]; !ok || t.Before(existing) {
		c.stages[StageReceived] = t
	}
}

// sinceStages measures between two marked stages (0 when unmarked).
func (c *RunClock) sinceStages(from, to string) int64 {
	if c == nil {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	a, okA := c.stages[from]
	b, okB := c.stages[to]
	if !okA || !okB {
		return 0
	}

	d := b.Sub(a).Milliseconds()
	if d < 0 {
		return 0
	}

	return d
}

// AddToolMs accumulates measured tool execution time.
func (c *RunClock) AddToolMs(ms int64) {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.toolMs += ms
	c.mu.Unlock()
}

// LastStageTime returns the measured timestamp of one stage (ok=false when
// the stage never fired — an honest unknown).
func (c *RunClock) LastStageTime(stage string) (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.stages[stage]
	return t, ok
}

// AddVerificationMs accumulates measured verification time.
func (c *RunClock) AddVerificationMs(ms int64) {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.verificationMs += ms
	c.mu.Unlock()
}

// Timing is the derived, measured duration table for one turn.
type Timing struct {
	ClassifyMs        int64 `json:"classifyMs,omitempty"`
	ContextMs         int64 `json:"contextMs,omitempty"`
	PromptMs          int64 `json:"promptMs,omitempty"`
	SerializationMs   int64 `json:"serializationMs,omitempty"`
	TTFTMs            int64 `json:"ttftMs,omitempty"`      // request_sent → first_token
	HeadersMs         int64 `json:"headersMs,omitempty"`   // request_sent → response_headers
	FirstByteMs       int64 `json:"firstByteMs,omitempty"` // response_headers → first_byte
	GenerationMs      int64 `json:"generationMs,omitempty"`
	ToolMs            int64 `json:"toolMs,omitempty"`
	VerificationMs    int64 `json:"verificationMs,omitempty"`
	TotalMs           int64 `json:"totalMs,omitempty"`
	FirstPromptTokens int   `json:"firstPromptTokens,omitempty"`

	// Stages (v1.2.6 continuation) carries the RAW unix-milli timestamp
	// of every marked stage — duration provenance for diagnostics, so a
	// consumer can verify that no duration was derived, reused or
	// invented. Stages that never fired are absent.
	Stages map[string]int64 `json:"stageTimestamps,omitempty"`
}

// Snapshot computes the derived durations. Unmarked stages report 0.
func (c *RunClock) Snapshot() Timing {
	if c == nil {
		return Timing{}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	at := func(stage string) (time.Time, bool) {
		t, ok := c.stages[stage]
		return t, ok
	}

	ms := func(from, to string) int64 {
		a, okA := at(from)
		b, okB := at(to)
		if !okA || !okB {
			return 0
		}

		d := b.Sub(a).Milliseconds()
		if d < 0 {
			return 0
		}

		return d
	}

	t := Timing{
		ClassifyMs:      ms(StageReceived, StageClassified),
		ContextMs:       ms(StageContextStart, StageContextEnd),
		PromptMs:        ms(StagePromptStart, StagePromptEnd),
		SerializationMs: ms(StagePromptEnd, StageSerialized),
		TTFTMs:          ms(StageRequestSent, StageFirstToken),
		HeadersMs:       ms(StageRequestSent, StageResponseHeaders),
		FirstByteMs:     ms(StageResponseHeaders, StageFirstByte),
		GenerationMs:    ms(StageRequestSent, StageGenerationEnd),
		ToolMs:          c.toolMs,
		VerificationMs:  c.verificationMs,
		Stages:          map[string]int64{},
	}

	// Raw timestamp provenance: every stage that actually fired.
	for stage, ts := range c.stages {
		t.Stages[stage] = ts.UnixMilli()
	}

	if len(t.Stages) == 0 {
		t.Stages = nil
	}

	if done, ok := at(StageDone); ok {
		if recv, okR := at(StageReceived); okR {
			t.TotalMs = done.Sub(recv).Milliseconds()
		}
	}

	return t
}

// SinceReceived reports elapsed ms since the received mark (live status).
func (c *RunClock) SinceReceived() int64 {
	if c == nil {
		return 0
	}

	c.mu.Lock()
	recv, ok := c.stages[StageReceived]
	c.mu.Unlock()

	if !ok {
		return 0
	}

	d := time.Since(recv).Milliseconds()
	if d < 0 {
		return 0
	}

	return d
}
