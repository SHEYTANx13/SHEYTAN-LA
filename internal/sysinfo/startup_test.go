package sysinfo

// startup_test.go — v1.2.6 continuation regression tests for the
// STARTUP-NEVER-BLOCKS-ON-THE-DEEP-PROBE contract and the MEASURED
// Windows identity mapping.
//
// The defect under test: api.Server.New() called sysinfo.Probe() (the DEEP
// probe — ONE batched PowerShell/CIM invocation on Windows, seconds)
// synchronously, so the whole server construction — listener binding
// included — waited for hardware probing. The fix serves ProbeFast() and
// warms the deep probe in the background (single-flight).

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// slowDeepProbe is a deep-probe stub that takes measurable time — the
// startup test proves interactive paths never wait for it.
var slowDeepProbeStarted atomic.Bool
var slowDeepProbeDone atomic.Bool

func slowDeepProbe() *SysInfo {
	slowDeepProbeStarted.Store(true)

	info := fastSnapshot()
	info.CPU.Name = "Slow Probe CPU"

	time.Sleep(600 * time.Millisecond)

	slowDeepProbeDone.Store(true)
	return info
}

// TestProbeFastNeverInvokesDeepProbe pins the non-blocking contract: with
// a slow deep probe armed behind the single-flight, ProbeFast returns
// immediately with fast facts and NEVER waits for the deep result.
func TestProbeFastNeverInvokesDeepProbe(t *testing.T) {
	original := deepProbeFn

	t.Cleanup(func() {
		deepProbeFn = original
		resetDeepProbeForTest()
	})

	deepProbeFn = slowDeepProbe
	slowDeepProbeStarted.Store(false)
	slowDeepProbeDone.Store(false)

	resetDeepProbeForTest()

	// Start the deep probe in the background (what hardware.WarmDeep
	// does). The goroutine is JOINED before the test returns — a leaked
	// probe goroutine would race the next test's resetDeepProbeForTest.
	deepDone := make(chan struct{})

	go func() {
		defer close(deepDone)
		_ = Probe()
	}()

	// Give the deep probe a moment to be INSIDE its sleep.
	deadline := time.Now().Add(2 * time.Second)

	for !slowDeepProbeStarted.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if !slowDeepProbeStarted.Load() {
		t.Fatal("deep probe never started — test rig broken")
	}

	// ProbeFast must return while the deep probe is STILL RUNNING.
	started := time.Now()

	fast := ProbeFast()

	if elapsed := time.Since(started); elapsed > 400*time.Millisecond {
		t.Fatalf("ProbeFast blocked %v — the interactive path waited for the deep probe", elapsed)
	}

	if fast == nil {
		t.Fatal("ProbeFast returned nil")
	}

	// Fast facts are present (OS/arch/cores measured in-process).
	if fast.OS == "" || fast.CPU.LogicalCores <= 0 {
		t.Fatalf("fast snapshot incomplete: %+v", fast)
	}

	if slowDeepProbeDone.Load() {
		t.Fatal("deep probe already finished — the delay window was not exercised")
	}

	// DeepReady is honest: false while the deep probe is still running.
	if DeepReady() {
		t.Fatal("DeepReady = true while the deep probe is still running — the wire flag lies")
	}

	// JOIN the background probe before returning (no leaked goroutines —
	// the next test's reset would race it).
	select {
	case <-deepDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the background deep probe never finished — test rig broken")
	}
}

// TestDeepProbeMergesIntoProbeFast pins the merge contract: once the deep
// probe lands, ProbeFast serves its measured facts (single-flight).
func TestDeepProbeMergesIntoProbeFast(t *testing.T) {
	original := deepProbeFn

	t.Cleanup(func() {
		deepProbeFn = original
		resetDeepProbeForTest()
	})

	deepProbeFn = func() *SysInfo {
		info := fastSnapshot()
		info.CPU.Name = "Deep Measured CPU"
		info.DeepProbedAt = time.Now().UTC()
		// Publish exactly like probeUncached does.
		storeDeep(info)
		return info
	}

	resetDeepProbeForTest()

	// Synchronous Probe (what non-interactive owners call).
	deep := Probe()

	if deep.CPU.Name != "Deep Measured CPU" {
		t.Fatalf("deep probe name = %q", deep.CPU.Name)
	}

	if !DeepReady() {
		t.Fatal("DeepReady = false after the deep probe completed")
	}

	merged := ProbeFast()

	if merged.CPU.Name != "Deep Measured CPU" {
		t.Fatalf("ProbeFast did not merge the deep facts: %q", merged.CPU.Name)
	}

	if !merged.DeepProbedAt.IsZero() {
		// fast snapshot has no DeepProbedAt; merged must carry the deep one.
		if merged.DeepProbedAt.IsZero() {
			t.Fatal("merged snapshot lost DeepProbedAt")
		}
	}
}

// TestProbeSingleFlight pins the single-flight contract: concurrent Probe
// callers run the deep probe EXACTLY once.
func TestProbeSingleFlight(t *testing.T) {
	original := deepProbeFn

	t.Cleanup(func() {
		deepProbeFn = original
		resetDeepProbeForTest()
	})

	var calls atomic.Int64

	deepProbeFn = func() *SysInfo {
		calls.Add(1)
		time.Sleep(150 * time.Millisecond)
		info := fastSnapshot()
		info.CPU.Name = "single-flight"
		return info
	}

	resetDeepProbeForTest()

	const callers = 8

	var wg sync.WaitGroup

	wg.Add(callers)

	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_ = Probe()
		}()
	}

	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("deep probe ran %d times under %d concurrent callers — the single-flight contract is broken", got, callers)
	}
}

// TestWindowsDisplayForBuild pins the MEASURED build → label mapping.
// Build 26200 is Windows 11 25H2 — a stale manually-labelled "Windows 10"
// must never win over the measured build.
func TestWindowsDisplayForBuild(t *testing.T) {
	cases := []struct {
		build int
		want  string
	}{
		{26200, "Windows 11 25H2 (build 26200)"},
		{26100, "Windows 11 24H2 (build 26100)"},
		{22631, "Windows 11 23H2 (build 22631)"},
		{22621, "Windows 11 22H2 (build 22621)"},
		{22000, "Windows 11 21H2 (build 22000)"},
		{19045, "Windows 10 22H2 (build 19045)"},
		{19044, "Windows 10 21H2 (build 19044)"},
		{19041, "Windows 10 (build 19041)"},
		{0, "unknown"},
		{-1, "unknown"},
	}

	for _, tc := range cases {
		if got := WindowsDisplayForBuild(tc.build); got != tc.want {
			t.Fatalf("WindowsDisplayForBuild(%d) = %q, want %q", tc.build, got, tc.want)
		}
	}
}

// TestFastSnapshotCarriesOSIdentity pins that the fast snapshot carries
// the measured identity fields (0/GOOS on non-Windows — the honest
// unknown, never an invented build).
func TestFastSnapshotCarriesOSIdentity(t *testing.T) {
	snap := fastSnapshot()

	if snap.OSDisplay == "" {
		t.Fatal("fast snapshot carries no OSDisplay — the identity contract is broken")
	}

	if snap.OSBuild < 0 {
		t.Fatalf("OSBuild = %d — negative builds are impossible", snap.OSBuild)
	}
}
