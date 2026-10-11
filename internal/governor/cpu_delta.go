// cpu_delta.go — v1.8.6: the platform-independent CPU delta state machine
// shared by the Governor's CPU load seam.
//
// GetSystemTimes (Windows) and /proc/stat (Linux) both expose CUMULATIVE
// counters — a utilization percentage only exists as a DELTA between two
// reads. This file owns that priming/delta discipline ONCE so the
// platform seams stay thin:
//
//   - the FIRST observation only primes the baseline (not measurable yet);
//   - each subsequent observation computes the real busy fraction of
//     the elapsed delta;
//   - a failed read stays UNKNOWN (ok=false) while the baseline is kept
//     (cumulative counters make the next delta across the gap still
//     valid — no fabricated instantaneous value is ever produced);
//   - a degenerate zero elapsed total stays unknown.
//
// Pure state machine: deterministic, unit-testable on every platform
// (cpu_delta_test.go — the "focused tests around the platform seam").
package governor

import "sync"

// cpuDeltaPercent computes the busy percentage of one cumulative-counter
// delta. Pure function — total <= 0 (no elapsed time) is not measurable.
func cpuDeltaPercent(prevIdle, prevBusy, idle, busy float64) (float64, bool) {
	dIdle := idle - prevIdle
	dBusy := busy - prevBusy

	total := dIdle + dBusy
	if total <= 0 {
		return 0, false
	}

	return 100 * dBusy / total, true
}

// cpuDeltaTracker is the priming/delta state machine over one platform's
// cumulative CPU counters. Not safe for concurrent use by itself — the
// platform seam wraps it with its own mutex (one sampler, one cadence).
type cpuDeltaTracker struct {
	primed   bool
	lastIdle float64
	lastBusy float64
}

// observe folds ONE platform read: the first primes, later reads produce
// the measured delta, failed reads stay unknown with the baseline kept.
func (t *cpuDeltaTracker) observe(idle, busy float64, ok bool) (float64, bool) {
	if !ok {
		return 0, false
	}

	if !t.primed {
		t.lastIdle, t.lastBusy, t.primed = idle, busy, true
		return 0, false
	}

	pct, measured := cpuDeltaPercent(t.lastIdle, t.lastBusy, idle, busy)

	if measured {
		t.lastIdle, t.lastBusy = idle, busy
	}

	return pct, measured
}

// reset drops the baseline (tests/diagnostics only).
func (t *cpuDeltaTracker) reset() {
	t.primed = false
	t.lastIdle, t.lastBusy = 0, 0
}

// cpuDeltaSeam binds the tracker to one platform reader with mutex
// protection — the complete platform-side plumbing behind
// CPULoadPlatform implementations that expose cumulative counters.
type cpuDeltaSeam struct {
	mu      sync.Mutex
	tracker cpuDeltaTracker
	read    func() (idle, busy float64, ok bool)
}

// newCPUDeltaSeam builds the seam over a platform reader.
func newCPUDeltaSeam(read func() (idle, busy float64, ok bool)) *cpuDeltaSeam {
	return &cpuDeltaSeam{read: read}
}

// load returns the current CPU busy percentage (0..100) or not-measurable.
func (s *cpuDeltaSeam) load() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idle, busy, ok := s.read()
	return s.tracker.observe(idle, busy, ok)
}
