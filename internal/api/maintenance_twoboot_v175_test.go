package api

// maintenance_twoboot_v175_test.go — v1.7.5 (§6): ENGINE INSTALL/UPDATE
// IDEMPOTENCY at the STARTUP MAINTENANCE GATE level.
//
// The v1.7.4 identity machinery (installer identity preservation, manifest
// fallback, EffectiveInstalledEngineTag) is proven at the unit level by
// internal/updater and internal/installer. The test THIS file adds is the
// END-TO-END two-boot proof the release contract requires:
//
//	Boot #1: a genuinely older engine; the gate runs the (seamed)
//	         transaction to b11223; the transaction commits the real
//	         identity artifacts (binary + engine-install.json manifest +
//	         installed.json engineTag) into the SHARED data root.
//	Boot #2: the SAME data root, a FRESH Server (a genuine process
//	         restart); normal maintenance must recognize the committed
//	         identity and complete with ZERO redundant download/install
//	         transactions.
//
// Plus the identity fallback matrix, driven through the SAME gate decision
// the production boot uses:
//
//   - state file without identity + valid manifest  → current, no transaction;
//   - missing manifest + valid state identity       → current, no transaction;
//   - both identity sources missing                 → honest transaction
//     (currency cannot be proven — the updater MUST install);
//   - target tag genuinely newer                    → transaction runs, targeting it;
//   - binary path changed                           → the installer seeds the
//     identity from the manifest beside the NEW binary, and the gate then
//     decides "current" from it (no transaction).
//
// Deterministic throughout: channel/condvar synchronization via the shared
// orderedEvents harness, no sleeps, no network.

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/installer"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// newTwoBootServer is newMaintenanceTestServer with a FIXED data root, so
// two consecutive boots share one state/manifest/bin tree.
func newTwoBootServer(t *testing.T, dataDir, model string) *maintenanceTestHarness {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.ModelsDir = dataDir + "/models"
	cfg.SessionsDir = dataDir + "/sessions"
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = true
	cfg.UpdateSchedule = "daily"
	cfg.LastUpdateCheck = "2020-01-01T00:00:00Z" // long expired → due every boot
	cfg.Model = model
	cfg.LlamaBinPath = "" // the managed bin dir under the shared data root

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	t.Cleanup(srv.Close)

	return &maintenanceTestHarness{srv: srv, cfg: cfg}
}

