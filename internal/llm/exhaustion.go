// exhaustion.go — v1.7.1: normalize llama.cpp context exhaustion into the
// ONE typed condition (recovery.ErrContextExhausted).
//
// llama.cpp exposes NO structured finish/error signal for context
// exhaustion: a request that does not fit the window is refused with an
// HTTP error whose TEXT is the only evidence ("the request exceeds the
// available context size …"). The Native Engine, by contrast, has an
// explicit structured context-bound rejection that maps into the same
// contract at its own boundary (internal/native/engine).
//
// Rules:
//   - classification happens HERE, at the backend boundary — exactly
//     once, never at the recovery site;
//   - a normal finish_reason == "length" is a bounded max_tokens stop,
//     NOT exhaustion — it must never trigger recovery;
//   - the marker vocabulary is conservative (shared with the existing
//     agent.CatContext evidence classes) so a normal transport failure
//     is never misread as exhaustion.
package llm

import (
	"fmt"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
)

// contextExhaustionMarkers are the llama.cpp (llama-server) refusal
// phrases that constitute real context-exhaustion evidence.
var contextExhaustionMarkers = []string{
	"exceeds the available context size", // llama-server exact refusal
	"maximum context length",             // OpenAI-style + llama.cpp variants
	"exceeds the model context",
	"the request exceeds the context",
	"context window is full",
	"prompt is too long",
	"input is too long",
	"too many tokens",
	"context shift is disabled", // generation no longer fits, shift refused
}

// IsContextExhaustionText reports whether a backend error text carries
// real context-exhaustion evidence. Exported for tests and for the
// runtime boundary that logs raw transport errors.
func IsContextExhaustionText(text string) bool {
	if text == "" {
		return false
	}
	t := strings.ToLower(text)
	for _, m := range contextExhaustionMarkers {
		if strings.Contains(t, m) {
			return true
		}
	}
	return false
}

// wrapContextExhaustion upgrades a backend error into the typed
// condition when its text carries exhaustion evidence. The original text
// is preserved verbatim as the typed detail — honest evidence, no
// rewriting. Non-matching errors pass through unchanged.
func wrapContextExhaustion(err error) error {
	if err == nil || recovery.IsContextExhausted(err) {
		return err
	}
	if IsContextExhaustionText(err.Error()) {
		return fmt.Errorf("LLM error: %w",
			recovery.NewExhaustion("llama.cpp", err.Error(), ""))
	}
	return err
}
