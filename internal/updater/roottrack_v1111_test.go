package updater

// roottrack_v1111_test.go — v1.11.1 (run 43995689377) P0 regression
// coverage for the engine-swap handle lifecycle.
//
// THE OBSERVED FAILURE this file pins: the Windows smoke app's engine
// maintenance failed at
//
//	stage engine swap: rename of data\bin to data\bin.update-old
//	                     returned Access is denied
//
// …then validation of the previous engine failed (llama-server.exe was
// never installed by the failed transaction's package), maintenance
// entered BLOCKED with update-failed-engine-unusable and prewarm was
// refused — while the smoke gate still printed a green SMOKE OK.
//
// The swap rename fails on Windows whenever ANY handle is open on the
// engine directory or a descendant. The installer itself held exactly
// such a handle: an os.Root on <binDir>/.engine-download deferred-closed
// across the transaction body. The tests here prove:
//
//  1. the quiesce gate REFUSES the swap while such a root is open
//     (deterministic on every OS via the registry — the refusal happens
//     before any mutation) and the previous engine survives byte-for-byte;
//  2. once the handle is closed, the SAME transaction succeeds — the fix
//     is the handle lifecycle, not a weakened swap;
//  3. the registry unregisters on Close (no phantom leaks across the
//     package's own transactions);
//  4. a genuine external rename failure is classified (sharing violation
//     vs access denied vs raw) instead of surfacing as a bare mystery
//     error.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

// seedOldEngineV1111 builds the pre-update state the smoke app had: a
// managed bin dir with an engine binary, a manifest-only identity marker
// and a native-host companion.
func seedOldEngineV1111(t *testing.T, cfg *config.Config) (binDir, hostPayload string) {
	t.Helper()

	binDir = EngineBinDir(cfg)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	name := "llama-server"
	if filepath.Ext(testBin) == ".exe" {
		name = "llama-server.exe"
	}

	if err := copyFileMode(testBin, filepath.Join(binDir, name), 0o755); err != nil {
		t.Fatalf("seed old engine: %v", err)
	}

	hostPayload = strings.Repeat("native host payload v1.11.1 ", 512)

	if err := os.WriteFile(filepath.Join(binDir, "shtn-engine-host.exe"), []byte(hostPayload), 0o755); err != nil {
		t.Fatalf("seed native host: %v", err)
	}

	write(t, filepath.Join(binDir, "engine-install.json"), `{"tag":"b10642","sha256":"x"}`)

	return binDir, hostPayload
}

// TestSwapRefusedWhileRootHandleOpenUnderBinDir is THE run-43995689377
// regression fixture: an os.Root on <binDir>/.engine-download — exactly
// the handle the installer used to leak across the swap — is still open
// when the transaction reaches the directory swap.
func TestSwapRefusedWhileRootHandleOpenUnderBinDir(t *testing.T) {
	cfg := v137Config(t)
	binDir, hostPayload := seedOldEngineV1111(t, cfg)

	// The leaked-handle shape: a tracked root on the engine download
	// staging directory, OPEN across the transaction.
	dlDir := filepath.Join(binDir, ".engine-download")
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		t.Fatalf("download staging dir: %v", err)
	}

	leak, err := openTrackedRoot(dlDir)
	if err != nil {
		t.Fatalf("open the retained root: %v", err)
	}
	defer leak.Close()

	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	staged, err := InstallStagedFromArchiveDeferred(cfg, "b12445", archive)
	if !errors.Is(err, errSwapBlockedByOpenRoot) {
		t.Fatalf("the swap must be REFUSED with the typed retained-handle error while the root is open — got %v", err)
	}
	if staged != nil {
		t.Fatal("a refused swap must not return a staged install")
	}

	// The refusal happened BEFORE any mutation: the previous engine is
	// intact byte-for-byte, no .update-old residue, no staging tree.
	if b, rerr := os.ReadFile(filepath.Join(binDir, "shtn-engine-host.exe")); rerr != nil || string(b) != hostPayload {
		t.Fatalf("the previous engine must survive the refused swap untouched: err=%v", rerr)
	}
	if b, rerr := os.ReadFile(filepath.Join(binDir, "engine-install.json")); rerr != nil || !strings.Contains(string(b), "b10642") {
		t.Fatalf("the previous install manifest must survive: err=%v", rerr)
	}
	if _, serr := os.Stat(binDir + ".update-old"); !os.IsNotExist(serr) {
		t.Fatal("a refused swap must leave no .update-old directory")
	}
	if _, serr := os.Stat(filepath.Join(binDir, ".update-stage")); !os.IsNotExist(serr) {
		t.Fatal("a refused swap must clean its staging tree")
	}

	// Releasing the handle unblocks the SAME transaction: the fix is the
	// handle lifecycle, never a weakened swap.
	if err := leak.Close(); err != nil {
		t.Fatalf("close the retained root: %v", err)
	}

	staged, err = InstallStagedFromArchiveDeferred(cfg, "b12445", archive)
	if err != nil {
		t.Fatalf("after Close the identical transaction must succeed: %v", err)
	}

	staged.Commit()

	if b, rerr := os.ReadFile(filepath.Join(binDir, "shtn-engine-host.exe")); rerr != nil || string(b) != hostPayload {
		t.Fatalf("the committed package must carry the native host: err=%v", rerr)
	}
	if _, merr := os.Stat(filepath.Join(binDir, "llama-server")); merr != nil && os.IsNotExist(merr) && filepath.Ext(os.Args[0]) != ".exe" {
		t.Fatalf("the new engine binary must be installed: %v", merr)
	}
}

