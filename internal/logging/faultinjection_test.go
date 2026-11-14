package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- v1.11.1 fault-injection tests (run 43601915826 work order §3.3B) ---
//
// The rotation/fallback failure paths are exercised with deterministic
// seams, not sleeps, ACL games or platform-specific chmod bets:
//   - rename failure: app-1.log pre-created as a DIRECTORY blocks the
//     rotation rename on every platform (Linux EISDIR, Windows access
//     denied) — no seam needed;
//   - reopen failure: the reopenAppSink seam;
//   - close failure: the closeAppSink seam (the injector always releases
//     the real descriptor first so the test stays leak-free on Windows).
//
// Every test ends with an explicit RemoveAll: a leaked descriptor is a
// test defect on Windows ("file still in use"), exactly the failure class
// run 43601915826 exposed in TestRotationFailureReportedNotSilent.

// TestRotationReopenFailureSurfacedAndBytesPreserved drives the reopen
// seam: rotation closes app.log, renames it to app-1.log, and the reopen
// fails. The failure must be observable (Health + stderr), the
// already-written bytes must survive in the rotated file, and every
// subsequent line must flow through the bounded emergency ring — never a
// nil sink that silently drops.
func TestRotationReopenFailureSurfacedAndBytesPreserved(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	prodReopen := reopenAppSink
	t.Cleanup(func() {
		reopenAppSink = prodReopen
		_ = m.Close()
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove log dir after reopen-failure test (descriptor leak?): %v", err)
		}
	})

	reopenAppSink = func(*Manager) (*os.File, int64, error) {
		return nil, 0, errors.New("injected reopen failure")
	}

	// The next accepted write crosses the rotation threshold.
	m.mu.Lock()
	m.appN = appMaxBytes - 1
	m.mu.Unlock()

	m.Info("boot", "write that triggers rotation with an injected reopen failure")

	h := m.Health()
	if h.SinksReady {
		t.Fatal("Health().SinksReady must be false after a failed rotation reopen")
	}
	if !strings.Contains(h.LastErr, "reopen app.log after rotation failed") {
		t.Fatalf("Health().LastErr = %q, want the reopen failure named", h.LastErr)
	}

	// Bytes written BEFORE the failure survive in the rotated generation.
	rotated, err := os.ReadFile(filepath.Join(dir, "app-1.log"))
	if err != nil {
		t.Fatalf("read rotated app-1.log: %v", err)
	}
	if !strings.Contains(string(rotated), "write that triggers rotation") {
		t.Fatalf("rotated bytes missing the pre-failure line: %q", rotated)
	}

	// Subsequent lines are preserved in the emergency ring, not discarded.
	m.Info("boot", "line after failed rotation reopen")
	if got := m.Emergency(10); len(got) != 1 {
		t.Fatalf("emergency ring = %d lines, want 1 (the post-failure line)", len(got))
	}

	// The failed reopen leaves NO fresh app.log — the manager never
	// pretends the canonical sink is healthy.
	if _, err := os.Stat(filepath.Join(dir, "app.log")); !os.IsNotExist(err) {
		t.Fatal("app.log must not exist after a failed rotation reopen")
	}
}

// TestRotationRenameFailureReportedNotFatal blocks the rotation rename
// with a directory at app-1.log: the failure is reported through the
// observable health state, the app sink stays healthy, and app.log keeps
// receiving every line (rotation is bookkeeping — no line is lost).
func TestRotationRenameFailureReportedNotFatal(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() {
		_ = m.Close()
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove log dir after rename-failure test (descriptor leak?): %v", err)
		}
	})

	// Deterministic rename blocker on every platform: a DIRECTORY at every
	// rotation slot (app-1 … app-5). The shift loop marches occupants
	// upward slot-by-slot, so any single blocker would simply be carried
	// to the next free slot; with every slot occupied by a directory, each
	// shift rename fails (Go's os.Rename refuses to replace an existing
	// directory — the EEXIST pre-check on Unix, the MoveFileEx denial on
	// Windows) and the final rename(app.log → app-1.log) fails too.
	for slot := 1; slot <= appKeepFiles; slot++ {
		if err := os.MkdirAll(filepath.Join(dir, fmt.Sprintf("app-%d.log", slot)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m.mu.Lock()
	m.appN = appMaxBytes - 1
	m.mu.Unlock()

	m.Info("boot", "write that triggers rotation with a blocked rename")

	h := m.Health()
	if !strings.Contains(h.LastErr, "rotate app.log") {
		t.Fatalf("Health().LastErr = %q, want the rotation rename failure named", h.LastErr)
	}
	if !h.SinksReady {
		t.Fatal("the app sink must stay ready when only the rename failed")
	}

	// Rotation is bookkeeping: app.log keeps receiving writes.
	m.Info("boot", "line after blocked rename")
	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("read app.log: %v", err)
	}
	if !strings.Contains(string(data), "line after blocked rename") {
		t.Fatalf("app.log missing the post-failure line: %q", data)
	}
	if !strings.Contains(string(data), "write that triggers rotation") {
		t.Fatalf("app.log missing the pre-failure line (already-written bytes must survive): %q", data)
	}
}

// TestCloseFailureJoinedReportedAndIdempotent injects a sink-close
// failure through the closeAppSink seam: Close attempts EVERY sink,
// returns the failures joined, records them in the observable health
// state, and stays idempotent. The injector releases the real descriptor
// so the test proves cleanup cleanliness rather than causing the leak it
// exists to guard against.
func TestCloseFailureJoinedReportedAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	prodClose := closeAppSink
	t.Cleanup(func() {
		closeAppSink = prodClose
		_ = m.Close() // idempotent second close with the seam restored
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove log dir after close-failure test (descriptor leak?): %v", err)
		}
	})

	closeAppSink = func(f *os.File) error {
		// Release the REAL descriptor first (hygiene), then report the
		// injected failure — simulating an OS-level close error without
		// leaking the handle.
		_ = f.Close()
		return errors.New("injected close failure")
	}

	if err := m.Close(); err == nil {
		t.Fatal("Close must fail when a sink close fails")
	} else if !strings.Contains(err.Error(), "close app.log") {
		t.Fatalf("joined close error = %v, want the failing sink named", err)
	}

	// Close attempts every sink even when one fails: all three failures
	// are joined into the returned error.
	if err := m.Close(); err != nil {
		t.Fatalf("second Close must be a no-op (idempotent), got: %v", err)
	}

	if h := m.Health(); !strings.Contains(h.LastErr, "close app.log") {
		t.Fatalf("Health().LastErr = %q, want the close failure recorded", h.LastErr)
	}
}
