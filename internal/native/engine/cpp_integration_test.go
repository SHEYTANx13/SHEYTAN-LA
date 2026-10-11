package engine

// cpp_integration_test.go — end-to-end test against the REAL C++ host
// binary (native/engine/build/shtn-engine-host).
//
// Skips when the C++ build artifact is not present (e.g. environments that
// only build the Go side); run `cmake -S native/engine -B
// native/engine/build && cmake --build native/engine/build` to enable it.
// When present, this test proves the actual Go↔C++ boundary: framing,
// handshake, health, hardware, metrics, cancel and shutdown all round-trip
// against the real C++ implementation.

import (
        "context"
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func realHostBinaryPath() string {
        // This file lives at internal/native/engine/; the C++ tree is at
        // <repo>/native/engine/build/.
        _, thisFile, _, _ := runtime.Caller(0)
        repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))

        name := "shtn-engine-host"
        if runtime.GOOS == "windows" {
                name += ".exe"
        }

        return filepath.Join(repoRoot, "native", "engine", "build", name)
}

func TestRealCppHostEndToEnd(t *testing.T) {
        bin := realHostBinaryPath()

        if !fileExists(bin) {
                t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", bin)
        }

        e := New(bin)

        if !e.Available() {
                t.Fatalf("host binary reported unavailable: %s", bin)
        }

        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        // Boot through the real handshake (protocol + ABI check).
        if err := e.Start(ctx); err != nil {
                t.Fatalf("start against real C++ host: %v", err)
        }

        t.Cleanup(func() {
                stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
                defer stopCancel()
                _ = e.Stop(stopCtx)
        })

        if e.State() != "ready" {
                t.Fatalf("state = %q, want ready", e.State())
        }

        // Active health round-trip.
        report, err := e.Health(ctx)
        if err != nil {
                t.Fatalf("health: %v", err)
        }

        if !report.Alive {
                t.Fatalf("health report: %+v", report)
        }

        // Real hardware facts from the C++ side merged with sysinfo.
        hw, err := e.Hardware(ctx)
        if err != nil {
                t.Fatalf("hardware: %v", err)
        }

        if hw.Architecture == "" {
                t.Fatal("architecture missing from real hwinfo")
        }

        if hw.CPU.LogicalCores <= 0 {
                t.Fatalf("logical cores = %d, want > 0", hw.CPU.LogicalCores)
        }

        if hw.RAM.TotalBytes == 0 {
                t.Fatal("RAM total missing from real hwinfo")
        }

        // Real measured metrics (the C++ engine measures its own RSS).
        m, err := e.MetricsSnapshot(ctx)
        if err != nil {
                t.Fatalf("metrics: %v", err)
        }

        if m.ProcessRSSBytes == 0 {
                t.Fatal("C++ engine did not measure its RSS")
        }

        if m.UptimeSeconds <= 0 {
                t.Fatal("uptime not measured")
        }

        // Cancel is a real round-trip. In Phase 5 there are no in-flight
        // requests yet in this test, so the cancel honestly reports a miss
        // for an unknown id (the real cancellation path — active request —
        // is covered by the Phase 5 generation tests below).
        err = e.Cancel(ctx, "req-e2e")
        if err == nil || !strings.Contains(err.Error(), "no queued or active") {
                t.Fatalf("cancel against real host: %v", err)
        }

        // Clean stop path against the real host.
        if err := e.Stop(ctx); err != nil {
                t.Fatalf("stop: %v", err)
        }

        if e.State() != "stopped" {
                t.Fatalf("state = %q, want stopped", e.State())
        }
}

func fileExists(path string) bool {
        _, err := os.Stat(path)
        return err == nil
}

// --- Phase 2: real Go → host → C++ → LoadModel → ModelInfo -------------------

