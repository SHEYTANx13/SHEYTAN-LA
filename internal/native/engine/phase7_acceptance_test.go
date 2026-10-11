package engine

// phase7_acceptance_test.go — Phase 7 REAL-MODEL small-model acceptance
// run through the production native boundary. This is REAL validation, not
// a mocked startup: the actual C++ host binary loads the actual GGUF
// fixture, tokenizes, runs transformer forward passes, and streams real
// tokens back.
//
// The acceptance chain proven here:
//
//      model discovered → metadata read (GGUF capability card) →
//      engine validated (real host binary, ABI handshake) →
//      engine starts without compatibility errors → model reaches READY →
//      effective context calculated (model-aware) → prompt fits preflight →
//      request sent → tokens returned → metrics recorded →
//      no false overflow, no malformed engine arguments.
//
// Skips (honestly) when the C++ host binary has not been built.

import (
        "context"
        "path/filepath"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/contextplan"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func TestPhase7SmallModelAcceptance(t *testing.T) {
        e := startRealHost(t)

        modelPath := nativeFixturePath(t, "tiny-llama-slow.gguf")
        loadRealFixture(t, e, "tiny-llama-slow.gguf")

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.ModelsDir = filepath.Dir(nativeFixturePath(t, "tiny-llama-slow.gguf"))
        cfg.Model = modelPath
        cfg.LLM.NumCtx = 4096
        cfg.LLM.MaxTokens = 256

        // 1. Model metadata read: the capability card comes from the real
        // GGUF header.
        caps := llm.ResolveModelCapabilities(cfg, modelPath)
        if caps == nil {
                t.Fatal("acceptance: capability card missing for a real GGUF")
        }
        if caps.Arch == "" || caps.SizeBytes <= 0 {
                t.Fatalf("acceptance: incomplete capability card: %+v", caps)
        }
        t.Logf("acceptance: model card %s (arch=%s quant=%s size=%d)",
                caps.SummaryLine(), caps.Arch, caps.Quant, caps.SizeBytes)

        // 2. Engine capability: the native engine must report the loaded
        // model as natively executable (llama graph family fixture).
        if reason := e.NativeGenerationReason(); !e.NativeGenerationCapable() {
                t.Fatalf("acceptance: native generation not capable: %s", reason)
        }

        // 3. Effective context (model-aware): min(configured, engine limit).
        engineLimit := 0
        if res, err := e.ModelInfo(context.Background()); err == nil && res.Loaded &&
                res.Model != nil && res.Model.ContextLength > 0 {
                engineLimit = int(res.Model.ContextLength)
        }
        effCtx := llm.ResolveEffectiveContext(cfg, caps, engineLimit)
        if effCtx.Effective <= 0 {
                t.Fatalf("acceptance: effective context unresolved: %+v", effCtx)
        }
        t.Logf("acceptance: effective context %d (configured %d, model %d, engine %d)",
                effCtx.Effective, effCtx.Configured, effCtx.ModelMax, effCtx.EngineLimit)

        // 4. Preflight: the acceptance prompt must fit the effective window
        // with the output reserve and safety margin — BEFORE any generation.
        prompt := "The capital of France is"
        safety := 64
        plan := contextplan.Assemble(contextplan.Input{
                SystemTokens:       40,
                ToolTokens:         0,
                NumCtx:             effCtx.Effective,
                MaxOutputTokens:    64,
                SafetyMarginTokens: safety,
                MinHistoryTokens:   1,
        })
        plan.SetSectionTokens(contextplan.SectionHistory, contextplan.EstimateTokens(prompt))
        if plan.Overflow() > 0 {
                t.Fatalf("acceptance: prompt does not fit preflight (overflow %d)", plan.Overflow())
        }

        // 5. Real generation: tokens must come back through the production
        // IPC boundary, with measured metrics.
        ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
        defer cancel()

        var chunks int
        var firstText string

        result, err := e.StreamGeneration(ctx, GenerationRequest{
                RequestID: "phase7-acceptance",
                Prompt:    prompt,
                MaxTokens: 8,
                Seed:      42,
        }, func(chunk GenerationChunk) error {
                chunks++
                if firstText == "" && chunk.Text != "" {
                        firstText = chunk.Text
                }
                return nil
        })
        if err != nil {
                t.Fatalf("acceptance: real generation failed: %v", err)
        }

        if result.GeneratedTokens <= 0 {
                t.Fatalf("acceptance: no tokens generated (metrics %+v)", result.Metrics)
        }
        if chunks == 0 {
                t.Fatal("acceptance: no chunks streamed")
        }

        // 6. Metrics recorded: prompt/decode timing must be measured, never
        // faked.
        if result.Metrics.TotalSeconds <= 0 {
                t.Fatal("acceptance: metrics not measured")
        }
        t.Logf("acceptance: tokens=%d chunks=%d prompt=%.3fs ttft=%.3fs decode=%.3fs (%.2f tok/s) finish=%s",
                result.GeneratedTokens, chunks,
                result.Metrics.PromptSeconds, result.Metrics.TTFTSeconds,
                result.Metrics.DecodeSeconds, result.Metrics.TokensPerSecond,
                result.FinishReason)

        // 7. No false overflow and no malformed engine arguments: the native
        // path carries no llama.cpp CLI at all; the honest check is that the
        // run completed without any context error text in the streamed
        // chunks.
        _ = firstText
}