// commitEngineTransaction simulates EXACTLY what a real engine install
// commit leaves behind: the managed binary, the engine-install.json
// manifest beside it, and the recorded tag in installed.json.
func commitEngineTransaction(t *testing.T, cfg *config.Config, tag string) {
	t.Helper()

	binDir := updater.EngineBinDir(cfg)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	bin := filepath.Join(binDir, "llama-server")

	if err := os.WriteFile(bin, []byte("fake llama-server "+tag), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	manifest := `{"tag":"` + tag + `","sha256":"deadbeef","size":1024,"source":"test"}`

	if err := os.WriteFile(filepath.Join(binDir, "engine-install.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	updater.RecordEngineTag(cfg, tag)
}

// TestMaintenanceTwoBootsDoNotRedownloadCommittedEngine — the §6 two-boot
// idempotency proof over ONE shared data root.
func TestMaintenanceTwoBootsDoNotRedownloadCommittedEngine(t *testing.T) {
	dataRoot := t.TempDir()

	// ---- BOOT #1: genuinely older engine → the transaction commits b11223.
	boot1 := newTwoBootServer(t, dataRoot, "fake-model.gguf")

	var boot1Transactions atomic.Int32

	events1 := newOrderedEvents()
	events1 = boot1.wire(
		events1,
		func(ctx context.Context) (string, error) { return "b11223", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			boot1Transactions.Add(1)

			if tag != "b11223" {
				t.Errorf("boot 1 transaction targeted %q, want b11223", tag)
			}

			commitEngineTransaction(t, boot1.cfg, tag)

			return "engine updated to llama.cpp " + tag, nil
		},
		nil,
		nil,
	)

	if err := boot1.srv.EnsureSetup(); err != nil {
		t.Fatalf("boot 1 EnsureSetup: %v", err)
	}

	events1.waitUntil(t, events1.has("prewarm"), "boot 1 prewarm after the update")

	st1 := boot1.srv.gateValue().Snapshot()

	if st1.Phase != PhaseReadyForPrewarm || !st1.Updated {
		t.Fatalf("boot 1: phase=%s updated=%v — the transaction must complete before prewarm", st1.Phase, st1.Updated)
	}

	if got := boot1Transactions.Load(); got != 1 {
		t.Fatalf("boot 1: %d install transactions, want exactly 1", got)
	}

	// The committed identity must be readable by the NEXT boot's decision.
	if tag := updater.EffectiveInstalledEngineTag(boot1.cfg); tag != "b11223" {
		t.Fatalf("boot 1: committed identity = %q, want b11223", tag)
	}

	// ---- BOOT #2: a genuine restart (fresh Server, SAME data root).
	boot2 := newTwoBootServer(t, dataRoot, "fake-model.gguf")

	var boot2Transactions atomic.Int32

	events2 := newOrderedEvents()
	events2 = boot2.wire(
		events2,
		func(ctx context.Context) (string, error) { return "b11223", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			boot2Transactions.Add(1) // a REDUNDANT download/install — must never happen

			return "engine updated to llama.cpp " + tag, nil
		},
		nil,
		nil,
	)

	if err := boot2.srv.EnsureSetup(); err != nil {
		t.Fatalf("boot 2 EnsureSetup: %v", err)
	}

	events2.waitUntil(t, events2.has("prewarm"), "boot 2 prewarm after a no-op maintenance")

	st2 := boot2.srv.gateValue().Snapshot()

	if st2.Phase != PhaseReadyForPrewarm {
		t.Fatalf("boot 2: phase = %s, want READY_FOR_PREWARM", st2.Phase)
	}

	if st2.Reason != "current" {
		t.Fatalf("boot 2: reason = %q, want \"current\" — the gate re-decided an update was required (detail: %s)", st2.Reason, st2.Detail)
	}

	if st2.Updated || st2.TargetTag != "" {
		t.Fatalf("boot 2: no update may run: updated=%v targetTag=%q", st2.Updated, st2.TargetTag)
	}

	if got := boot2Transactions.Load(); got != 0 {
		t.Fatalf("boot 2: %d redundant engine transactions — a committed b11223 engine was re-downloaded", got)
	}
}

// TestMaintenanceGateIdentityFallbackMatrix drives the gate decision
// through every identity-evidence combination §6 names.
func TestMaintenanceGateIdentityFallbackMatrix(t *testing.T) {
	const latest = "b11223"

	t.Run("state without identity but manifest valid → current, no transaction", func(t *testing.T) {
		h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")
		commitEngineTransaction(t, h.cfg, latest)

		// Wipe the recorded identity from installed.json (keep the shape a
		// detection rewrite produces: status + path, no tag/version).
		binDir := updater.EngineBinDir(h.cfg)
		tagless := `{"appVersion":"1.7.4","components":{"llamaServer":{"status":"installed","meta":{"path":"` +
			filepath.Join(binDir, "llama-server") + `"}}}}`

		if err := os.WriteFile(h.cfg.StatePath(), []byte(tagless), 0o644); err != nil {
			t.Fatalf("write tagless state: %v", err)
		}

		assertGateCurrentWithoutTransaction(t, h, latest)
	})

	t.Run("manifest missing but state valid → current, no transaction", func(t *testing.T) {
		h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")
		commitEngineTransaction(t, h.cfg, latest)

		if err := os.Remove(filepath.Join(updater.EngineBinDir(h.cfg), "engine-install.json")); err != nil {
			t.Fatalf("remove manifest: %v", err)
		}

		assertGateCurrentWithoutTransaction(t, h, latest)
	})

	t.Run("both identity sources missing → honest transaction", func(t *testing.T) {
		h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")

		// No manifest, no state file: currency CANNOT be proven — the
		// updater must run the transaction rather than assume the best.
		var ran atomic.Bool

		events := newOrderedEvents()
		events = h.wire(
			events,
			func(ctx context.Context) (string, error) { return latest, nil },
			func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
				ran.Store(true)

				if tag != latest {
					t.Errorf("transaction targeted %q, want %q (the bundled default cannot claim currency)", tag, latest)
				}

				commitEngineTransaction(t, h.cfg, tag)

				return "engine updated to llama.cpp " + tag, nil
			},
			nil,
			nil,
		)

		if err := h.srv.EnsureSetup(); err != nil {
			t.Fatalf("EnsureSetup: %v", err)
		}

		events.waitUntil(t, events.has("prewarm"), "prewarm after the honest transaction")

		if !ran.Load() {
			t.Fatal("with no identity evidence the gate skipped the transaction — it cannot know the engine is current")
		}

		st := h.srv.gateValue().Snapshot()

		if st.Phase != PhaseReadyForPrewarm || !st.Updated {
			t.Fatalf("phase=%s updated=%v — the honest transaction must complete", st.Phase, st.Updated)
		}
	})

	t.Run("target tag genuinely newer → transaction runs targeting it", func(t *testing.T) {
		h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")
		commitEngineTransaction(t, h.cfg, latest)

		var targeted atomic.Bool

		events := newOrderedEvents()
		events = h.wire(
			events,
			func(ctx context.Context) (string, error) { return "b12000", nil },
			func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
				targeted.Store(true)

				if tag != "b12000" {
					t.Errorf("transaction targeted %q, want b12000", tag)
				}

				commitEngineTransaction(t, h.cfg, tag)

				return "engine updated to llama.cpp " + tag, nil
			},
			nil,
			nil,
		)

		if err := h.srv.EnsureSetup(); err != nil {
			t.Fatalf("EnsureSetup: %v", err)
		}

		events.waitUntil(t, events.has("prewarm"), "prewarm after the newer-target transaction")

		if !targeted.Load() {
			t.Fatal("a genuinely newer release must be downloaded/installed")
		}
	})

	t.Run("binary path changed → identity reseeded from the new manifest, gate current", func(t *testing.T) {
		h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")

		// Boot 1 committed b11223 at the managed path.
		commitEngineTransaction(t, h.cfg, latest)

		// The binary then GENUINELY changed to a different file (user
		// dropped in their own build / LKG restore): the recorded tag
		// belongs to the OLD file.
		newBin := filepath.Join(updater.EngineBinDir(h.cfg), "llama-server-next")

		if err := os.WriteFile(newBin, []byte("fake llama-server next"), 0o755); err != nil {
			t.Fatalf("write replacement binary: %v", err)
		}

		// The installer identity merge must seed the NEW state's tag from
		// the manifest beside the NEW binary (b11223), not carry the old
		// recorded identity blindly.
		m := installer.New(h.cfg)

		if _, _, err := m.EnsureRun(false); err != nil {
			t.Fatalf("EnsureRun after binary change: %v", err)
		}

		if tag := updater.EffectiveInstalledEngineTag(h.cfg); tag != latest {
			t.Fatalf("identity after binary change = %q, want %q (seeded from the manifest beside the new binary)", tag, latest)
		}

		// The gate then decides from the seeded identity: current, and the
		// state/manifest contract never oscillates.
		assertGateCurrentWithoutTransaction(t, h, latest)
	})
}

// assertGateCurrentWithoutTransaction runs a full boot against h and fails
// the test unless maintenance finishes READY_FOR_PREWARM/"current" with
// ZERO install transactions.
func assertGateCurrentWithoutTransaction(t *testing.T, h *maintenanceTestHarness, latest string) {
	t.Helper()

	var transactions atomic.Int32

	events := newOrderedEvents()
	events = h.wire(
		events,
		func(ctx context.Context) (string, error) { return latest, nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			transactions.Add(1)

			return "engine updated to llama.cpp " + tag, nil
		},
		nil,
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	events.waitUntil(t, events.has("prewarm"), "prewarm after a no-op maintenance decision")

	st := h.srv.gateValue().Snapshot()

	if st.Phase != PhaseReadyForPrewarm {
		t.Fatalf("phase = %s, want READY_FOR_PREWARM (identity proves %s is installed)", st.Phase, latest)
	}

	if st.Reason != "current" {
		t.Fatalf("reason = %q, want \"current\" (detail: %s)", st.Reason, st.Detail)
	}

	if st.Updated || st.TargetTag != "" {
		t.Fatalf("no update may run: updated=%v targetTag=%q", st.Updated, st.TargetTag)
	}

	if got := transactions.Load(); got != 0 {
		t.Fatalf("%d redundant engine transactions — the committed identity was not honored", got)
	}
}
