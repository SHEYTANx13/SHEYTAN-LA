package engine

// phase4_integration_test.go — end-to-end tests for the Phase 4 native
// engine foundation: tokenizer, KV cache, and scheduler through the real
// C++ host binary.
//
// Skips when the C++ host binary is not built (the same skip policy as
// cpp_integration_test.go). When present, these tests prove the actual
// Go↔C++ boundary for the Phase 4 ops:
//   - tokenizer_init / tokenizer_info / tokenizer_encode / tokenizer_decode
//   - kv_cache_info
//   - scheduler_info
//
// What these tests do NOT do:
//   - they do NOT generate text (no forward pass exists);
//   - they do NOT claim measured inference metrics (TTFT, tokens/sec);
//   - they do NOT exercise a real production model — they use a small
//     synthetic BPE GGUF fixture written inline (the same pattern
//     cpp_integration_test.go uses for the Phase 2 model-lifecycle test).

import (
        "context"
        "os"
        "path/filepath"
        "testing"
        "time"
)

// writeBPEGGUF builds a small valid GGUF v3 file with a real BPE
// tokenizer (8 vocab entries, 4 merges, BOS/EOS/UNK ids) and returns its
// path. The file is the same shape llama.cpp produces; the C++ reader
// parses it natively.
func writeBPEGGUF(t *testing.T, dir, name string) string {
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
        // f32 array (used for token scores if we add them).
        _ = put32

        b = append(b, 'G', 'G', 'U', 'F')
        put32(3)  // version
        put64(1)  // tensor count
        put64(13) // kv count (carefully counted below)

        // Metadata KV (count must match put64 above).
        putStr("general.architecture")
        put32(8)
        putStr("llama") // 1
        putStr("tokenizer.ggml.model")
        put32(8)
        putStr("llama") // 2
        // tokenizer.ggml.tokens — string array of 8 entries.
        tokens := []string{
                "<unk>", "<s>", "</s>",
                "\xE2\x96\x81", // ▁
                "h", "e",
                "\xE2\x96\x81he", // ▁he
                "llo",
        }
        putStr("tokenizer.ggml.tokens")
        put32(9)
        put32(8)
        put64(uint64(len(tokens))) // 3
        for _, tok := range tokens {
                putStr(tok)
        }
        // tokenizer.ggml.token_type — int32 array of 8 entries.
        tokenTypes := []uint32{2, 3, 3, 1, 1, 1, 1, 1} // unk, ctrl, ctrl, normal...
        putStr("tokenizer.ggml.token_type")
        put32(9)
        put32(5)
        put64(uint64(len(tokenTypes))) // 4
        for _, tt := range tokenTypes {
                put32(tt)
        }
        // tokenizer.ggml.merges — string array of 4 entries.
        merges := []string{
                "\xE2\x96\x81 h",
                "\xE2\x96\x81he", // wait, the merge is "▁h e" not "▁he" — let me check
        }
        // Actually merges are "a b" (space-separated pair). The 4 merges:
        merges = []string{
                "\xE2\x96\x81 h",  // ▁ h
                "\xE2\x96\x81h e", // ▁h e
                "l l",             // l l
                "ll o",            // ll o
        }
        putStr("tokenizer.ggml.merges")
        put32(9)
        put32(8)
        put64(uint64(len(merges))) // 5
        for _, m := range merges {
                putStr(m)
        }
        putStr("tokenizer.ggml.bos_token_id")
        put32(4)
        put32(1) // 6
        putStr("tokenizer.ggml.eos_token_id")
        put32(4)
        put32(2) // 7
        putStr("tokenizer.ggml.unknown_token_id")
        put32(4)
        put32(0) // 8
        putStr("llama.context_length")
        put32(4)
        put32(64) // 9
        putStr("llama.embedding_length")
        put32(4)
        put32(8) // 10
        putStr("llama.block_count")
        put32(4)
        put32(1) // 11
        putStr("llama.vocab_size")
        put32(4)
        put32(8) // 12
        putStr("general.name")
        put32(8)
        putStr("bpe-test") // 13

        // One dummy tensor so the file isn't rejected as "no tensors".
        putStr("token_embd.weight")
        put32(2) // n_dims
        put64(8)
        put64(8)
        put32(0) // F32
        put64(0) // offset

        // Pad header to 32-byte alignment.
        for len(b)%32 != 0 {
                b = append(b, 0)
        }

        // Data section: 8x8 F32 = 256 bytes.
        dataBytes := 8 * 8 * 4
        b = append(b, make([]byte, dataBytes)...)

        path := filepath.Join(dir, name)
        if err := os.WriteFile(path, b, 0o644); err != nil {
                t.Fatalf("write BPE gguf: %v", err)
        }
        return path
}

