//go:build windows

package resources

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS (psapi).
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	psapi             = windows.NewLazySystemDLL("psapi.dll")
	pOpenProcess      = kernel32.NewProc("OpenProcess")
	pGetProcessMemory = psapi.NewProc("GetProcessMemoryInfo")
	pCloseHandle      = kernel32.NewProc("CloseHandle")
)

const processQueryLimited = 0x1000

func procRAMImpl(pid int) (int64, error) {
	handle, _, _ := pOpenProcess.Call(processQueryLimited, 0, uintptr(pid))
	if handle == 0 {
		return 0, fmt.Errorf("OpenProcess(%d) failed", pid)
	}
	defer pCloseHandle.Call(handle)

	var pmc processMemoryCounters
	pmc.CB = uint32(unsafe.Sizeof(pmc))
	ret, _, _ := pGetProcessMemory.Call(handle, uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.CB))
	if ret == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo(%d) failed", pid)
	}
	return int64(pmc.WorkingSetSize), nil
}
