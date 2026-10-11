package llm

import (
        "errors"
        "fmt"
        "math"
        "net"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "runtime"
        "strconv"
        "strings"
        "sync/atomic"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// TestMain implements the fake llama.cpp engine: when the test binary is
// re-executed by LlamaServer itself (the engine-under-test spawns
// cfg.LlamaBinPath as a subprocess), the env marker turns this binary into
// a stand-in llama-server that serves /health on the requested port. This
// is the only portable way to test the REAL spawn → health → ready path
// without shipping an actual llama.cpp binary into CI.
//
// v1.3.5: TestMain also answers the CAPABILITY-PROBE invocations
// (`--help`, `--list-devices`) so timingmarks_test.go can drive
// parseHelpCaps / EnumerateEngineDevices against a real executable.
// The --help dispatch is deliberately ARGV-based: the production probe
// runs the engine under a sanitized environment (sanitizedHelpEnv strips
// every custom variable), so a mode marker CANNOT be used there — the
// help text must be self-describing from the argv alone.
func TestMain(m *testing.M) {
        // The deterministic loader-failure mode reproduces the failure on
        // EVERY invocation (preflight --version, capability probes and real
        // server launches alike) — the semantics of the pre-v1.6.2
        // env-marker-first order. The boot must be refused at preflight,
        // before any model launch.
        if os.Getenv("GO_FAKE_LLAMA_MODE") == "loader-fail" {
                reproduceLoaderFailure(os.Args)
                return
        }

        // Capability-probe dispatch (v1.6.2): the probes are
        // ARGV-triggered and must win over the fake-server env marker —
        // the runtime backend verification now enumerates devices on
        // the SAME binary that is also spawned as the serving fake
        // engine (the marker env is inherited by the enumeration
        // subprocess, and the probes must still answer). The serving
        // path never starts with one of these flags as argv[1] (it
        // starts with --model/--host), so the order is unambiguous.
        if len(os.Args) > 1 {
                switch os.Args[1] {
                case "--help":
                        runFakeEngineHelpProbe()

                        return
                case "--list-devices":
                        runFakeEngineDeviceProbe()

                        return
                case "--version":
                        // v1.3.6 preflight identity probe (spec §6): the fake
                        // engine reports a realistic llama.cpp version line so
                        // the preflight gate passes in tests.
                        fmt.Println("version: 4818 (abcdef12)")

                        return
                }
        }

        if os.Getenv("SHEYTAN_FAKE_LLAMA") == "1" {
                runFakeLlamaServer()

                return
        }

        os.Exit(m.Run())
}

// fakeEngineHelpText is the canonical --help contract the probe parses:
// every option the capability profile tracks is present, in the shape a
// modern llama.cpp build prints (tri-state --flash-attn included).
const fakeEngineHelpText = `usage: llama-server [options]

  -h, --help            show this help message and exit
  --device DEVICE       select the device for inference
  --flash-attn [on|off|auto]  enable/disable Flash Attention
  --cache-reuse N       minimize KV-cache re-computation
  --jinja               use the jinja template engine for chat
  --no-webui            disable the built-in web UI
  --ubatch-size N       logical maximum batch size
  --threads-batch N     number of threads used during batch generation
`

// runFakeEngineHelpProbe answers `<engine> --help`. No environment is
// required on purpose: the production probe sanitizes the child
// environment, so the output is fixed by the argv.
func runFakeEngineHelpProbe() {
        fmt.Print(fakeEngineHelpText)
}

// runFakeEngineDeviceProbe answers `<engine> --list-devices`. The
// enumeration invocation passes the parent environment through, so the
// scripted variant is selected by SHEYTAN_FAKE_ENGINE_DEVICES; with no
// marker the DEFAULT variant (two devices) still works — the probe
// never depends exclusively on a custom environment variable.
func runFakeEngineDeviceProbe() {
        switch os.Getenv("SHEYTAN_FAKE_ENGINE_DEVICES") {
        case "one":
                fmt.Print("Available devices:\n" +
                        "  Vulkan0: Intel(R) Arc(TM) A770M Graphics (8192 MiB)\n")
        case "unknown":
                fmt.Fprintln(os.Stderr, "error: unknown argument --list-devices")
                os.Exit(1)
        default: // "two" and unset
                fmt.Print("Available devices:\n" +
                        "  Vulkan0: Intel(R) Graphics (2048 MiB)\n" +
                        "  Vulkan1: Intel(R) Arc(TM) A770M Graphics (16384 MiB)\n")
        }
}

// reproduceLoaderFailure is the deterministic loader-failure mode
// (v1.3.6 spec §5, v1.6.2 extraction): the binary records the launch
// (count file), prints the textual NTSTATUS evidence to stderr, and
// exits with a classifiable code:
//   Windows — the REAL 0xC0000139 exit status;
//   Unix    — exit code 57 (not classifiable by code) so the
//             TEXTUAL fallback classification is exercised.
//
// v1.6.2: TestMain reproduces this on EVERY invocation when
// GO_FAKE_LLAMA_MODE=loader-fail — including the preflight --version
// and capability --list-devices probes — preserving the semantics of
// the pre-v1.6.2 env-marker-first dispatch order (the loader failure
// must reproduce at preflight so the boot is refused before any model
// launch).
func reproduceLoaderFailure(args []string) {
        // Only REAL server launches count as launches — capability
        // probes (--help) and preflight probes (--version) carry no
        // --port flag and must not pollute the launch-count evidence.
        launchedAsServer := false

        for _, a := range args {
                if a == "--port" {
                        launchedAsServer = true
                        break
                }
        }

        if launchedAsServer {
                if countFile := os.Getenv("SHEYTAN_FAKE_LAUNCH_COUNT"); countFile != "" {
                        f, err := os.OpenFile(countFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
                        if err == nil {
                                _, _ = f.WriteString("launch\n")
                                _ = f.Close()
                        }
                }
        }

        fmt.Fprintln(os.Stderr, "loader error: STATUS_ENTRYPOINT_NOT_FOUND (0xC0000139) — the procedure entry point ggml_backend_sched_alloc could not be located")

        if runtime.GOOS == "windows" {
                // 0xC0000139 = STATUS_ENTRYPOINT_NOT_FOUND = -1073741511
                // as int32. The v1.3.7 fixture mistakenly exited with
                // -1073741515 (0xC0000135, DLL-not-found): the exit code
                // IS classifiable on Windows, so the structured
                // FailureClass surfaced dll-not-found while the test
                // asserted the entry-point class — a genuine fixture
                // defect, not a test over-assertion (run 35996462352).
                os.Exit(-1073741511)
        }

        os.Exit(57)
}

// runFakeLlamaServer serves /health (200) and optionally /v1/chat/completions
// until killed. GO_FAKE_LLAMA_MODE=crash makes it exit shortly after becoming
// healthy, driving the watchdog's bounded auto-restart.
func runFakeLlamaServer() {
        port := 0

        args := os.Args
        for i, a := range args {
                if a == "--port" && i+1 < len(args) {
                        port, _ = strconv.Atoi(args[i+1])
                }
        }

        mode := os.Getenv("GO_FAKE_LLAMA_MODE")

        // Phase 7: record the argv the engine was launched with so tests can
        // assert the actual launch contract.
        if out := os.Getenv("SHEYTAN_FAKE_ARGS_OUT"); out != "" {
                _ = os.WriteFile(out, []byte(strings.Join(args, "\n")), 0o644)
        }

        // v1.3.6 (spec §5): deterministic loader-failure mode.
        if mode == "loader-fail" {
                reproduceLoaderFailure(args)
                return
        }

        // v1.3.7: model-architecture failure modes for the auto-update ladder
        // (needsNewerEngine). The engine pretends the model requires a newer
        // llama.cpp — the exact signature the ladder matches:
        //
        //   arch-fail-committed — a server launch fails with the architecture
        //       error while an install MANIFEST sits beside the executable
        //       (a committed package that cannot load the model); a staged,
        //       unverified package (no manifest yet) serves normally, so the
        //       deferred update VERIFIES and commits.
        //   arch-fail-always — every server launch fails with the
        //       architecture error: the deferred update can never verify, so
        //       the ladder must ROLL BACK to the previous package.
        if mode == "arch-fail-committed" || mode == "arch-fail-always" {
                launchedAsServer := false

                for _, a := range args {
                        if a == "--port" {
                                launchedAsServer = true
                                break
                        }
                }

                if launchedAsServer {
                        fail := mode == "arch-fail-always"

                        if !fail {
                                exe, err := os.Executable()
                                if err == nil {
                                        _, statErr := os.Stat(filepath.Join(filepath.Dir(exe), "engine-install.json"))
                                        fail = statErr == nil // committed package present → fail
                                }
                        }

                        if fail {
                                fmt.Fprintln(os.Stderr,
                                        "error: unknown model architecture: 'x-custom-arch' — this model requires a newer llama.cpp")
                                os.Exit(1)
                        }
                }
        }

        // v1.6.1 (P0 reproduction): deterministic invalid-argument mode.
        //
        // Mimics the REAL llama.cpp argument-parser rejection reported in
        // the wild (run --repeat-penalty 0 through the compat ladder):
        //
        //   error while handling argument "--repeat-penalty":
        //   repeat-penalty must be finite and greater than 0
        //
        // The engine prints the two-line error on stderr and exits 1 —
        // BEFORE any model load. Every server launch (identified by the
        // --port flag) is recorded in SHEYTAN_FAKE_LAUNCH_COUNT so tests
        // can prove how many engine processes were actually spawned.
        if mode == "invalid-arg" {
                launchedAsServer := false

                for _, a := range args {
                        if a == "--port" {
                                launchedAsServer = true
                                break
                        }
                }

                if launchedAsServer {
                        if countFile := os.Getenv("SHEYTAN_FAKE_LAUNCH_COUNT"); countFile != "" {
                                f, cerr := os.OpenFile(countFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
                                if cerr == nil {
                                        _, _ = f.WriteString("launch\n")
                                        _ = f.Close()
                                }
                        }

                        for i, a := range args {
                                if a != "--repeat-penalty" || i+1 >= len(args) {
                                        continue
                                }

                                raw := args[i+1]

                                v, verr := strconv.ParseFloat(raw, 64)

                                if verr != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
                                        fmt.Fprintln(os.Stderr,
                                                "error while handling argument \"--repeat-penalty\":")
                                        fmt.Fprintln(os.Stderr,
                                                "repeat-penalty must be finite and greater than 0")
                                        os.Exit(1)
                                }
                        }
                }
        }

        // Phase 7: strict CLI-contract emulation.
        //
        // strict-new-args    — mimics the on|off|auto contract: a --flash-attn
        //                      followed by anything else reproduces the EXACT
        //                      historical error and exits 1.
        // strict-legacy-args — mimics the boolean-flag contract: a
        //                      --flash-attn followed by a value is rejected.
        strictNew := mode == "strict-new-args"
        strictLegacy := mode == "strict-legacy-args"
        if strictNew || strictLegacy {
                for i, a := range args {
                        if a != "--flash-attn" {
                                continue
                        }
                        next := ""
                        if i+1 < len(args) {
                                next = args[i+1]
                        }
                        isValue := next == "on" || next == "off" || next == "auto"

                        if strictNew {
                                // New contract: a value is REQUIRED. The next option
                                // being consumed as the value is the historical
                                // Phase 6 failure.
                                if next == "" || strings.HasPrefix(next, "--") || !isValue {
                                        fmt.Fprintf(os.Stderr,
                                                "error while handling argument \"--flash-attn\": \nunknown value for --flash-attn: '%s'\n",
                                                next)
                                        os.Exit(1)
                                }
                        }
                        if strictLegacy && isValue {
                                fmt.Fprintf(os.Stderr,
                                        "error: invalid argument: %s\n", next)
                                os.Exit(1)
                        }
                }
        }

        if port == 0 {
                os.Exit(2)
        }

        // v1.7.2: the GPU-offload evidence line — mode "vulkan-offload"
        // makes the fake engine print the exact llama.cpp load-time output
        // the runtime evidence collector captures ("offloaded N/M layers to
        // GPU"), BEFORE the health endpoint goes live.
        if mode == "vulkan-offload" {
                fmt.Println("llm_load_tensors: offloaded 33/33 layers to GPU")
        }

        mux := http.NewServeMux()
        mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
                w.WriteHeader(http.StatusOK)
                _, _ = w.Write([]byte(`{"status":"ok"}`))
        })
        // v1.7.2: the raw /completion endpoint — the AUTO GPU candidate's
        // real-generation probe posts here (present in every llama-server
        // build, independent of chat-template flags). Mode "gen-fail"
        // simulates a candidate whose generation path is broken.
        mux.HandleFunc("/completion", func(w http.ResponseWriter, r *http.Request) {
                if mode == "gen-fail" {
                        http.Error(w, "simulated generation failure", http.StatusInternalServerError)
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"content":"pong","stopped_eos":true}`))
        })
        mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"object":"list","data":[{"id":"fake-model.gguf"}]}`))
        })
        mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"default_generation_settings":{"n_ctx":16384}}`))
        })

        server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: mux}

        if mode == "crash" {
                go func() {
                        // Become healthy, then die — the watchdog must observe a real
                        // process death while running.
                        time.Sleep(500 * time.Millisecond)
                        _ = server.Close()
                        os.Exit(1)
                }()
        }

        _ = server.ListenAndServe()
        // Exit when the server closes or the parent kills us.
        os.Exit(0)
}

func freePort(t *testing.T) int {
        t.Helper()

        l, err := net.Listen("tcp", "127.0.0.1:0")
        if err != nil {
                t.Fatalf("free port: %v", err)
        }

        defer l.Close()

        return l.Addr().(*net.TCPAddr).Port
}

func fakeEngineConfig(t *testing.T, mode string) (*config.Config, string) {
        t.Helper()

        testBin, err := os.Executable()
        if err != nil {
                t.Fatalf("test binary: %v", err)
        }

        dir := t.TempDir()

        // A fake but structurally valid model file: ResolveModelPath only
        // requires an existing .gguf file in the models dir.
        // v1.2.0: build the paths with the OS separator (filepath.Join), not
        // string concatenation — a forward-slash spelling on Windows made the
        // test's EXPECTED model path differ from the runtime's OS-canonical
        // "loaded model" report. See sameFilePath below.
        modelPath := filepath.Join(dir, "models")
        if err := os.MkdirAll(modelPath, 0o755); err != nil {
                t.Fatalf("models dir: %v", err)
        }

        modelFile := filepath.Join(modelPath, "fake-model.gguf")
        if err := os.WriteFile(modelFile, []byte("fake gguf payload"), 0o644); err != nil {
                t.Fatalf("model file: %v", err)
        }

        cfg := config.Default()
        cfg.DataDir = dir
        cfg.ModelsDir = modelPath
        cfg.Provider = "local"
        cfg.LlamaBinPath = testBin
        cfg.LlamaHost = "127.0.0.1"
        cfg.LlamaPort = freePort(t)
        cfg.EngineCompat = 3 // bare flags: the fake engine ignores all tuning
        cfg.Model = "fake-model.gguf"
        cfg.VisionEnabled = false

        t.Setenv("SHEYTAN_FAKE_LLAMA", "1")

        if mode != "" {
                t.Setenv("GO_FAKE_LLAMA_MODE", mode)
        }

        return cfg, modelFile
}

func TestEngineStartReachesReady(t *testing.T) {
        cfg, modelFile := fakeEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        if got := srv.State(); got != StateIdle {
                t.Fatalf("fresh engine must be idle, got %s", got)
        }

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        if got := srv.State(); got != StateReady {
                t.Fatalf("healthy engine must report ready, got %s", got)
        }

        if !srv.IsRunning() || !srv.IsAlive() {
                t.Fatal("ready engine must count as alive/running")
        }

        // v1.2.0: OS-aware canonical comparison — never raw path strings.
        assertSameFilePath(t, srv.LoadedModel(), modelFile)

        if srv.Pid() <= 0 {
                t.Fatal("ready engine must expose a pid")
        }
}

func TestEngineStartFailsWithMissingBinary(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")
        cfg.LlamaBinPath = "/nonexistent/llama-server-missing"

        srv := NewLlamaServer(config.NewSource(cfg))

        err := srv.Start()
        if err == nil {
                t.Fatal("missing binary must fail")
        }

        if got := srv.State(); got != StateFailed {
                t.Fatalf("failed boot must report failed, got %s", got)
        }
}

func TestEngineStartFailsWithNoModel(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        dir := t.TempDir()
        cfg.ModelsDir = filepath.Join(dir, "empty-models")

        srv := NewLlamaServer(config.NewSource(cfg))

        err := srv.Start()
        if err == nil {
                t.Fatal("missing model must fail")
        }

        if got := srv.State(); got != StateFailed {
                t.Fatalf("failed boot must report failed, got %s", got)
        }
}

func TestEngineStopWalksStoppingToStopped(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        if err := srv.Stop(); err != nil {
                t.Fatalf("Stop: %v", err)
        }

        if got := srv.State(); got != StateStopped {
                t.Fatalf("after Stop the engine must be stopped, got %s", got)
        }

        if srv.IsRunning() {
                t.Fatal("stopped engine must not report running")
        }

        if srv.Pid() != 0 {
                t.Fatal("stopped engine must not report a pid")
        }
}

func TestEngineEventsArePublished(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        events, unsubscribe := srv.SubscribeEvents()
        defer unsubscribe()

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        deadline := time.After(5 * time.Second)

        var sawReady bool

        for !sawReady {
                select {
                case ev, ok := <-events:
                        if !ok {
                                t.Fatal("event channel closed unexpectedly")
                        }

                        if ev.State == StateReady {
                                sawReady = true
                        }

                case <-deadline:
                        t.Fatal("timed out waiting for a ready event")
                }
        }
}

func TestEngineDeathTriggersBoundedAutoRestart(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "crash")

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        events, unsubscribe := srv.SubscribeEvents()
        defer unsubscribe()

        // The fake engine dies ~500ms after becoming healthy. The watchdog must
        // restart it (bounded), reaching ready again — real recovery, no stale
        // running state.
        deadline := time.After(20 * time.Second)

        restarts := atomic.Int32{}

        lastState := ""

        for {
                select {
                case ev, ok := <-events:
                        if !ok {
                                t.Fatal("event channel closed")
                        }

                        lastState = ev.State

                        if ev.State == StateReady {
                                restarts.Add(1)

                                if restarts.Load() >= 2 {
                                        // Initial ready + at least one post-crash ready.
                                        t.Logf("recovered to ready after crash (last=%s)", lastState)

                                        return
                                }
                        }

                case <-deadline:
                        t.Fatalf("engine did not recover after crash (restarts=%d state=%s)", restarts.Load(), lastState)
                }
        }
}

func TestMarkBusyFlipsReadyAndBusy(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        // Idle engine: busy reporting must be a no-op.
        srv.MarkBusy(true)

        if got := srv.State(); got != StateIdle {
                t.Fatalf("busy on idle engine must be a no-op, got %s", got)
        }

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        srv.MarkBusy(true)

        if got := srv.State(); got != StateBusy {
                t.Fatalf("engine must be busy during inference, got %s", got)
        }

        if !srv.IsRunning() {
                t.Fatal("busy engine is still alive")
        }

        srv.MarkBusy(false)

        if got := srv.State(); got != StateReady {
                t.Fatalf("engine must return to ready after inference, got %s", got)
        }
}

func TestStopWithoutProcessIsSafe(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))

        if err := srv.Stop(); err != nil {
                t.Fatalf("Stop on never-started engine: %v", err)
        }

        if got := srv.State(); got != StateStopped {
                t.Fatalf("expected stopped, got %s", got)
        }
}

// TestResolveModelPathRequiresSelection is the v1.5.0 MODEL-FIRST
// contract test (repaired from TestResolveModelPathPicksFirstAvailable,
// which codified exactly the defect this release removes: an empty model
// name resolving to an arbitrary "first .gguf in directory order").
//
// The new contract: NO implicit model selection, ever. An empty name
// returns ErrNoModelSelected whether the models dir has zero, one or
// many models; explicit names resolve exactly as before (absolute path
// → exact case-fold match → substring → fuzzy) and unknown names still
// fail with a helpful message.
func TestResolveModelPathRequiresSelection(t *testing.T) {
        dir := t.TempDir()

        if err := os.WriteFile(dir+"/model-a.gguf", []byte("a"), 0o644); err != nil {
                t.Fatal(err)
        }

        if err := os.WriteFile(dir+"/model-b.gguf", []byte("b"), 0o644); err != nil {
                t.Fatal(err)
        }

        // Empty name with models PRESENT: no implicit first-GGUF pick.
        _, err := ResolveModelPath(dir, "")
        if !errors.Is(err, ErrNoModelSelected) {
                t.Fatalf("resolve without name must fail with ErrNoModelSelected, got: %v", err)
        }

        // Empty name with an EMPTY models dir: the same sentinel (with the
        // honest "no models yet" detail).
        empty := t.TempDir()
        _, err = ResolveModelPath(empty, "")
        if !errors.Is(err, ErrNoModelSelected) {
                t.Fatalf("empty models dir without name must fail with ErrNoModelSelected, got: %v", err)
        }

        // Exact (case-insensitive) match — unchanged behavior.
        path, err := ResolveModelPath(dir, "MODEL-B.GGUF")
        if err != nil {
                t.Fatalf("resolve exact: %v", err)
        }

        if !strings.Contains(path, "model-b.gguf") {
                t.Fatalf("unexpected exact resolve: %s", path)
        }

        // Unknown name must fail with a helpful message.
        _, err = ResolveModelPath(dir, "does-not-exist")
        if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
                t.Fatalf("unknown model must fail: %v", err)
        }

        // An absolute path still resolves directly.
        if _, err := ResolveModelPath(dir, path); err != nil {
                t.Fatalf("absolute path resolve: %v", err)
        }
}

// compile-time guard: exec used by helper re-exec through proc package.
var _ = exec.Command

// ---------------------------------------------------------------------------
// v1.2.5 engine lifecycle contract tests (watchdog ownership + honest
// exit classification). These pin the invariants from the repair brief:
//
//      1. unexpected exit triggers restart          (bounded recovery)
//      2. deliberate stop does not restart          (no false crash)
//      3. stop during backoff cancels the restart   (cancelable watchdog)
//      4. repeated crashes exhaust the budget       (terminal failed)
//      5. stale restart cannot resurrect an engine  (generation guard)
//      6. budget resets only after a stable episode (crash loops terminate)
//      7. shutdown leaves no watchdog goroutine     (deterministic ownership)
// ---------------------------------------------------------------------------

// waitForState drains the subscription channel until one of the wanted
// states arrives (or the deadline passes).
func waitForState(t *testing.T, events <-chan EngineEvent, deadline time.Duration, wanted ...string) (EngineEvent, bool) {
        t.Helper()

        timer := time.After(deadline)

        for {
                select {
                case ev, ok := <-events:
                        if !ok {
                                return EngineEvent{}, false
                        }

                        for _, w := range wanted {
                                if ev.State == w {
                                        return ev, true
                                }
                        }

                case <-timer:
                        return EngineEvent{}, false
                }
        }
}

func TestUnexpectedExitReportsDiagnosticDetail(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "crash")

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        events, unsubscribe := srv.SubscribeEvents()
        defer unsubscribe()

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        if _, ok := waitForState(t, events, 10*time.Second, StateReady); !ok {
                t.Fatal("engine never became ready")
        }

        // The fake dies ~500ms after becoming healthy. The honest contract:
        // the detail exposes what happened, not a generic failure.
        deadline := time.Now().Add(10 * time.Second)

        for {
                if d := srv.Detail(); strings.Contains(d, "Unexpected exit") {
                        break
                }

                if time.Now().After(deadline) {
                        t.Fatalf("detail never reported the unexpected exit: %q", srv.Detail())
                }

                time.Sleep(25 * time.Millisecond)
        }

        // Recovery: a fresh ready episode clears the recovery detail — the
        // engine is ready again, verified, and no longer "restarting".
        if _, ok := waitForState(t, events, 15*time.Second, StateReady); !ok {
                t.Fatal("engine never recovered to ready after the crash")
        }

        if d := srv.Detail(); strings.Contains(d, "restarting") {
                t.Fatalf("recovered engine still reports a restart in flight: %q", d)
        }
}

func TestDeliberateStopDoesNotRestart(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))

        events, unsubscribe := srv.SubscribeEvents()
        defer unsubscribe()

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        if _, ok := waitForState(t, events, 10*time.Second, StateReady); !ok {
                t.Fatal("engine never became ready")
        }

        if err := srv.Stop(); err != nil {
                t.Fatalf("Stop: %v", err)
        }

        if got := srv.State(); got != StateStopped {
                t.Fatalf("deliberate stop must end stopped, got %s", got)
        }

        if got := srv.Restarts(); got != 0 {
                t.Fatalf("deliberate stop must not count a restart, got %d", got)
        }

        // No delayed watchdog may bring the engine back: no starting/ready
        // event may arrive after the deliberate stop.
        if ev, ok := waitForState(t, events, 2*time.Second,
                StateStarting, StateReady, StateRunning, StateBusy, StateDownloading); ok {
                t.Fatalf("engine resurrected after a deliberate stop: %s (detail %q)", ev.State, ev.Detail)
        }
}

func TestStopDuringRestartBackoffCancelsRestart(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "crash")

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        events, unsubscribe := srv.SubscribeEvents()
        defer unsubscribe()

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        if _, ok := waitForState(t, events, 10*time.Second, StateReady); !ok {
                t.Fatal("engine never became ready")
        }

        // Wait for the crash (stopped) — the watchdog is now in its 1s backoff.
        if _, ok := waitForState(t, events, 10*time.Second, StateStopped); !ok {
                t.Fatal("crashed engine never reported stopped")
        }

        // Deliberate stop DURING the backoff must cancel the pending restart.
        if err := srv.Stop(); err != nil {
                t.Fatalf("Stop during backoff: %v", err)
        }

        if got := srv.State(); got != StateStopped {
                t.Fatalf("state after stop-during-backoff = %s, want stopped", got)
        }

        // The 1s watchdog would have fired well inside this window if the
        // cancellation leaked: no boot may follow.
        if ev, ok := waitForState(t, events, 2500*time.Millisecond,
                StateStarting, StateReady, StateRunning, StateBusy, StateDownloading); ok {
                t.Fatalf("pending restart was not canceled by Stop: %s", ev.State)
        }
}

func TestRepeatedCrashesExhaustBoundedBudget(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "crash")

        srv := NewLlamaServer(config.NewSource(cfg))

        events, unsubscribe := srv.SubscribeEvents()
        defer unsubscribe()

        if err := srv.Start(); err != nil {
                t.Fatalf("Start: %v", err)
        }

        // The fake crashes ~500ms after every healthy boot. Backoff 1s+2s+4s
        // plus boot overhead must exhaust the budget in bounded time and land
        // in the terminal failed state — never loop forever.
        if _, ok := waitForState(t, events, 60*time.Second, StateFailed); !ok {
                t.Fatalf("crash loop never reached the terminal failed state (state=%s restarts=%d)",
                        srv.State(), srv.Restarts())
        }

        if d := srv.Detail(); !strings.Contains(d, "recovery exhausted") {
                t.Fatalf("failed detail must say recovery is exhausted, got %q", d)
        }

        _ = srv.Stop()
}

func TestStaleWatchdogCannotResurrectOldEpisode(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))

        // Arm a watchdog for episode 0 (no engine has ever started — the
        // server is idle and the arming path is purely state-machine work).
        srv.scheduleAutoRestart("fake-model.gguf")

        srv.mu.Lock()
        armed := srv.watchArmed
        oldGen := srv.gen
        srv.mu.Unlock()

        if !armed {
                t.Fatal("watchdog must arm when a restart is scheduled")
        }

        // A NEW lifecycle episode begins (e.g. a manual start). The stale
        // watchdog from the old episode must stand down.
        srv.mu.Lock()
        srv.beginEpisodeLocked()
        newGen := srv.gen
        srv.mu.Unlock()

        if newGen != oldGen+1 {
                t.Fatalf("episode generation must advance, %d -> %d", oldGen, newGen)
        }

        // Wait past the 1s backoff: the stale watchdog must NOT have started
        // anything.
        time.Sleep(2200 * time.Millisecond)

        if got := srv.State(); got != StateIdle {
                t.Fatalf("stale watchdog resurrected the engine: state=%s, want idle", got)
        }
}

func TestRestartBudgetResetsOnlyAfterStableEpisode(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        // Shrink the stability window so the test proves both branches.
        old := restartBudgetResetAfter
        restartBudgetResetAfter = 150 * time.Millisecond
        t.Cleanup(func() { restartBudgetResetAfter = old })

        srv := NewLlamaServer(config.NewSource(cfg))

        // Episode 1.
        srv.mu.Lock()
        srv.beginEpisodeLocked()
        srv.restarts = 2
        srv.mu.Unlock()

        // Episode 2 starts IMMEDIATELY: the previous episode was far below
        // the stability window, so the budget must NOT reset.
        srv.mu.Lock()
        srv.beginEpisodeLocked()
        got := srv.restarts
        srv.mu.Unlock()

        if got != 2 {
                t.Fatalf("short-lived episode must keep the restart budget (got %d, want 2)", got)
        }

        // Let the episode live past the stability window, then begin episode 3:
        // a genuinely healthy episode resets the budget.
        time.Sleep(250 * time.Millisecond)

        srv.mu.Lock()
        srv.beginEpisodeLocked()
        got = srv.restarts
        srv.mu.Unlock()

        if got != 0 {
                t.Fatalf("stable episode must reset the restart budget (got %d, want 0)", got)
        }
}

func TestStopLeavesNoWatchdogGoroutineBehind(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))

        srv.scheduleAutoRestart("fake-model.gguf")

        srv.mu.Lock()
        armed := srv.watchArmed
        srv.mu.Unlock()

        if !armed {
                t.Fatal("watchdog must be armed before Stop")
        }

        // Stop (no live process — the early path) must cancel the pending
        // watchdog AND wait for its goroutine to exit.
        if err := srv.Stop(); err != nil {
                t.Fatalf("Stop: %v", err)
        }

        srv.mu.Lock()
        armed = srv.watchArmed
        pendingStop := srv.watchStop != nil
        pendingDone := srv.watchDone != nil
        srv.mu.Unlock()

        if armed || pendingStop || pendingDone {
                t.Fatalf("watchdog survived shutdown: armed=%v pendingStop=%v pendingDone=%v",
                        armed, pendingStop, pendingDone)
        }

        // watchDone was awaited inside Stop, so the goroutine has provably
        // exited — no delayed Start() can follow. Belt and braces: no boot.
        time.Sleep(1500 * time.Millisecond)

        if got := srv.State(); got != StateStopped {
                t.Fatalf("engine state after shutdown = %s, want stopped", got)
        }
}
