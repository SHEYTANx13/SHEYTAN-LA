package config

// migrate_rename_test.go — v1.7.4 regression: the legacy-root fold commits
// every verified copy with a RENAME onto the canonical destination. On
// Windows that rename fails with "Access is denied" when another process
// (the canonical logger, under the v1.7.3 boot order) holds the
// destination open. copyVerified must classify that conflict honestly,
// clean up its temp file and leave BOTH sides intact, so the fold
// completes on the next start (restart-safe, never data loss).

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// TestCopyVerifiedClassifiesHeldOpenDestination simulates the Windows
// rename conflict through the renameForTest seam and then models a
// GENUINE restart lifecycle (v1.7.5 repair):
//
//	boot N     : the canonical sink file exists AND is held open by the
//	             previous process (exactly what the v1.7.3 boot order
//	             produced); the legacy llm.jsonl is newer, so the fold
//	             routes it through copyVerified — whose commit rename
//	             fails once with a Windows-style permission error;
//	shutdown   : the previous process EXITS and the conflicting handle
//	             is actually released (the sink is closed here);
//	boot N+1   : the retry — the NEXT genuine startup — completes the
//	             fold and removes the legacy root only after full
//	             verification.
//
// The retry previously ran while the handle was still open: on Windows
// that second rename deterministically fails with "Access is denied"
// again, so the "restart" was logically impossible — it exercised two
// boots of the SAME live process, not a restart.
func TestCopyVerifiedClassifiesHeldOpenDestination(t *testing.T) {
	unsetDataDirOverride(t)

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	legacy := seedLegacyAppDataRoot(t, localAppData, false)
	canonical := t.TempDir()

	// The v1.7.3 boot-order defect shape: the canonical logger already
	// created and OPENED <canonical>/logs/llm.jsonl before the migration.
	canonLogs := filepath.Join(canonical, "logs")
	if err := os.MkdirAll(canonLogs, 0o755); err != nil {
		t.Fatal(err)
	}
	dstLLM := filepath.Join(canonLogs, "llm.jsonl")
	const dstContent = `{"ts":"canonical-boot"}`
	if err := os.WriteFile(dstLLM, []byte(dstContent), 0o644); err != nil {
		t.Fatal(err)
	}
	sink, err := os.OpenFile(dstLLM, os.O_APPEND|os.O_WRONLY, 0o644) // held until test end
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	// The legacy llm.jsonl is NEWER with different content — the
	// newer-wins collision rule routes it through copyVerified (a verified
	// replace of the held-open destination).
	srcLLM := filepath.Join(legacy, "logs", "llm.jsonl")
	const srcContent = `{"ts":"legacy-run","model":"legacy.gguf"}`
	if err := os.WriteFile(srcLLM, []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(srcLLM, future, future); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(dstLLM, past, past); err != nil {
		t.Fatal(err)
	}

	// Windows-style conflict on the FIRST commit rename; honest behavior
	// (the real rename) afterwards.
	orig := renameForTest
	failed := false
	renameForTest = func(oldpath, newpath string) error {
		if !failed {
			failed = true
			return &os.PathError{Op: "rename", Path: newpath, Err: syscall.EACCES}
		}
		return orig(oldpath, newpath)
	}

	_, err = MigrateLegacyAppDataRoot(&Config{DataDir: canonical})
	renameForTest = orig

	if err == nil {
		t.Fatal("the simulated rename conflict must surface as a migration error")
	}
	if !strings.Contains(err.Error(), "held open") || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("error must classify the held-open rename conflict, got: %v", err)
	}

	// Temp file cleaned up; the destination survives byte-for-byte.
	if _, serr := os.Stat(dstLLM + ".migrating"); !os.IsNotExist(serr) {
		t.Fatalf("the .migrating temp file survived the failed commit: %v", serr)
	}
	data, rerr := os.ReadFile(dstLLM)
	if rerr != nil || string(data) != dstContent {
		t.Fatalf("destination corrupted by the failed commit: %v %q", rerr, data)
	}

	// The legacy source survives untouched — the fold retries next start.
	data, rerr = os.ReadFile(srcLLM)
	if rerr != nil || string(data) != srcContent {
		t.Fatalf("legacy source lost by the failed commit: %v %q", rerr, data)
	}
	if _, serr := os.Stat(legacy); serr != nil {
		t.Fatalf("legacy root must survive an incomplete fold: %v", serr)
	}

	// SHUTDOWN: the previous process exits and the conflicting sink
	// handle is released. This is the step the v1.7.4 simulation missed —
	// without it the retry below is not a restart but a second attempt
	// inside the same live process, which on Windows deterministically
	// fails with the same EACCES.
	if err := sink.Close(); err != nil {
		t.Fatalf("release the conflicting handle: %v", err)
	}

	// BOOT N+1 (the genuine restart, real rename): the fold completes, the
	// newer legacy record lands VERIFIED (never truncated, never mixed),
	// and the legacy root is removed only after full verification.
	if _, err := MigrateLegacyAppDataRoot(&Config{DataDir: canonical}); err != nil {
		t.Fatalf("the retry after the handle was released must complete the fold: %v", err)
	}
	data, rerr = os.ReadFile(dstLLM)
	if rerr != nil || string(data) != srcContent {
		t.Fatalf("canonical llm.jsonl was not folded by the retry: %v %q", rerr, data)
	}
	if _, serr := os.Stat(legacy); !os.IsNotExist(serr) {
		t.Fatalf("a completed fold must remove the legacy root: %v", serr)
	}
}

// TestMigrateLegacyAppDataRootTwiceThenLoggingOpens pins the v1.7.4 boot
// order contract end to end: the first pass folds the legacy root, the
// second pass is a clean no-op (no collisions, no changes), and the
// canonical logger then opens its sinks in the (folded) canonical logs
// directory.
func TestMigrateLegacyAppDataRootTwiceThenLoggingOpens(t *testing.T) {
	unsetDataDirOverride(t)

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	legacy := seedLegacyAppDataRoot(t, localAppData, false)

	canonical := t.TempDir()
	cfg := &Config{DataDir: canonical}

	first, err := MigrateLegacyAppDataRoot(cfg)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if !first.HasMigrated() {
		t.Fatalf("first pass did nothing: %+v", first)
	}
	if _, serr := os.Stat(legacy); !os.IsNotExist(serr) {
		t.Fatalf("legacy root must be gone after a verified fold: %v", serr)
	}

	second, err := MigrateLegacyAppDataRoot(cfg)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(second.Detected) != 0 || len(second.Merged) != 0 ||
		len(second.Recovered) != 0 || len(second.Collisions) != 0 || len(second.Removed) != 0 {
		t.Fatalf("second pass must be a clean no-op, got %+v", second)
	}

	// Migrations FIRST, sinks SECOND: logging.New must open cleanly in the
	// canonical logs dir afterwards (no conflict with a leftover fold).
	mgr, err := logging.New(filepath.Join(canonical, "logs"))
	if err != nil {
		t.Fatalf("logging.New after the migration: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("logging close: %v", err)
	}
}
