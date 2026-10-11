//go:build windows

package api

// perf_windows.go — cheap live CPU/RAM samples for /api/perf on Windows.
// Both come straight from kernel32 (LazyProc syscalls): no WMI, no CIM
// round-trips, no new dependencies.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")

var (
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

// filetimeToSec converts a FILETIME (100-ns intervals since 1601) to
// seconds.
func filetimeToSec(ft windows.Filetime) float64 {
	return float64(uint64(ft.HighDateTime)<<32|uint64(ft.LowDateTime)) / 1e7
}

// cpuTimesSnapshot reads cumulative CPU idle/busy times via GetSystemTimes.
// Busy = all time the processors were NOT idle (the kernel time already
// includes idle time, so busy = kernel + user − idle).
func cpuTimesSnapshot() (idle, busy float64, ok bool) {
	var idleFT, kernelFT, userFT windows.Filetime

	r1, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleFT)),
		uintptr(unsafe.Pointer(&kernelFT)),
		uintptr(unsafe.Pointer(&userFT)),
	)
	if r1 == 0 {
		_ = err
		return 0, 0, false
	}

	idleSec := filetimeToSec(idleFT)
	totalSec := filetimeToSec(kernelFT) + filetimeToSec(userFT)

	return idleSec, totalSec - idleSec, true
}

// memoryStatusEx mirrors the MEMORYSTATUSEX structure.
type memoryStatusEx struct {
	dwLength     uint32
	dwMemoryLoad uint32
	ullTotalPhys uint64
	ullAvailPhys uint64
	// Remaining fields of the fixed-size struct (page file etc.).
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

// sampleRAM reads live memory pressure via GlobalMemoryStatusEx.
func sampleRAM() (ramSample, bool) {
	var ms memoryStatusEx
	ms.dwLength = uint32(unsafe.Sizeof(ms))

	r1, _, err := procGlobalMemoryStatusEx.Call(
		uintptr(unsafe.Pointer(&ms)),
	)
	if r1 == 0 {
		_ = err
		return ramSample{}, false
	}

	total := ms.ullTotalPhys
	avail := ms.ullAvailPhys
	used := total - avail

	out := ramSample{
		TotalBytes:     total,
		AvailableBytes: avail,
		UsedBytes:      used,
	}
	if total > 0 {
		out.UsedPercent = 100 * float64(used) / float64(total)
	}

	// dwMemoryLoad is the OS's own busy percentage (0-100); the computed
	// usedPercent from the byte counters is what the UI shows.
	_ = ms.dwMemoryLoad

	return out, true
}
