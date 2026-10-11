// exhaustion_test.go — v1.7.1: llama.cpp evidence classification at the
// backend boundary (typed condition, no upstream re-grepping).
package llm

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
)

func TestIsContextExhaustionTextEvidence(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"LLM HTTP 400: {\"error\":{\"message\":\"the request exceeds the available context size ...\"}}", true},
		{"LLM HTTP 400: This model's maximum context length is 4096 tokens", true},
		{"the request exceeds the context", true},
		{"prompt is too long", true},
		{"too many tokens in the request", true},
		{"context shift is disabled", true},
		{"connection reset by peer", false},
		{"LLM HTTP 500: internal error", false},
		{"model is loading", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsContextExhaustionText(c.text); got != c.want {
			t.Errorf("IsContextExhaustionText(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestWrapContextExhaustionPreservesEvidence(t *testing.T) {
	raw := fmt.Errorf("LLM HTTP 400: %s", "the request exceeds the available context size")
	wrapped := wrapContextExhaustion(raw)

	if !recovery.IsContextExhausted(wrapped) {
		t.Fatal("exhaustion evidence not typed")
	}
	if !stringsContain(wrapped.Error(), "the request exceeds the available context size") {
		t.Fatalf("original evidence text lost: %q", wrapped.Error())
	}
	ex, ok := recovery.AsExhaustion(wrapped)
	if !ok || ex.Backend != "llama.cpp" {
		t.Fatalf("typed detail wrong: %+v", ex)
	}

	// Non-matching errors pass through unchanged (same identity).
	plain := errors.New("connection reset by peer")
	if wrapped2 := wrapContextExhaustion(plain); wrapped2 != plain {
		t.Fatalf("non-exhaustion error was rewritten: %v", wrapped2)
	}
}

func stringsContain(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
