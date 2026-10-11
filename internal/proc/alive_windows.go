//go:build windows

package proc

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

// stillActive is the GetExitCodeProcess sentinel for a live process
// (WAIT_TIMEOUT / STILL_ACTIVE = 259; not exported by x/sys/windows).
const stillActive = 259

// processAliveImpl reports liveness through OpenProcess + exit-code
// probing (real Windows APIs — no shell parsing, spec §18).
func processAliveImpl(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}

	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}

	return code == stillActive
}

// processStartTimeImpl returns the process creation time through
// OpenProcess + GetProcessTimes (kernel64 — the same API class as
// QueryFullProcessImageName).
func processStartTimeImpl(pid int) (time.Time, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, ErrProcessNotFound
	}

	defer windows.CloseHandle(h)

	var (
		creation windows.Filetime
		exit     windows.Filetime
		kernel   windows.Filetime
		user     windows.Filetime
	)

	if err := windows.GetProcessTimes(
		h,
		&creation,
		&exit,
		&kernel,
		&user,
	); err != nil {
		return time.Time{}, fmt.Errorf("GetProcessTimes(pid %d): %w", pid, err)
	}

	return time.Unix(0, creation.Nanoseconds()).UTC(), nil
}
