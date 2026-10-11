// estimator.go — v1.2.9 tiered token estimator: the single context-
// budget accounting chain (exact engine tokenizer when available →
// model-family-tuned estimate → conservative CJK-aware fallback).
// Split out of chunking.go (behavior-identical file-level move).
package chunking

import (
	"strings"
	"sync/atomic"
)

// ---------------------------------------------------------------------------
// v1.2.9 TIERED TOKEN ESTIMATOR
//
// Context-budget correctness requires ONE explicit chain, strongest
// first: exact model/engine tokenizer accounting when available →
// model-family-tuned estimator → conservative fallback estimator.
// EstimateTokens dispatches through this chain. The active tier is
// observable (EstimatorLabel) so plans/logs/UI can never claim exact
// accounting while only an estimate is in play — a plan built on an
// estimate is conservative, never "mathematically exact".
// ---------------------------------------------------------------------------

// TokenEstimatorKind ranks the estimator tiers.
type TokenEstimatorKind int

const (
	// EstimatorHeuristic is the conservative fallback: CJK-aware
	// ~4-bytes-per-rune heuristic (see EstimateTokens). Never exact.
	EstimatorHeuristic TokenEstimatorKind = iota
	// EstimatorModelFamily is a family-tuned heuristic (SPM vs BPE
	// tokenizers have measurably different chars-per-token ratios).
	// Still an estimate.
	EstimatorModelFamily
	// EstimatorExact is real tokenizer accounting from the loaded
	// model/engine (native engine tokenizer). Counting is exact for
	// the model it was initialized from.
	EstimatorExact
)

// estimatorState is the atomically-swappable active estimator.
type estimatorState struct {
	kind   TokenEstimatorKind
	family string // model family label (diagnostics; "" for exact/heuristic)
	exact  func(s string) int
	ratio  float64 // non-CJK runes per token for the family tier
}

var activeEstimator atomic.Value // holds estimatorState

func loadEstimator() estimatorState {
	if v, ok := activeEstimator.Load().(estimatorState); ok {
		return v
	}
	return estimatorState{kind: EstimatorHeuristic}
}

// SetExactTokenCounter installs tier 1 (exact tokenizer accounting).
// count must be safe for concurrent use and must return the EXACT token
// count of s under the loaded model's tokenizer, or a strict
// OVER-estimate — never an under-estimate (callers rely on budgets
// erring safe). Passing nil reverts to the lower tiers.
func SetExactTokenCounter(count func(s string) int) {
	if count == nil {
		activeEstimator.Store(estimatorState{kind: EstimatorHeuristic})
		return
	}
	activeEstimator.Store(estimatorState{kind: EstimatorExact, exact: count})
}

// SetModelFamilyEstimator installs tier 2 (family-tuned estimate) for a
// known tokenizer family (e.g. "llama", "qwen", "gemma", "gpt").
// Unknown families keep the conservative fallback ratios.
func SetModelFamilyEstimator(family string) {
	st := estimatorState{kind: EstimatorModelFamily, family: strings.ToLower(strings.TrimSpace(family)), ratio: familyCharsPerToken(family)}
	activeEstimator.Store(st)
}

// ResetTokenEstimator restores the conservative fallback (tests and
// config changes that drop model knowledge).
func ResetTokenEstimator() {
	activeEstimator.Store(estimatorState{kind: EstimatorHeuristic})
}

// EstimatorKind reports the active tier.
func EstimatorKind() TokenEstimatorKind {
	return loadEstimator().kind
}

// EstimatorLabel renders the active tier for plans/logs/UI: "exact",
// "family:qwen", or "heuristic".
func EstimatorLabel() string {
	st := loadEstimator()
	switch st.kind {
	case EstimatorExact:
		return "exact"
	case EstimatorModelFamily:
		if st.family == "" {
			return "family"
		}
		return "family:" + st.family
	default:
		return "heuristic"
	}
}

// familyCharsPerToken maps a tokenizer family onto its measured
// non-CJK chars-per-token ratio. Values are deliberately on the
// CONSERVATIVE (fewer-chars-per-token = more tokens) side of published
// BPE/SPM measurements so family estimates never under-count.
func familyCharsPerToken(family string) float64 {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "llama", "llama-spm", "spm", "mistral":
		return 3.6 // SentencePiece-style merges are denser than GPT BPE
	case "qwen", "gpt", "bpe", "deepseek", "phi":
		return 3.9
	case "gemma", "t5", "unigram", "albert":
		return 3.5 // unigram models tokenize sparsely
	default:
		return 3.5 // unknown family: conservative default
	}
}

// EstimateTokens returns a fast approximation of the token count of s
// through the active estimator tier (see the tier notes above).
//
// v1.2.9: the fallback heuristic is CJK-aware. The previous
// ~4-bytes-per-RUNE formula UNDER-counted CJK text by roughly 4x (one
// CJK rune ≈ one token, not a quarter of one) — a latent overflow bug
// for any non-Latin conversation. The conservative model now counts
// CJK runes as whole tokens and non-CJK runes at ~1/4 (or the family
// ratio when tier 2 is active). UTF-8 aware: invalid bytes count as-is.
// This remains an ESTIMATE — plans built on it are labeled as such and
// carry a safety margin; only the exact tier removes that caveat.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	st := loadEstimator()
	if st.kind == EstimatorExact && st.exact != nil {
		if n := st.exact(s); n >= 0 {
			if n == 0 {
				return 1
			}
			return n
		}
		// A negative count from a failed exact path: fall through
		// to the estimate tiers rather than trust a broken number.
	}
	ratio := 4.0
	if st.kind == EstimatorModelFamily && st.ratio > 0 {
		ratio = st.ratio
	}
	cjk, other := countRunesCJK(s)
	n := cjk + other
	if n == 0 {
		n = len(s)
	}
	est := cjk + int(float64(other)/ratio+0.999)
	if est < 1 {
		est = 1
	}
	return est
}

// countRunesCJK splits the rune count of s into CJK-ish runes (each
// ≈ one token for mainstream tokenizers) and all others.
func countRunesCJK(s string) (cjk, other int) {
	for _, r := range s {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF, // CJK Unified Ideographs
			r >= 0x3400 && r <= 0x4DBF, // Extension A
			r >= 0x3040 && r <= 0x30FF, // Hiragana + Katakana
			r >= 0xAC00 && r <= 0xD7AF, // Hangul syllables
			r >= 0xF900 && r <= 0xFAFF, // Compatibility Ideographs
			r >= 0x3000 && r <= 0x303F: // CJK punctuation
			cjk++
		default:
			other++
		}
	}
	return cjk, other
}
