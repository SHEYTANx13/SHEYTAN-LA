//go:build windows

package llm

// importlock_windows.go — the cross-process advisory lock for model
// imports on Windows: LockFileEx with LOCKFILE_EXCLUSIVE_LOCK. The
// kernel releases the byte-range lock when the owning process (or its
// job object) dies, so a crashed importer never wedges the models
// directory. The in-process mutex layer (importlock.go) serializes
// same-process contenders before we get here.
//
// NOTE: Go's stdlib syscall package does not expose LockFileEx (only
// the non-blocking LockFile), so this file binds kernel32.dll directly
// — no new module dependency, CGO-free, exactly like the proc package's
// own Windows bindings.

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

// errImportLockTimeout marks a bounded-timeout acquisition failure.
var errImportLockTimeout = errorString("timed out waiting for the import lock")

type errorString string

func (e errorString) Error() string { return string(e) }

// LockFileEx flags and the shared-violation errno (winerror.h).
const (
	lockfileExclusiveLock   = 0x00000002 // LOCKFILE_EXCLUSIVE_LOCK
	lockfileFailImmediately = 0x00000001 // LOCKFILE_FAIL_IMMEDIATELY
	errorLockViolation      = syscall.Errno(33)
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx       = kernel32.NewProc("LockFileEx")
	procUnlockFileEx     = kernel32.NewProc("UnlockFileEx")
)

// lockImportFile takes an exclusive LockFileEx byte-range (the first
// byte) on the lock file handle, waiting up to importLockAcquireTimeout.
func lockImportFile(f *os.File) error {
	deadline := time.Now().Add(importLockAcquireTimeout)

	var overlapped syscall.Overlapped

	for {
		r1, _, callErr := procLockFileEx.Call(
			uintptr(f.Fd()),
			uintptr(lockfileExclusiveLock|lockfileFailImmediately),
			0,
			1, 0, // lock one byte at offset 0
			uintptr(unsafe.Pointer(&overlapped)),
		)
		if r1 != 0 {
			return nil
		}
		if callErr != errorLockViolation {
			return callErr
		}
		if time.Now().After(deadline) {
			return errImportLockTimeout
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// unlockImportFile releases the byte range taken by lockImportFile
// (tests use it to verify the lock is held/released).
func unlockImportFile(f *os.File) error {
	var overlapped syscall.Overlapped

	r1, _, callErr := procUnlockFileEx.Call(
		uintptr(f.Fd()),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		return callErr
	}
	return nil
}
