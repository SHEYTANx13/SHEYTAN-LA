package engine

// execution_contract_test.go — v1.3.2 native execution test contract.
//
// These tests pin the execution properties the release depends on beyond
// "it compiles": deterministic startup (including handshake rejection and
// bounded boots), executable discovery, model-path failure behavior,
// shutdown-dURING-generation, and orphan-process prevention. The
// REAL-host tests run whenever native/engine/build/shtn-engine-host exists
// (the CI workflow builds it in every job that runs this suite); the
// fake-host tests always run.
//
// Contract items and where they are proven (this file unless noted):
//
//   1. executable discovery (DefaultHostPath + Available)
//   2. startup: protocol-mismatch rejection fails closed, no orphan
//   3. startup: hung handshake torn down deterministically (bounded boot)
//   4. startup retry after a failed start
//   5. invalid/missing model path: clean error, engine stays alive
//   6. shutdown during active generation: aborts, no orphan, restartable
//   7. orphan prevention across repeated start/stop cycles
//   8. fallback reason observability (llm.GenerationFallbackReporter)
//
// Real inference, streaming, cancellation, repeated generation, lane
// recycling and context-overflow rejection are proven by the Phase 5
// integration tests in this package (phase5_integration_test.go) and are
// deliberately not duplicated here.

import (
        "context"
        "fmt"
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// --- 1. executable discovery ------------------------------------------------

func TestDefaultHostPathDiscovery(t *testing.T) {
        dir := t.TempDir()

        // The explicit override always wins.
        if got := DefaultHostPath(dir, "/opt/custom/shtn-engine-host"); got != "/opt/custom/shtn-engine-host" {
                t.Fatalf("override path = %q, want the override verbatim", got)
        }

        // The default resolves under {dataDir}/bin with the platform suffix.
        name := "shtn-engine-host"
        if runtime.GOOS == "windows" {
                name += ".exe"
        }
        want := filepath.Join(dir, "bin", name)

        if got := DefaultHostPath(dir, ""); got != want {
                t.Fatalf("default path = %q, want %q", got, want)
        }

        // Available() must track the resolved path exactly: a binary at the
        // default location makes the engine available, a missing one does not.
        e := New(DefaultHostPath(dir, ""))

        if e.Available() {
                t.Fatal("engine must be unavailable while the host binary is missing")
        }

        if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
                t.Fatal(err)
        }
        // A binary at the default location makes the engine available.
        // The marker is a plain cross-platform placeholder — Available()
        // is a filesystem-existence check, so the payload must carry no
        // platform-specific executable assumption (the pre-v1.3.5 shell
        // script here was meaningless on Windows).
        if err := os.WriteFile(want, []byte("SHEYTAN placeholder — availability marker, never executed\n"), 0o755); err != nil {
                t.Fatal(err)
        }

        if !e.Available() {
                t.Fatal("engine must be available once the host binary exists at the default path")
        }

        // A dangling override path is never "available" even when the default
        // location has a binary (the override redirects discovery).
        e2 := New(DefaultHostPath(dir, filepath.Join(dir, "does-not-exist")))
        if e2.Available() {
                t.Fatal("override to a missing binary must report unavailable")
        }
}

// --- 2-4. deterministic startup (fake host) ---------------------------------

// TestEngineStartRejectsProtocolMismatch: a host speaking a different
// protocol version is rejected at the handshake (fail closed), the state
// walks to failed with the mismatch detail, and the failed start leaves no
// process behind. A retry after the failed start must be a fresh attempt
// (the engine is not wedged).
func TestEngineStartRejectsProtocolMismatch(t *testing.T) {
        e := newFakeEngine(t, "badping")

        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        err := e.Start(ctx)
        if err == nil {
                t.Fatal("start must fail on protocol mismatch")
        }
        if !strings.Contains(err.Error(), "protocol mismatch") {
                t.Fatalf("error = %v, want the protocol mismatch diagnostic", err)
        }

        if e.State() != "failed" {
                t.Fatalf("state = %q, want failed", e.State())
        }
        if e.Pid() != 0 {
                t.Fatalf("pid = %d after failed start, want 0 (no surviving process)", e.Pid())
        }
        if e.IsAlive() {
                t.Fatal("engine must not report alive after a failed handshake")
        }

        // The observable fallback reason names the infrastructure problem
        // (v1.3.2 contract) instead of a generic "not capable".
        b := NewBackend(e)
        if reason := b.GenerationFallbackReason(); !strings.Contains(reason, "not running") {
                t.Fatalf("fallback reason = %q, want the not-running diagnostic", reason)
        }
        if b.GenerationCapable() {
                t.Fatal("engine must not be generation capable after a failed handshake")
        }

        // Retry after failed startup: switch the SAME test-binary host to
        // well-behaved mode (the fake host reads its mode at spawn time) and
        // start fresh — the engine is not wedged by the earlier rejection.
        t.Setenv("SHEYTAN_FAKE_NATIVE_MODE", "")

        e2 := New(e.Path())
        if err := e2.Start(ctx); err != nil {
                t.Fatalf("retry start after protocol mismatch failure: %v", err)
        }
        if e2.State() != "ready" {
                t.Fatalf("retry state = %q, want ready", e2.State())
        }
        if err := e2.Stop(ctx); err != nil {
                t.Fatalf("stop after retry: %v", err)
        }
}

