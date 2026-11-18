//go:build darwin

package proc

import (
	"fmt"
	"path/filepath"
)

// listeningProcessImpl on darwin: the Linux /proc discovery path does
// not exist on macOS, and this build does not link the Darwin libproc
// (proc_pidinfo) fd walk that a genuine port-owner lookup requires
// (run 44306343338 repair §4.7). The typed UNSUPPORTED result is the
// honest answer: callers treat the operation as unproven and refuse
// sensitive adoption (fail closed) — a listener-owner guess is never
// manufactured from socket paths or partial evidence.
//
// If port-owner identity on macOS becomes a product requirement, it must
// be implemented through the genuine darwin-specific API (proc_pidinfo
// PROC_PIDLISTFDS + PROC_PIDFDSOCKETINFO) and verified on a native
// runner — not approximated here.
func listeningProcessImpl(port int) (int, string, bool, error) {
	return 0, "", false, fmt.Errorf("%w: darwin listening-process ownership requires the libproc fd walk, which this build does not link", ErrPortOwnerUnsupported)
}

// processExePathImpl on darwin: no image-path probe is linked in this
// build — typed unsupported, never a fabricated path.
func processExePathImpl(pid int) (string, error) {
	return "", fmt.Errorf("%w (pid %d): darwin image-path lookup requires the libproc/sysctl path, which this build does not link", ErrProcessMetadataUnsupported, pid)
}

// sameExecutableImpl on darwin resolves symlinks and compares paths
// (darwin is case-sensitive, unlike the Windows implementation).
func sameExecutableImpl(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}

	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}

	return a == b
}