// TestRealCppHostPhase4Tokenizer exercises the Phase 4 tokenizer surface
// end-to-end through the real C++ host: load_model → tokenizer_init →
// tokenizer_info → tokenizer_encode → tokenizer_decode.
func TestRealCppHostPhase4Tokenizer(t *testing.T) {
        bin := realHostBinaryPath()

        if !fileExists(bin) {
                t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", bin)
        }

        // v1.4.0 (run 35996462352): create the fixture directory BEFORE the
        // engine-stop cleanup is registered — t.Cleanup runs LIFO, so this
        // ordering guarantees the host process is stopped and reaped before
        // the temp tree (holding the loaded BPE GGUF) is removed. The
        // previous order made Windows TempDir cleanup delete a file the
        // engine still held open ("Access is denied").
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

        // Fresh engine: tokenizer_info reports uninitialized.
        info, err := e.TokenizerInfo(ctx)
        if err != nil {
                t.Fatalf("tokenizer info on fresh engine: %v", err)
        }
        if info.Initialized {
                t.Fatalf("fresh engine tokenizer should be uninitialized: %+v", info)
        }

        // Load the real BPE GGUF.
        model := writeBPEGGUF(t, fixtureDir, "bpe.gguf")

        if err := e.LoadModel(ctx, ModelSpec{Path: model}); err != nil {
                t.Fatalf("load model: %v", err)
        }

        // Initialize the tokenizer.
        initResult, err := e.InitTokenizer(ctx)
        if err != nil {
                t.Fatalf("init tokenizer: %v", err)
        }
        if !initResult.Initialized {
                t.Fatalf("tokenizer init did not initialize: %+v", initResult)
        }
        if initResult.Info.VocabSize != 8 {
                t.Fatalf("vocab size = %d, want 8", initResult.Info.VocabSize)
        }
        if initResult.Info.Model != "bpe" {
                t.Fatalf("model kind = %q, want bpe", initResult.Info.Model)
        }
        if !initResult.Info.HasBOS || initResult.Info.BOSID != 1 {
                t.Fatalf("BOS missing/wrong: %+v", initResult.Info)
        }
        if !initResult.Info.HasEOS || initResult.Info.EOSID != 2 {
                t.Fatalf("EOS missing/wrong: %+v", initResult.Info)
        }
        if initResult.Info.MergeCount != 4 {
                t.Fatalf("merge count = %d, want 4", initResult.Info.MergeCount)
        }

        // Idempotent init: a second call succeeds.
        if _, err := e.InitTokenizer(ctx); err != nil {
                t.Fatalf("idempotent init: %v", err)
        }

        // Encode "hello" with BOS+EOS → [1, 6, 7, 2] (BOS, ▁he, llo, EOS).
        enc, err := e.TokenizerEncode(ctx, "hello", EncodeOptions{
                AddBOS:    true,
                AddEOS:    true,
                MaxTokens: 64,
        })
        if err != nil {
                t.Fatalf("encode: %v", err)
        }
        if enc.Count != 4 {
                t.Fatalf("encode count = %d, want 4: %+v", enc.Count, enc)
        }
        want := []uint32{1, 6, 7, 2}
        for i, id := range want {
                if i >= int(enc.Count) || enc.IDs[i] != id {
                        t.Fatalf("encode ids[%d] = %d, want %d (full: %v)", i, enc.IDs[i], id, enc.IDs)
                }
        }

        // Decode [6, 7] → " hello" (▁he + llo → " hello" with leading space).
        dec, err := e.TokenizerDecode(ctx, []uint32{6, 7}, DecodeOptions{
                SkipSpecial: true,
                MaxBytes:    1024,
        })
        if err != nil {
                t.Fatalf("decode: %v", err)
        }
        if dec.Text != " hello" {
                t.Fatalf("decode text = %q, want \" hello\"", dec.Text)
        }

        // Decode with BOS/EOS skipped → same text.
        dec2, err := e.TokenizerDecode(ctx, []uint32{1, 6, 7, 2}, DecodeOptions{
                SkipSpecial: true,
                MaxBytes:    1024,
        })
        if err != nil {
                t.Fatalf("decode with specials: %v", err)
        }
        if dec2.Text != " hello" {
                t.Fatalf("decode with specials text = %q, want \" hello\"", dec2.Text)
        }
}

// TestRealCppHostPhase4KVCache exercises the kv_cache_info op through the
// real C++ host. In Phase 4 the cache is NOT auto-allocated, so this
// verifies the honest zero-state.
func TestRealCppHostPhase4KVCache(t *testing.T) {
        bin := realHostBinaryPath()

        if !fileExists(bin) {
                t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", bin)
        }

        e := New(bin)

        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        if err := e.Start(ctx); err != nil {
                t.Fatalf("start: %v", err)
        }

        t.Cleanup(func() {
                stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
                defer stopCancel()
                _ = e.Stop(stopCtx)
        })

        info, err := e.KVCacheInfo(ctx)
        if err != nil {
                t.Fatalf("kv cache info: %v", err)
        }

        // Phase 4 honest zero-state: not allocated.
        if info.Allocated {
                t.Fatalf("Phase 4 KV cache should not be auto-allocated: %+v", info)
        }
        if info.CapacityBytes != 0 {
                t.Fatalf("capacity bytes = %d, want 0 (not allocated)", info.CapacityBytes)
        }
}

// TestRealCppHostPhase4Scheduler exercises the scheduler_info op through
// the real C++ host. Counts are real (queued=0, totals since create);
// active is 0 in Phase 4 (no worker thread).
func TestRealCppHostPhase4Scheduler(t *testing.T) {
        bin := realHostBinaryPath()

        if !fileExists(bin) {
                t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", bin)
        }

        e := New(bin)

        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        if err := e.Start(ctx); err != nil {
                t.Fatalf("start: %v", err)
        }

        t.Cleanup(func() {
                stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
                defer stopCancel()
                _ = e.Stop(stopCtx)
        })

        info, err := e.SchedulerInfo(ctx)
        if err != nil {
                t.Fatalf("scheduler info: %v", err)
        }

        if info.MaxConcurrent != 1 {
                t.Fatalf("max concurrent = %d, want 1 (single-slot)", info.MaxConcurrent)
        }
        if info.ActiveRequests != 0 {
                t.Fatalf("active requests = %d, want 0 (Phase 4: no worker)", info.ActiveRequests)
        }
        if info.QueuedRequests != 0 {
                t.Fatalf("queued requests = %d, want 0 (fresh engine)", info.QueuedRequests)
        }
        if info.QueueDepthLimit == 0 {
                t.Fatal("queue depth limit = 0, want > 0")
        }
}
