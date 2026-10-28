package api

// maintenance_twoboot_v175_test.go — v1.7.5 (§6): ENGINE INSTALL/UPDATE
// IDEMPOTENCY at the STARTUP MAINTENANCE GATE level.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/installer"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

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
	cfg.LastUpdateCheck = "2020-01-01T00:00:00Z"
	cfg.Model = model
	cfg.LlamaBinPath = ""

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	t.Cleanup(srv.Close)

	return &maintenanceTestHarness{srv: srv, cfg: cfg}
}

// commitEngineTransaction simulates a real engine install commit.
// Retries WriteFile on ETXTBSY ("text file busy") when prewarm still holds the binary.
func commitEngineTransaction(t *testing.T, cfg *config.Config, tag string) {
	t.Helper()

	binDir := updater.EngineBinDir(cfg)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	bin := filepath.Join(binDir, "llama-server")
	payload := []byte("fake llama-server " + tag)

	var last error
	for attempt := 0; attempt < 20; attempt++ {
		last = os.WriteFile(bin, payload, 0o755)
		if last == nil {
			break
		}
		if !isTextFileBusy(last) {
			t.Fatalf("write binary: %v", last)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("write binary after retries: %v", last)
	}

	manifest := `{"tag":"` + tag + `","sha256":"deadbeef","size":1024,"source":"test"}`
	if err := os.WriteFile(filepath.Join(binDir, "engine-install.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	updater.RecordEngineTag(cfg, tag)
}

func isTextFileBusy(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "text file busy") || strings.Contains(s, "etxtbsy")
}

func TestMaintenanceTwoBootsDoNotRedownloadCommittedEngine(t *testing.T) {
	dataRoot := t.TempDir()

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
	if tag := updater.EffectiveInstalledEngineTag(boot1.cfg); tag != "b11223" {
		t.Fatalf("boot 1: committed identity = %q, want b11223", tag)
	}

	boot2 := newTwoBootServer(t, dataRoot, "fake-model.gguf")
	var boot2Transactions atomic.Int32
	events2 := newOrderedEvents()
	events2 = boot2.wire(
		events2,
		func(ctx context.Context) (string, error) { return "b11223", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			boot2Transactions.Add(1)
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
		t.Fatalf("boot 2: reason = %q, want \"current\" (detail: %s)", st2.Reason, st2.Detail)
	}
	if st2.Updated || st2.TargetTag != "" {
		t.Fatalf("boot 2: no update may run: updated=%v targetTag=%q", st2.Updated, st2.TargetTag)
	}
	if got := boot2Transactions.Load(); got != 0 {
		t.Fatalf("boot 2: %d redundant engine transactions — a committed b11223 engine was re-downloaded", got)
	}
}

func TestMaintenanceGateIdentityFallbackMatrix(t *testing.T) {
	const latest = "b11223"

	t.Run("state without identity but manifest valid → current, no transaction", func(t *testing.T) {
		h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")
		commitEngineTransaction(t, h.cfg, latest)
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
		var ran atomic.Bool
		events := newOrderedEvents()
		events = h.wire(
			events,
			func(ctx context.Context) (string, error) { return latest, nil },
			func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
				ran.Store(true)
				if tag != latest {
					t.Errorf("transaction targeted %q, want %q", tag, latest)
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
			t.Fatal("with no identity evidence the gate skipped the transaction")
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
		commitEngineTransaction(t, h.cfg, latest)
		newBin := filepath.Join(updater.EngineBinDir(h.cfg), "llama-server-next")
		if err := os.WriteFile(newBin, []byte("fake llama-server next"), 0o755); err != nil {
			t.Fatalf("write replacement binary: %v", err)
		}
		m := installer.New(h.cfg)
		if _, _, err := m.EnsureRun(false); err != nil {
			t.Fatalf("EnsureRun after binary change: %v", err)
		}
		if tag := updater.EffectiveInstalledEngineTag(h.cfg); tag != latest {
			t.Fatalf("identity after binary change = %q, want %q", tag, latest)
		}
		assertGateCurrentWithoutTransaction(t, h, latest)
	})
}

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
