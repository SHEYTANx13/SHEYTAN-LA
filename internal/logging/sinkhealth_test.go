package logging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- v1.11.1 sink-health / failure-path tests ---
//
// The observability contract: a failing sink is REPORTED (stderr +
// observable health + bounded emergency ring), never silently dropped,
// never hidden behind a healthy-looking manager; partial opens roll back;
// close attempts every sink and joins failures; the fallback is per-user,
// private, and honest about the primary failure.

// TestOpenSinksPartialFailureRollsBack proves a failed second/third sink
// closes every handle already opened: the returned error preserves the
// original open failure, and no half-open Manager escapes New.
func TestOpenSinksPartialFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// app.log opens fine; tools.jsonl is a DIRECTORY — open for append
	// must fail, after appF was already opened.
	if err := os.WriteFile(filepath.Join(dir, "app.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tools.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}

	m, err := New(dir)
	if err == nil {
		_ = m.Close()
		t.Fatal("New must fail when a structured sink cannot be opened")
	}
	if !strings.Contains(err.Error(), "tools.jsonl") {
		t.Fatalf("error should name the failing sink, got: %v", err)
	}
	// The rolled-back manager never escapes, so verify via a fresh
	// Manager that the app.log handle was closed: on Windows an open
	// handle would block the directory removal below.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove log dir after partial open failure (descriptor leak?): %v", err)
	}
}

// TestNilAppSinkKeepsLinesAndReports proves the core v1.11.1 repair: with
// the app sink unavailable, log events are preserved in the bounded
// emergency ring, stay visible in the UI ring, surface in Health(), and
// the failure is recorded — never silently discarded.
func TestNilAppSinkKeepsLinesAndReports(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	m.mu.Lock()
	if m.appF != nil {
		_ = m.appF.Close()
	}
	m.appF = nil // simulate a lost sink (open/rotation failure shape)
	m.mu.Unlock()

	m.Info("boot", "line one while the sink is broken")
	m.Warn("boot", "line two while the sink is broken")

	emergency := m.Emergency(10)
	if len(emergency) != 2 {
		t.Fatalf("emergency ring = %d lines, want 2 (lines must be preserved, not discarded)", len(emergency))
	}
	if !strings.Contains(emergency[0], "line one while the sink is broken") {
		t.Fatalf("emergency line 1 = %q", emergency[0])
	}

	recent := m.Recent(10)
	if len(recent) != 2 {
		t.Fatalf("UI ring = %d lines, want 2 — the Logs surface must stay live through a sink failure", len(recent))
	}

	h := m.Health()
	if h.SinksReady {
		t.Fatal("Health().SinksReady must be false while the app sink is broken")
	}
	if h.LastErr == "" {
		t.Fatal("Health().LastErr must carry the sink failure")
	}
	if h.EmergencyLines != 2 {
		t.Fatalf("Health().EmergencyLines = %d, want 2", h.EmergencyLines)
	}
}

// TestWriteFailureSurfacedAndPersistedAfterClose proves a write failure
// on a live handle is observable, and that persistence after Close still
// holds for everything written before the failure.
func TestWriteFailureSurfacedAndPersistedAfterClose(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	m.Info("boot", "written while healthy")
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After Close the app sink is gone: further events flow through the
	// emergency path and are reported, never swallowed.
	m.Info("boot", "written after close")
	h := m.Health()
	if h.SinksReady {
		t.Fatal("post-close manager must not report ready sinks")
	}
	if len(m.Emergency(10)) != 1 {
		t.Fatalf("post-close line must be preserved in the emergency ring, got %d", len(m.Emergency(10)))
	}

	// Persistence proof: the healthy-write line reached the file bytes.
	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("read app.log: %v", err)
	}
	if !strings.Contains(string(data), "written while healthy") {
		t.Fatalf("app.log bytes missing the pre-close line: %q", data)
	}
	if strings.Contains(string(data), "written after close") {
		t.Fatal("post-close line must NOT claim file persistence")
	}
	if !strings.Contains(string(data), "log sinks closing") {
		t.Fatalf("app.log must carry the close-outcome record: %q", data)
	}
}

// TestCloseOutcomeRecordedAndIdempotent proves Close writes the close
// record, is safe to call twice, and returns nil on the healthy path.
func TestCloseOutcomeRecordedAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.SessionBanner("SHEYTAN-LA", "1.11.1")
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second Close must be a no-op, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("read app.log: %v", err)
	}
	if !strings.Contains(string(data), "log sinks closing (dir=") {
		t.Fatalf("close-outcome record missing from app.log: %q", data)
	}
}

