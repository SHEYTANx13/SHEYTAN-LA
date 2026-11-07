package preflight

import (
	"strings"
	"testing"
	"time"
)

// v1.11.1 regression (CodeQL go/useless-assignment-to-field, work order
// §6.3): classify computed the classification reasons but wrote them into
// its VALUE copy of the sample — Poll()'s returned Sample, the monitor's
// stored Last() sample and every callback-delivered Sample carried an
// EMPTY Reasons even when a classification had produced them. The level
// and the reasons now travel together out of classify, and Poll assigns
// both to the real sample.
func TestPollSampleCarriesClassificationReasons(t *testing.T) {
	sampler := Sampler(func() Sample {
		return Sample{
			At:                time.Now(),
			RAMTotalBytes:     10_000,
			RAMAvailableBytes: 100, // 1% — below every threshold below
		}
	})

	m := NewLiveMonitor(sampler, PressureThresholds{
		Warning:  0.25,
		High:     0.15,
		Critical: 0.08,
		Swap:     0.90,
	}, 1)

	s := m.Poll()

	if s.Level != PressureCritical {
		t.Fatalf("1%% available RAM must classify critical, got %s", s.Level)
	}

	if len(s.Reasons) == 0 {
		t.Fatal("Poll()'s sample must carry the classification reasons — the dead value-copy write is back")
	}

	if !strings.Contains(s.Reasons[0], "critical") {
		t.Fatalf("the reason must name the critical threshold, got %q", s.Reasons[0])
	}

	last := m.Last()
	if len(last.Reasons) == 0 {
		t.Fatal("the monitor's stored last sample must carry the same reasons")
	}
	if len(last.Reasons) != len(s.Reasons) {
		t.Fatalf("stored and returned reasons diverge: %v vs %v", last.Reasons, s.Reasons)
	}
}

// The no-pressure path stays honest: a healthy sample carries an EMPTY
// Reasons (nothing was diagnosed), not a stale list.
func TestPollHealthySampleHasNoReasons(t *testing.T) {
	sampler := Sampler(func() Sample {
		return Sample{
			At:                time.Now(),
			RAMTotalBytes:     10_000,
			RAMAvailableBytes: 9_000, // 90% — no pressure
		}
	})

	m := NewLiveMonitor(sampler, PressureThresholds{
		Warning: 0.25, High: 0.15, Critical: 0.08, Swap: 0.90,
	}, 1)

	s := m.Poll()

	if s.Level != PressureOK {
		t.Fatalf("90%% available RAM must classify ok, got %s", s.Level)
	}

	if len(s.Reasons) != 0 {
		t.Fatalf("a healthy sample must carry no reasons, got %v", s.Reasons)
	}
}
