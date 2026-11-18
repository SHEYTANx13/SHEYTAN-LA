//go:build !windows

package proc

import "syscall"

// processAliveImpl reports liveness via signal 0 on Unix (works on Linux
// and darwin — no platform-specific process state is needed).
func processAliveImpl(pid int) bool {
	// Signal 0 performs permission + existence checks without
	// actually signalling the process.
	return syscall.Kill(pid, 0) == nil
}

// processStartTimeImpl is per-OS (run 44306343338 repair §4.7):
// alive_linux.go reads /proc/<pid>/stat + /proc/stat btime;
// alive_darwin.go returns the typed unsupported result. The Linux-only
// /proc implementation must never be compiled under a broad Unix tag
// again.
