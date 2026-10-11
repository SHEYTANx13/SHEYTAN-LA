// rediscover_v137_test.go — v1.3.7 Rediscover (Repair) regressions.
//
// The v1.3.6 Rediscover had three defects these tests pin:
//
//  1. os.Stat alone was treated as proof the managed engine is "usable" —
//     a present-but-BROKEN package (garbage bytes, wrong arch, missing
//     DLLs) made the repair action a no-op that reported ok, exactly when
//     repair was needed. v1.3.7: the real preflight gate decides; a broken
//     package falls through to the discovery ladder and is REPLACED.
//
//  2. the restart condition was INVERTED: an engine that WAS running was
//     stopped for the operation and then never restarted (ok returned,
//     engine left down); an engine that was deliberately stopped was
//     started. v1.3.7: wasRunning is restored, a stopped engine stays
//     stopped.
//
//  3. a restart failure after a successful import was logged and swallowed
//     (ok returned). v1.3.7: it is a non-nil error, per the file's own
//     header contract.
package llm

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// rediscoverFixtureConfig builds the managed-engine test config WITHOUT
// pointing LlamaBinPath at the test binary (the managed location is the
// default DataDir/bin path these tests deliberately control).
func rediscoverFixtureConfig(t *testing.T) *config.Config {
	t.Helper()

	dir := t.TempDir()

	modelDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("models dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(modelDir, "fake-model.gguf"), []byte("fake gguf payload"), 0o644); err != nil {
		t.Fatalf("model file: %v", err)
	}

	cfg := config.Default()
	cfg.DataDir = dir
	cfg.ModelsDir = modelDir
	cfg.Provider = "local"
	cfg.LlamaBinPath = "" // the managed default: <DataDir>/bin/llama-server
	cfg.LlamaHost = "127.0.0.1"
	cfg.LlamaPort = freePort(t)
	cfg.EngineCompat = 3
	cfg.Model = "fake-model.gguf"
	cfg.VisionEnabled = false

	t.Setenv("SHEYTAN_FAKE_LLAMA", "1")

	return cfg
}

// TestRediscoverRepairsBrokenManagedPackage pins defect 1: garbage at the
// managed path is NOT "present and usable" — the ladder imports a validated
// replacement and the engine starts.
func TestRediscoverRepairsBrokenManagedPackage(t *testing.T) {
	root := t.TempDir()

	// A valid engine candidate in the only PATH entry.
	extDir := filepath.Join(root, "external")
	fixture := stageEngineFixture(t, extDir, engineBinaryName())

	cfg := rediscoverFixtureConfig(t)

	// The managed location holds a PRESENT but BROKEN package: plain
	// bytes — StaticValidate rejects it (no recognized format).
	binDir := filepath.Join(cfg.DataDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	managed := filepath.Join(binDir, engineBinaryName())
	if err := os.WriteFile(managed, []byte("this is not an executable — corrupt engine payload"), 0o644); err != nil {
		t.Fatalf("seed broken engine: %v", err)
	}

	t.Setenv("PATH", extDir)

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	outcome, err := srv.Rediscover()
	if err != nil {
		t.Fatalf("rediscover must REPAIR a broken managed package, got error: %v", err)
	}

	if strings.Contains(outcome, "nothing to rediscover") {
		t.Fatalf("a broken package must never be reported as usable — outcome: %s", outcome)
	}

	// The managed engine was REPLACED by the discovered candidate.
	if got := fileSHA(t, managed); got != fileSHA(t, fixture) {
		t.Fatal("the broken managed engine was not replaced by the discovered candidate")
	}

	// The import path restarts and verifies: the engine must be running.
	if !srv.IsRunning() {
		t.Fatal("the engine must be running after a successful rediscovery import")
	}
}

// TestRediscoverRestoresRunningEngine pins defect 2: an engine that was
// running before Rediscover is running again afterwards (the v1.3.6 code
// left it stopped while reporting ok).
func TestRediscoverRestoresRunningEngine(t *testing.T) {
	cfg := rediscoverFixtureConfig(t)

	// A VALID managed engine: the test binary itself, preflight-clean.
	binDir := filepath.Join(cfg.DataDir, "bin")
	stageEngineFixture(t, binDir, engineBinaryName())

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	if err := srv.Start(); err != nil {
		t.Fatalf("start engine: %v", err)
	}

	if !srv.IsRunning() {
		t.Fatal("precondition: the engine must be running before rediscover")
	}

	outcome, err := srv.Rediscover()
	if err != nil {
		t.Fatalf("rediscover of a valid running engine must succeed: %v", err)
	}

	if !strings.Contains(outcome, "nothing to rediscover") {
		t.Fatalf("a valid managed engine is a no-op, got: %s", outcome)
	}

	if !srv.IsRunning() {
		t.Fatal("a previously-running engine must be RUNNING again after rediscover — the v1.3.6 code left it stopped")
	}
}

// TestRediscoverKeepsStoppedEngineStopped pins the other half of defect 2:
// a deliberately-stopped engine stays stopped (preflight is the honest
// verification for a present package — no surprise auto-start).
func TestRediscoverKeepsStoppedEngineStopped(t *testing.T) {
	cfg := rediscoverFixtureConfig(t)

	binDir := filepath.Join(cfg.DataDir, "bin")
	stageEngineFixture(t, binDir, engineBinaryName())

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	if srv.IsRunning() {
		t.Fatal("precondition: the engine must be stopped before rediscover")
	}

	outcome, err := srv.Rediscover()
	if err != nil {
		t.Fatalf("rediscover of a valid stopped engine must succeed: %v", err)
	}

	if !strings.Contains(outcome, "nothing to rediscover") {
		t.Fatalf("a valid managed engine is a no-op, got: %s", outcome)
	}

	if srv.IsRunning() {
		t.Fatal("a deliberately-stopped engine must stay stopped after a no-op rediscover")
	}
}

// TestRediscoverSurfacesRestartFailure pins defect 3: when the imported
// engine cannot start, Rediscover returns a NON-NIL error (the v1.3.6 code
// logged and swallowed it, reporting ok).
func TestRediscoverSurfacesRestartFailure(t *testing.T) {
	root := t.TempDir()

	extDir := filepath.Join(root, "external")
	stageEngineFixture(t, extDir, engineBinaryName())

	cfg := rediscoverFixtureConfig(t)

	// Broken managed package → the ladder imports the PATH candidate…
	binDir := filepath.Join(cfg.DataDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(binDir, engineBinaryName()), []byte("corrupt engine payload"), 0o644); err != nil {
		t.Fatalf("seed broken engine: %v", err)
	}

	t.Setenv("PATH", extDir)

	// …but the engine port is held by a FOREIGN listener, so the
	// post-import restart cannot come ready — startLocked fails and the
	// failure must surface as an error, never an ok.
	blocker, err := net.Listen("tcp", net.JoinHostPort(cfg.LlamaHost, strconv.Itoa(cfg.LlamaPort)))
	if err != nil {
		t.Fatalf("occupy engine port: %v", err)
	}
	defer blocker.Close()

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	_, rerr := srv.Rediscover()
	if rerr == nil {
		t.Fatal("a restart failure after a successful import must surface as an ERROR — the v1.3.6 code swallowed it and reported ok")
	}

	if !strings.Contains(rerr.Error(), "restart after rediscovery failed") {
		t.Fatalf("the error must name the restart failure, got: %v", rerr)
	}
}
