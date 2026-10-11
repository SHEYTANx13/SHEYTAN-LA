// est.go — the recovery package's LOCAL token estimator.
//
// recovery must stay import-free of the context stack (llm imports
// recovery for the typed condition; chunking imports llm), so the
// budget arithmetic here mirrors the same magnitude as the global
// estimator (chunking.EstimateTokens): CJK runes ≈ one token each,
// other runes ≈ four per token. Recovery budgets are coarse ceilings —
// the exact tiered estimator remains the authority for prompt assembly.
package recovery

// estTokens returns the estimated token count of s.
func estTokens(s string) int {
	if s == "" {
		return 0
	}
	cjk, other := 0, 0
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
	est := cjk + (other+3)/4 // ceil(other/4)
	if est < 1 {
		est = 1
	}
	return est
}