// TestRegistryUnregistersOnClose pins the registry primitive itself: a
// closed root is never reported as a leak, and roots outside the probed
// directory do not interfere.
func TestRegistryUnregistersOnClose(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	if leaks := openRootsUnder(dir); len(leaks) != 0 {
		t.Fatalf("fresh state must report no leaks, got %v", leaks)
	}

	root, err := openTrackedRoot(dir)
	if err != nil {
		t.Fatalf("open tracked root: %v", err)
	}

	outside, err := openTrackedRoot(other)
	if err != nil {
		t.Fatalf("open outside root: %v", err)
	}

	if leaks := openRootsUnder(dir); len(leaks) != 1 || leaks[0] != filepath.Clean(dir) {
		t.Fatalf("the open root under dir must be reported, got %v", leaks)
	}
	if leaks := openRootsUnder(other); len(leaks) != 1 {
		t.Fatalf("the outside root must be reported under its own dir, got %v", leaks)
	}

	if err := root.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if leaks := openRootsUnder(dir); len(leaks) != 0 {
		t.Fatalf("a closed root must never be reported as a leak, got %v", leaks)
	}

	if err := outside.Close(); err != nil {
		t.Fatalf("close outside: %v", err)
	}

	// Double close is a no-op (defensive callers).
	if err := root.Close(); err != nil {
		t.Fatalf("double close must be safe: %v", err)
	}
}

// TestSwapRenameErrorClassification pins the diagnostics wrapper: an
// external rename failure is attributed (access denied vs sharing
// violation vs raw) so the maintenance log can distinguish retained
// handles, AV/indexer contention and other causes — without retrying,
// disabling protections or mutating the previous engine.
func TestSwapRenameErrorClassification(t *testing.T) {
	// A plain error passes through with the operation context only.
	raw := errors.New("no such file or directory")
	if got := swapRenameContext("rename aside", raw); got == nil || !strings.Contains(got.Error(), "rename aside") || !strings.Contains(got.Error(), "no such file or directory") {
		t.Fatalf("raw error must be wrapped with context only, got %v", got)
	}

	// The win32 code table is only interpreted by Windows binaries —
	// on other platforms Errno(5) is a different error (EIO on Linux),
	// so the same inputs must pass through unclassified.
	if runtime.GOOS == "windows" {
		// win32 ERROR_ACCESS_DENIED (5): the run-43995689377 class.
		denied := &os.LinkError{Op: "rename", Old: "data\\bin", New: "data\\bin.update-old", Err: winErrAccessDenied}
		if got := swapRenameContext("rename aside", denied); !strings.Contains(got.Error(), "ERROR_ACCESS_DENIED") || !strings.Contains(got.Error(), "descendant") {
			t.Fatalf("access denied must be classified with the handle-retention hint, got %v", got)
		}

		// win32 ERROR_SHARING_VIOLATION (32): external handle contention.
		shared := &os.LinkError{Op: "rename", Old: "data\\bin", New: "data\\bin.update-old", Err: winErrSharingViolation}
		if got := swapRenameContext("rename aside", shared); !strings.Contains(got.Error(), "ERROR_SHARING_VIOLATION") || !strings.Contains(got.Error(), "antivirus") {
			t.Fatalf("sharing violation must be classified with the external-handle hint, got %v", got)
		}

		// A different errno keeps its raw classification.
		otherErr := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.Errno(123)}
		if got := swapRenameContext("rename aside", otherErr); strings.Contains(got.Error(), "ERROR_ACCESS_DENIED") || strings.Contains(got.Error(), "ERROR_SHARING_VIOLATION") {
			t.Fatalf("an unrelated errno must not be misclassified, got %v", got)
		}
	} else {
		denied := &os.LinkError{Op: "rename", Old: "data\\bin", New: "data\\bin.update-old", Err: winErrAccessDenied}
		if got := swapRenameContext("rename aside", denied); strings.Contains(got.Error(), "ERROR_ACCESS_DENIED") {
			t.Fatalf("non-windows binaries must not interpret win32 codes (Errno(5) is EIO here), got %v", got)
		}
	}
}

// TestQuiesceGateSeesExtractionRootsDuringExtract proves the gate would
// catch a hypothetical future regression where an extraction root is held
// across the swap: the registry sees roots under the STAGING path (which
// lives inside binDir) while open.
func TestQuiesceGateSeesExtractionRootsDuringExtract(t *testing.T) {
	cfg := v137Config(t)
	binDir := EngineBinDir(cfg)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	stageDir := filepath.Join(binDir, ".update-stage")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("stage dir: %v", err)
	}

	root, err := openTrackedRoot(stageDir)
	if err != nil {
		t.Fatalf("open extraction root: %v", err)
	}

	// The gate must refuse: the extraction root is under binDir.
	if err := assertSwapQuiesce(binDir); !errors.Is(err, errSwapBlockedByOpenRoot) {
		t.Fatalf("the gate must refuse while an extraction root is open under binDir, got %v", err)
	}

	if err := root.Close(); err != nil {
		t.Fatalf("close extraction root: %v", err)
	}

	if err := assertSwapQuiesce(binDir); err != nil {
		t.Fatalf("quiesced bin dir must pass the gate, got %v", err)
	}
}
