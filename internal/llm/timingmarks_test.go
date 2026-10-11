package llm

// timingmarks_test.go — v1.2.6 continuation regression tests for the
// TRANSPORT-LEVEL timing marks and the deterministic --device selection:
//
//   1. streamOnce emits request_sent BEFORE the HTTP Do, response_headers
//      when headers arrive, first_byte at the first body byte READ —
//      measured at the transport, never derived from content deltas;
//   2. a keep-alive comment line before any data line still counts as the
//      first network byte;
//   3. the engine capability probe detects --device support;
//   4. deterministicDevice selects the best ENUMERATED device through the
//      REAL enumeration path (a fake engine binary answering
//      --list-devices) and fails closed without enumeration;
//   5. the launch args carry --device <name> only when the caps profile
//      proves the build supports it.

import (
        "context"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// TestStreamOnceEmitsTransportMarks pins the mark ordering + the
// keep-alive-first-byte semantics against a real SSE server.
func TestStreamOnceEmitsTransportMarks(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/event-stream")
                w.WriteHeader(http.StatusOK)

                // A keep-alive comment line BEFORE any data: the first network
                // byte arrives before the first content chunk.
                _, _ = w.Write([]byte(": keep-alive\n\n"))

                if f, ok := w.(http.Flusher); ok {
                        f.Flush()
                }

                time.Sleep(50 * time.Millisecond)

                _, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
                _, _ = w.Write([]byte("data: [DONE]\n\n"))
        }))
        defer server.Close()

        cfg := config.Default()
        cfg.Provider = "remote"
        cfg.RemoteBaseURL = server.URL + "/v1"
        cfg.RemoteAPIKey = "test"
        cfg.RemoteModel = "m"
        cfg.DataDir = t.TempDir()

        client := NewClient(config.NewSource(cfg))

        req := client.BuildChatRequest("m", []Message{{Role: "user", Content: "x"}}, nil)

        body, err := req.MarshalJSON()
        if err != nil {
                t.Fatalf("marshal: %v", err)
        }

        var order []string
        seen := map[string]bool{}

        err = client.streamOnce(context.Background(), req, body, func(ev StreamEvent) error {
                if ev.TimingMark != "" && !seen[ev.TimingMark] {
                        seen[ev.TimingMark] = true
                        order = append(order, ev.TimingMark)
                }
                return nil
        })
        if err != nil {
                t.Fatalf("streamOnce: %v", err)
        }

        want := []string{TimingMarkRequestSent, TimingMarkResponseHeader, TimingMarkFirstByte}

        if len(order) != len(want) {
                t.Fatalf("marks = %v, want exactly %v", order, want)
        }

        for i := range want {
                if order[i] != want[i] {
                        t.Fatalf("mark order = %v, want %v (the transport ladder must be ordered)", order, want)
                }
        }
}

// fakeEngineBinary returns the fake engine executable path (this test
// binary, re-executed via the TestMain dispatch in llama_test.go).
// The --help probe is argv-triggered so it keeps working under the
// sanitized probe environment; the --list-devices variant is selected
// by SHEYTAN_FAKE_ENGINE_DEVICES (the enumeration invocation passes the
// parent environment through).
func fakeEngineBinary(t *testing.T, deviceMode string) string {
        t.Helper()

        exe, err := os.Executable()
        if err != nil {
                t.Fatalf("test binary path: %v", err)
        }

        if deviceMode != "" {
                t.Setenv("SHEYTAN_FAKE_ENGINE_DEVICES", deviceMode)
        }

        // The engine binary path keys the per-binary enumeration cache
        // (a 24h TTL map); every test here shares ONE binary, so the
        // cache must start empty or a previous test's enumeration would
        // leak into the next scenario.
        resetEngineDeviceCacheForTests()

        return exe
}

// TestDeviceFlagCapabilityParses pins the capability detection for the
// --device flag (help-parse against a real fake binary + conservative tag
// fallback).
func TestDeviceFlagCapabilityParses(t *testing.T) {
        bin := fakeEngineBinary(t, "") // --help is argv-triggered: no env mode needed

        caps := parseHelpCaps(bin, "b4600")
        if caps == nil {
                t.Fatal("help parse returned nil although the options are listed")
        }

        if !caps.DeviceFlag {
                t.Fatal("the help output lists --device but the caps profile says unsupported")
        }

        if old := defaultCapsForTag("b1000"); old.DeviceFlag {
                t.Fatal("tag b1000 predates --device — the fallback must be fail-closed")
        }

        if modern := defaultCapsForTag("b4500"); !modern.DeviceFlag {
                t.Fatal("tag b4500 is past the --device arrival — the fallback should support it")
        }
}

