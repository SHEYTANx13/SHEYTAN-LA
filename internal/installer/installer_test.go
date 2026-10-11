package installer

// v1.7.4 (P0 #3): engine-identity persistence regressions.
//
// EnsureRun rewrites <DataDir>/installed.json on every boot (server.go
// EnsureSetup) and on every UI state poll (handleState). These tests pin
// the repaired contract: the recorded llama.cpp engine identity
// (components.llamaServer.meta.engineTag / version) survives the rewrite,
// while detection results (paths, statuses, timestamps) still refresh.
//
// The restart-idempotency test at the bottom replays the full unit-level
// boot sequence — commit → EnsureRun → maintenance-gate tag decision —
// that used to re-download the engine on EVERY boot.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

func installerTestConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.LlamaBinPath = "" // resolves to the managed bin dir

	return cfg
}

func installerEngineBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}

	return "llama-server"
}

// seedManagedEngine places a (fake) managed engine binary and, when
// manifestTag is non-empty, a committed engine-install.json beside it.
func seedManagedEngine(t *testing.T, cfg *config.Config, manifestTag string) string {
	t.Helper()

	binPath := updater.EngineBinaryPath(cfg)

	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	if err := os.WriteFile(binPath, []byte("fake llama-server binary"), 0o755); err != nil {
		t.Fatalf("seed engine binary: %v", err)
	}

	if manifestTag != "" {
		manifest := `{"tag":"` + manifestTag + `","sha256":"x","size":24,"source":"test"}`

		if err := os.WriteFile(filepath.Join(filepath.Dir(binPath), "engine-install.json"), []byte(manifest), 0o644); err != nil {
			t.Fatalf("seed engine manifest: %v", err)
		}
	}

	return binPath
}

// writeStateFile persists st as the installed.json state file.
func writeStateFile(t *testing.T, cfg *config.Config, st *State) {
	t.Helper()

	data, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}

	if err := os.WriteFile(cfg.StatePath(), data, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// TestEnsureRunPreservesEngineTag pins mechanism H1 of the every-boot
// engine re-download: a state file carrying the committed engine tag must
// survive the detection rewrite, and the rewrite must still refresh the
// detection-owned fields (observedAt, status, path).
func TestEnsureRunPreservesEngineTag(t *testing.T) {
	cfg := installerTestConfig(t)
	binPath := seedManagedEngine(t, cfg, "b11223")

	observed := time.Now().UTC().Add(-time.Hour)

	writeStateFile(t, cfg, &State{
		AppVersion: "1.7.3",
		LastRunAt:  observed,
		Components: map[string]Component{
			"llamaServer": {
				Status:     "installed",
				ObservedAt: observed,
				Meta: map[string]string{
					"path":      binPath,
					"engineTag": "b11223",
					"updatedAt": observed.Format(time.RFC3339),
				},
			},
		},
	})

	m := New(cfg)

	st, _, err := m.EnsureRun(false)
	if err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}

	c, ok := st.Components["llamaServer"]
	if !ok {
		t.Fatal("detection must keep the llamaServer component")
	}

	if got := c.Meta["engineTag"]; got != "b11223" {
		t.Fatalf("engine tag must survive the EnsureRun rewrite, got %q", got)
	}

	if c.Status != "installed" {
		t.Fatalf("status must be re-detected, got %q", c.Status)
	}

	if c.Meta["path"] != binPath {
		t.Fatalf("path fields must be refreshed, got %q", c.Meta["path"])
	}

	if !c.ObservedAt.After(observed) {
		t.Fatal("observedAt must be refreshed by the detection pass")
	}

	// The PERSISTED file carries the identity too — not just the returned
	// snapshot (saveState is what the next boot reads).
	reloaded, err := m.LoadState()
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if got := reloaded.Components["llamaServer"].Meta["engineTag"]; got != "b11223" {
		t.Fatalf("persisted state must keep the engine tag, got %q", got)
	}
}