// TestEngineStartHangBoundedTeardown: a host that never answers the
// handshake must be torn down deterministically by the CALLER's bounded
// context — the boot cannot hang forever and cannot leave an orphan.
func TestEngineStartHangBoundedTeardown(t *testing.T) {
        e := newFakeEngine(t, "hang")

        ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
        defer cancel()

        started := time.Now()

        err := e.Start(ctx)
        if err == nil {
                t.Fatal("start against a hung host must fail within the caller bound")
        }

        if elapsed := time.Since(started); elapsed > 5*time.Second {
                t.Fatalf("bounded boot took %v — the hang was not bounded by the caller context", elapsed)
        }

        if e.State() != "failed" {
                t.Fatalf("state = %q, want failed", e.State())
        }
        if e.Pid() != 0 {
                t.Fatalf("pid = %d after bounded teardown, want 0 (no orphan)", e.Pid())
        }
}

// --- 5. missing model path (real host) --------------------------------------

// TestRealCppHostMissingModelPath: a nonexistent model path is a clean,
// diagnosable rejection — the engine stays alive and healthy, the model
// concern walks to failed with the file error, and a subsequent VALID load
// recovers.
func TestRealCppHostMissingModelPath(t *testing.T) {
        // v1.5.0 lifecycle audit: the temp tree is acquired BEFORE the
        // engine-stop cleanup is registered (the generic real-host contract)
        // even though the missing file is never opened — the ordering stays
        // uniform across every real-host test.
        missingRoot := t.TempDir()

        e := startRealHost(t)

        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        missing := filepath.Join(missingRoot, "does-not-exist.gguf")

        err := e.LoadModel(ctx, ModelSpec{Path: missing})
        if err == nil {
                t.Fatal("missing model path must fail")
        }
        if !strings.Contains(err.Error(), "model file") {
                t.Fatalf("error = %v, want the model-file diagnostic", err)
        }

        if state := e.NativeModelState(); state != ModelStateFailed {
                t.Fatalf("model state = %q, want failed", state)
        }

        // The engine itself is not the failure: infrastructure problems must
        // not be converted into engine failures.
        if e.State() != "ready" {
                t.Fatalf("engine state = %q, want ready after a model-path rejection", e.State())
        }

        report, err := e.Health(ctx)
        if err != nil || !report.Alive {
                t.Fatalf("engine must stay healthy after a model-path rejection: %+v (%v)", report, err)
        }

        // Recovery: a valid load succeeds on the same engine.
        loadRealFixture(t, e, "tiny-llama-f32.gguf")

        if state := e.NativeModelState(); state != ModelStateLoaded {
                t.Fatalf("model state after recovery = %q, want loaded", state)
        }
}

// --- 6-7. shutdown during generation + orphan prevention (real host) --------

// processGone verifies the host process is actually dead. The verdict is
// PLATFORM-NATIVE and lives in build-tagged files so no unsupported API
// ever compiles on the wrong platform (the v1.3.5 Windows failure:
// syscall.Kill is undefined there even behind a runtime.GOOS branch):
//
//   - Unix (process_liveness_unix_test.go): a bounded signal-0 probe to
//     the exact pid must fail — the process was reaped, not merely
//     forgotten by the supervisor's bookkeeping.
//   - Windows (process_liveness_windows_test.go): a bounded
//     OpenProcess/exit-code probe — a real kernel check, not a no-op,
//     so the orphan contract stays meaningful on Windows.
func processGone(t *testing.T, label string, pid int) {
        t.Helper()

        if pid <= 0 {
                t.Fatalf("%s: invalid pid %d for the liveness probe", label, pid)
        }

        platformProcessGone(t, label, pid)
}

// TestRealCppHostOrphanPrevention: repeated start/stop cycles must never
// leave a host process behind, and the engine must report stopped with a
// cleared pid after every stop.
func TestRealCppHostOrphanPrevention(t *testing.T) {
        bin := realHostBinaryPath()
        if !fileExists(bin) {
                t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", bin)
        }

        e := New(bin)
        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()

        const cycles = 3

        for i := 0; i < cycles; i++ {
                if err := e.Start(ctx); err != nil {
                        t.Fatalf("cycle %d: start: %v", i+1, err)
                }

                pid := e.Pid()
                if pid <= 0 {
                        t.Fatalf("cycle %d: no host pid after start", i+1)
                }

                if err := e.Stop(ctx); err != nil {
                        t.Fatalf("cycle %d: stop: %v", i+1, err)
                }

                if e.State() != "stopped" {
                        t.Fatalf("cycle %d: state = %q, want stopped", i+1, e.State())
                }
                if e.Pid() != 0 {
                        t.Fatalf("cycle %d: pid = %d after stop, want 0", i+1, e.Pid())
                }

                // The process itself must be gone — a stopped engine that leaves
                // the host running in the background is an orphan bug.
                processGone(t, fmt.Sprintf("cycle %d", i+1), pid)
        }
}

