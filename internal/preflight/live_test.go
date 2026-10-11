// live_test.go — v1.7.1: live pressure monitor — thresholds, hysteresis
// (no oscillation), and the protection path reaching the registered
// callback exactly on the transition INTO critical.
package preflight

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedSampler returns prepared samples in order, then repeats last.
type scriptedSampler struct {
	mu      sync.Mutex
	samples []Sample
	idx     int
}

func (s *scriptedSampler) next() Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx < len(s.samples) {
		out := s.samples[s.idx]
		s.idx++
		return out
	}
	if len(s.samples) == 0 {
		return Sample{At: time.Now()}
	}
	return s.samples[len(s.samples)-1]
}

func ramSample(total, available int64) Sample {
	return Sample{At: time.Now(), RAMTotalBytes: total, RAMAvailableBytes: available}
}

func TestLiveMonitorThresholdsAndHysteresis(t *testing.T) {
	sampler := &scriptedSampler{}
	total := int64(32 << 30)
	// Sequence: ok → ok → warning×1 → ok (flap, never accepted) →
	// warning×2 (accepted) → critical×2 (accepted).
	sampler.samples = []Sample{
		ramSample(total, total),                    // ok
		ramSample(total, total),                    // ok
		ramSample(total, int64(float64(total)*0.2)), // warning (1st)
		ramSample(total, total),                    // flap back
		ramSample(total, int64(float64(total)*0.2)), // warning (1st of 2)
		ramSample(total, int64(float64(total)*0.2)), // warning (2nd → ACCEPT)
		ramSample(total, int64(float64(total)*0.05)), // critical (1st)
		ramSample(total, int64(float64(total)*0.05)), // critical (2nd → ACCEPT)
	}

	monitor := NewLiveMonitor(sampler.next, DefaultPressureThresholds(), 2)

	// ok, ok
	if got := monitor.Poll().Level; got != PressureOK {
		t.Fatalf("sample 1: %s", got)
	}
	if got := monitor.Poll().Level; got != PressureOK {
		t.Fatalf("sample 2: %s", got)
	}
	// One warning sample: pending, NOT accepted.
	if got := monitor.Poll().Level; got != PressureWarning {
		t.Fatalf("sample 3 raw: %s", got)
	}
	if monitor.Level() != PressureOK {
		t.Fatalf("single warning must not flip level (hysteresis): %s", monitor.Level())
	}
	// Flap back to ok: resets the streak.
	monitor.Poll()
	if monitor.Level() != PressureOK {
		t.Fatalf("flap must keep level ok: %s", monitor.Level())
	}
	// warning ×2 → accepted on the second.
	monitor.Poll()
	if monitor.Level() != PressureOK {
		t.Fatalf("one agreeing warning again must still hold: %s", monitor.Level())
	}
	if got := monitor.Poll().Level; got != PressureWarning {
		t.Fatalf("second agreeing warning must accept: %s", got)
	}
	if monitor.Level() != PressureWarning {
		t.Fatalf("accepted level: %s", monitor.Level())
	}
	// critical ×2 → accepted.
	monitor.Poll()
	if monitor.Level() != PressureWarning {
		t.Fatalf("one critical must hold warning: %s", monitor.Level())
	}
	if got := monitor.Poll().Level; got != PressureCritical {
		t.Fatalf("second critical must accept: %s", got)
	}
}

func TestLiveMonitorCriticalReachesProtectionPath(t *testing.T) {
	sampler := &scriptedSampler{}
	total := int64(16 << 30)
	sampler.samples = []Sample{
		ramSample(total, total),
		ramSample(total, int64(float64(total)*0.03)),
		ramSample(total, int64(float64(total)*0.03)),
	}
	monitor := NewLiveMonitor(sampler.next, DefaultPressureThresholds(), 2)

	var fired atomic.Int32
	var gotLevel atomic.Value
	gotLevel.Store(PressureOK)
	monitor.OnCritical(func(s Sample) {
		fired.Add(1)
		gotLevel.Store(s.Level)
	})

	monitor.Poll() // ok
	monitor.Poll() // critical #1 — pending
	if fired.Load() != 0 {
		t.Fatal("protection must not fire before hysteresis accepts")
	}
	monitor.Poll() // critical #2 — accepted → PROTECTION
	if fired.Load() != 1 {
		t.Fatalf("protection fired %d times, want exactly 1 (transition only)", fired.Load())
	}
	if lvl := gotLevel.Load().(PressureLevel); lvl != PressureCritical {
		t.Fatalf("protection level: %s", lvl)
	}

	// Staying critical does NOT re-fire (transition-only).
	monitor.Poll()
	if fired.Load() != 1 {
		t.Fatal("critical must not re-fire while already critical")
	}
}

func TestLiveMonitorUnknownFactsStayCalm(t *testing.T) {
	// A sampler that can measure nothing must never escalate.
	monitor := NewLiveMonitor(func() Sample { return Sample{At: time.Now()} },
		DefaultPressureThresholds(), 1)
	for i := 0; i < 5; i++ {
		if got := monitor.Poll().Level; got != PressureOK {
			t.Fatalf("unmeasurable facts must stay ok, got %s", got)
		}
	}
}

func TestLiveMonitorStop(t *testing.T) {
	sampler := &scriptedSampler{}
	total := int64(8 << 30)
	sampler.samples = []Sample{ramSample(total, int64(float64(total)*0.01))}
	monitor := NewLiveMonitor(sampler.next, DefaultPressureThresholds(), 1)
	monitor.Stop()
	if got := monitor.Poll().Level; got != PressureOK {
		t.Fatalf("stopped monitor must not accept new levels, got %s", got)
	}
}
