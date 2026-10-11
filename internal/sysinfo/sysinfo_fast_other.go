//go:build !windows

// sysinfo_fast_other.go — fast facts on non-Windows platforms (v1.2.6).
// /proc/meminfo (Linux) and statfs (darwin/bsd) are in-process reads —
// no external command is spawned.
package sysinfo

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func fastRAM() RAMInfo {
	r := RAMInfo{}

	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/meminfo"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				val = strings.TrimSuffix(val, " kB")
				n, _ := strconv.ParseUint(val, 10, 64)
				switch key {
				case "MemTotal":
					r.TotalBytes = n * 1024
				case "MemFree":
					r.FreeBytes = n * 1024
				case "MemAvailable":
					r.Available = n * 1024
				}
			}
		}
	}

	return r
}

func fastDisk(path string) DiskInfo {
	d := DiskInfo{Path: path}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err == nil {
		d.TotalBytes = uint64(stat.Blocks) * uint64(stat.Bsize)
		d.FreeBytes = uint64(stat.Bavail) * uint64(stat.Bsize)
	}

	return d
}

// cimBatch is Windows-only (see cim_windows.go). The stub exists so the
// shared probe code compiles on every platform; it is never reached —
// the caller gates on runtime.GOOS == "windows".
func cimBatch() (cimFacts, error) {
	return cimFacts{}, nil
}

// fastOSIdentity on non-Windows platforms: no build number is measured
// (the honest unknown — 0). The display label is the plain GOOS value;
// nothing is invented.
func fastOSIdentity() (int, string) {
	return 0, runtime.GOOS
}
