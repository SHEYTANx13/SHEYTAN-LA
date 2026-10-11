package agent

// localchat_v181_test.go — v1.8.1 P0 REGRESSION: the real-Windows `hi`
// crash (local backend + small Gemma-class model + one-word chat + no
// capability signals).
//
// THE DEFECT (reproduced from the v1.8.0 Windows runtime log): a Gemma-
// class GGUF whose tokenizer metadata block exceeds the parser's read
// bound makes llm.ReadModelCard fail → ResolveModelCapabilities returns
// nil → resolveEffectiveContext dereferenced caps.TokenizerFamily →
// panic "runtime error: invalid memory address or nil pointer
// dereference" between `task classified` and `tier selected`, the run
// settling as error in ~57 ms with no request reaching the engine.
//
// The regressions here pin BOTH layers of the repair:
//
//   1. the nil-caps guard (the crash itself — any unreadable card);
//   2. the raised GGUF read bound (real Gemma-class cards parse again).

import (
        "bytes"
        "encoding/binary"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// putU32/putU64/putStr write little-endian GGUF scalars.
func v181PutU32(buf *bytes.Buffer, v uint32) {
        var b [4]byte
        binary.LittleEndian.PutUint32(b[:], v)
        buf.Write(b[:])
}

func v181PutU64(buf *bytes.Buffer, v uint64) {
        var b [8]byte
        binary.LittleEndian.PutUint64(b[:], v)
        buf.Write(b[:])
}

func v181PutStr(buf *bytes.Buffer, s string) {
        v181PutU64(buf, uint64(len(s)))
        buf.WriteString(s)
}

// writeGemmaClassGGUF writes a VALID GGUF v3 file with the Gemma E2B
// tokenizer shape: `arch` metadata plus a 262144-entry tokenizer block
// (tokens + scores + token_type). tokenLen tunes the metadata size past
// or under the parser's read bound.
func writeGemmaClassGGUF(t *testing.T, path, arch string, tokenCount, tokenLen int) {
        t.Helper()

        var buf bytes.Buffer
        buf.WriteString("GGUF")
        v181PutU32(&buf, 3) // version 3
        v181PutU64(&buf, 0) // tensor count (unused by the parser)
        v181PutU64(&buf, 7) // kv count

        v181PutStr(&buf, "general.architecture")
        v181PutU32(&buf, 8)
        v181PutStr(&buf, arch)

        v181PutStr(&buf, "general.name")
        v181PutU32(&buf, 8)
        v181PutStr(&buf, "Gemma E2B IT")

        v181PutStr(&buf, arch+".context_length")
        v181PutU32(&buf, 4)
        v181PutU32(&buf, 32768)

        v181PutStr(&buf, arch+".block_count")
        v181PutU32(&buf, 4)
        v181PutU32(&buf, 28)

        v181PutStr(&buf, arch+".embedding_length")
        v181PutU32(&buf, 4)
        v181PutU32(&buf, 2048)

        v181PutStr(&buf, "tokenizer.ggml.tokens")
        v181PutU32(&buf, 9) // array
        v181PutU32(&buf, 8) // element type: string
        v181PutU64(&buf, uint64(tokenCount))
        tok := strings.Repeat("a", tokenLen)
        for i := 0; i < tokenCount; i++ {
                v181PutStr(&buf, tok)
        }

        v181PutStr(&buf, "tokenizer.ggml.scores")
        v181PutU32(&buf, 9)
        v181PutU32(&buf, 6) // float32
        v181PutU64(&buf, uint64(tokenCount))
        for i := 0; i < tokenCount; i++ {
                v181PutU32(&buf, 0)
        }

        v181PutStr(&buf, "tokenizer.ggml.token_type")
        v181PutU32(&buf, 9)
        v181PutU32(&buf, 5) // int32
        v181PutU64(&buf, uint64(tokenCount))
        for i := 0; i < tokenCount; i++ {
                v181PutU32(&buf, 1)
        }

        if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
                t.Fatalf("write gguf: %v", err)
        }
}

