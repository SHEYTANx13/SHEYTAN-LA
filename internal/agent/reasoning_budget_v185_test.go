package agent

// reasoning_budget_v185_test.go — v1.8.5 P0 coverage for the end-to-end
// reasoning-depth contract (Low / Mid / High / Ultra).
//
// The four levels must have a REAL backend meaning: each level maps to a
// numeric thinking-token budget carried on the generation request as the
// llama.cpp request-level `reasoning_budget_tokens` parameter (verified
// present in BOTH managed engine builds — b10642 and b11205 — in the
// actual server sources this repository's CLI contract fixtures pin).
//
// Proven here:
//
//   1. the ladder values normalize onto themselves;
//   2. the legacy v1.2.5 vocabulary (auto/fast/thinking/deep) migrates to
//      the ladder and is never re-emitted;
//   3. each level's budget is the documented numeric value;
//   4. applyReasoningBudget stamps the budget onto a LOCAL request and
//      leaves a REMOTE request untouched (nil budget — the same gating as
//      the other llama.cpp-specific request fields);
//   5. ultra sends NO client-side cap (nil) — the honest encoding of
//      "engine default", never a fabricated number;
//   6. low's budget of 0 stays on the wire (pointer — omitempty must not
//      eat the "disable thinking" value);
//   7. the tier posture mapping (low = latency floor, mid = neutral,
//      high/ultra = nudge + escalation ceiling) matches the ladder;
//   8. the wire JSON carries exactly the documented field name.

import (
        "encoding/json"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/taskclassify"
)

func TestNormalizeThinkingControlLadder(t *testing.T) {
        cases := map[string]string{
                "low":      ThinkingLow,
                "mid":      ThinkingMid,
                "high":     ThinkingHigh,
                "ultra":    ThinkingUltra,
                "LOW":      ThinkingLow,
                " Ultra ":  ThinkingUltra,
                "":         ThinkingMid,
                "auto":     ThinkingMid,
                "fast":     ThinkingLow,
                "thinking": ThinkingHigh,
                "deep":     ThinkingHigh,
                "max":      ThinkingUltra,
                "junk":     ThinkingMid,
        }

        for in, want := range cases {
                if got := NormalizeThinkingControl(in); got != want {
                        t.Errorf("NormalizeThinkingControl(%q) = %q, want %q", in, got, want)
                }
        }
}

func TestReasoningBudgetTokensPerLevel(t *testing.T) {
        cases := map[string]int{
                ThinkingLow:   0,
                ThinkingMid:   1024,
                ThinkingHigh:  4096,
                ThinkingUltra: -1,
        }

        for level, want := range cases {
                if got := reasoningBudgetTokens(level); got != want {
                        t.Errorf("reasoningBudgetTokens(%q) = %d, want %d", level, got, want)
                }
        }
}

func localTestConfig(t *testing.T) *config.Config {
        t.Helper()

        return &config.Config{Provider: ""}
}

func remoteTestConfig(t *testing.T) *config.Config {
        t.Helper()

        return &config.Config{Provider: config.ProviderRemote}
}

func TestApplyReasoningBudgetLocal(t *testing.T) {
        cases := map[string]int{
                ThinkingLow: 0,
                ThinkingMid: 1024,
                ThinkingHigh: 4096,
        }

        for level, want := range cases {
                req := &llm.ChatRequest{}
                applyReasoningBudget(localTestConfig(t), level, req)

                if req.ReasoningBudget == nil {
                        t.Fatalf("level %q: budget not stamped on a local request", level)
                }

                if *req.ReasoningBudget != want {
                        t.Errorf("level %q: budget = %d, want %d", level, *req.ReasoningBudget, want)
                }
        }
}

func TestApplyReasoningBudgetUltraSendsNoCap(t *testing.T) {
        req := &llm.ChatRequest{}
        applyReasoningBudget(localTestConfig(t), ThinkingUltra, req)

        if req.ReasoningBudget != nil {
                t.Fatalf("ultra stamped a client-side cap (%d) — the honest encoding is nil (engine default)", *req.ReasoningBudget)
        }
}

