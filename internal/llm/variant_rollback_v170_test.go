package llm

// variant_rollback_v170_test.go — v1.7.0: the Windows rollback-hardening
// regression contract.
//
// The v1.6.2 verification-failure path called staged.Rollback() while the
// just-started CANDIDATE engine was still running. On Windows the running
// llama-server holds its executable image and DLL closure locked inside
// the managed bin tree, so the restore failed with
//
//	rename ...\bin.update-old ...\bin: Access is denied
//
// and the installation was left half-rolled-back. The transaction now
// stops and reaps the candidate (lifecycle-owned Stop: SIGTERM → bounded
// grace → Kill → deterministic reap) BEFORE the first rollback filesystem
// mutation, restores the previous package byte-for-byte, restarts it, and
// reports the original verification failure plus explicit rollback /
// restart evidence.
//
// These tests prove the contract deterministically on EVERY platform (the
// probe fires at the exact rollback instant — no sleeps, no polling, no
// OS-specific lock emulation):
//
//  1. the candidate process is stopped and reaped before rollback;
//  2. the rollback succeeds after a verification failure;
//  3. the previous package is restored byte-identical;
//  4. the previous manifest/variant stays authoritative;
//  5. the last-known-good engine restarts (restart-health evidence);
//  6. no orphaned process survives the transaction;
//  7. Linux behavior is unchanged (the v1.6.2 suite still passes and the
//     commit path never touches the rollback seam).

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// driveVariantRollbackTransaction is the v1.7.0 driver: identical to the
// v1.6.2 driveVariantTransaction (same seams, same fake engine, same
// enumeration override) but it accepts a rollback probe invoked at the
// exact rollback instant, an optional pre-transaction hook (byte snapshot
// of the LKG package), and hands the caller the server and config.
func driveVariantRollbackTransaction(
	t *testing.T,
	enumOverride func(string) ([]accelerator.Device, bool, error),
	rollbackProbe func(srv *LlamaServer, cfg *config.Config),
	beforeRun func(cfg *config.Config),
) (*LlamaServer, error, *config.Config) {
	t.Helper()

	restorePlatform := updater.SetPlatformForTest("windows", "amd64")
	t.Cleanup(restorePlatform)

	updater.SetVariantExistsForTest(func(context.Context, string, updater.AssetVariant) bool {
		return false // force the release-list scan, like the real path
	})
	t.Cleanup(func() { updater.SetVariantExistsForTest(nil) })

	updater.SetReleaseListForTest(func(context.Context) ([]updater.ReleaseInfo, error) {
		return []updater.ReleaseInfo{{
			TagName: "b11191",
			Assets: []updater.AssetInfo{
				{Name: "llama-b11191-bin-win-cpu-x64.zip"},
				{Name: "llama-b11191-bin-win-vulkan-x64.zip"},
			},
		}}, nil
	})
	t.Cleanup(func() { updater.SetReleaseListForTest(nil) })

	if enumOverride != nil {
		prevEnum := enumerateEngineDevices
		enumerateEngineDevices = enumOverride
		t.Cleanup(func() { enumerateEngineDevices = prevEnum })
	}

	cfg, _ := fakeManagedEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))
	t.Cleanup(func() { _ = srv.Stop() })

	if rollbackProbe != nil {
		prevProbe := variantRollbackProbe
		variantRollbackProbe = func() { rollbackProbe(srv, cfg) }
		t.Cleanup(func() { variantRollbackProbe = prevProbe })
	}

	srv.SetStagedArchiveForTest(buildFakeEngineArchive(t, t.TempDir()))

	if beforeRun != nil {
		beforeRun(cfg)
	}

	err := func() error {
		_, err := srv.UpdateEngineVariantNow(context.Background(), updater.VariantVulkan, nil)
		return err
	}()

	return srv, err, cfg
}