// TestEnsureRunCarriesVersionForward pins the version half of the merge.
func TestEnsureRunCarriesVersionForward(t *testing.T) {
	cfg := installerTestConfig(t)
	binPath := seedManagedEngine(t, cfg, "")

	writeStateFile(t, cfg, &State{
		AppVersion: "1.7.3",
		Components: map[string]Component{
			"llamaServer": {
				Version:    "b10642",
				Status:     "installed",
				ObservedAt: time.Now().UTC(),
				Meta:       map[string]string{"path": binPath},
			},
		},
	})

	m := New(cfg)

	st, _, err := m.EnsureRun(false)
	if err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}

	c := st.Components["llamaServer"]

	if c.Version != "b10642" {
		t.Fatalf("detection cannot observe the build — the recorded version must carry forward, got %q", c.Version)
	}

	if got := c.Meta["engineTag"]; got != "b10642" {
		t.Fatalf("a version-only identity must seed the engine tag, got %q", got)
	}
}

// TestEnsureRunSeedsTagFromManifestWhenBinaryChanged pins the
// path-changed rule: the recorded tag belongs to the OLD binary, so a
// genuinely different binary file re-detects — seeded from the committed
// manifest beside the NEW binary, never from the stale record.
func TestEnsureRunSeedsTagFromManifestWhenBinaryChanged(t *testing.T) {
	cfg := installerTestConfig(t)
	newPath := seedManagedEngine(t, cfg, "b20000")

	writeStateFile(t, cfg, &State{
		AppVersion: "1.7.3",
		Components: map[string]Component{
			"llamaServer": {
				Version:    "b10642",
				Status:     "installed",
				ObservedAt: time.Now().UTC(),
				Meta: map[string]string{
					"path":      filepath.Join(filepath.Dir(newPath), installerEngineBinaryName()+"-gone"),
					"engineTag": "b10642",
				},
			},
		},
	})

	m := New(cfg)

	st, _, err := m.EnsureRun(false)
	if err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}

	c := st.Components["llamaServer"]

	if got := c.Meta["engineTag"]; got != "b20000" {
		t.Fatalf("a changed binary must re-seed its tag from the install manifest, got %q", got)
	}

	if c.Meta["path"] != newPath {
		t.Fatalf("path must reflect the new binary, got %q", c.Meta["path"])
	}
}

// TestEnsureRunFirstBootHasNoTag pins the honest baseline: with no previous
// state and no manifest, detection records no tag (the updater decision
// points apply their own DefaultEngineTag fallback).
func TestEnsureRunFirstBootHasNoTag(t *testing.T) {
	cfg := installerTestConfig(t)
	seedManagedEngine(t, cfg, "")

	st, _, err := New(cfg).EnsureRun(false)
	if err != nil {
		t.Fatalf("EnsureRun: %v", err)
	}

	if got := st.Components["llamaServer"].Meta["engineTag"]; got != "" {
		t.Fatalf("first boot with no manifest must not invent a tag, got %q", got)
	}
}

// TestEngineUpdateDoesNotReDownloadAcrossRestart is the unit-level replay
// of the production incident (P0 #3): boot → engine update to b11223 →
// commit → EnsureRun (the pass BEFORE the startup maintenance gate) →
// gate tag decision. The gate must see b11223 — not the bundled default —
// and a second boot (another EnsureRun, the state-poll path) must not
// disturb the identity either.
func TestEngineUpdateDoesNotReDownloadAcrossRestart(t *testing.T) {
	cfg := installerTestConfig(t)
	seedManagedEngine(t, cfg, "")

	// The updater's commit after the (simulated) b11223 update.
	updater.RecordEngineTag(cfg, "b11223")

	// Boot 1: EnsureRun precedes the maintenance gate (server.go).
	m := New(cfg)

	if _, _, err := m.EnsureRun(false); err != nil {
		t.Fatalf("boot 1 EnsureRun: %v", err)
	}

	// The gate's decision math (maintenance.go / checkAndApply /
	// updateEngineForModel all share it): latest == current → NO update.
	current := updater.EffectiveInstalledEngineTag(cfg)
	if current == "" {
		current = updater.DefaultEngineTag
	}

	const latest = "b11223"

	if current != latest {
		t.Fatalf("gate would decide update required (%s → %s) — the identity did not survive the boot pass", current, latest)
	}

	// Boot 2 (and the every-state-poll rewrite): the identity must STILL
	// hold — this is the exact loop that re-downloaded the engine before.
	for i := 0; i < 3; i++ {
		if _, _, err := New(cfg).EnsureRun(false); err != nil {
			t.Fatalf("restart %d EnsureRun: %v", i+2, err)
		}

		if got := updater.EffectiveInstalledEngineTag(cfg); got != latest {
			t.Fatalf("restart %d: gate tag = %q, want %q — the engine would re-download", i+2, got, latest)
		}
	}
}
