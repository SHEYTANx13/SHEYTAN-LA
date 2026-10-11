//go:build !windows

package llm

// importlock_unix.go — the cross-process advisory lock for model
// imports on Unix (Linux/macOS/BSD): flock(2) LOCK_EX. The kernel
// releases the lock when the owning process dies, so a crashed importer
// never wedges the models directory. flock is per open-file-description:
// two opens in the SAME process are separate holders, which is exactly
// what the in-process mutex layer (importlock.go) serializes before we
// get here.

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// errImportLockTimeout marks a bounded-timeout acquisition failure.
var errImportLockTimeout = errors.New("timed out waiting for the import lock")

// lockImportFile takes an exclusive flock on the lock file, waiting up
// to importLockAcquireTimeout. A crash by the previous holder releases
// the lock at the kernel level — no stale-lock recovery is needed.
func lockImportFile(f *os.File) error {
	deadline := time.Now().Add(importLockAcquireTimeout)

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EINTR && err != syscall.EAGAIN {
			return err
		}
		if time.Now().After(deadline) {
			return errImportLockTimeout
		}
		time.Sleep(25 * time.Millisecond)
	}
}
