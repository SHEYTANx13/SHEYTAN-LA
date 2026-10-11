package governor

// cpu_delta_test.go — v1.8.6 focused tests around the CPU platform seam
// (the §9 contract). These run on EVERY platform because the state
// machine (priming → real delta → honest unknowns) is platform-independent
// (cpu_delta.go); the Windows GetSystemTimes reader and the Linux
// /proc/loadavg reader are thin bindings over it / beside it.
//
// THE CONTRACT:
//
//   - the first sample PRIMES (not measurable — never a fabricated 0%);
//   - subsequent samples compute a REAL delta over the elapsed window;
//   - failures remain unknown;
//   - a failed read keeps the baseline valid (cumulative counters — the
//     delta across the gap is still the truth);
//   - a degenerate zero-elapsed window stays unknown;
//   - the seam is safe under concurrent callers (the Governor's poll path
//     is the only producer, but tests prove no race).

import (
        "sync"
        "sync/atomic"
        "testing"
)

func TestCPUDeltaFirstSamplePrimes(t *testing.T) {
        var tr cpuDeltaTracker

        pct, ok := tr.observe(100, 100, true)
        if ok {
                t.Fatalf("the first sample must prime, not measure (got %f)", pct)
        }

        // The SECOND sample measures.
        pct, ok = tr.observe(200, 300, true)
        if !ok {
                t.Fatal("the second sample must measure a real delta")
        }

        // delta: idle +100, busy +200 → busy fraction 2/3.
        if pct < 66.6 || pct > 66.7 {
                t.Fatalf("delta percentage = %f, want ~66.67", pct)
        }
}

func TestCPUDeltaFailureStaysUnknown(t *testing.T) {
        var tr cpuDeltaTracker

        tr.observe(100, 100, true) // prime

        if _, ok := tr.observe(0, 0, false); ok {
                t.Fatal("a failed read must stay unknown")
        }

        // The baseline is KEPT: the next successful read computes the delta
        // across the failure gap (cumulative counters keep it valid).
        pct, ok := tr.observe(300, 700, true)
        if !ok {
                t.Fatal("the post-failure delta must be measurable")
        }

        // delta: idle +200, busy +600 → busy fraction 3/4.
        if pct != 75 {
                t.Fatalf("delta percentage = %f, want 75", pct)
        }
}

func TestCPUDeltaZeroElapsedStaysUnknown(t *testing.T) {
        var tr cpuDeltaTracker

        tr.observe(100, 100, true) // prime

        // Identical counters: zero elapsed time — not measurable.
        if _, ok := tr.observe(100, 100, true); ok {
                t.Fatal("a zero-elapsed window must stay unknown")
        }

        // Counters moving again → measured again.
        if _, ok := tr.observe(110, 190, true); !ok {
                t.Fatal("a moving window must measure")
        }
}

func TestCPUDeltaPercentPure(t *testing.T) {
        if _, ok := cpuDeltaPercent(0, 0, 0, 0); ok {
                t.Fatal("zero total is not measurable")
        }

        if pct, ok := cpuDeltaPercent(10, 10, 20, 40); !ok || pct != 75 {
                t.Fatalf("cpuDeltaPercent = (%f, %t), want (75, true)", pct, ok)
        }

        // Regressive counters (system reboot / counter wrap): unknown, never
        // a negative fabrication.
        if _, ok := cpuDeltaPercent(100, 100, 50, 50); ok {
                t.Fatal("regressive counters must stay unknown")
        }
}

func TestCPUDeltaSeamConcurrency(t *testing.T) {
        var calls atomic.Int64

        seam := newCPUDeltaSeam(func() (float64, float64, bool) {
                c := calls.Add(1)
                // Deterministic monotonic counters.
                return float64(100 + c), float64(100 + 2*c), true
        })

        var wg sync.WaitGroup

        var measured atomic.Int64

        for i := 0; i < 32; i++ {
                wg.Add(1)

                go func() {
                        defer wg.Done()

                        if _, ok := seam.load(); ok {
                                measured.Add(1)
                        }
                }()
        }

        wg.Wait()

        // Exactly one priming call means at most 31 measured — the exact count
        // is scheduling-dependent, but a FABRICATED measurement can never
        // appear before the second call.
        if m := measured.Load(); m > 31 {
                t.Fatalf("measured %d of 32 calls — more than possible after one prime", m)
        }
}
