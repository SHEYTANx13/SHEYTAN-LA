// Package perf implements the SHEYTAN-LA R&D measurement baseline
// framework (authoritative decision: Parsaetak/Contents,
// SHEYTAN-LA-2026-10-11.md, §10 "Measurement Framework").
//
// The framework defines the release metric set, captures what the host
// can HONESTLY measure, and labels everything else NOT MEASURED — never
// invented, never interpolated, never claimed from a target. It owns NO
// runtime behavior: the Go API remains the single orchestration authority
// and this package only observes it through its public contracts.
//
// Contract (evidence discipline inherited from the release process):
//   - every metric carries its measurement METHOD (what was actually
//     observed, at which boundary) and its LIMITATIONS;
//   - hosted-CI numbers are labeled HostedCI=true — they are never
//     physical-hardware benchmarks;
//   - P50/P95 use the nearest-rank definition over the recorded samples;
//   - a target (R&D §4) is reported next to the measured value as a GAP,
//     never as an achievement.
package perf

import (
        "encoding/json"
        "fmt"
        "sort"
        "strings"
        "time"
)

// SchemaVersion identifies the measurement artifact layout.
const SchemaVersion = "sheytan.perf.baseline/1"

// Canonical metric names (R&D §10). A report must define every metric
// below — measured or NOT MEASURED with a reason — so a baseline can
// never silently drop a dimension.
const (
        MetricColdStartInteractive = "cold_start_to_interactive_ms"
        MetricEngineReady          = "engine_ready_ms"
        MetricWarmTTFT             = "warm_ttft_ms"
        MetricStreamToVisible      = "stream_chunk_to_visible_ms"
        MetricStreamDuration       = "stream_duration_ms"
        MetricIdleMemoryMB         = "idle_memory_mb"
        MetricPeakMemoryMB         = "peak_memory_mb"
        MetricToolRoundTrip        = "tool_round_trip_ms"
        MetricFileOutputOverhead   = "file_output_overhead_ms"
)

// MetricStatus is the honest capture status of one metric.
type MetricStatus string

const (
        // StatusMeasured: real samples were recorded on this host.
        StatusMeasured MetricStatus = "measured"
        // StatusNotMeasured: no honest sample exists on this host. The
        // Reason must state exactly why (no model, no physical hardware,
        // boundary not observable in this boot mode, ...).
        StatusNotMeasured MetricStatus = "NOT MEASURED"
)

// Stats summarizes one metric's samples (nearest-rank percentiles).
type Stats struct {
        N    int     `json:"n"`
        Min  float64 `json:"min"`
        P50  float64 `json:"p50"`
        P95  float64 `json:"p95"`
        Max  float64 `json:"max"`
        Mean float64 `json:"mean"`
        Unit string  `json:"unit"`
}

// Summarize computes the nearest-rank P50/P95 over ms samples. An empty
// slice yields the zero Stats (n=0) — callers must mark it NOT MEASURED.
func Summarize(samples []float64) Stats {
        if len(samples) == 0 {
                return Stats{N: 0}
        }

        sorted := append([]float64(nil), samples...)
        sort.Float64s(sorted)

        n := len(sorted)
        sum := 0.0
        for _, v := range sorted {
                sum += v
        }

        return Stats{
                N:    n,
                Min:  sorted[0],
                P50:  sorted[nearestRankIndex(n, 0.50)],
                P95:  sorted[nearestRankIndex(n, 0.95)],
                Max:  sorted[n-1],
                Mean: sum / float64(n),
                Unit: "ms",
        }
}

// nearestRankIndex returns the 0-based index of the nearest-rank
// percentile p for n samples (ceil(p*n) - 1, clamped to [0, n-1]).
func nearestRankIndex(n int, p float64) int {
        if n <= 0 {
                return 0
        }

        rank := int(p*float64(n) + 0.999999) // ceil
        if rank < 1 {
                rank = 1
        }
        if rank > n {
                rank = n
        }

        return rank - 1
}

// MetricResult is one metric's honest capture.
type MetricResult struct {
        Status      MetricStatus `json:"status"`
        Stats       Stats        `json:"stats,omitempty"`
        Method      string       `json:"method"`
        Limitations []string     `json:"limitations,omitempty"`
        Reason      string       `json:"reason,omitempty"`
        // TargetMs is the R&D §4 target for this metric when one exists.
        // Reporting carries the GAP, never a claim of achievement.
        TargetMs   *float64 `json:"targetMs,omitempty"`
        TargetNote string   `json:"targetNote,omitempty"`
        Samples    []float64 `json:"samples,omitempty"`
}

// Envelope is the machine-readable baseline artifact.
type Envelope struct {
        Schema        string                  `json:"schema"`
        CodeSHA       string                  `json:"codeSha"`
        Release       string                  `json:"release"`
        OS            string                  `json:"os"`
        Arch          string                  `json:"arch"`
        CPU           string                  `json:"cpu"`
        GPU           string                  `json:"gpu,omitempty"`
        GPUDriver     string                  `json:"gpuDriver,omitempty"`
        Model         string                  `json:"model,omitempty"`
        Quantization  string                  `json:"quantization,omitempty"`
        ContextTokens string                  `json:"contextTokens,omitempty"`
        Backend       string                  `json:"backend"`
        State         string                  `json:"state"`
        HostedCI      bool                    `json:"hostedCI"`
        StartedAt     time.Time               `json:"startedAt"`
        Duration      time.Duration           `json:"durationMs"`
        Metrics       map[string]MetricResult `json:"metrics"`
        Notes         []string                `json:"notes,omitempty"`
}

