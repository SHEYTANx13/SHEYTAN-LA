package api

// config_sampling_patch_test.go — v1.6.1 P0: the live-configuration gate.
//
// The Settings panel's numeric inputs turn a cleared or malformed field
// into 0 (numberValue's Number.isFinite fallback). Before v1.6.1 that 0
// was stored verbatim and reached the engine as "--repeat-penalty 0" —
// the in-the-wild launch failure that fed the compatibility ladder.
// The PATCH endpoint now REJECTS parser-invalid sampling values with an
// actionable 400 before anything is stored.

import (
	"net/http"
	"strings"
	"testing"
)

func TestConfigPatchRejectsInvalidSamplingValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		value any
	}{
		{"repeatPenalty zero", "repeatPenalty", 0},
		{"repeatPenalty negative", "repeatPenalty", -1.1},
		{"temperature negative", "temperature", -0.5},
		{"topP above one", "topP", 1.5},
		{"numCtx zero", "numCtx", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := newTestServer(t)
			defer server.Close()

			code, out := patchConfig(t, server, map[string]any{
				"llm": map[string]any{
					tc.field: tc.value,
				},
			})

			if code != http.StatusBadRequest {
				t.Fatalf("patch with %s=%v must be rejected with 400, got %d: %v",
					tc.field, tc.value, code, out)
			}

			msg := errorString(out)
			for _, want := range []string{tc.field, "invalid sampling"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("rejection must name %q and the classification, got: %s", want, msg)
				}
			}
		})
	}
}

func TestConfigPatchAcceptsValidSamplingValues(t *testing.T) {
	server, _ := newTestServer(t)
	defer server.Close()

	code, out := patchConfig(t, server, map[string]any{
		"llm": map[string]any{
			"repeatPenalty": 1.07,
			"temperature":   0.42,
			"topP":          0.91,
			"minP":          0,
			"numCtx":        8192,
		},
	})

	if code != http.StatusOK {
		t.Fatalf("valid sampling patch must be accepted, got %d: %v", code, out)
	}

	// And the stored value round-trips.
	code2, out2 := patchConfig(t, server, map[string]any{
		"llm": map[string]any{"repeatPenalty": 0.55},
	})
	if code2 != http.StatusOK {
		t.Fatalf("second valid patch must be accepted, got %d: %v", code2, out2)
	}

	llm, _ := out2["llm"].(map[string]any)
	if llm == nil {
		t.Fatalf("no llm in response: %v", out2)
	}
	if rp, _ := llm["repeatPenalty"].(float64); rp != 0.55 {
		t.Fatalf("repeatPenalty must round-trip as 0.55, got %v", llm["repeatPenalty"])
	}
}

// errorString extracts the writeErr payload's message.
func errorString(out map[string]any) string {
	if e, ok := out["error"].(string); ok {
		return e
	}
	if e, ok := out["error"].(map[string]any); ok {
		if m, ok := e["message"].(string); ok {
			return m
		}
	}
	return ""
}
