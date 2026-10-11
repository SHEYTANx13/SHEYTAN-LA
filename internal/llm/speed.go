package llm

// speed.go — v1.0.4 Speed Pack: the llama.cpp launch flags that make
// SHEYTAN the fastest way to run local models on Windows.
//
// Research (Aug 2026, see release notes):
//   - Direct llama.cpp beats wrapper apps (LM Studio / Ollama) by 5-20% on
//     identical hardware — we ARE the engine host, zero wrapper overhead.
//   - --flash-attn: fused attention kernels; a straight throughput win on
//     both CPU and the bundled Vulkan backend. PHASE 7: the flag became the
//     tri-state `--flash-attn on|off|auto` option in newer releases; the
//     emitted form is now chosen from the engine capability profile
//     (capability.go), never hard-coded.
//   - --cache-reuse N: KV-shift prompt-cache reuse. Agent turns share a
//     long stable prefix (AI context + tool schemas), so after the first
//     turn the repeated prefill collapses to near zero — dramatically
//     lower time-to-first-token on every follow-up message. It is an
//     INDEPENDENT option: `--cache-reuse N` stands on its own and must
//     never be attached to another option's value slot.
//   - --ubatch-size 512: prompt-processing physical batch; the measured
//     sweet spot on x86-64 multicore.
//   - --threads = PHYSICAL cores for generation + --threads-batch =
//     LOGICAL cores for prefill: SMT siblings cost 5-15% tok/s when two
//     generation threads share one physical core, but prefill parallelizes
//     fine across all logical cores.
//   - --cache-type-k/-v q8_0 (optional): halves KV-cache memory at <5%
//     speed cost on modern GPUs — the one way to fit big contexts in
//     limited VRAM. Off by default: a minority of Vulkan drivers regress.
//     v1.7.2: the layout is capability-gated (split -k/-v on modern
//     builds, the legacy shared --cache-type-kv only when the engine's
//     own --help reports it).
//   - --model-draft (optional): speculative decoding with a small draft
//     model — 20-50% more tokens/sec for same-family model pairs.
//   - --mlock (optional): pin weights in RAM so nothing evicts them
//     mid-chat.
//   - --no-webui: the engine's built-in web UI is dead weight; SHEYTAN is
//     the interface.

import (
        "fmt"
        "os"
        "path/filepath"
        "strings"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
)

// threadsFor resolves the generation thread count: explicit config value
// wins; otherwise the detected PHYSICAL core count (falls back to logical).
func threadsFor(cfg *config.Config) int {
        if cfg.LLM.NumThread > 0 {
                return cfg.LLM.NumThread
        }
        gen, _ := sysinfo.RecommendThreads()
        return gen
}

// threadsBatchFor resolves the prefill thread count (logical cores by
// default).
func threadsBatchFor(cfg *config.Config) int {
        if cfg.ThreadsBatch > 0 {
                return cfg.ThreadsBatch
        }
        _, batch := sysinfo.RecommendThreads()
        return batch
}

// SpeedArgs builds the speed flag set for the llama.cpp server. The exact
// form of each flag follows the engine capability profile resolved from the
// config (persisted verified profile, else release-tag heuristic), so a
// newer llama.cpp release can never silently reinterpret an older option
// layout.
//
// Exported so the stress suite can lock the launch contract in.
func SpeedArgs(cfg *config.Config) []string {
        return SpeedArgsWithCaps(cfg, resolveEngineCaps(cfg))
}

