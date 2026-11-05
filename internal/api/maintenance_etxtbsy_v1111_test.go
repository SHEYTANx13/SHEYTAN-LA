package api

// maintenance_etxtbsy_v1111_test.go — v1.11.1 regression for the
// CONFIRMED Linux CI failure (Fyne UI gates (Linux), run 41933333132):
//
//      --- FAIL: TestMaintenanceGateGenuinelyNewerTargetStillUpdatesCorruptState
//      maintenance_identity_v176_test.go:142: write binary:
//          open .../bin/llama-server: text file busy
//      maintenance_identity_v176_test.go:154: timed out waiting for prewarm
//
// Root cause (proven here, not hypothesized): the fixture committed the
// managed engine binary with os.WriteFile — an O_WRONLY|O_CREATE|O_TRUNC
// open at the LIVE executable path. Linux refuses exactly that open with
// ETXTBSY whenever any process holds the file open for execution. The
// second assertion (the missing prewarm) is not an independent defect:
// the t.Fatalf inside the gate-owned transaction goroutine terminated
// that goroutine via runtime.Goexit, so the gate never reached a
// terminal phase and the prewarm event could never fire — the test then
// degraded into the misleading 15 s timeout on top of the real error.
//
// The production installer has always swapped STAGED content into the
// managed bin directory through directory-entry renames; the test
// fixture now uses the same file-level transaction (unique sibling temp
// file → fsync → close → os.Rename). A rename replaces the directory
// entry and never needs write access to the target inode, so it cannot
// fail with ETXTBSY: a process executing the old image keeps running
// against the old inode.
//
// Windows note: os.Rename maps to MoveFileEx(REPLACE_EXISTING) and is
// the correct idle-path replacement there; a RUNNING engine image on
// Windows is never overwritten at all — the updater's transactional
// stop-before-swap (internal/updater + internal/installer directory
// swap, which has its own tests) remains the authority. The ETXTBSY
// syscall semantics below are Linux-specific, so this reproduction runs
// on linux/amd64 only.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/downloader"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/updater"
)

// TestCommitEngineTransactionSwapsExecutingBinary is the deterministic
// minimal reproduction of the CI failure: a REAL long-lived process
// executes FROM bin/llama-server while the fixture commits a new engine
// version. The O_TRUNC write the old helper used fails with ETXTBSY —
// asserted here against the kernel itself — while the atomic staged
// rename succeeds and the executing process is untouched.
func TestCommitEngineTransactionSwapsExecutingBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ETXTBSY is the Linux exclusive-open-for-exec semantics; Windows replaces a running engine image only through the updater's stop-before-swap transaction (covered by the installer/updater swap tests)")
	}

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")

	binDir := updater.EngineBinDir(cfg)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	// A REAL executable at the managed path: the test binary itself
	// re-executed as the fake llama.cpp engine (the same established
	// re-exec dispatch as fakellama_v181_test.go — a genuine ELF that
	// the kernel holds open for execution while it runs).
	live, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary path: %v", err)
	}

	if err := copyFileExact(live, filepath.Join(binDir, "llama-server"), 0o755); err != nil {
		t.Fatalf("stage real executable at the managed path: %v", err)
	}

	// Launch it as a LONG-LIVED engine subprocess FROM that exact path.
	port := freePortForProbe(t)

	cmd := exec.Command(filepath.Join(binDir, "llama-server"), "--port", fmt.Sprint(port))
	cmd.Env = append(os.Environ(), "SHEYTAN_FAKE_LLAMA=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		t.Fatalf("start executing fixture engine: %v", err)
	}

	// Single lifecycle owner: THIS test owns Start and Wait. Stop is
	// deterministic (Kill + Wait), never a leak.
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	pid := cmd.Process.Pid

	// The kernel's own record: the process executes FROM the managed
	// path (work-order §3.3 — never process-name matching).
	exeLink, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		t.Fatalf("read /proc/%d/exe: %v", pid, err)
	}
	if !strings.HasSuffix(exeLink, filepath.Join(binDir, "llama-server")) {
		t.Fatalf("subprocess executes from %q, want the managed path %q", exeLink, filepath.Join(binDir, "llama-server"))
	}

	// THE MECHANISM, asserted against the kernel: while the process
	// executes the file, the O_TRUNC open the old helper performed is
	// refused with ETXTBSY. os.WriteFile would have failed exactly
	// here ("write binary: open ...: text file busy").
	busy, openErr := os.OpenFile(filepath.Join(binDir, "llama-server"), os.O_WRONLY|os.O_TRUNC, 0)
	if openErr == nil {
		busy.Close()
		t.Fatal("expected O_TRUNC on the executing binary to fail with ETXTBSY — the reproduction precondition is gone")
	}
	if !errors.Is(openErr, syscall.ETXTBSY) {
		t.Fatalf("O_TRUNC on the executing binary: got %v, want ETXTBSY", openErr)
	}

	// The FIX: the transactional commit must succeed WHILE the process
	// executes the old image — staged temp file + rename swaps the
	// directory entry without touching the executing inode.
	if err := commitEngineTransactionErr(cfg, "b99999"); err != nil {
		t.Fatalf("commit engine transaction over an EXECUTING binary: %v (the ETXTBSY regression)", err)
	}

	// The swap took effect at the live path...
	content, err := os.ReadFile(filepath.Join(binDir, "llama-server"))
	if err != nil {
		t.Fatalf("read swapped binary: %v", err)
	}
	if string(content) != "fake llama-server b99999" {
		t.Fatalf("swapped binary content = %q, want the new version payload", content)
	}

	// ...the executing process survived (old inode semantics)...
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("the swap disturbed the executing process: %v", err)
	}

	// ...and the staged temp file left no residue in the bin directory.
	if err := assertNoStagedResidue(binDir); err != nil {
		t.Fatalf("staged temp residue: %v", err)
	}

	// Deterministic stop + reap: the process is stopped and waited for
	// BEFORE the path would ever be rewritten by a non-transactional
	// tool (the lifecycle order the production updater enforces).
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill fixture engine: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Log("fixture engine exited cleanly on kill")
	}

	// After the stop+reap the same path is freely writable — and the
	// transactional commit still yields the identical result.
	if err := commitEngineTransactionErr(cfg, "b11223"); err != nil {
		t.Fatalf("commit after stop+reap: %v", err)
	}
}