// NewEnvelope returns an envelope with every canonical metric defined as
// NOT MEASURED — the caller fills in what it honestly captured. A
// baseline therefore cannot forget a dimension by construction.
func NewEnvelope() *Envelope {
        metrics := map[string]MetricResult{}
        for _, name := range CanonicalMetrics() {
                metrics[name] = MetricResult{
                        Status: StatusNotMeasured,
                        Method: "not captured in this session",
                        Reason: "no sample recorded",
                }
        }

        return &Envelope{
                Schema:    SchemaVersion,
                StartedAt: time.Now().UTC(),
                Metrics:   metrics,
        }
}

// CanonicalMetrics returns the metric names in report order.
func CanonicalMetrics() []string {
        return []string{
                MetricColdStartInteractive,
                MetricEngineReady,
                MetricWarmTTFT,
                MetricStreamToVisible,
                MetricStreamDuration,
                MetricIdleMemoryMB,
                MetricPeakMemoryMB,
                MetricToolRoundTrip,
                MetricFileOutputOverhead,
        }
}

// Record stores measured samples for one metric (unit: ms).
func (e *Envelope) Record(name string, samples []float64, method string, limitations ...string) {
        e.recordUnit(name, samples, "ms", method, limitations...)
}

// RecordMB stores measured samples for one metric (unit: MB — memory).
func (e *Envelope) RecordMB(name string, samples []float64, method string, limitations ...string) {
        e.recordUnit(name, samples, "MB", method, limitations...)
}

func (e *Envelope) recordUnit(name string, samples []float64, unit, method string, limitations ...string) {
        st := Summarize(samples)
        st.Unit = unit
        e.Metrics[name] = MetricResult{
                Status:      StatusMeasured,
                Stats:       st,
                Method:      method,
                Limitations: limitations,
                Samples:     append([]float64(nil), samples...),
        }
}

// RecordWithTarget stores measured samples plus the R&D target (ms).
func (e *Envelope) RecordWithTarget(name string, samples []float64, targetMs float64, method string, limitations ...string) {
        e.Record(name, samples, method, limitations...)
        m := e.Metrics[name]
        m.TargetMs = &targetMs
        m.TargetNote = targetNote(m.Stats.P95, targetMs)
        e.Metrics[name] = m
}

// MarkNotMeasured stamps the reason a metric has no honest sample and
// clears any stale capture (samples, target, limitations) — a metric is
// either measured or NOT MEASURED, never both.
func (e *Envelope) MarkNotMeasured(name, reason string) {
        m := e.Metrics[name]
        m.Status = StatusNotMeasured
        m.Stats = Stats{N: 0}
        m.Reason = reason
        m.Method = "not captured in this session"
        m.Limitations = nil
        m.TargetMs = nil
        m.TargetNote = ""
        m.Samples = nil
        e.Metrics[name] = m
}

func targetNote(p95, target float64) string {
        if p95 <= 0 {
                return "no sample"
        }
        if p95 <= target {
                return fmt.Sprintf("P95 %.1f ms meets the %.1f ms target on THIS host (not a product-wide claim)", p95, target)
        }
        return fmt.Sprintf("P95 %.1f ms exceeds the %.1f ms target by %.1f ms — gap recorded", p95, target, p95-target)
}

// JSON renders the machine-readable artifact.
func (e *Envelope) JSON() ([]byte, error) {
        e.Duration = time.Since(e.StartedAt)
        return json.MarshalIndent(e, "", "  ")
}

// Report renders the concise human report. NOT MEASURED dimensions are
// listed with their reasons — an honest baseline shows its holes.
func (e *Envelope) Report() string {
        e.Duration = time.Since(e.StartedAt)

        var b strings.Builder

        b.WriteString("SHEYTAN-LA performance baseline (R&D 2026-10-11 §10)\n")
        b.WriteString(fmt.Sprintf("  release: %s  code: %s\n", e.Release, shortSHA(e.CodeSHA)))
        b.WriteString(fmt.Sprintf("  host: %s/%s cpu=%q gpu=%q\n", e.OS, e.Arch, e.CPU, e.GPU))
        if e.Model != "" {
                b.WriteString(fmt.Sprintf("  model: %s quant=%s ctx=%s backend=%s\n", e.Model, e.Quantization, e.ContextTokens, e.Backend))
        } else {
                b.WriteString("  model: (none selected) backend=" + e.Backend + "\n")
        }
        b.WriteString(fmt.Sprintf("  state: %s  hostedCI: %t  started: %s\n", e.State, e.HostedCI, e.StartedAt.Format(time.RFC3339)))
        b.WriteString("\n")

        for _, name := range CanonicalMetrics() {
                m := e.Metrics[name]
                switch m.Status {
                case StatusMeasured:
                        b.WriteString(fmt.Sprintf("  %-28s P50 %10.1f ms   P95 %10.1f ms   n=%d\n", name, m.Stats.P50, m.Stats.P95, m.Stats.N))
                        if m.TargetMs != nil {
                                b.WriteString("      target: " + m.TargetNote + "\n")
                        }
                        if m.Method != "" {
                                b.WriteString("      method: " + m.Method + "\n")
                        }
                        for _, l := range m.Limitations {
                                b.WriteString("      limit:  " + l + "\n")
                        }
                default:
                        b.WriteString(fmt.Sprintf("  %-28s NOT MEASURED — %s\n", name, m.Reason))
                }
        }

        return b.String()
}

func shortSHA(sha string) string {
        if len(sha) > 12 {
                return sha[:12]
        }
        if sha == "" {
                return "(unknown)"
        }
        return sha
}