// TestResolveEffectiveContextSurvivesUnreadableCard pins the crash
// repair itself: an unreadable model card (nil caps) must fall back to
// the documented config-values path, never panic. The pre-v1.8.1 code
// panicked here with the exact Windows signature.
func TestResolveEffectiveContextSurvivesUnreadableCard(t *testing.T) {
        dir := t.TempDir()
        models := filepath.Join(dir, "models")
        if err := os.MkdirAll(models, 0o755); err != nil {
                t.Fatal(err)
        }

        // A file that is NOT a GGUF at all: ReadModelCard fails at the magic,
        // ResolveModelCapabilities returns nil — the pure nil-caps posture.
        notGGUF := filepath.Join(models, "broken-model.gguf")
        if err := os.WriteFile(notGGUF, []byte("THIS IS NOT A GGUF FILE......"), 0o644); err != nil {
                t.Fatal(err)
        }

        cfg := config.Default()
        cfg.DataDir = dir
        cfg.ModelsDir = models
        cfg.Model = "broken-model.gguf"
        cfg.Provider = config.ProviderLocal

        src := config.NewSource(cfg)
        orch := New(src, nil)

        // The llama.cpp-backend posture: no exact tokenizer installed, so the
        // estimator block (the old deref site) executes on every local run.
        chunking.ResetTokenEstimator()

        // The pre-fix panic surfaces here; a panic in a test fails the test
        // with the stack trace — the honest failure mode, no swallowing.
        ec := orch.resolveEffectiveContext(cfg, 0)

        if ec.Effective <= 0 {
                t.Fatalf("nil-caps fallback: effective context = %d, want the configured default", ec.Effective)
        }
        if ec.ModelMax != 0 {
                t.Fatalf("nil-caps fallback: modelMax = %d, want 0 (unknown, honestly reported)", ec.ModelMax)
        }
        if k := chunking.EstimatorKind(); k != chunking.EstimatorHeuristic {
                t.Fatalf("nil-caps fallback: estimator kind = %v, want the conservative heuristic", k)
        }
}

// TestGemmaClassCardParsesUnderRaisedBound pins the readability repair:
// a Gemma-class GGUF whose tokenizer block exceeds the OLD 8 MiB bound
// parses under the v1.8.1 bound, so the model-aware context decision
// actually sees the small model's training limit.
func TestGemmaClassCardParsesUnderRaisedBound(t *testing.T) {
        dir := t.TempDir()
        models := filepath.Join(dir, "models")
        if err := os.MkdirAll(models, 0o755); err != nil {
                t.Fatal(err)
        }

        // 262144 tokens x (8 + 24) bytes ≈ 8.4 MiB of tokens + 1 MiB scores
        // + 1 MiB types ≈ 10.5 MiB metadata — past the old bound, well under
        // the new one.
        path := filepath.Join(models, "gemma-4-E2B-it-Q4_K_M.gguf")
        writeGemmaClassGGUF(t, path, "gemma3n", 262144, 24)

        card, err := llm.ReadModelCard(path)
        if err != nil {
                t.Fatalf("Gemma-class card must parse under the v1.8.1 bound: %v", err)
        }
        if card.Arch != "gemma3n" {
                t.Fatalf("arch = %q, want gemma3n", card.Arch)
        }
        if card.ContextLength != 32768 {
                t.Fatalf("context length = %d, want 32768 (the model-aware clamp source)", card.ContextLength)
        }

        caps := llm.ResolveModelCapabilities(nil, path)
        if caps == nil {
                t.Fatal("ResolveModelCapabilities must not be nil for a readable Gemma-class card")
        }
        if caps.TokenizerFamily != "BPE" {
                t.Fatalf("tokenizer family = %q, want BPE (gemma lineage)", caps.TokenizerFamily)
        }
        if !caps.ChatTemplate {
                t.Fatal("gemma lineage ships a chat template — the --jinja hint must be on")
        }
}

// TestResolveEffectiveContextGemmaClassEndToEnd drives the exact crash
// environment class end-to-end at the unit seam: local provider, small
// Gemma-class model, heuristic estimator (llama.cpp backend), a
// session-context policy of 0 — the same inputs RunDetailed feeds
// resolveEffectiveContext on every turn.
func TestResolveEffectiveContextGemmaClassEndToEnd(t *testing.T) {
        dir := t.TempDir()
        models := filepath.Join(dir, "models")
        if err := os.MkdirAll(models, 0o755); err != nil {
                t.Fatal(err)
        }

        path := filepath.Join(models, "gemma-4-E2B-it-Q4_K_M.gguf")
        writeGemmaClassGGUF(t, path, "gemma3n", 262144, 24)

        cfg := config.Default()
        cfg.DataDir = dir
        cfg.ModelsDir = models
        cfg.Model = "gemma-4-E2B-it-Q4_K_M.gguf"
        cfg.Provider = config.ProviderLocal
        cfg.LLM.NumCtx = 16384 // under the model limit: the configured ceiling stands

        src := config.NewSource(cfg)
        orch := New(src, nil)

        chunking.ResetTokenEstimator()

        ec := orch.resolveEffectiveContext(cfg, 0)

        // The card parsed: ModelMax is honestly reported (32768), and the
        // configured ceiling (16384, BELOW the model limit) stands — the
        // small-model clamp only engages when the model limit is SMALLER.
        if ec.ModelMax != 32768 {
                t.Fatalf("modelMax = %d, want 32768 (parsed from the Gemma-class card)", ec.ModelMax)
        }
        if ec.Effective != 16384 {
                t.Fatalf("effective = %d, want 16384 (the configured ceiling under the model limit)", ec.Effective)
        }

        // The family estimator engages from the parsed card.
        if k := chunking.EstimatorKind(); k != chunking.EstimatorModelFamily {
                t.Fatalf("estimator kind = %v, want the family tier from the parsed Gemma-class card", k)
        }
}
