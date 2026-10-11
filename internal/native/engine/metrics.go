package engine

// metrics.go — native engine metrics (v1.1.5 Phase 1).
//
// The MetricsResult mirrors what the C++ engine actually measures today:
// its state, uptime, its own process RSS and the (zero) active request
// count, plus the concern snapshots (scheduler / memory / KV / generation)
// that carry the structure forward. Every number here is either measured
// by the engine or is an honest zero with an explicit "not applicable in
// Phase 1" meaning (no model loaded → no KV cells, no decode speed).
//
// The Go side (Engine.MetricsSnapshot) contributes the facts only Go can
// measure: pid, restarts, uptime since boot.

import (
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// MetricsResult is the JSON shape of the C++ "metrics" op result.
type MetricsResult struct {
	// EngineState is the native engine's own state string.
	EngineState string `json:"engineState"`

	// UptimeSeconds is measured by the engine process itself.
	UptimeSeconds float64 `json:"uptimeSeconds,omitempty"`

	// ProcessRSSBytes is the engine process's resident set size.
	ProcessRSSBytes uint64 `json:"processRssBytes,omitempty"`

	// Scheduler carries the request scheduler snapshot.
	Scheduler SchedulerStats `json:"scheduler"`

	// Memory carries the native memory snapshot.
	Memory MemoryUsage `json:"memory"`

	// KV carries the KV-cache snapshot.
	KV KVCacheStats `json:"kv"`

	// Generation carries the generation (inference) snapshot.
	Generation GenerationStats `json:"generation"`
}

// ApplyMetricsResult folds a native metrics reading into the unified
// llm.Metrics. Only measured values are copied; zero fields stay absent.
func ApplyMetricsResult(m *llm.Metrics, result MetricsResult) {
	if m == nil {
		return
	}

	if result.EngineState != "" {
		m.EngineState = result.EngineState
	}

	if result.UptimeSeconds > 0 {
		m.UptimeSeconds = result.UptimeSeconds
	}

	if result.ProcessRSSBytes > 0 {
		m.ProcessRSSBytes = result.ProcessRSSBytes
	}

	m.ActiveRequests = result.Scheduler.ActiveRequests

	if result.Generation.TTFTSeconds > 0 {
		m.TTFTSeconds = result.Generation.TTFTSeconds
	}

	if result.Generation.PromptTokensPerSecond > 0 {
		m.PromptTokensPerSecond = result.Generation.PromptTokensPerSecond
	}

	if result.Generation.DecodeTokensPerSecond > 0 {
		m.DecodeTokensPerSecond = result.Generation.DecodeTokensPerSecond
	}

	if result.Memory.NativeRSSBytes > 0 && m.ProcessRSSBytes == 0 {
		m.ProcessRSSBytes = result.Memory.NativeRSSBytes
	}
}

// HealthResult is the JSON shape of the C++ "health" op result.
type HealthResult struct {
	Healthy bool   `json:"healthy"`
	State   string `json:"state,omitempty"`
	Detail  string `json:"detail,omitempty"`
}