// TestCommitEngineTransactionAtomicSwapWhileIdle pins the cross-platform
// contract of the transactional fixture commit: the live path always
// holds either the old or the new full content, and no staged temp file
// survives the commit. Runs everywhere the suite runs.
func TestCommitEngineTransactionAtomicSwapWhileIdle(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")

	if err := commitEngineTransactionErr(cfg, "b1"); err != nil {
		t.Fatalf("first commit: %v", err)
	}

	bin := filepath.Join(updater.EngineBinDir(cfg), "llama-server")

	if content, err := os.ReadFile(bin); err != nil || string(content) != "fake llama-server b1" {
		t.Fatalf("first commit content = %q (%v)", content, err)
	}

	if err := commitEngineTransactionErr(cfg, "b2"); err != nil {
		t.Fatalf("second commit: %v", err)
	}

	if content, err := os.ReadFile(bin); err != nil || string(content) != "fake llama-server b2" {
		t.Fatalf("second commit content = %q (%v)", content, err)
	}

	if err := assertNoStagedResidue(updater.EngineBinDir(cfg)); err != nil {
		t.Fatalf("staged temp residue: %v", err)
	}
}

// TestTransactionErrorSettlesGateAndSurfaces pins the second half of the
// CI signature: a transaction error must reach the gate and settle it —
// the prewarm contract then decides (permitted for FAILED) — and the
// test must observe the REAL error immediately, never a 15 s prewarm
// timeout masking the cause.
func TestTransactionErrorSettlesGateAndSurfaces(t *testing.T) {
	h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")

	events := newOrderedEvents()
	events = h.wire(
		events,
		func(ctx context.Context) (string, error) { return "b12000", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			// Fail the transaction deterministically — a poisoned bin
			// path (bin is a FILE) makes the staged commit fail before
			// any write. The callback NEVER calls t.Fatal: a Fatal here
			// would Goexit the gate goroutine — the very defect this
			// regression exists to prevent.
			binDir := updater.EngineBinDir(h.cfg)

			if err := os.RemoveAll(binDir); err != nil {
				return "", fmt.Errorf("clear bin dir: %w", err)
			}
			if err := os.WriteFile(binDir, []byte("not a directory"), 0o644); err != nil {
				return "", fmt.Errorf("poison bin path: %w", err)
			}

			err := commitEngineTransactionErr(h.cfg, "b12000")
			if err == nil {
				return "", errors.New("expected the poisoned bin path to fail the transaction")
			}

			return "", err
		},
		// The installed (last-known-good) engine stays statically valid:
		// the gate must classify the failure as FAILED — the usable-
		// engine case, which PERMITS the prewarm — not BLOCKED.
		func() error { return nil },
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	// The gate must REACH A TERMINAL PHASE on its own (FAILED — the
	// installed engine stays usable), releasing the prewarm decision.
	// Against the pre-fix behavior the gate goroutine died mid-
	// transaction and this wait burned the full 15 s deadline.
	events.waitUntil(t, events.has("prewarm"), "prewarm release after the gate settled a failed transaction")

	st := h.srv.gateValue().Snapshot()
	if st.Phase != PhaseFailed {
		t.Fatalf("phase = %s, want FAILED — a transaction error must settle the gate, not kill it", st.Phase)
	}
}

// copyFileExact copies src to dst byte-for-byte with the requested mode.
func copyFileExact(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}

	if err := out.Close(); err != nil {
		return err
	}

	return os.Chmod(dst, mode)
}

// assertNoStagedResidue fails when any staged temp file from the
// transactional commit survived in dir.
func assertNoStagedResidue(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".llama-server-") && strings.HasSuffix(e.Name(), ".new") {
			return fmt.Errorf("staged temp file survived: %s", e.Name())
		}
	}

	return nil
}

// freePortForProbe reserves an unused local TCP port.
func freePortForProbe(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}