// writeTestGGUF builds a small, fully valid GGUF v3 file (llama arch,
// 3 tensors, vocab array) and returns its path. This is the real format
// the C++ reader parses — no fakes on either side of the boundary.
func writeTestGGUF(t *testing.T, dir, name string) string {
        t.Helper()

        var b []byte
        put32 := func(v uint32) {
                b = append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
        }
        put64 := func(v uint64) {
                put32(uint32(v & 0xFFFFFFFF))
                put32(uint32(v >> 32))
        }
        putStr := func(s string) {
                put64(uint64(len(s)))
                b = append(b, s...)
        }

        b = append(b, 'G', 'G', 'U', 'F')
        put32(3) // version
        put64(3) // tensor count
        put64(8) // kv count

        // Metadata.
        putStr("general.architecture")
        put32(8) // string
        putStr("llama")
        putStr("general.name")
        put32(8)
        putStr("integration-model")
        putStr("general.file_type")
        put32(4) // uint32
        put32(15)
        putStr("llama.context_length")
        put32(4)
        put32(256)
        putStr("llama.embedding_length")
        put32(4)
        put32(64)
        putStr("llama.block_count")
        put32(4)
        put32(2)
        putStr("llama.vocab_size")
        put32(4)
        put32(96)
        putStr("tokenizer.ggml.tokens")
        put32(9) // array
        put32(8) // of strings
        put64(96)
        for i := 0; i < 96; i++ {
                putStr("tok")
        }

        // Tensor table.
        putStr("token_embd.weight")
        put32(2) // n_dims
        put64(64)
        put64(96)
        put32(0) // F32
        put64(0) // offset

        putStr("output.weight")
        put32(2)
        put64(96)
        put64(64)
        put32(0)
        put64(64 * 96 * 4)

        putStr("blk.0.attn_norm.weight")
        put32(1)
        put64(64)
        put32(1) // F16
        put64(64*96*4 + 96*64*4)

        // Pad the header to the 32-byte alignment, then the data section.
        for len(b)%32 != 0 {
                b = append(b, 0)
        }
        dataBytes := 64*96*4 + 96*64*4 + 64*2
        b = append(b, make([]byte, dataBytes)...)

        path := filepath.Join(dir, name)
        if err := os.WriteFile(path, b, 0o644); err != nil {
                t.Fatalf("write test gguf: %v", err)
        }
        return path
}

