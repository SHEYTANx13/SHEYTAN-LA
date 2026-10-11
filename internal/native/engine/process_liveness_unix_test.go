//go:build !windows

package engine

// process_liveness_unix_test.go — Unix half of the orphan-process
// liveness probe (see processGone in execution_contract_test.go).
//
// The reaped-pid contract is verified with a REAL signal-0 probe to the
// exact pid: the process was reaped by the supervisor, not merely
// forgotten by its bookkeeping.

import (
	"syscall"
	"testing"
	"time"
)

// processAlive reports whether pid still exists (signal 0 delivers
// nothing — it only asks the kernel whether the process is there).
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// processLivenessDeadline bounds the wait for the reaped pid to
// disappear.
const processLivenessDeadline = 5 * time.Second

// platformProcessGone polls the kernel until the pid is gone. On Unix
// the ESRCH verdict is the exact, honest answer.
func platformProcessGone(t *testing.T, label string, pid int) {
	t.Helper()

	deadline := time.Now().Add(processLivenessDeadline)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return // ESRCH: the process is gone
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("%s: pid %d still responds to signal 0 after stop — orphaned host process", label, pid)
}
