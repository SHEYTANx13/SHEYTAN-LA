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
// identity_darwin.go (run 44547031698 §5: the cgo libproc bridge) carries
// the darwin implementation, identity_darwin_nocgo.go and identity_ios.go
// the typed unsupported/contained results. The Linux-only /proc
// implementation must never be compiled under a broad Unix tag again.