// TestRealCppHostStopDuringGeneration: stopping the engine while a
// generation is in flight must abort that generation (no wedge), terminate
// the host process completely, and leave the engine restartable — the
// application-shutdown-during-inference path.
func TestRealCppHostStopDuringGeneration(t *testing.T) {
        bin := realHostBinaryPath()
        if !fileExists(bin) {
                t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", bin)
        }

        e := New(bin)

        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()

        if err := e.Start(ctx); err != nil {
                t.Fatalf("start: %v", err)
        }

        loadRealFixture(t, e, "tiny-llama-slow.gguf")

        pid := e.Pid()

        genDone := make(chan error, 1)

        go func() {
                _, err := e.StreamGeneration(ctx, GenerationRequest{
                        RequestID: "stop-during-gen",
                        Prompt:    "hello",
                        MaxTokens: 2000, // slow fixture: generation certainly still active
                }, func(chunk GenerationChunk) error { return nil })
                genDone <- err
        }()

        // Give the generation time to reach the decode loop (the slow fixture
        // takes milliseconds per token — 500 ms guarantees in-flight state).
        time.Sleep(500 * time.Millisecond)

        if err := e.Stop(ctx); err != nil {
                t.Fatalf("stop during generation: %v", err)
        }

        if e.State() != "stopped" {
                t.Fatalf("state = %q, want stopped", e.State())
        }

        // The in-flight generation must return (aborted), not hang forever.
        select {
        case err := <-genDone:
                // Either a cooperative "cancelled" outcome or the connection-lost
                // error is acceptable; a WEDGE (timeout below) is the bug class.
                _ = err
        case <-time.After(15 * time.Second):
                t.Fatal("generation did not return after engine stop — wedged stream")
        }

        // No orphaned host process.
        if e.Pid() != 0 {
                t.Fatalf("pid = %d after stop, want 0", e.Pid())
        }
        processGone(t, "stop during generation", pid)

        // Restart after the abort: the engine must be fully reusable.
        if err := e.Start(ctx); err != nil {
                t.Fatalf("restart after stop-during-generation: %v", err)
        }
        if e.State() != "ready" {
                t.Fatalf("restart state = %q, want ready", e.State())
        }
        if err := e.Stop(ctx); err != nil {
                t.Fatalf("final stop: %v", err)
        }
}

// --- 8. fallback-reason observability (fake host) ---------------------------

// TestBackendFallbackReasonStates: the v1.3.2 fallback reporter must
// distinguish the three infrastructure classes instead of a generic
// "not capable" — not running, running without a model, and running with a
// non-executable model — and report an empty reason when serving.
func TestBackendFallbackReasonStates(t *testing.T) {
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        // Class 1: engine down.
        down := NewBackend(New("/nonexistent/shtn-engine-host"))
        if reason := down.GenerationFallbackReason(); !strings.Contains(reason, "not running") {
                t.Fatalf("down reason = %q, want the not-running diagnostic", reason)
        }

        // Classes 2 and 3 against the live fake host.
        e := newFakeEngine(t, "")
        if err := e.Start(ctx); err != nil {
                t.Fatalf("start fake host: %v", err)
        }

        b := NewBackend(e)

        if !b.GenerationCapable() {
                // Engine up, no model loaded → class 2.
                reason := b.GenerationFallbackReason()
                if !strings.Contains(reason, "no model is loaded") {
                        t.Fatalf("no-model reason = %q, want the no-model diagnostic", reason)
                }
        } else {
                t.Fatal("fake engine without a loaded model must not be generation capable")
        }

        // Load the (fake) model: the fake host reports a generation-capable
        // card, so the reason must clear.
        fakeModel := filepath.Join(t.TempDir(), "fake.gguf")
        if err := os.WriteFile(fakeModel, []byte("fake"), 0o644); err != nil {
                t.Fatal(err)
        }

        if err := b.LoadModel(ctx, llm.ModelSpec{Path: fakeModel}); err != nil {
                t.Fatalf("load fake model: %v", err)
        }

        if !b.GenerationCapable() {
                t.Fatal("fake host model card must report generation capable")
        }
        if reason := b.GenerationFallbackReason(); reason != "" {
                t.Fatalf("capable reason = %q, want empty", reason)
        }

        // Class 3: the inspectable reason when the engine's own verdict says
        // the loaded model is not executable is covered by the real-host
        // unsupported-model path (phase5 TestRealCppHostPhase5UnsupportedModel)
        // and the model-state machine's NativeGenerationReason.
}