func TestApplyReasoningBudgetRemoteNeverStamped(t *testing.T) {
        for _, level := range []string{ThinkingLow, ThinkingMid, ThinkingHigh, ThinkingUltra} {
                req := &llm.ChatRequest{}
                applyReasoningBudget(remoteTestConfig(t), level, req)

                if req.ReasoningBudget != nil {
                        t.Fatalf("remote request carried a reasoning budget (%d) for level %q", *req.ReasoningBudget, level)
                }
        }
}

func TestApplyReasoningBudgetLegacyMigration(t *testing.T) {
        cases := map[string]int{
                // fast → low (0), auto → mid (1024), thinking → high (4096).
                "fast":     0,
                "auto":     1024,
                "thinking": 4096,
        }

        for legacy, want := range cases {
                req := &llm.ChatRequest{}
                applyReasoningBudget(localTestConfig(t), legacy, req)

                if req.ReasoningBudget == nil {
                        t.Fatalf("legacy %q: budget not stamped", legacy)
                }

                if *req.ReasoningBudget != want {
                        t.Errorf("legacy %q: budget = %d, want %d", legacy, *req.ReasoningBudget, want)
                }
        }
}

func TestApplyReasoningBudgetDefensive(t *testing.T) {
        applyReasoningBudget(nil, ThinkingMid, &llm.ChatRequest{})

        req := &llm.ChatRequest{}
        applyReasoningBudget(localTestConfig(t), "not-a-level", req)

        // Unknown levels degrade to the mid default (normalized), never to a
        // fabricated budget class.
        if req.ReasoningBudget == nil || *req.ReasoningBudget != 1024 {
                t.Fatalf("unknown level did not degrade to the mid default: %+v", req.ReasoningBudget)
        }
}

func TestReasoningBudgetWireFieldName(t *testing.T) {
        budget := 0
        body, err := json.Marshal(&llm.ChatRequest{ReasoningBudget: &budget})
        if err != nil {
                t.Fatal(err)
        }

        if want := `"reasoning_budget_tokens":0`; !contains(string(body), want) {
                t.Fatalf("wire body %s does not carry %s", body, want)
        }

        // The unset case stays off the wire.
        body, err = json.Marshal(&llm.ChatRequest{})
        if err != nil {
                t.Fatal(err)
        }

        if contains(string(body), "reasoning_budget_tokens") {
                t.Fatalf("unset budget leaked onto the wire: %s", body)
        }
}

func contains(haystack, needle string) bool {
        return len(haystack) >= len(needle) && (haystack == needle ||
                (len(needle) == 0) ||
                indexOfSub(haystack, needle) >= 0)
}

func indexOfSub(haystack, needle string) int {
        for i := 0; i+len(needle) <= len(haystack); i++ {
                if haystack[i:i+len(needle)] == needle {
                        return i
                }
        }

        return -1
}

// TestLadderTierPosture pins the tier mapping: low keeps the latency-first
// floor (like legacy fast), mid is neutral, high/ultra never force context
// inflation (depth comes from the budget, not the tier).
func TestLadderTierPosture(t *testing.T) {
        base := taskclassify.Resources{
                EffectiveContext: 32768,
                SystemRAMMB:      16384,
        }

        // A deep-by-complexity profile at LOW must come down to FAST.
        deepProfile := taskclassify.Profile{Complexity: 90}

        res := base
        res.UserDepth = "low"

        if tier := taskclassify.SelectTier(deepProfile, res); taskclassify.TierRank(tier) > taskclassify.TierRank(taskclassify.TierFast) {
                t.Fatalf("low did not hold the FAST floor: %s", tier)
        }

        // The same profile at MID (neutral) stays evidence-driven.
        res.UserDepth = "mid"

        if tier := taskclassify.SelectTier(deepProfile, res); tier == taskclassify.TierFast {
                t.Fatalf("mid wrongly forced the FAST floor: %s", tier)
        }

        // high/ultra never force context inflation — a trivial profile stays
        // FAST (reasoning depth is NOT context size; the budget is the depth).
        trivial := taskclassify.Profile{Complexity: 0}

        for _, level := range []string{"high", "ultra", "thinking"} {
                res.UserDepth = level

                if tier := taskclassify.SelectTier(trivial, res); taskclassify.TierRank(tier) > taskclassify.TierRank(taskclassify.TierStandard) {
                        t.Fatalf("level %q inflated the context tier: %s", level, tier)
                }
        }
}
