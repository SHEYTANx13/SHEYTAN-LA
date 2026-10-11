//go:build windows

package engine

// process_liveness_windows_test.go — Windows half of the orphan-process
// liveness probe (see processGone in execution_contract_test.go).
//
// v1.3.5: this replaces the previous no-op ("return — no POSIX signal-0
// probe on Windows"). The orphan-process contract must remain MEANINGFUL
// on Windows, so the reaped-pid verdict comes from a real bounded
// kernel probe:
//
//   - OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION) failing (even with
//     ERROR_INVALID_PARAMETER after the handle table drains) means the
//     process object is gone;
//   - a still-open handle only counts as alive when the exit code is
//     still STILL_ACTIVE — a terminated-but-not-reaped pid reports its
//     exit code and therefore does not count as alive.
//
// No POSIX APIs, no shell, no cgo — golang.org/x/sys/windows only.

import (
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// processLivenessDeadline bounds the wait for the reaped pid to
// disappear (matches the Unix probe).
const processLivenessDeadline = 5 * time.Second

// stillActive is the Windows STILL_ACTIVE exit code (259): a process
// that opened but reports this exit code has not terminated yet.
// x/sys/windows does not export the constant, so it is pinned here with
// its documented value.
const stillActive = 259

// processAlive reports whether the pid still hosts a running process.
// A terminated pid whose handles were released no longer opens; a pid
// that opens is alive only while its exit code is still STILL_ACTIVE.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(pid),
	)
	if err != nil {
		// The kernel refuses to open the pid: the process object is
		// gone (or was never there). That is the reaped verdict.
		return false
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		// Unqueryable handle: treat as gone rather than orphaned-forever.
		return false
	}

	return exitCode == stillActive
}

// platformProcessGone polls the kernel until the pid is gone — the
// Windows mirror of the Unix signal-0 loop.
func platformProcessGone(t *testing.T, label string, pid int) {
	t.Helper()

	deadline := time.Now().Add(processLivenessDeadline)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("%s: pid %d is still a running process after stop — orphaned host process", label, pid)
}
