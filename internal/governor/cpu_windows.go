//go:build windows

package governor

// cpu_windows.go — v1.8.6: the Windows CPU load seam for the Governor.
//
// THE v1.8.0 GAP THIS CLOSES: cpu_other.go documented that "until a live
// Windows load authority exists, the CPU seam reports not measurable and
// every CPU policy branch stays OFF". The authority exists all along —
// kernel32 GetSystemTimes, the same cumulative idle/kernel/user counters
// /proc/stat gives Linux. This file wires it through the ONE shared
// priming/delta state machine (cpu_delta.go): no second sampler, no
// second cadence, no new thread — CPULoadPlatform keeps its exact
// signature and the Governor keeps its existing poll-path ownership.
//
// Semantics (pinned by cpu_delta_test.go on every platform):
//
//   - the first call PRIMES (not measurable — an honest unknown);
//   - subsequent calls compute a REAL delta over the elapsed window;
//   - a failed GetSystemTimes call stays unknown (the baseline is kept —
//     cumulative counters keep the next delta valid across the gap);
//   - Linux behavior stays intact (cpu_linux.go is untouched).

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var windowsKernel32 = windows.NewLazySystemDLL("kernel32.dll")

var procGetSystemTimes = windowsKernel32.NewProc("GetSystemTimes")

// windowsCPUDelta is the process-wide seam state (one sampler, one
// cadence — the Governor's Observe path is the only caller).
var windowsCPUDelta = newCPUDeltaSeam(readWindowsSystemTimes)

// CPULoadPlatform is the Windows CPUSampler: the GetSystemTimes delta.
func CPULoadPlatform() (float64, bool) {
	return windowsCPUDelta.load()
}

// readWindowsSystemTimes reads the cumulative CPU counters via
// GetSystemTimes. Busy = all time the processors were NOT idle (kernel
// time includes idle, so busy = kernel + user − idle) — the exact
// normalization the /proc/stat reader uses on Linux.
func readWindowsSystemTimes() (idle, busy float64, ok bool) {
	var idleFT, kernelFT, userFT windows.Filetime

	r1, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleFT)),
		uintptr(unsafe.Pointer(&kernelFT)),
		uintptr(unsafe.Pointer(&userFT)),
	)
	if r1 == 0 {
		return 0, 0, false
	}

	idleSec := filetimeToSeconds(idleFT)
	totalSec := filetimeToSeconds(kernelFT) + filetimeToSeconds(userFT)

	return idleSec, totalSec - idleSec, true
}

// filetimeToSeconds converts a FILETIME (100-ns intervals since 1601)
// to seconds.
func filetimeToSeconds(ft windows.Filetime) float64 {
	return float64(uint64(ft.HighDateTime)<<32|uint64(ft.LowDateTime)) / 1e7
}