// SpeedArgsWithCaps is the capability-explicit variant: tests and the
// launcher pass the profile that will be validated and launched.
func SpeedArgsWithCaps(cfg *config.Config, caps *EngineCaps) []string {
        if caps == nil {
                caps = defaultCapsForTag("")
        }

        var args []string

        // Fused attention kernels (default ON). Two possible layouts:
        //   value form (new):  --flash-attn on   /  --flash-attn off
        //   flag form  (old):  --flash-attn
        // The Phase 6 regression emitted the bare flag right before
        // --cache-reuse on value-form engines; that combination can never be
        // produced again.
        if caps.FlashAttnEnabled {
                if caps.FlashAttnValue {
                        if cfg.FlashAttention {
                                args = append(args, "--flash-attn", "on")
                        } else {
                                // The user disabled flash attention: on value-form
                                // engines say so explicitly instead of leaving the
                                // engine's default (auto) to silently re-enable it.
                                args = append(args, "--flash-attn", "off")
                        }
                } else if cfg.FlashAttention {
                        args = append(args, "--flash-attn")
                }
        }

        // Prompt-cache reuse for agent loops (stable prefix across turns).
        // Independent option: always its own flag + value pair.
        if caps.CacheReuse {
                if n := cfg.EffectiveCacheReuse(); n > 0 {
                        args = append(args, "--cache-reuse", fmt.Sprintf("%d", n))
                }
        }

        // Physical prompt-processing batch.
        if caps.UBatchSize {
                args = append(args, "--ubatch-size", fmt.Sprintf("%d", cfg.EffectiveUBatchSize()))
        }

        // Separate prefill thread pool (generation threads come from
        // threadsFor() in llama.go's base args).
        if caps.ThreadsBatch {
                args = append(args, "--threads-batch", fmt.Sprintf("%d", threadsBatchFor(cfg)))
        }

        // KV-cache compression (opt-in, v1.7.2 capability-gated). The
        // emitted layout follows the engine's OWN contract exactly:
        //
        //      split      → --cache-type-k q  --cache-type-v q   (modern)
        //      shared     → --cache-type-kv q                     (legacy builds whose --help says so)
        //      neither    → nothing (the build has no KV option)
        //
        // The v1.7.1 defect emitted `--cache-type-kv q8_0` unconditionally:
        // every modern llama.cpp build rejects that option ("error: invalid
        // argument: --cache-type-kv") and the failure cascaded into
        // compatibility mode 2 — hiding a one-option layout defect behind a
        // full speed-flag strip. A modern engine can never receive
        // --cache-type-kv again.
        if q := cfg.EffectiveKVCacheQuant(); q != "" {
                switch {
                case caps.CacheTypeK:
                        args = append(args, "--cache-type-k", q)
                case caps.CacheTypeKVShared:
                        args = append(args, "--cache-type-kv", q)
                }
                if caps.CacheTypeV {
                        args = append(args, "--cache-type-v", q)
                }
        }

        // Pin weights in RAM (opt-in, v1.7.2 capability-gated). `--mlock`
        // was replaced upstream by `--load-mode MODE`; emit whichever form
        // the engine's contract carries, nothing when neither is known.
        if cfg.Mlock {
                switch {
                case caps.LoadMode:
                        args = append(args, "--load-mode", "mlock")
                case caps.Mlock:
                        args = append(args, "--mlock")
                }
        }

        // Speculative decoding with a draft model (opt-in). Resolved against
        // the models dir so a bare filename works like the main model.
        if dm := strings.TrimSpace(cfg.DraftModel); dm != "" {
                if path, err := ResolveModelPath(cfg.ModelsDir, dm); err == nil {
                        args = append(args, "--model-draft", path, "--draft-max", "16")
                } else if abs := strings.TrimSpace(dm); isExistingFile(abs) {
                        args = append(args, "--model-draft", abs, "--draft-max", "16")
                }
                // Unresolvable draft names are silently dropped — a stale config
                // value must never brick the engine.
        }

        // SHEYTAN is the interface; the engine's web UI is dead weight.
        if caps.NoWebUI {
                args = append(args, "--no-webui")
        }

        return args
}

func isExistingFile(p string) bool {
        if p == "" {
                return false
        }
        if !filepath.IsAbs(p) {
                if wd, err := os.Getwd(); err == nil {
                        p = filepath.Join(wd, p)
                }
        }
        fi, err := os.Stat(p)
        return err == nil && !fi.IsDir()
}
