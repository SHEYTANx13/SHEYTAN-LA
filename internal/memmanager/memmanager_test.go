package memmanager

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestRunTrackingDefersCleanup(t *testing.T) {
	m := New()
	m.now = func() time.Time { return time.Now() }

	var trimmed bool
	m.RegisterTrim("probe", func() int64 {
		trimmed = true
		return 0
	})

	m.TrackRunStart()
	// While a run is active, MaybeCleanup must NOT run trims.
	if _, ran := m.MaybeCleanup("test", 0); ran {
		t.Fatal("cleanup ran while a run was active")
	}
	if trimmed {
		t.Fatal("trim ran while a run was active")
	}

	// TrackRunEnd (success OR abort path) releases the claim and cleans.
	m.TrackRunEnd()
	if !trimmed {
		t.Fatal("trim did not run after run end")
	}
	if m.ActiveRuns() != 0 {
		t.Fatalf("active runs = %d, want 0", m.ActiveRuns())
	}
}

func TestCleanupMeasuresFreedAndDuration(t *testing.T) {
	m := New()
	blob := make([]byte, 1<<20) // give GC something real to collect
	_ = blob

	m.RegisterTrim("frees-1k", func() int64 { return 1024 })
	m.RegisterTrim("frees-2k", func() int64 { return 2048 })

	stats := m.Cleanup("test")
	if stats.LastFreedBytes != 3072 {
		t.Fatalf("freed = %d, want 3072", stats.LastFreedBytes)
	}
	if stats.FreedBytesTotal != 3072 {
		t.Fatalf("freed total = %d", stats.FreedBytesTotal)
	}
	if stats.LastCleanupReason != "test" {
		t.Fatalf("reason = %q", stats.LastCleanupReason)
	}
	if stats.HeapAfterLast == 0 || stats.HeapBeforeLast == 0 {
		t.Fatal("heap before/after not recorded")
	}
	if len(stats.PerTrimFreedLast) != 2 {
		t.Fatalf("per-trim records = %d, want 2", len(stats.PerTrimFreedLast))
	}
	if stats.CleanupsRun != 1 {
		t.Fatalf("cleanups = %d", stats.CleanupsRun)
	}
}

func TestTrimRegistrationIsIdempotent(t *testing.T) {
	m := New()
	calls := 0
	m.RegisterTrim("x", func() int64 { calls += 1; return 0 })
	m.RegisterTrim("x", func() int64 { calls += 10; return 0 })
	m.Cleanup("t")
	if calls != 10 {
		t.Fatalf("calls = %d, want 10 (re-register replaces)", calls)
	}
	stats := m.Stats()
	if len(stats.RegisteredTrims) != 1 {
		t.Fatalf("registered = %v", stats.RegisteredTrims)
	}
}

func TestConcurrentCleanupAndRunTracking(t *testing.T) {
	m := New()
	for i := 0; i < 4; i++ {
		m.RegisterTrim("t", func() int64 { return 0 })
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				m.TrackRunStart()
				m.MaybeCleanup("race", 0)
				m.TrackRunEnd()
				_ = m.Stats()
			}
		}()
	}
	wg.Wait()
	if m.ActiveRuns() != 0 {
		t.Fatalf("active runs leaked: %d", m.ActiveRuns())
	}
}

// TestRepeatedRunMemoryStaysBounded is the "no memory growth across
// repeated runs" verification: 200 simulated run cycles that allocate
// one-shot buffers, mutate a growing cache, and end with TrackRunEnd.
// The post-run cleanup must keep steady-state heap from ratcheting upward.
func TestRepeatedRunMemoryStaysBounded(t *testing.T) {
	m := WithHighWatermark(8 << 20)

	cache := make(map[int][]byte) // simulates an unbounded per-run cache the trim keeps in check
	m.RegisterTrim("sim-cache", func() int64 {
		freed := 0
		for k := range cache {
			freed += len(cache[k])
			delete(cache, k)
		}
		return int64(freed)
	})

	runtime.GC()
	var before uint64
	for cycle := 0; cycle < 200; cycle++ {
		m.TrackRunStart()
		// Per-run one-shot allocations (streaming buffers, tool results).
		oneShot := make([]byte, 64<<10)
		_ = oneShot
		// Leak into the "cache" (bounded only by the registered trim).
		cache[cycle] = make([]byte, 32<<10)
		m.TrackRunEnd()

		if cycle == 50 {
			runtime.GC()
			before = heapInUse()
		}
	}
	runtime.GC()
	after := heapInUse()

	// Without the post-run trim the cache alone would retain 200×32 KiB =
	// 6.4 MB; growth from cycle 50 → 200 must stay far below that.
	growth := int64(after) - int64(before)
	if growth > int64(2<<20) {
		t.Fatalf("heap grew %d bytes between cycle 50 and 200 — reclamation not working", growth)
	}
	if m.Stats().CleanupsRun == 0 {
		t.Fatal("no cleanups recorded")
	}
}
