package updater

// roottrack.go — v1.11.1 (run 43995689377) P0 fix: the engine-swap handle
// lifecycle gate.
//
// ROOT CAUSE this file closes: the Windows smoke app's engine maintenance
// failed at
//
//      stage engine swap: rename data\bin → data\bin.update-old: Access is denied
//
// because the installer opened an os.Root on <binDir>/.engine-download (the
// archive staging directory) and deferred its Close until AFTER the
// transaction returned — while the transaction's SWAP step renames the
// WHOLE bin directory aside. On Windows, an open handle to any descendant
// of a directory (an os.Root keeps one on its root directory) blocks
// renaming that directory: MoveFileExW fails with ERROR_ACCESS_DENIED.
// Go's os.Root documentation states the same for the root directory
// itself; the descendant case is the one this transaction hit.
//
// The fix has three parts, all in this package:
//
//  1. the leaked handle is closed BEFORE the swap (the archive cleanup
//     root now lives in a short scope after the transaction body —
//     install.go, InstallStagedFromArchiveWithVariant);
//  2. EVERY os.Root this package opens is registered here at Open and
//     unregistered at Close, and stageFromArchive consults the registry
//     immediately before the swap: if any tracked root under binDir is
//     still open the swap is REFUSED with a precise, attributable typed
//     error instead of a platform-dependent mystery "Access is denied".
//     The old engine is left untouched (the refusal happens before any
//     mutation). This makes the failure class deterministic and testable
//     on every OS, not only on Windows;
//  3. a genuine external rename failure (antivirus, search indexer,
//     another process holding a handle, ACLs) is wrapped with the OS
//     error classification so the maintenance log distinguishes sharing
//     violation (ERROR_SHARING_VIOLATION, 32) from access denied
//     (ERROR_ACCESS_DENIED, 5) from other causes — diagnostics only;
//     no protection is disabled, no retry loop is added, the previous
//     engine is preserved exactly as before.
//
// Scope note: the registry covers os.Root handles opened by THIS package
// (the engine provisioning authority — the only component that opens
// roots under the managed bin directory). File handles are closed by
// their owning scopes before the swap by construction (extraction,
// validation and hashing complete before the swap step); external
// processes cannot be registered and are handled by part 3's honest
// error classification instead.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

// trackedRoot is one registered, open os.Root with the path it is bound
// to. Close unregisters FIRST (so a half-closed root is never reported as
// a leak) and closes the handle second. The *os.Root is EMBEDDED so
// every root-relative operation (MkdirAll, Remove, Open, Stat, ...)
// keeps its exact os.Root semantics.
type trackedRoot struct {
	*os.Root
	path string
}

var (
	rootTrackMu sync.Mutex
	openRoots   = map[*trackedRoot]string{} // root → absolute target path
)

// openTrackedRoot opens dir as an os.Root and registers it. Every
// engine-swap-relevant root in this package must come from here so the
// quiesce gate can see it.
func openTrackedRoot(dir string) (*trackedRoot, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}

	tr := &trackedRoot{Root: root, path: filepath.Clean(dir)}

	rootTrackMu.Lock()
	openRoots[tr] = tr.path
	rootTrackMu.Unlock()

	return tr, nil
}

// Close unregisters the root and releases its OS handle. Safe to call
// twice; a nil root is a no-op (callers use the ok-idiom when the root is
// optional). Overrides the embedded os.Root.Close so the registry can
// never report a closed root as a leak.
func (t *trackedRoot) Close() error {
	if t == nil {
		return nil
	}

	rootTrackMu.Lock()
	delete(openRoots, t)
	rootTrackMu.Unlock()

	return t.Root.Close()
}

// Remove/Open/MkdirAll/... are inherited from the embedded *os.Root
// (root-relative, traversal-safe).