// TestDeterministicDeviceSelectsBestEnumerated pins the deterministic
// selection through the REAL enumeration path: a fake engine enumerating
// an iGPU and a discrete Arc resolves to the Arc (most memory), and the
// selection is the engine's OWN device name.
func TestDeterministicDeviceSelectsBestEnumerated(t *testing.T) {
        bin := fakeEngineBinary(t, "two")

        devices, supported, err := EnumerateEngineDevices(bin)
        if err != nil || !supported {
                t.Fatalf("enumeration failed: supported=%t err=%v", supported, err)
        }

        if len(devices) != 2 {
                t.Fatalf("enumerated %d devices, want 2: %+v", len(devices), devices)
        }

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.LlamaBinPath = bin

        srv := NewLlamaServer(config.NewSource(cfg))

        dev, ok := srv.deterministicDevice(cfg)
        if !ok {
                t.Fatal("deterministic selection failed although the engine enumerated two devices")
        }

        if dev != "Vulkan1" {
                t.Fatalf("selected device = %q, want Vulkan1 (the Arc — most memory, the multi-GPU laptop case)", dev)
        }
}

// TestDeterministicDeviceFailsClosedWithoutEnumeration pins fail-closed:
// no enumeration support → NO inferred device name is ever passed.
func TestDeterministicDeviceFailsClosedWithoutEnumeration(t *testing.T) {
        // A binary that rejects --list-devices (the unknown-argument error).
        bin := fakeEngineBinary(t, "unknown")

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.LlamaBinPath = bin

        srv := NewLlamaServer(config.NewSource(cfg))

        if dev, ok := srv.deterministicDevice(cfg); ok {
                t.Fatalf("deterministic selection returned %q without enumeration support — must fail closed", dev)
        }

        // No binary at all.
        cfg.LlamaBinPath = ""

        if dev, ok := srv.deterministicDevice(cfg); ok {
                t.Fatalf("deterministic selection returned %q with no binary — must fail closed", dev)
        }
}

// TestLaunchArgsCarryDeviceWhenSupported pins the launcher wiring: with
// caps reporting --device support and an enumerated device, the launch
// args carry --device <name>; without caps support they never do.
func TestLaunchArgsCarryDeviceWhenSupported(t *testing.T) {
        bin := fakeEngineBinary(t, "one")

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.LlamaBinPath = bin
        cfg.GPUAutoOffload = true
        cfg.LLM.NumGPU = 0 // exercise the auto-offload path
        cfg.LlamaHost = "127.0.0.1"
        cfg.LlamaPort = 1

        model := filepath.Join(t.TempDir(), "model.gguf")
        if err := os.WriteFile(model, []byte("x"), 0o644); err != nil {
                t.Fatalf("write model: %v", err)
        }

        srv := NewLlamaServer(config.NewSource(cfg))

        // v1.8.6: auto-offload is enabled by PROVEN execution, not by device
        // enumeration. The current boot's measured offload line is the
        // per-boot execution proof (ObserveEngineLine is the same collector
        // the engine's stdout line writers feed).
        srv.ObserveEngineLine("llm_load_tensors: offloaded 33/33 layers to GPU")

        capsWith := &EngineCaps{Tag: "b4600", DeviceFlag: true, NoWebUI: true, Jinja: true}

        args := srv.buildArgsWithCaps(cfg, model, 1, capsWith)

        found := false

        for i := 0; i < len(args)-1; i++ {
                if args[i] == "--device" && args[i+1] == "Vulkan0" {
                        found = true
                }
        }

        if !found {
                t.Fatalf("--device Vulkan0 missing from launch args: %v", args)
        }

        // Fail-closed: caps WITHOUT --device support never pass it.
        capsWithout := &EngineCaps{Tag: "b1000", DeviceFlag: false, NoWebUI: true, Jinja: true}

        for _, a := range srv.buildArgsWithCaps(cfg, model, 1, capsWithout) {
                if a == "--device" {
                        t.Fatal("--device passed although the caps profile says the build does not support it — fail-closed violated")
                }
        }

        // No GPU layers → no --device either (CPU launches never select a device).
        cpuCfg := config.Default()
        cpuCfg.DataDir = cfg.DataDir
        cpuCfg.LlamaBinPath = bin
        cpuCfg.GPUAutoOffload = false
        cpuCfg.LLM.NumGPU = 0
        cpuCfg.LlamaHost = "127.0.0.1"
        cpuCfg.LlamaPort = 1

        for _, a := range srv.buildArgsWithCaps(cpuCfg, model, 1, capsWith) {
                if a == "--device" {
                        t.Fatal("--device passed on a CPU launch (no GPU layers) — device selection must be GPU-gated")
                }
        }
}

// resetEngineDeviceCacheForTests clears the package-private per-binary
// enumeration cache (24h TTL, keyed by the engine binary path). The
// fake engine IS the test binary here, so every scenario would
// otherwise share one cache entry and a previous test's enumeration
// result would leak into the next. Same-package white-box access keeps
// the production code free of test hooks.
func resetEngineDeviceCacheForTests() {
        deviceMu.Lock()
        deviceCache = map[string]deviceCacheEntry{}
        deviceMu.Unlock()
}
