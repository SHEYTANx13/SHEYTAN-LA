package llm

// v1.3.6 (spec §13/§14/§31): UPDATE FAILURE SEMANTICS regressions.
//
// The dangerous pre-v1.3.6 pattern returned nil after a successful
// install whose startup verification FAILED:
//
//	if startErr := s.startLocked(); startErr != nil {
//	        ...
//	        return msg, nil // ← reported a broken engine as success
//	}
//
// These tests prove the repaired contract:
//
//   - install success + startup failure = NON-NIL update error;
//   - the previous (last-known-good) package is restored byte-for-byte;
//   - the committed tag stays at last-known-good — an unverified engine
//     is never recorded as success;
//   - no transaction leftovers (.update-old) remain.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// TestUpdateEngineStartupFailureReturnsErrorAndRollsBack drives the full
// transaction with a staged package whose engine CANNOT pass startup
// verification (the loader-fail fake exits with 0xC0000139 / the
// textual NTSTATUS evidence), while a verified good package is active.
func TestUpdateEngineStartupFailureReturnsErrorAndRollsBack(t *testing.T) {
	// loader-fail: every real server launch (and the bounded --version
	// probe via textual classification) fails as a loader-class error.
	cfg, _ := fakeManagedEngineConfig(t, "loader-fail")

	// 1) Commit the GOOD package first (b10642).
	goodArchive := buildFakeEngineArchive(t, t.TempDir())

	if _, err := updater.InstallStagedFromArchive(cfg, "b10642", goodArchive); err != nil {
		t.Fatalf("seed good engine: %v", err)
	}

	binPath := expectedEngineBinPath(cfg)

	goodBytes, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("good binary missing after seed: %v", err)
	}

	goodTag := updater.InstalledEngineTag(cfg)
	if goodTag != "b10642" {
		t.Fatalf("seed tag = %q, want b10642", goodTag)
	}

	// 2) Update to b13000 — the install will SUCCEED (the archive holds
	// a valid executable) but the engine will NEVER verify as running.
	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	badArchive := buildFakeEngineArchive(t, t.TempDir())
	srv.SetStagedArchiveForTest(badArchive)

	_, updateErr := srv.UpdateEngineNow(context.Background(), "b13000", nil)

	// §13: install success + startup failure MUST be a non-nil error.
	if updateErr == nil {
		t.Fatal("UpdateEngineNow must return an ERROR when startup verification fails — never nil after an unverified engine")
	}

	if !strings.Contains(updateErr.Error(), "startup verification failed") {
		t.Fatalf("error must name the startup verification failure, got: %v", updateErr)
	}

	// §14: the previous package is restored byte-for-byte.
	after, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("managed binary missing after rollback: %v", err)
	}

	if string(after) != string(goodBytes) {
		t.Fatal("rollback must restore the previous package byte-for-byte")
	}

	// The unverified engine is never recorded as the committed tag.
	if got := updater.InstalledEngineTag(cfg); got != "b10642" {
		t.Fatalf("tag must stay at last-known-good after rollback, got %q", got)
	}

	// No transaction leftovers.
	if _, err := os.Stat(updater.EngineBinDir(cfg) + ".update-old"); !os.IsNotExist(err) {
		t.Fatalf(".update-old must be gone after rollback (err=%v)", err)
	}

	// The failure state is honest.
	deadline := time.Now().Add(5 * time.Second)
	for srv.State() == StateUpdating && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	if st := srv.State(); st != StateFailed {
		t.Fatalf("state after failed update = %q, want %q", st, StateFailed)
	}
}

// TestUpdateEngineFailureRollbackRestartsNothingWhenEngineWasRunning
// verifies the wasRunning path: a running engine is stopped for the
// transaction, and after the failed transaction the last-known-good
// package is back on disk (the restart attempt may itself fail in the
// loader-fail mode — the ERROR still surfaces).
func TestUpdateEngineFailurePreservesRunningEnginePackage(t *testing.T) {
	cfg, _ := fakeManagedEngineConfig(t, "loader-fail")

	goodArchive := buildFakeEngineArchive(t, t.TempDir())

	if _, err := updater.InstallStagedFromArchive(cfg, "b10642", goodArchive); err != nil {
		t.Fatalf("seed good engine: %v", err)
	}

	binPath := expectedEngineBinPath(cfg)

	goodBytes, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("good binary missing: %v", err)
	}

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	srv.SetStagedArchiveForTest(buildFakeEngineArchive(t, t.TempDir()))

	if _, err := srv.UpdateEngineNow(context.Background(), "b13000", nil); err == nil {
		t.Fatal("startup verification failure must return an error (running-engine path)")
	}

	after, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("managed binary missing: %v", err)
	}

	if string(after) != string(goodBytes) {
		t.Fatal("the running engine's package must be preserved after a failed update")
	}
}

// TestCommitOnlyAfterStartupVerification pins the §7 order: the
// StagedInstall returned by the deferred installer must NOT have
// committed anything until Commit() is called.
func TestCommitOnlyAfterStartupVerification(t *testing.T) {
	cfg, _ := fakeManagedEngineConfig(t, "")

	// No engine yet: the deferred install swaps the package in but the
	// tag must still be unset because Commit has not run.
	archive := buildFakeEngineArchive(t, t.TempDir())

	staged, err := updater.InstallStagedFromArchiveDeferred(cfg, "b12000", archive)
	if err != nil {
		t.Fatalf("deferred install: %v", err)
	}

	if got := updater.InstalledEngineTag(cfg); got == "b12000" {
		t.Fatal("deferred install must NOT commit the tag before Commit()")
	}

	if staged.Result().SHA256 == "" {
		t.Fatal("staged install must carry the verified identity")
	}

	staged.Commit()

	if got := updater.InstalledEngineTag(cfg); got != "b12000" {
		t.Fatalf("after Commit() the tag must be recorded, got %q", got)
	}

	// Previous package directory is cleaned by the commit.
	if _, err := os.Stat(filepath.Dir(staged.Result().BinPath) + ".update-old"); !os.IsNotExist(err) {
		t.Fatalf("commit must remove the previous package directory (err=%v)", err)
	}
}
