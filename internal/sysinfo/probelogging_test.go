// Clean-logging contract regressions.
//
// Contracts under test (release contract §4):
//
//  1. repeated ProbeFast() calls (multiple UI surfaces reading the same
//     cached information) do NOT spam repeated INFO records;
//  2. the one-time fast-environment summary IS emitted exactly once;
//  3. the deep probe still produces useful evidence exactly once;
//  4. WARN/ERROR records are never blank — an empty message is upgraded
//     to an actionable record pointing at the call site.
package sysinfo

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// withLogManager installs a temporary log manager writing to a temp
// directory and returns a function reading back app.log.
func withLogManager(t *testing.T) func() string {
	t.Helper()

	dir := t.TempDir()
	mgr, err := logging.New(dir)
	if err != nil {
		t.Fatalf("log manager: %v", err)
	}

	previous := logging.Default()
	logging.SetDefault(mgr)
	t.Cleanup(func() {
		logging.SetDefault(previous)
		_ = mgr.Close()
	})

	return func() string {
		data, err := os.ReadFile(filepath.Join(dir, "app.log"))
		if err != nil {
			t.Fatalf("read app.log: %v", err)
		}
		return string(data)
	}
}

func resetSysinfoState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		resetDeepProbeForTest()
		resetFastLogForTest()
	})
	resetDeepProbeForTest()
	resetFastLogForTest()
}

// TestRepeatedProbeFastDoesNotSpamInfo — the v1.2.9 defect: every
// ProbeFast() call logged "fast snapshot in 0 ms" at INFO, so a normal
// session produced dozens of identical lines. v1.3.0: the read/cache
// event is silent; ONE summary is emitted per process.
func TestRepeatedProbeFastDoesNotSpamInfo(t *testing.T) {
	readLog := withLogManager(t)
	resetSysinfoState(t)

	const uiPolls = 25
	var wg sync.WaitGroup
	for i := 0; i < uiPolls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ProbeFast()
		}()
	}
	wg.Wait()
	_ = ProbeFast()
	_ = ProbeFast()

	log := readLog()

	if got := strings.Count(log, "fast snapshot in"); got != 0 {
		t.Fatalf("repeated per-call snapshot log found %d times — routine polling must be silent:\n%s", got, log)
	}

	if got := strings.Count(log, "fast environment ready"); got != 1 {
		t.Fatalf("one-time fast summary logged %d times (want exactly 1):\n%s", got, log)
	}
}

// TestDeepProbeStillLogsOnce — the measurement event (the actual deep
// probe) still produces useful evidence, exactly once per process.
func TestDeepProbeStillLogsOnce(t *testing.T) {
	readLog := withLogManager(t)
	resetSysinfoState(t)

	// The REAL deep probe (bounded, in-process on linux) — the evidence
	// line must come from the production path, not a stub.
	_ = Probe()
	_ = Probe()
	_ = ProbeFast()
	_ = ProbeFast()

	log := readLog()

	if got := strings.Count(log, "deep probe completed in"); got != 1 {
		t.Fatalf("deep probe evidence logged %d times (want exactly 1):\n%s", got, log)
	}

	if !strings.Contains(log, "deep probe completed in ") {
		t.Fatalf("deep probe evidence missing entirely:\n%s", log)
	}
}

// TestNoBlankWarningsFromSysinfo — every WARN/ERROR record the package
// emits carries a message.
func TestNoBlankWarningsFromSysinfo(t *testing.T) {
	readLog := withLogManager(t)
	resetSysinfoState(t)

	// Exercise the failing-probe path (unknown sources on linux produce
	// honest facts, not hangs).
	_ = ProbeFast()

	for _, line := range strings.Split(readLog(), "\n") {
		if strings.Contains(line, "WARN") || strings.Contains(line, "ERROR") {
			// The message is everything after "]".
			idx := strings.LastIndex(line, "]")
			if idx >= 0 && strings.TrimSpace(line[idx+1:]) == "" {
				t.Fatalf("blank warning emitted: %q", line)
			}
		}
	}
}