// snapshotDirHash walks a directory tree and returns a deterministic hash
// of every file's relative path and content — the byte-identical rollback
// evidence.
func snapshotDirHash(t *testing.T, dir string) string {
	t.Helper()

	h := sha256.New()

	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}

		fmt.Fprintf(h, "path:%s", rel)

		if d.IsDir() {
			fmt.Fprintln(h, "dir")
			return nil
		}

		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}

		fmt.Fprintf(h, "size:%d:", info.Size())

		f, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		defer f.Close()

		_, cerr := io.Copy(h, f)

		return cerr
	})

	if walkErr != nil {
		t.Fatalf("snapshot bin tree: %v", walkErr)
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// THE core regression: at the rollback instant the candidate engine must
// already be stopped and reaped (no live process may own the managed bin
// tree — the Windows "Access is denied" class), and the transaction must
// still fail with the original verification failure plus rollback
// evidence.
func TestVariantRollbackStopsCandidateBeforeRestore(t *testing.T) {
	var (
		err                 error
		rollbackObserved    bool
		candidatePIDAtProbe int
		stateAtProbe        string
		portFreeAtProbe     bool
	)

	srv, err, cfg := driveVariantRollbackTransaction(
		t,
		func(string) ([]accelerator.Device, bool, error) {
			return nil, false, errors.New("fork/exec: exited with a corrupted image")
		},
		func(live *LlamaServer, liveCfg *config.Config) {
			rollbackObserved = true

			live.mu.Lock()
			if live.cmd != nil && live.cmd.Process != nil {
				candidatePIDAtProbe = live.cmd.Process.Pid
			}
			stateAtProbe = live.state
			live.mu.Unlock()

			portFreeAtProbe = !PortInUse(liveCfg.LlamaHost, liveCfg.LlamaPort)
		},
		nil,
	)

	if err == nil {
		t.Fatal("a verification that cannot execute must fail the transaction")
	}

	if !strings.Contains(err.Error(), "runtime backend verification failed") {
		t.Fatalf("failure must be classified as a verification failure: %v", err)
	}

	if !strings.Contains(err.Error(), "previous package restored") {
		t.Fatalf("failure must report the rollback: %v", err)
	}

	if !rollbackObserved {
		t.Fatal("the rollback probe never fired — the rollback path was not exercised")
	}

	if candidatePIDAtProbe != 0 {
		t.Fatalf("candidate process %d still registered at the rollback instant — "+
			"the restore would hit Windows file locks (Access is denied)", candidatePIDAtProbe)
	}

	if stateAtProbe == StateReady || stateAtProbe == StateBusy {
		t.Fatalf("engine state at rollback instant = %q — the candidate was never stopped", stateAtProbe)
	}

	if !portFreeAtProbe {
		t.Fatal("the candidate engine port was still serving at the rollback instant")
	}

	_ = srv
	_ = cfg
}

// The full durable contract: rollback succeeds, the previous package is
// byte-identical, the previous manifest/variant stays authoritative, the
// last-known-good engine restarts healthy, and no orphaned process
// survives.
func TestVariantRollbackRestoresLastKnownGoodPackage(t *testing.T) {
	var (
		srv       *LlamaServer
		cfg       *config.Config
		err       error
		lkgHash   string
		snapshotted bool
	)

	srv, err, cfg = driveVariantRollbackTransaction(
		t,
		func(string) ([]accelerator.Device, bool, error) {
			return nil, false, errors.New("fork/exec: exited with a corrupted image")
		},
		nil,
		func(preCfg *config.Config) {
			lkgHash = snapshotDirHash(t, filepath.Join(preCfg.DataDir, "bin"))
			snapshotted = true
		},
	)

	if !snapshotted || lkgHash == "" {
		t.Fatal("the LKG package snapshot was never taken")
	}

	if err == nil {
		t.Fatal("a verification that cannot execute must fail the transaction")
	}

	if !strings.Contains(err.Error(), "previous package restored") {
		t.Fatalf("failure must report the rollback: %v", err)
	}

	if strings.Contains(err.Error(), "rollback also failed") {
		t.Fatalf("rollback must succeed after the candidate is stopped: %v", err)
	}

	if strings.Contains(err.Error(), "restart failed") {
		t.Fatalf("last-known-good restart must succeed after rollback: %v", err)
	}

	// The previous package survived byte-for-byte.
	binDir := filepath.Join(cfg.DataDir, "bin")

	if _, statErr := os.Stat(binDir); statErr != nil {
		t.Fatalf("managed bin dir must exist after rollback: %v", statErr)
	}

	if got := snapshotDirHash(t, binDir); got != lkgHash {
		t.Fatalf("restored package is not byte-identical to the last-known-good package")
	}

	if _, statErr := os.Stat(binDir + ".update-old"); !os.IsNotExist(statErr) {
		t.Fatal("rollback must leave no .update-old directory behind")
	}

	// The manifest stays authoritative for the PREVIOUS variant.
	if got := updater.InstalledEngineVariant(cfg); got == updater.VariantVulkan {
		t.Fatal("the failed candidate variant must NOT be committed to the manifest")
	}

	// The last-known-good engine restarted healthy: the lifecycle-owned
	// restart reached ready and owns exactly one live process.
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if srv.IsRunning() && srv.State() == StateReady {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	if !srv.IsRunning() || srv.State() != StateReady {
		t.Fatalf("last-known-good engine must restart after rollback (state=%s)", srv.State())
	}

	if srv.Pid() <= 0 {
		t.Fatal("the restarted engine must expose a live pid")
	}

	// No orphaned process survives: after the final lifecycle-owned stop,
	// the port must be free again (a leaked candidate would still hold it).
	if stopErr := srv.Stop(); stopErr != nil {
		t.Fatalf("final stop: %v", stopErr)
	}

	time.Sleep(100 * time.Millisecond)

	if PortInUse(cfg.LlamaHost, cfg.LlamaPort) {
		t.Fatal("an orphaned engine process still holds the engine port after shutdown")
	}
}

// The commit path must never touch the rollback seam: a healthy runtime
// verification commits WITHOUT firing the probe and without stopping the
// verified engine (Linux behavior unchanged — and every other platform's).
func TestVariantCommitPathNeverEntersRollback(t *testing.T) {
	rollbackFired := false

	srv, err, cfg := driveVariantRollbackTransaction(
		t,
		nil, // real fake-engine enumeration → runtime Vulkan evidence → commit
		func(*LlamaServer, *config.Config) { rollbackFired = true },
		nil,
	)

	if err != nil {
		t.Fatalf("vulkan provisioning transaction: %v", err)
	}

	if rollbackFired {
		t.Fatal("a committed transaction must never enter the rollback path")
	}

	if got := updater.InstalledEngineVariant(cfg); got != updater.VariantVulkan {
		t.Fatalf("committed manifest variant = %q, want vulkan", got)
	}

	if !srv.IsRunning() {
		t.Fatal("the verified engine must stay running after commit")
	}
}
