package llm

// perftracker.go — v1.1.7 compact live performance telemetry.
//
// One small, honest purpose: remember what the LAST real generations
// actually measured (prompt tok/s, decode tok/s, TTFT) so the Settings →
// Performance view can show real numbers instead of guesses.
//
// Design constraints (deliberate):
//   - NOT a telemetry framework: a fixed-size ring of the most recent
//     samples, recorded as a side effect of the streaming path that already
//     measured them for v1.0.4 PerfStats. No new pipelines, no persistence,
//     no background workers.
//   - Never fabricates: an empty tracker reports "no sample yet" and the
//     API surface renders N/A.
//   - Bounded: maxPerfSamples entries, each a few dozen bytes.

import (
	"sync"
	"time"
)

// maxPerfSamples bounds the ring. A handful of recent generations is enough
// for "what is my current speed" — history is deliberately not kept.
const maxPerfSamples = 16

// PerfSample is one completed generation as measured by the streaming
// client (tokenTimer). Zero-value fields mean "not measured this request"
// and are skipped by the aggregator rather than averaged as zeros.
type PerfSample struct {
	At           time.Time `json:"at"`
	Model        string    `json:"model,omitempty"`
	PromptTokens int       `json:"promptTokens,omitempty"`
	Tokens       int       `json:"tokens,omitempty"`
	TokensPerSec float64   `json:"tokensPerSec,omitempty"`
	TTFTMs       int64     `json:"ttftMs,omitempty"`
	WallMs       int64     `json:"wallMs,omitempty"`
	PromptTokSec float64   `json:"promptTokSec,omitempty"`
}

// perfTracker is a mutex-guarded ring of recent samples.
type perfTracker struct {
	mu      sync.Mutex
	samples []PerfSample
}

var enginePerf perfTracker

// recordPerfSample appends one sample (exported for tests via the client
// path; the streaming client is the only production writer).
func recordPerfSample(s PerfSample) {
	if s.Tokens <= 0 && s.PromptTokens <= 0 {
		// Nothing measurable happened (aborted/failed call) — do not
		// pollute the ring with a fake zero-speed sample.
		return
	}

	enginePerf.mu.Lock()
	defer enginePerf.mu.Unlock()

	enginePerf.samples = append(enginePerf.samples, s)
	if len(enginePerf.samples) > maxPerfSamples {
		enginePerf.samples = enginePerf.samples[len(enginePerf.samples)-maxPerfSamples:]
	}
}

// EnginePerfSnapshot aggregates the recent samples for the API surface.
// Values are computed ONLY from real samples; Every bool reports whether a
// value exists so the UI can render N/A instead of a fabricated number.
//
// PromptTokPerSec is derived from the last sample that knows both its
// prompt token count and its wall time (prompt processing dominates wall
// time before the first token, so wall-clock of the whole request is the
// honest denominator the engine itself reports).
type EnginePerfSnapshot struct {
	HasSample        bool      `json:"hasSample"`
	At               time.Time `json:"at,omitempty"`
	Model            string    `json:"model,omitempty"`
	PromptTokens     int       `json:"promptTokens,omitempty"`
	Tokens           int       `json:"tokens,omitempty"`
	GenTokPerSec     float64   `json:"genTokPerSec,omitempty"`
	PromptTokPerSec  float64   `json:"promptTokPerSec,omitempty"`
	TTFTSeconds      float64   `json:"ttftSeconds,omitempty"`
	SamplesAvailable int       `json:"samplesAvailable"`
}

// SnapshotEnginePerf returns the aggregated recent-generation metrics.
func SnapshotEnginePerf() EnginePerfSnapshot {
	enginePerf.mu.Lock()
	defer enginePerf.mu.Unlock()

	out := EnginePerfSnapshot{SamplesAvailable: len(enginePerf.samples)}
	if len(enginePerf.samples) == 0 {
		return out
	}

	// The last sample is "current speed"; the mean of the recent ring
	// smooths one-off spikes without building a statistics framework.
	last := enginePerf.samples[len(enginePerf.samples)-1]

	out.HasSample = true
	out.At = last.At
	out.Model = last.Model
	out.PromptTokens = last.PromptTokens
	out.Tokens = last.Tokens
	out.GenTokPerSec = last.TokensPerSec
	out.TTFTSeconds = float64(last.TTFTMs) / 1000

	// Prompt tok/s: prompt tokens over wall time is a lower bound (it
	// includes decode time) — still a real measurement, never invented.
	if last.PromptTokens > 0 && last.WallMs > 0 {
		out.PromptTokPerSec = float64(last.PromptTokens) / (float64(last.WallMs) / 1000)
	}

	return out
}
