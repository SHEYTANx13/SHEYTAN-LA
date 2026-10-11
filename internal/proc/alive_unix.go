//go:build !windows

package proc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// processAliveImpl reports liveness via signal 0 on Unix.
func processAliveImpl(pid int) bool {
	// Signal 0 performs permission + existence checks without
	// actually signalling the process.
	return syscall.Kill(pid, 0) == nil
}

// processStartTimeImpl reads the process start time from /proc:
// field 22 of /proc/<pid>/stat is the start time in clock ticks since
// boot; /proc/stat carries the boot epoch (btime) to make it absolute.
func processStartTimeImpl(pid int) (time.Time, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}, ErrProcessNotFound
	}

	// The comm field may contain spaces/parens — parse after the last ')'.
	s := string(stat)
	idx := strings.LastIndex(s, ")")
	if idx < 0 || idx+2 >= len(s) {
		return time.Time{}, fmt.Errorf("malformed proc stat for pid %d", pid)
	}

	fields := strings.Fields(s[idx+2:])

	// After the comm field, fields[0] is "state"; starttime is field 22
	// overall → index 19 in this slice.
	if len(fields) < 20 {
		return time.Time{}, fmt.Errorf("short proc stat for pid %d", pid)
	}

	ticks, err := strconv.ParseFloat(fields[19], 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad starttime for pid %d: %w", pid, err)
	}

	btime, err := bootTimeEpoch()
	if err != nil {
		return time.Time{}, err
	}

	clkTck := 100.0 // Linux user Hz is fixed at 100 (AJAX: sysconf(_SC_CLK_TCK))
	secs := btime + ticks/clkTck

	return time.Unix(int64(secs), 0).UTC(), nil
}

func bootTimeEpoch() (float64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			v, err := strconv.ParseFloat(strings.TrimSpace(line[6:]), 64)
			if err != nil {
				return 0, err
			}

			return v, nil
		}
	}

	return 0, fmt.Errorf("btime not found in /proc/stat")
}
