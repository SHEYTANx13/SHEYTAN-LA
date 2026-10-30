package llm

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
)

// withEnumerationSeam swaps the swappable PROBE authority (exec + parse
// only — the cache wrapper stays REAL) and restores it on cleanup. The
// fake counts real probes so the tests assert on PROBES, not on cached
// replays.
func withEnumerationSeam(t *testing.T, fake func(binPath string) ([]accelerator.Device, bool, error)) (probes *int64) {
	t.Helper()
	var n int64
	orig := probeEngineDevices
	probeEngineDevices = func(binPath string) ([]accelerator.Device, bool, error) {
		atomic.AddInt64(&n, 1)
		return fake(binPath)
	}
	t.Cleanup(func() { probeEngineDevices = orig })
	return &n
}

func resetDeviceCache(t *testing.T) {
	t.Helper()
	deviceMu.Lock()
	deviceCache = map[string]deviceCacheEntry{}
	deviceMu.Unlock()
	t.Cleanup(func() {
		deviceMu.Lock()
		deviceCache = map[string]deviceCacheEntry{}
		deviceMu.Unlock()
	})
}

// TestFailedDeviceProbeReprobesAfterBoundedFailureWindow is the restored
// v1.11.0 regression pin: a failed capability probe is cached only for
// the bounded failure window — never for the full 24h success TTL (the
// e487dd0 simplification let a transient loader/driver failure poison
// the cache for a day; this test fails if that regression returns).
func TestFailedDeviceProbeReprobesAfterBoundedFailureWindow(t *testing.T) {
	resetDeviceCache(t)
	prevFailureTTL := deviceFailureTTL
	deviceFailureTTL = 5 * time.Minute
	t.Cleanup(func() { deviceFailureTTL = prevFailureTTL })

	// Deterministic clock: advance past the failure window without any
	// sleep (no sleep-based correctness).
	prevNow := timeNow
	now := timeNow()
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = prevNow })

	calls := 0
	withEnumerationSeam(t, func(binPath string) ([]accelerator.Device, bool, error) {
		calls++
		return nil, false, errors.New("transient loader failure")
	})

	const bin = "/fake/llama-server-failure"
	d1, s1, err1 := EnumerateEngineDevices(bin)
	if err1 == nil || s1 {
		t.Fatalf("first probe must surface the failure: %+v %+v %+v", d1, s1, err1)
	}
	// The failed probe IS cached (bounded) — an immediate second call
	// must not re-probe.
	_, _, _ = EnumerateEngineDevices(bin)
	if calls != 1 {
		t.Fatalf("failed probe re-probed immediately (calls=%d) — the bounded cache is gone", calls)
	}
	// Advance past the failure window: the probe runs again — a failed
	// probe never poisons the cache for the full success TTL.
	now = now.Add(6 * time.Minute)
	_, _, _ = EnumerateEngineDevices(bin)
	if calls != 2 {
		t.Fatalf("failed probe served beyond the failure window (calls=%d)", calls)
	}
}

func TestSuccessfulDeviceProbeServesFullTTL(t *testing.T) {
	resetDeviceCache(t)
	prevNow := timeNow
	now := timeNow()
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = prevNow })
	calls := 0
	withEnumerationSeam(t, func(binPath string) ([]accelerator.Device, bool, error) {
		calls++
		return []accelerator.Device{{Name: "Vulkan0", TotalMB: 24576}}, true, nil
	})
	const bin = "/fake/llama-server-success"
	if _, supported, err := EnumerateEngineDevices(bin); err != nil || !supported {
		t.Fatalf("first probe: %+v %+v", supported, err)
	}
	for i := 0; i < 3; i++ {
		d, supported, err := EnumerateEngineDevices(bin)
		if err != nil || !supported || len(d) != 1 {
			t.Fatalf("cached probe drifted: %+v %+v %+v", d, supported, err)
		}
	}
	if calls != 1 {
		t.Fatalf("successful probe re-probed within TTL (calls=%d)", calls)
	}
}

func TestDeviceCacheKeyEmbedsBinaryIdentity(t *testing.T) {
	k := deviceCacheKey("/some/bin")
	if !strings.Contains(k, "/some/bin") {
		t.Fatalf("key must embed the path: %q", k)
	}
	// The TTL contract depends on the key changing when the binary
	// changes: two distinct paths never collide.
	if deviceCacheKey("/some/other/bin") == k {
		t.Fatal("distinct binaries produced the same cache key")
	}
}

func TestDeviceFailureTTLIsBoundedAndBelowSuccessTTL(t *testing.T) {
	if deviceFailureTTL <= 0 || deviceFailureTTL >= deviceCacheTTL {
		t.Fatalf("failure TTL %v must be positive and strictly below the success TTL %v", deviceFailureTTL, deviceCacheTTL)
	}
	if deviceCacheTTL != 24*time.Hour {
		t.Fatalf("success TTL drifted: %v", deviceCacheTTL)
	}
}

// TestExecFailureIsCachedAsFailureNotSuccess pins the error-folding fix:
// a real exec failure (binary absent) must be stored as a FAILED probe —
// the replayed cached verdict keeps the error instead of degrading into
// a success-shaped (supported, nil err) answer for the next window.
func TestExecFailureIsCachedAsFailureNotSuccess(t *testing.T) {
	resetDeviceCache(t)
	const bin = "/nonexistent/llama-server-fold-test"
	_, supported, err := EnumerateEngineDevices(bin)
	if err == nil || supported {
		t.Fatalf("exec failure must surface as an error verdict: supported=%v err=%v", supported, err)
	}
	if !strings.Contains(err.Error(), "engine device enumeration") {
		t.Fatalf("exec failure must keep the folded verdict: %v", err)
	}
	// Second call replays the CACHED failure (same verdict), never a
	// success-shaped drift.
	_, s2, err2 := EnumerateEngineDevices(bin)
	if err2 == nil || s2 || !strings.Contains(err2.Error(), "engine device enumeration") {
		t.Fatalf("cached exec failure drifted: supported=%v err=%v", s2, err2)
	}
}
