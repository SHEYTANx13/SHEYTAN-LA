// live.go — v1.7.1 live resource monitoring + the protection path
// (§4.6/§4.7).
//
// The monitor observes MEASURABLE system pressure with bounded polling
// and hysteresis while runs are active. It fabricates nothing: the
// sampler is injected (the runtime wires the existing sysinfo / process
// RSS authorities), and a fact that cannot be measured stays out of the
// verdict.
//
// Hysteresis: a level change requires N consecutive samples agreeing, so
// tiny measurement fluctuations never oscillate warning↔clear states.
//
// Protection: when the level becomes critical, the monitor invokes the
// registered protection callbacks — which use the EXISTING cancellation
// ownership (run contexts, the scheduler's CancelRun) to reach
//
//	RUNNING → WARNING → PROTECTION → SAFE CANCEL/PAUSE → persist state
//
// The monitor never kills processes and never touches engine binaries.
package preflight

import (
	"sync"
	"time"
)

// PressureLevel is the live system-pressure class (§4.5 vocabulary).
type PressureLevel string

const (
	// PressureOK: no measurable pressure.
	PressureOK PressureLevel = "ok"
	// PressureWarning: elevated — surface, no action.
	PressureWarning PressureLevel = "warning"
	// PressureHigh: high pressure — prepare protection.
	PressureHigh PressureLevel = "high_pressure"
	// PressureCritical: critical pressure — PROTECTION NOW.
	PressureCritical PressureLevel = "critical_pressure"
)

// Sample is one measurable pressure observation.
type Sample struct {
	At time.Time `json:"at"`

	// RAM facts (bytes; 0 = not measurable on this platform).
	RAMTotalBytes     int64 `json:"ramTotalBytes,omitempty"`
	RAMAvailableBytes int64 `json:"ramAvailableBytes,omitempty"`

	// ProcRSSBytes is THIS process's resident set (0 = unmeasurable).
	ProcRSSBytes int64 `json:"procRssBytes,omitempty"`

	// SwapPressure is the fraction of swap in use when measurable
	// (0..1; a negative value means "unknown").
	SwapPressure float64 `json:"swapPressure,omitempty"`

	Level   PressureLevel `json:"level"`
	Reasons []string      `json:"reasons,omitempty"`
}

// PressureThresholds define the level boundaries as fractions of TOTAL
// RAM still available. They are conservative defaults, overridable.
type PressureThresholds struct {
	// Warning: available RAM below 25% of total.
	Warning float64
	// High: available RAM below 15% of total.
	High float64
	// Critical: available RAM below 8% of total.
	Critical float64
	// Swap: swap usage fraction that signals pressure (when measurable).
	Swap float64
}

// DefaultPressureThresholds are the v1.7.1 defaults.
func DefaultPressureThresholds() PressureThresholds {
	return PressureThresholds{Warning: 0.25, High: 0.15, Critical: 0.08, Swap: 0.80}
}

// Sampler reads one measurable pressure snapshot. Implementations use
// the EXISTING system authorities; they must not block for long.
type Sampler func() Sample

// LiveMonitor is the bounded, hysteresis-protected pressure monitor.
type LiveMonitor struct {
	mu sync.Mutex

	sampler    Sampler
	thresholds PressureThresholds
	hysteresis int // consecutive samples required to change level

	level       PressureLevel
	streak      int            // consecutive agreeing samples
	pending     PressureLevel  // level the streak is voting for
	last        Sample
	onCritical  []func(Sample)
	onAnyChange []func(Sample)

	startedAt time.Time
	stopped   bool
}

// NewLiveMonitor builds a monitor. hysteresis below 1 becomes 1.
func NewLiveMonitor(sampler Sampler, thresholds PressureThresholds, hysteresis int) *LiveMonitor {
	if sampler == nil {
		sampler = func() Sample { return Sample{Level: PressureOK} }
	}
	if hysteresis < 1 {
		hysteresis = 1
	}
	return &LiveMonitor{
		sampler:    sampler,
		thresholds: thresholds,
		hysteresis: hysteresis,
		level:      PressureOK,
	}
}

// OnCritical registers a protection callback fired when the level
// BECOMES critical (transition into critical, not every sample).
func (m *LiveMonitor) OnCritical(fn func(Sample)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCritical = append(m.onCritical, fn)
}

// OnChange registers a callback for every accepted level transition.
func (m *LiveMonitor) OnChange(fn func(Sample)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onAnyChange = append(m.onAnyChange, fn)
}

// Stop marks the monitor stopped: Poll becomes a no-op.
func (m *LiveMonitor) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
}

// Level returns the accepted current level.
func (m *LiveMonitor) Level() PressureLevel {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.level
}

// Last returns the most recent accepted sample (zero-value before the
// first poll).
func (m *LiveMonitor) Last() Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

// Poll takes ONE sample, applies thresholds + hysteresis, and fires the
// callbacks when the accepted level changes. The caller owns the polling
// cadence (bounded — the runtime polls every 15 s during active runs).
func (m *LiveMonitor) Poll() Sample {
	s := m.sampler()
	s.Level = m.classify(s)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		// A stopped monitor reports its frozen accepted level, never a
		// fresh classification.
		s.Level = m.level
		return s
	}

	if s.Level == m.level {
		m.streak = 0
		m.pending = ""
	} else if s.Level == m.pending {
		m.streak++
	} else {
		m.pending = s.Level
		m.streak = 1
	}

	if m.streak >= m.hysteresis {
		prev := m.level
		m.level = m.pending
		m.pending = ""
		m.streak = 0
		if m.level != prev {
			m.last = s
			if m.level == PressureCritical {
				// Protection runs SYNCHRONOUSLY: reaching the cancel
				// ownership is the whole point of the critical level and
				// the callbacks are bounded cooperative cancels.
				for _, fn := range m.onCritical {
					fn(s)
				}
			}
			for _, fn := range m.onAnyChange {
				go fn(s)
			}
			return s
		}
	}

	if m.last.At.IsZero() || s.Level == m.level {
		m.last = s
	}
	return s
}

// classify maps one sample's measured facts to a level. Unmeasurable
// facts never escalate the level (unknown stays unknown).
func (m *LiveMonitor) classify(s Sample) PressureLevel {
	level := PressureOK
	var reasons []string

	if s.RAMTotalBytes > 0 && s.RAMAvailableBytes > 0 {
		frac := float64(s.RAMAvailableBytes) / float64(s.RAMTotalBytes)
		switch {
		case frac < m.thresholds.Critical:
			level = PressureCritical
			reasons = append(reasons, "available RAM below the critical threshold")
		case frac < m.thresholds.High:
			level = PressureHigh
			reasons = append(reasons, "available RAM below the high-pressure threshold")
		case frac < m.thresholds.Warning:
			level = PressureWarning
			reasons = append(reasons, "available RAM below the warning threshold")
		}
	}

	if m.thresholds.Swap > 0 && s.SwapPressure >= m.thresholds.Swap {
		if level == PressureOK {
			level = PressureHigh
		}
		reasons = append(reasons, "swap pressure is high")
	}

	s.Reasons = reasons
	return level
}
