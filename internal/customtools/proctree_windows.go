//go:build windows

// Per-invocation process-tree ownership for custom command tools
// (v1.6.0 repair, spec §4).
//
// exec.CommandContext's default cancellation kills ONLY the direct
// child. A custom tool that spawns descendants (cmd /C ping …) leaves
// the grandchildren alive, and they keep the inherited stdout/stderr
// pipe write-ends open, so cmd.Wait() blocks until the orphans exit.
//
// Windows Job Objects own a process GROUP as a unit: assigning the
// started process to a job with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// makes every descendant (children inherit the job; no breakaway is
// permitted) die with the job. Terminating the job on
// timeout/cancellation therefore releases the pipes immediately and
// cmd.Wait() returns promptly — correctness by process ownership, not
// by timing.
//
// This mirrors the established internal/sandbox/sandbox_windows.go
// pattern, but per-invocation: each custom-tool run gets its own job
// so terminating one tool's tree can never affect another tool's
// processes or unrelated system processes.
package customtools

import (
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree tracks the descendant tree of one custom-tool command
// invocation through a dedicated Job Object.
type processTree struct {
	mu sync.Mutex
	h  windows.Handle // 0 when no job could be created (degradation)
}

// newProcessTree creates the per-invocation Job Object. Failure to
// create it is non-fatal: the executor still has the direct-process
// kill from exec.CommandContext as a fallback (the pre-v1.6 behavior).
func newProcessTree() *processTree {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return &processTree{h: 0}
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	_, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		_ = windows.CloseHandle(h)
		return &processTree{h: 0}
	}

	return &processTree{h: h}
}

// attach assigns a just-started process to the job. Descendants the
// process spawns afterwards inherit the job automatically. A tiny
// window exists between CreateProcess and this assignment; it is
// bounded by KILL_ON_JOB_CLOSE (handle closed at the end of every
// invocation) and the caller's context timeout. A process that exits
// before the assignment is not an error — the invocation is already
// finishing on its own.
func (t *processTree) attach(p *os.Process) {
	if p == nil || p.Pid <= 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return
	}

	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return // process exited before assignment — clean race, not fatal
	}
	defer windows.CloseHandle(ph)

	_ = windows.AssignProcessToJobObject(t.h, ph)
}

// terminate kills every process currently associated with the job.
// Called on context cancellation/timeout so the whole tree dies at
// once and the inherited pipe write-ends close immediately.
func (t *processTree) terminate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return
	}
	_ = windows.TerminateJobObject(t.h, 1)
}

// close terminates anything still alive in the job and drops the
// handle (KILL_ON_JOB_CLOSE makes the kernel finish the job's
// processes). Always called — success and failure alike — so the job
// handle never leaks.
func (t *processTree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return
	}
	_ = windows.TerminateJobObject(t.h, 1)
	_ = windows.CloseHandle(t.h)
	t.h = 0
}