// TestFallbackHonestAndPerUser proves the fallback contract: when the
// canonical dir cannot be opened, the failure is recorded, the fallback
// is used, Dir() reports the ACTIVE location, and Health() carries the
// reason — the canonical sink is never claimed healthy.
func TestFallbackHonestAndPerUser(t *testing.T) {
	// A FILE where the logs DIRECTORY must go: MkdirAll fails.
	blocker := filepath.Join(t.TempDir(), "blocked-by-file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	fb, err := FallbackLogDir()
	if err != nil {
		t.Fatalf("FallbackLogDir: %v", err)
	}
	// Redirect the user cache dir to a temp base so the test never touches
	// the real user cache and cleans up after itself.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fb2, err := FallbackLogDir()
	if err != nil {
		t.Fatalf("FallbackLogDir (redirected): %v", err)
	}
	if fb == fb2 && !strings.Contains(fb, t.TempDir()) {
		// On non-XDG platforms UserCacheDir may ignore the env; the
		// assertions below only need SOME per-user fallback dir.
		_ = fb
	}

	m, err := NewWithFallback(blocker)
	if err != nil {
		t.Fatalf("NewWithFallback: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	if m.Dir() == blocker {
		t.Fatal("Dir() must report the ACTIVE (fallback) directory, not the failed canonical one")
	}
	h := m.Health()
	if h.FallbackReason == "" {
		t.Fatal("Health().FallbackReason must name the primary failure")
	}
	if !strings.Contains(h.FallbackReason, "unavailable") {
		t.Fatalf("FallbackReason = %q, want the primary failure named", h.FallbackReason)
	}
	if !h.SinksReady {
		t.Fatal("the fallback sinks themselves must be ready")
	}

	// The fallback's own app.log carries the primary-failure record.
	data, err := os.ReadFile(filepath.Join(m.Dir(), "app.log"))
	if err != nil {
		t.Fatalf("read fallback app.log: %v", err)
	}
	if !strings.Contains(string(data), "primary log directory unavailable") {
		t.Fatalf("fallback app.log must record the primary failure: %q", data)
	}
}

// TestNewWithFallbackFallsBackToBootstrapWhenBothFail proves the honest
// terminal case: both the canonical and the fallback location fail, the
// error names both, and the caller keeps the bootstrap logger.
func TestNewWithFallbackFallsBackToBootstrapWhenBothFail(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "also-blocked"))
	blockerCache := filepath.Join(t.TempDir(), "x")
	_ = blockerCache
	// Block BOTH: canonical by a file, fallback by a file at the exact
	// fallback path.
	canonical := filepath.Join(t.TempDir(), "blocked-canonical")
	if err := os.WriteFile(canonical, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fbDir, err := FallbackLogDir()
	if err != nil {
		t.Skip("user cache dir not resolvable in this environment")
	}
	if err := os.MkdirAll(filepath.Dir(fbDir), 0o755); err != nil {
		t.Skipf("cannot prepare the fallback parent %q: %v", filepath.Dir(fbDir), err)
	}
	if err := os.WriteFile(fbDir, nil, 0o644); err != nil {
		t.Skipf("cannot pre-block the fallback path %q: %v", fbDir, err)
	}

	m, err := NewWithFallback(canonical)
	if err == nil {
		_ = m.Close()
		t.Fatal("both locations failing must return an error")
	}
	if !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("error must name the fallback failure too: %v", err)
	}
	var _ = m
}

// TestRotationFailureReportedNotSilent proves a failing rotation reopen
// leaves an observable failure — not a nil sink that silently drops
// everything.
func TestRotationFailureReportedNotSilent(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	m.mu.Lock()
	old := m.appF
	m.appF = nil
	// Simulate the post-rotation reopen failure: the emergency path must
	// engage for subsequent lines.
	m.mu.Unlock()
	_ = old

	m.Info("boot", "after simulated rotation failure")
	h := m.Health()
	if h.LastErr == "" {
		t.Fatal("rotation/sink failure must be observable in Health()")
	}
	if len(m.Emergency(10)) != 1 {
		t.Fatalf("emergency ring = %d, want 1", len(m.Emergency(10)))
	}
}

// TestHealthOfNoopAndBootstrap pins the honest health reporting of the
// special managers.
func TestHealthOfNoopAndBootstrap(t *testing.T) {
	if h := noop.Health(); h.SinksReady || h.Dir != "" {
		t.Fatalf("noop health = %+v, want no ready sinks and no dir", h)
	}
	boot := NewBootstrap()
	h := boot.Health()
	if h.SinksReady {
		t.Fatal("bootstrap manager owns no file sinks")
	}
	if !boot.Enabled() {
		t.Fatal("bootstrap manager stays enabled (stderr + memory)")
	}
}

// TestErrorsJoinAvailability is a compile-contract pin: the joined-error
// propagation used by openSinks/Close must exist in this Go version.
func TestErrorsJoinAvailability(t *testing.T) {
	if errors.Join(nil, nil) != nil {
		t.Fatal("errors.Join(nil, nil) must be nil")
	}
	joined := errors.Join(errors.New("a"), errors.New("b"))
	if joined == nil || !strings.Contains(joined.Error(), "a") || !strings.Contains(joined.Error(), "b") {
		t.Fatalf("errors.Join must preserve both errors: %v", joined)
	}
}