// openRootsUnder returns the absolute paths of every tracked root that is
// currently open at or under dir. Deterministic: it is a pure snapshot of
// the registry under the mutex.
func openRootsUnder(dir string) []string {
	dir = filepath.Clean(dir)

	rootTrackMu.Lock()
	defer rootTrackMu.Unlock()

	var out []string
	for _, p := range openRoots {
		if p == dir || isSubPath(dir, p) {
			out = append(out, p)
		}
	}

	return out
}

// isSubPath reports whether child is inside parent or equal to it (both
// cleaned, absolute). A pure lexical check is correct here: registry
// paths are produced by this package from cleaned configuration paths.
// "../sibling" and any deeper "…" escape is rejected — only the subtree
// counts.
func isSubPath(parent, child string) bool {
	if parent == "" || child == "" {
		return false
	}

	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	return !filepath.IsAbs(rel)
}

// errSwapBlockedByOpenRoot is the typed refusal when the quiesce gate
// finds an in-process root handle still open under the engine directory.
// The swap has NOT happened when this is returned; the previous engine is
// untouched.
var errSwapBlockedByOpenRoot = errors.New("engine swap blocked by a retained in-process os.Root handle")

// assertSwapQuiesce is the gate stageFromArchive calls immediately before
// the directory swap: it refuses the transaction while ANY tracked root
// under the engine bin directory is still open — the exact handle-lifecycle
// defect that produced run 43995689377's "stage engine swap: ... Access is
// denied" on Windows (rename blocked by the open handle, engine update
// failed, previous-engine validation failed, maintenance BLOCKED).
func assertSwapQuiesce(binDir string) error {
	leaks := openRootsUnder(binDir)
	if len(leaks) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: %d open root handle(s) under %s (%s) — close every root/file under the engine directory before the swap; on Windows an open descendant handle blocks the directory rename",
		errSwapBlockedByOpenRoot, len(leaks), binDir, joinPaths(leaks))
}

func joinPaths(ps []string) string {
	out := ""
	for i, p := range ps {
		if i > 0 {
			out += ", "
		}

		out += p
	}

	return out
}

// Windows error codes the swap classifier names (constants, not builds
// tags — syscall.Errno comparison is portable, the INTERPRETATION is only
// asserted for windows binaries).
const (
	winErrAccessDenied     = syscall.Errno(5)
	winErrSharingViolation = syscall.Errno(32)
)

// swapRenameContext wraps a failed swap rename with the OS-level cause
// classification, so the maintenance log can distinguish:
//
//   - ERROR_ACCESS_DENIED (5): a handle is held on the directory or a
//     descendant (retained root/file handle, a process working directory
//     inside it), or an ACL denial — check what still holds the tree;
//   - ERROR_SHARING_VIOLATION (32): another process (antivirus, search
//     indexer, backup) has an open handle — transient external contention,
//     never "fixed" by disabling the protector;
//   - anything else: the raw OS error stands.
//
// The classification is DIAGNOSTIC: it never retries, never disables
// security software, and never mutates the previous engine.
func swapRenameContext(op string, err error) error {
	if err == nil {
		return nil
	}

	var errno syscall.Errno
	if runtime.GOOS != "windows" || !errors.As(err, &errno) {
		// Non-Windows platforms (or non-errno errors) keep the raw
		// wrapped error — the win32 code table below is only meaningful
		// for Windows binaries (on Linux, say, Errno(5) is EIO).
		return fmt.Errorf("%s: %w", op, err)
	}

	switch {
	case errno == winErrAccessDenied:
		return fmt.Errorf(
			"%s: %w (win32 ERROR_ACCESS_DENIED — a handle is open on the engine directory or one of its descendants (os.Root, open file, or a process working directory), or the ACL denies the move; run-43995689377 root cause class: retained in-process root handle)",
			op, err)

	case errno == winErrSharingViolation:
		return fmt.Errorf(
			"%s: %w (win32 ERROR_SHARING_VIOLATION — another process holds an open handle on the engine tree (antivirus, search indexer, backup); the previous engine is preserved, retry after the external handle releases)",
			op, err)

	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
