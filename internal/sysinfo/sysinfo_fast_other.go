//go:build !windows

// sysinfo_fast_other.go — shared non-Windows fast facts (v1.2.6). The
// fastRAM probe is per-OS (run 44306343338 repair §4.6):
// sysinfo_fast_linux.go (/proc/meminfo), sysinfo_fast_darwin.go
// (hw.memsize sysctl) and sysinfo_fast_otherunix.go (honest unknown).
// statfs (darwin/bsd/linux) is an in-process read — no external command
// is spawned.
package sysinfo

import (
	"runtime"
	"syscall"
)

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
