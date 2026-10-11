//go:build windows

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobObject CPU-rate control flags (not exposed by x/sys as constants).
const (
	cpuRateControlEnable  = 0x00000001
	cpuRateControlHardCap = 0x00000004
)

// cpuRateControlInfo mirrors JOBOBJECT_CPU_RATE_CONTROL_INFORMATION:
// ControlFlags + a union whose CpuRate branch carries the rate in
// hundredths of a percent (25% = 2500).
type cpuRateControlInfo struct {
	ControlFlags uint32
	Value        uint32 // CpuRate when Enable|HardCap is set
}

// jobGovernor caps child processes with a Windows Job Object:
//   - JOB_OBJECT_LIMIT_PROCESS_MEMORY  → hard memMB commit cap
//   - JOB_OBJECT_LIMIT_ACTIVE_PROCESS  → no fork bombs
//   - JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE → children die with the app
//   - CPU-rate control (hard cap when the OS supports it) → cpuPct
type jobGovernor struct {
	mu sync.Mutex
	h  windows.Handle
}

// newGovernor returns the Windows job governor, or nil when the Job
// Object cannot be created (timeout-only degradation — the app keeps
// working, uncapped).
func newGovernor(memMB, cpuPct int) resourceGovernor {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil
	}

	// 1. Memory + process-count + kill-on-close.
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.ProcessMemoryLimit = uintptr(memMB) * 1024 * 1024
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY |
		windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	info.BasicLimitInformation.ActiveProcessLimit = 32
	_, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil
	}

	// 2. CPU rate: hard cap first (Windows 8+), soft rate as fallback,
	//    nothing on Windows 7 (memory/process caps above still apply).
	rate := uint32(cpuPct) * 100
	hard := cpuRateControlInfo{ControlFlags: cpuRateControlEnable | cpuRateControlHardCap, Value: rate}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectCpuRateControlInformation,
		uintptr(unsafe.Pointer(&hard)), uint32(unsafe.Sizeof(hard))); err != nil {
		soft := cpuRateControlInfo{ControlFlags: cpuRateControlEnable, Value: rate}
		_, _ = windows.SetInformationJobObject(h, windows.JobObjectCpuRateControlInformation,
			uintptr(unsafe.Pointer(&soft)), uint32(unsafe.Sizeof(soft)))
	}

	return &jobGovernor{h: h}
}

// prepare is a no-op pre-Start hook (caps are applied post-Start).
func (g *jobGovernor) prepare(timeoutSec int, name string, args []string) (string, []string) {
	return name, args
}

// attach assigns a just-started process to the job by PID. A tiny window
// exists between CreateProcess and the assignment — bounded by the
// kill-on-close flag and the caller's context timeout, both of which
// still terminate stragglers.
func (g *jobGovernor) attach(p *os.Process) error {
	if p == nil || p.Pid <= 0 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.h == 0 {
		return nil
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(g.h, ph)
}

// close terminates anything still alive in the job and drops the handle.
func (g *jobGovernor) close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.h == 0 {
		return nil
	}
	_ = windows.TerminateJobObject(g.h, 0)
	err := windows.CloseHandle(g.h)
	g.h = 0
	return err
}

// resolvePython finds a Python interpreter on Windows (python, then the
// py launcher).
func resolvePython() (string, error) {
	for _, cand := range []string{"python", "py"} {
		if path, err := exec.LookPath(cand); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("python not found (install from python.org or the Microsoft Store)")
}
