package llm

// modelcaps.go — Phase 7 small-model first-class support.
//
// One authoritative ModelCapabilities object per loaded model, derived from
// the real GGUF metadata (ReadModelCard) and the active configuration. It
// is the single source consumed by the UI, the context planner, the engine
// launcher, the backend selector, the agent planner, verification and
// diagnostics — there is deliberately no second capability path.
//
// For small local models the system optimizes for USABLE context, not for
// the maximum configured number: configured, model training limit, engine
// limit and a safe effective limit are computed separately, and the
// effective value is the minimum valid one. The launcher passes that value
// as --ctx-size; the planner budgets against it; the UI reports why.

import (
        "fmt"
        "os"
        "sort"
        "strings"
        "sync"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// ModelCapabilities is the measured capability card of one GGUF model.
type ModelCapabilities struct {
        // Path and identity.
        Path       string `json:"path"`
        FileName   string `json:"fileName"`
        Arch       string `json:"arch"`
        Name       string `json:"name,omitempty"`
        Quant      string `json:"quant"`
        ParamsText string `json:"paramsText,omitempty"`
        SizeBytes  int64  `json:"sizeBytes"`

        // ParameterCount is the raw GGUF parameter count (0 when absent).
        ParameterCount uint64 `json:"parameterCount,omitempty"`

        // ContextLength is the model's own training context limit from the
        // GGUF header (0 when the header does not declare one).
        ContextLength int `json:"contextLength,omitempty"`

        // Architecture shape.
        Layers       int `json:"layers,omitempty"`
        EmbeddingLen int `json:"embeddingLen,omitempty"`

        // TokenizerFamily is a coarse family name derived from the
        // architecture (useful for diagnostics and template hints).
        TokenizerFamily string `json:"tokenizerFamily,omitempty"`

        // ChatTemplate is true when the architecture is known to ship a
        // usable chat template (guides --jinja decisions, never forces them).
        ChatTemplate bool `json:"chatTemplate"`

        // Multimodal is true when a vision projector is paired with this
        // model in the models directory.
        Multimodal bool   `json:"multimodal"`
        MMProjPath string `json:"mmprojPath,omitempty"`

        // NativeBackend reports whether the native C++ engine can execute
        // this architecture (the llama.cpp backend remains authoritative for
        // everything else).
        NativeBackend bool   `json:"nativeBackend"`
        NativeReason  string `json:"nativeReason,omitempty"`

        // Memory estimates (measured from file size + layer shape, honestly
        // labeled as estimates).
        EstimatedRAMBytes  int64 `json:"estimatedRamBytes"`
        EstimatedVRAMBytes int64 `json:"estimatedVramBytes"`

        // Recommended values for THIS model on THIS machine.
        RecommendedCtx       int `json:"recommendedCtx"`
        RecommendedGenBudget int `json:"recommendedGenBudget"`

        // ReadAt is when the card was produced (cache diagnostics).
        ReadAt time.Time `json:"readAt"`
}

// Meta renders the compact one-line summary (delegates to ModelCard.Meta
// formatting so UI text stays identical across surfaces).
func (m *ModelCapabilities) SummaryLine() string {
        if m == nil {
                return ""
        }
        return fmt.Sprintf("%s · %s · %s",
                m.ParamsText, m.Quant, formatCtxShort(m.ContextLength))
}

func formatCtxShort(n int) string {
        if n <= 0 {
                return "ctx unknown"
        }
        if n >= 1024 {
                return fmt.Sprintf("%dK ctx", n/1024)
        }
        return fmt.Sprintf("%d ctx", n)
}

// ---------------------------------------------------------------------------
// Effective context — model-aware window resolution.
// ---------------------------------------------------------------------------

// EffectiveContext is the model-aware context decision for one model.
type EffectiveContext struct {
        // Configured is cfg.LLM.NumCtx (what the user asked for).
        Configured int `json:"configured"`

        // Requested is the explicit request this decision served — the
        // per-session policy when one is set, otherwise Configured.
        Requested int `json:"requested,omitempty"`

        // SessionPolicy is the raw per-session context policy (0 = none).
        SessionPolicy int `json:"sessionPolicy,omitempty"`

        // ModelMax is the GGUF training context limit (0 = unknown).
        ModelMax int `json:"modelMax,omitempty"`

        // EngineLimit is a limit reported by the actual engine/backend
        // (0 = unknown).
        EngineLimit int `json:"engineLimit,omitempty"`

        // Effective is the minimum VALID of the known limits — the value the
        // launcher and planner must use.
        Effective int `json:"effective"`

        // Reasons records each clamp applied, for honest UI reporting.
        Reasons []string `json:"reasons,omitempty"`
}

// ResolveEffectiveContext computes the model-aware context decision.
//
// Rules:
//   - configured <= 0 falls back to the default 16384.
//   - a known model limit SMALLER than configured wins (small-model rule:
//     never hand a small model a window larger than it has).
//   - a known engine limit smaller than the current minimum wins.
//   - nothing is ever raised beyond the configured value: the user's
//     explicit number is a ceiling, not a floor.
func ResolveEffectiveContext(cfg *config.Config, caps *ModelCapabilities, engineLimit int) EffectiveContext {
        configured := cfg.LLM.NumCtx
        if configured <= 0 {
                configured = 16384
        }

        ec := EffectiveContext{
                Configured: configured,
                Effective:  configured,
        }

        if caps != nil && caps.ContextLength > 0 && caps.ContextLength < ec.Effective {
                ec.ModelMax = caps.ContextLength
                ec.Effective = caps.ContextLength
                ec.Reasons = append(ec.Reasons, fmt.Sprintf(
                        "model GGUF context limit %d < configured %d — using the model limit",
                        caps.ContextLength, configured))
        } else if caps != nil {
                ec.ModelMax = caps.ContextLength
        }

        if engineLimit > 0 && engineLimit < ec.Effective {
                ec.EngineLimit = engineLimit
                ec.Effective = engineLimit
                ec.Reasons = append(ec.Reasons, fmt.Sprintf(
                        "engine reports context limit %d — clamping", engineLimit))
        } else {
                ec.EngineLimit = engineLimit
        }

        return ec
}

// ResolveSessionContext resolves the 1.1.6 per-session context policy.
//
// The effective value is the minimum of EVERY limit in the chain
// (1.1.6 §3) — no session may advertise a window the model, engine,
// global configuration or the machine cannot serve:
//
//      effective = min(session policy | global configured,
//                      global configured,
//                      model GGUF maximum,
//                      engine-verified window)
//      floored at 1024 tokens.
//
// A session policy of 0 means "inherit the global configured context"
// and behaves exactly like ResolveEffectiveContext. A non-zero policy
// NARROWS the window for this chat only — it never mutates any other
// session and never writes back to config. Every clamp is recorded in
// Reasons so the UI can explain the decision honestly.
func ResolveSessionContext(cfg *config.Config, caps *ModelCapabilities, engineLimit, sessionTokens int) EffectiveContext {
        // Base decision: min(configured, model max, engine limit).
        ec := ResolveEffectiveContext(cfg, caps, engineLimit)

        if sessionTokens <= 0 {
                ec.Requested = ec.Configured
                return ec
        }

        ec.Requested = sessionTokens
        ec.SessionPolicy = sessionTokens

        if sessionTokens < ec.Effective {
                ec.Reasons = append(ec.Reasons, fmt.Sprintf(
                        "session context policy %d < resolved %d — using the session policy",
                        sessionTokens, ec.Effective))
                ec.Effective = sessionTokens
        } else if sessionTokens > ec.Effective {
                // The session asked for more than the resolved limits allow
                // (configured ceiling, model limit or engine window). The base
                // clamp stands; the trail explains which limit bound it.
                ec.Reasons = append(ec.Reasons, fmt.Sprintf(
                        "session context policy %d exceeds the resolved limit %d — clamped (raise the global context in Settings and restart the engine to go higher)",
                        sessionTokens, ec.Effective))
        }

        if ec.Effective < 1024 {
                ec.Effective = 1024
                ec.Reasons = append(ec.Reasons, "effective context floored at 1024 tokens")
        }

        return ec
}

// launchContextSize resolves the --ctx-size for one boot: the minimum of
// the configured context and the model's own GGUF limit. Failures to read
// the model never block the launch (the planner still protects the request).
func launchContextSize(cfg *config.Config, modelPath string) int {
        configured := cfg.LLM.NumCtx
        if configured <= 0 {
                configured = 16384
        }

        caps := ResolveModelCapabilities(cfg, modelPath)
        if caps == nil || caps.ContextLength <= 0 {
                return configured
        }

        if caps.ContextLength < configured {
                return caps.ContextLength
        }
        return configured
}

// ---------------------------------------------------------------------------
// Capability resolution with identity-based cache.
// ---------------------------------------------------------------------------

// v1.8.2 CAPABILITY CACHE REPAIR: the pre-1.8.2 cache validated entries
// with path+size+mtime PLUS an arbitrary 10-second TTL. For turns
// separated by minutes (the normal chat cadence) the TTL always expired,
// so EVERY turn re-parsed the full GGUF metadata — for real Gemma-class
// cards that is a 32 MiB metadata read per turn of pure avoidable
// pre-generation latency.
//
// The cache is now IDENTITY-BASED with no TTL at all:
//
//   - the IMMUTABLE parsed card (GGUF metadata facts) is cached under
//     (path, size, mtime). Those facts cannot change while the identity
//     holds — a different file content IS a different size/mtime;
//   - the CONFIG-SENSITIVE derived fields (multimodal pairing,
//     recommendations) are re-derived from the cached card whenever the
//     relevant configuration fingerprint changes — cheap arithmetic and
//     at most one projector lookup, never a GGUF re-parse;
//   - a changed identity (path/size/mtime) re-parses from scratch, so a
//     replaced or re-downloaded model file is always re-read exactly
//     once.
//
// The map is bounded (capsCacheMaxEntries, oldest-evicted) so a long
// session that browses many models cannot grow it without bound. No
// second capability authority exists: this is still the one resolver.

type capsCacheEntry struct {
        card   *ModelCard  // immutable GGUF facts (identity-keyed)
        caps   *ModelCapabilities
        size   int64
        mtime  time.Time
        cfgFP  string // fingerprint of config-sensitive inputs
        stamp  int64  // monotonic insertion counter for eviction order
}

var (
        capsCacheMu      sync.Mutex
        capsCache        = map[string]capsCacheEntry{}
        capsCacheCounter int64
)

// capsCacheMaxEntries bounds the identity-keyed cache. A model card is
// metadata only (no vocab arrays) — a few KiB each; 16 is generous for
// any real installation while keeping the map trivially small.
const capsCacheMaxEntries = 16

// capsConfigFingerprint fingerprints the configuration inputs that feed
// the CONFIG-SENSITIVE derived fields. Immutable GGUF facts are excluded
// by design — they belong to the card, not the config.
func capsConfigFingerprint(cfg *config.Config, modelPath string) string {
        if cfg == nil {
                return "nil"
        }

        visionPart := "off"
        if cfg.VisionEnabled {
                visionPart = "on:" + cfg.VisionMMProj
        }

        return fmt.Sprintf("%s|%d|%d|%s|%s",
                cfg.ModelsDir, cfg.LLM.NumCtx, cfg.LLM.MaxTokens,
                visionPart, modelPath)
}

// capsCacheLookup returns a cache hit under one lock acquisition. ok is
// true exactly when the caller can serve the returned caps without any
// further work.
func capsCacheLookup(modelPath string, fiSize int64, fiMod time.Time, cfgFP string) (*ModelCapabilities, *ModelCard, bool) {
        capsCacheMu.Lock()
        defer capsCacheMu.Unlock()

        e, ok := capsCache[modelPath]
        if !ok || e.size != fiSize || !e.mtime.Equal(fiMod) {
                return nil, nil, false
        }

        if e.cfgFP != cfgFP || e.caps == nil {
                // Identity still valid; the config-sensitive derivation must be
                // redone FROM THE CACHED CARD (no GGUF re-parse). The card return
                // tells the caller which immutable facts to re-derive from.
                return nil, e.card, false
        }

        return e.caps, e.card, true
}

// capsCacheStore records the parsed card and the derived caps for one
// identity, evicting the oldest entry beyond the bound.
func capsCacheStore(modelPath string, fiSize int64, fiMod time.Time, cfgFP string, card *ModelCard, caps *ModelCapabilities) {
        capsCacheMu.Lock()
        defer capsCacheMu.Unlock()

        capsCacheCounter++
        capsCache[modelPath] = capsCacheEntry{
                card:  card,
                caps:  caps,
                size:  fiSize,
                mtime: fiMod,
                cfgFP: cfgFP,
                stamp: capsCacheCounter,
        }

        for len(capsCache) > capsCacheMaxEntries {
                oldestPath := ""
                var oldestStamp int64 = -1
                for p, e := range capsCache {
                        if oldestStamp < 0 || e.stamp < oldestStamp {
                                oldestPath = p
                                oldestStamp = e.stamp
                        }
                }
                if oldestPath == "" {
                        break
                }
                delete(capsCache, oldestPath)
        }
}

// ResolveModelCapabilities builds (or serves from cache) the capability
// card for one model file. Nil when the file cannot be read — callers must
// treat nil as "unknown model, fall back to config values".
func ResolveModelCapabilities(cfg *config.Config, modelPath string) *ModelCapabilities {
        if modelPath == "" {
                return nil
        }

        fi, err := os.Stat(modelPath)
        if err != nil || fi.IsDir() {
                return nil
        }

        cfgFP := capsConfigFingerprint(cfg, modelPath)

        if caps, card, ok := capsCacheLookup(modelPath, fi.Size(), fi.ModTime(), cfgFP); ok {
                return caps
        } else if card != nil {
                // Identity hit + config drift: re-derive the config-sensitive
                // fields from the IMMUTABLE cached card — the GGUF is NOT parsed.
                caps := deriveModelCapabilities(cfg, modelPath, card)
                capsCacheStore(modelPath, fi.Size(), fi.ModTime(), cfgFP, card, caps)
                return caps
        }

        card, err := ReadModelCard(modelPath)
        if err != nil {
                return nil
        }

        caps := deriveModelCapabilities(cfg, modelPath, card)
        capsCacheStore(modelPath, fi.Size(), fi.ModTime(), cfgFP, card, caps)

        return caps
}

// deriveModelCapabilities builds the config-sensitive capability card
// from the immutable parsed GGUF card. All GGUF-derived facts are copied
// here and nowhere else so the card/caps boundary stays exact.
func deriveModelCapabilities(cfg *config.Config, modelPath string, card *ModelCard) *ModelCapabilities {
        caps := &ModelCapabilities{
                Path:           modelPath,
                FileName:       baseName(modelPath),
                Arch:           card.Arch,
                Name:           card.Name,
                Quant:          card.Quant,
                ParamsText:     card.Meta(),
                ParameterCount: card.ParamsCount,
                ContextLength:  card.ContextLength,
                Layers:         card.Layers,
                EmbeddingLen:   card.EmbeddingLen,
                SizeBytes:      card.SizeBytes,
                ReadAt:         time.Now(),
        }

        caps.TokenizerFamily = tokenizerFamilyFor(card.Arch)
        caps.ChatTemplate = archHasChatTemplate(card.Arch)
        caps.NativeBackend, caps.NativeReason = NativeArchitectureSupport(card.Arch)

        // Multimodal: is a vision projector paired for this model?
        if cfg != nil && cfg.VisionEnabled {
                if p := vision.FindProjector(cfg.ModelsDir, modelPath, cfg.VisionMMProj); p != "" {
                        caps.Multimodal = true
                        caps.MMProjPath = p
                }
        }

        // Memory estimates: weights dominate (file size ≈ weights), KV cache
        // and workspace add on top. These are ESTIMATES for planning/UI and
        // are labeled as such everywhere they surface.
        caps.EstimatedRAMBytes = card.SizeBytes + estimateKVCacheBytes(card)
        caps.EstimatedVRAMBytes = caps.EstimatedRAMBytes

        // Recommendations for small models: usable context beats maximum
        // context. Cap the recommendation at the model's own training limit.
        // v1.8.1: a nil cfg uses the same defaults as an unset config — the
        // function already guards cfg for the vision block, so the whole
        // contract is now uniformly nil-safe (the pre-v1.8.1 unguarded
        // cfg.LLM reads were a nil deref of the same class as the orchestrator
        // crash).
        rec := 16384
        gen := 1024
        if cfg != nil {
                if cfg.LLM.NumCtx > 0 {
                        rec = cfg.LLM.NumCtx
                }
                if cfg.LLM.MaxTokens > 0 {
                        gen = cfg.LLM.MaxTokens
                }
        }
        if caps.ContextLength > 0 && caps.ContextLength < rec {
                rec = caps.ContextLength
        }
        caps.RecommendedCtx = rec

        // Generation budget: at most half the usable window, floored at 512,
        // capped by config. Small models get a proportionate budget instead
        // of an outsized reserve.
        if gen > rec/2 {
                gen = rec / 2
        }
        if gen < 512 {
                gen = 512
        }
        caps.RecommendedGenBudget = gen

        return caps
}

// NativeArchitectureSupport reports whether the native C++ engine can run
// an architecture. The native engine ships the llama graph family; the
// honest answer for everything else is "no — llama.cpp backend serves it".
func NativeArchitectureSupport(arch string) (bool, string) {
        switch strings.ToLower(strings.TrimSpace(arch)) {
        case "llama":
                return true, "llama graph family is natively executable"
        case "":
                return false, "architecture unknown — native engine verdict deferred to load time"
        default:
                return false, "native engine executes the llama graph family only; llama.cpp backend serves this architecture"
        }
}

// v1.8.1: gemma3n (the E2B/E4B effective-parameter family) and gemma4
// join the Gemma lineage lists — same BPE tokenizer family, same
// ships-a-chat-template contract as gemma1–3. Previously these archs
// fell to "unknown", disabling family-tuned token estimation and the
// chat-template hint for exactly the small-model class v1.8.1 repairs.
func tokenizerFamilyFor(arch string) string {
        switch strings.ToLower(strings.TrimSpace(arch)) {
        case "llama":
                return "SPM (SentencePiece BPE)"
        case "qwen2", "qwen3", "phi2", "phi3", "gemma", "gemma2", "gemma3",
                "gemma3n", "gemma4":
                return "BPE"
        case "falcon", "mpt", "gpt2", "gptj":
                return "BPE/GPT-style"
        default:
                return "unknown"
        }
}

func archHasChatTemplate(arch string) bool {
        switch strings.ToLower(strings.TrimSpace(arch)) {
        case "llama", "qwen2", "qwen3", "phi2", "phi3", "gemma", "gemma2",
                "gemma3", "gemma3n", "gemma4", "mistral", "falcon", "mpt", "starcoder":
                return true
        default:
                return false
        }
}

// estimateKVCacheBytes approximates KV-cache size from the model shape
// (2 * layers * embedding * 2 bytes fp16 per token * 16k assumed window).
// Rough by design — a planning prior, not a measurement.
func estimateKVCacheBytes(card *ModelCard) int64 {
        if card == nil || card.Layers <= 0 || card.EmbeddingLen <= 0 {
                return 0
        }
        perToken := int64(card.Layers) * int64(card.EmbeddingLen) * 2 * 2
        const assumedTokens = 16384
        return perToken * assumedTokens
}

// SortedCapabilityKeys returns the capability fields for diagnostics output
// (stable order for tests and CLI rendering).
func (m *ModelCapabilities) SortedCapabilityKeys() []string {
        return []string{
                "arch", "quant", "params", "contextLength", "tokenizer",
                "chatTemplate", "multimodal", "nativeBackend", "estRAM",
                "estVRAM", "recommendedCtx", "recommendedGenBudget",
        }
}

// supportedArchList is the sorted architecture list the native engine
// executes (currently exactly the llama graph family).
func supportedArchList() []string {
        out := []string{"llama"}
        sort.Strings(out)
        return out
}