func TestRealCppHostModelLifecycle(t *testing.T) {
        bin := realHostBinaryPath()

        if !fileExists(bin) {
                t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", bin)
        }

        // v1.4.0 (run 35996462352): the GGUF fixture directory is created
        // BEFORE the engine-stop cleanup is registered. Go runs t.Cleanup in
        // LIFO order, so the previously-late t.TempDir registration made the
        // temp-tree removal run BEFORE the engine stopped — on Windows the
        // host process still held the loaded GGUF open, the delete failed
        // with "Access is denied", and the whole test was marked failed
        // during cleanup. With this ordering the engine is stopped (and its
        // process reaped) first, and the temp tree is deleted only
        // afterwards.
        fixtureDir := t.TempDir()

        e := New(bin)

        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        if err := e.Start(ctx); err != nil {
                t.Fatalf("start: %v", err)
        }

        t.Cleanup(func() {
                stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
                defer stopCancel()
                _ = e.Stop(stopCtx)
        })

        // Fresh engine: unloaded.
        info, err := e.ModelInfo(ctx)
        if err != nil {
                t.Fatalf("model info on fresh engine: %v", err)
        }
        if info.Loaded || info.State != ModelStateUnloaded {
                t.Fatalf("fresh engine model info: %+v", info)
        }

        // Load the real GGUF through the real C++ reader.
        model := writeTestGGUF(t, fixtureDir, "integration.gguf")

        if err := e.LoadModel(ctx, ModelSpec{Path: model}); err != nil {
                t.Fatalf("native load: %v", err)
        }

        if state := e.NativeModelState(); state != ModelStateLoaded {
                t.Fatalf("model state = %q, want loaded", state)
        }

        info, err = e.ModelInfo(ctx)
        if err != nil {
                t.Fatalf("model info after load: %v", err)
        }

        if !info.Loaded || info.State != ModelStateLoaded {
                t.Fatalf("model info after load: %+v", info)
        }

        m := info.Model
        if m == nil {
                t.Fatal("no model card after load")
        }

        if m.Path != model {
                t.Fatalf("model path = %q, want %q", m.Path, model)
        }
        if m.Architecture != "llama" {
                t.Fatalf("architecture = %q, want llama", m.Architecture)
        }
        if m.Name != "integration-model" {
                t.Fatalf("name = %q, want integration-model", m.Name)
        }
        if m.Quantization != "Q4_K_M" {
                t.Fatalf("quantization = %q, want Q4_K_M (file_type 15)", m.Quantization)
        }
        if m.GGUFVersion != 3 {
                t.Fatalf("gguf version = %d, want 3", m.GGUFVersion)
        }
        if m.TensorCount != 3 {
                t.Fatalf("tensor count = %d, want 3", m.TensorCount)
        }
        if m.ContextLength != 256 || m.VocabularySize != 96 ||
                m.EmbeddingLength != 64 || m.LayerCount != 2 {
                t.Fatalf("metadata mismatch: %+v", m)
        }

        // Parameter count derived from the tensor table: 64*96 + 96*64 + 64.
        wantParams := uint64(64*96 + 96*64 + 64)
        if m.ParameterCount != wantParams {
                t.Fatalf("parameter count = %d, want %d", m.ParameterCount, wantParams)
        }

        // Memory plan: weights = data span; KV = 2*2*layers*ctx*emb.
        p := info.Memory
        if p == nil {
                t.Fatal("no memory plan after load")
        }
        if p.WeightsBytes == 0 || p.WeightsBytes > p.ModelFileBytes {
                t.Fatalf("weights bytes implausible: %+v", p)
        }
        if wantKV := uint64(2 * 2 * 2 * 256 * 64); p.KVCacheBytes != wantKV {
                t.Fatalf("kv cache bytes = %d, want %d", p.KVCacheBytes, wantKV)
        }
        if wantWS := uint64(256 * 96 * 4); p.WorkspaceBytes != wantWS {
                t.Fatalf("workspace bytes = %d, want %d", p.WorkspaceBytes, wantWS)
        }
        if p.TotalBytes != p.ModelFileBytes+p.KVCacheBytes+p.WorkspaceBytes+p.RuntimeOverheadBytes {
                t.Fatalf("plan total mismatch: %+v", p)
        }

        // Unload → unloaded; reload → loaded again.
        if err := e.UnloadModel(ctx); err != nil {
                t.Fatalf("unload: %v", err)
        }

        info, err = e.ModelInfo(ctx)
        if err != nil {
                t.Fatal(err)
        }
        if info.Loaded || info.State != ModelStateUnloaded {
                t.Fatalf("model info after unload: %+v", info)
        }

        if err := e.LoadModel(ctx, ModelSpec{Path: model}); err != nil {
                t.Fatalf("reload: %v", err)
        }
        if state := e.NativeModelState(); state != ModelStateLoaded {
                t.Fatalf("model state after reload = %q", state)
        }

        // Failed load: a garbage file fails cleanly, state walks to failed,
        // and the engine stays alive. The garbage fixture goes into the SAME
        // pre-created fixtureDir (v1.5.0 lifecycle audit): every temp-tree
        // registration stays BEFORE the engine-stop cleanup, so the removal
        // order can never invert on Windows.
        garbage := filepath.Join(fixtureDir, "garbage.gguf")
        if err := os.WriteFile(garbage, []byte("definitely not a gguf file"), 0o644); err != nil {
                t.Fatal(err)
        }

        if err := e.LoadModel(ctx, ModelSpec{Path: garbage}); err == nil {
                t.Fatal("garbage file must fail to load")
        }

        if state := e.NativeModelState(); state != ModelStateFailed {
                t.Fatalf("model state after failed load = %q, want failed", state)
        }

        report, err := e.Health(ctx)
        if err != nil || !report.Alive {
                t.Fatalf("engine must stay healthy after a failed load: %+v (%v)", report, err)
        }

        // Recovery: a valid load succeeds again afterwards.
        if err := e.LoadModel(ctx, ModelSpec{Path: model}); err != nil {
                t.Fatalf("load after failure must recover: %v", err)
        }

        // Backend mapping onto the shared llm.ModelInfo contract.
        backend := NewBackend(e)
        llmInfo, err := backend.ModelInfo(ctx)
        if err != nil {
                t.Fatalf("backend model info: %v", err)
        }
        if !llmInfo.Loaded || llmInfo.Backend != "native" {
                t.Fatalf("backend model info: %+v", llmInfo)
        }
        if llmInfo.Architecture != "llama" || llmInfo.ContextLength != 256 ||
                llmInfo.Quantization != "Q4_K_M" {
                t.Fatalf("backend model card mismatch: %+v", llmInfo)
        }
        if llmInfo.Parameters != "12K" { // 12352 → 12K by llm formatting
                t.Fatalf("parameters = %q, want 12K", llmInfo.Parameters)
        }
        if llmInfo.VocabSize != 96 || llmInfo.LayerCount != 2 ||
                llmInfo.TensorCount != 3 || llmInfo.GGUFVersion != 3 {
                t.Fatalf("backend additive fields mismatch: %+v", llmInfo)
        }
        if llmInfo.KVCacheEstimateBytes == 0 || llmInfo.TotalMemoryEstimateBytes == 0 {
                t.Fatalf("backend memory estimates missing: %+v", llmInfo)
        }

        // Generation honesty on a NON-executable synthetic model: this
        // writeTestGGUF fixture carries no llama tensor graph or usable
        // tokenizer, so the load verdict is generation-capable=0 — the honest
        // capability signal the selection layer routes to llama.cpp (real
        // generation through a capable fixture is proven by the Phase 5
        // integration tests). An empty request must error, never fake success.
        if _, err := backend.Generate(ctx, &llm.ChatRequest{}); err == nil {
                t.Fatal("Generate must fail without a usable prompt/model")
        }
        if backend.GenerationCapable() {
                t.Fatal("synthetic fixture must not report generation capability (no llama graph / tokenizer)")
        }
}
