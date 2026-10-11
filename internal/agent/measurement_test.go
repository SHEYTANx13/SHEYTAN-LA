package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/taskclassify"
)

// First-request prompt cost measurement for a trivial chat turn.
//
// v1.2.4 baseline (measured on this same code path before the change, see
// worklog): EVERY turn composed the FULL AI-context briefing
// (29,691 chars ≈ 7,368 tokens) + every enabled tool schema + recall +
// project card + skills BEFORE the first engine call — a one-line "hi"
// paid a ~10K-token prefill.
//
// This test measures what the engine ACTUALLY receives under v1.2.5 for
// the same turn and asserts the measured reduction.

func TestFirstPromptCostMeasurement(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("hello!") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	// Register a realistic tool surface so the tool-schema cost is real.
	for _, tool := range []Tool{
		&fakeTool{name: "shell"},
		&fakeTool{name: "files"},
		&fakeTool{name: "codeExec"},
		&fakeTool{name: "webSearch"},
		&fakeTool{name: "git"},
		&fakeTool{name: "browser"},
		&fakeTool{name: "dataAnalysis"},
		&fakeTool{name: "json"},
		&fakeTool{name: "archive"},
		&fakeTool{name: "fetch"},
		&fakeTool{name: "diff"},
		&fakeTool{name: "screenshot"},
		&fakeTool{name: "linux"},
		&fakeTool{name: "research"},
		&fakeTool{name: "memory"},
		&fakeTool{name: "pipeline"},
	} {
		orch.Register(tool)
	}

	orch.SetProjectCard(func() string {
		return strings.Repeat("## PROJECT INTELLIGENCE\n- facts\n", 20)
	})

	orch.SetRecaller(recallerFunc(func(query string, k, maxTokens int) string {
		return "user asked: baseline\nassistant answered: baseline"
	}))

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "hi"}},
		func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	// MEASURED: the prompt the engine received (all message content).
	promptTokens := chunking.EstimateTokens(seen.allContent())

	// MEASURED: the tool schemas the engine received.
	toolSchemas := len(seen.tools)

	t.Logf("v1.2.5 measured first-request prompt: ~%d tokens (tier=%s, tools offered=%d, firstPromptTokens=%d)",
		promptTokens, res.Tier, toolSchemas, res.Timing.FirstPromptTokens)

	if res.Tier != "FAST" {
		t.Fatalf("tier = %s; measurement only meaningful on FAST", res.Tier)
	}

	// The v1.2.4 full briefing ALONE was ~7,368 tokens; the complete
	// v1.2.4 first prompt (briefing + 17 tool schemas + recall + card)
	// measured ~10K tokens. The v1.2.5 FAST prompt must be a small
	// fraction of that: compact briefing (~353) + bounded tools + the
	// one-line turn.
	if promptTokens > 4000 {
		t.Fatalf("FAST first prompt = ~%d tokens — expected a small fraction of the ~10K v1.2.4 baseline", promptTokens)
	}

	// The offered tool surface is tier-bounded (FAST caps at 6).
	if toolSchemas > 6 {
		t.Fatalf("FAST offered %d tool schemas; the tier caps at 6", toolSchemas)
	}

	// The measured timeline reported the same initial cost.
	if res.Timing.FirstPromptTokens <= 0 || res.Timing.FirstPromptTokens > 4000 {
		t.Fatalf("FirstPromptTokens = %d (out of the expected FAST range)", res.Timing.FirstPromptTokens)
	}

	// Honest context accounting on the result.
	if res.Timing.ClassifyMs < 0 || res.Timing.ContextMs < 0 || res.Timing.PromptMs < 0 {
		t.Fatalf("negative timing values: %+v", res.Timing)
	}
}

// --- thinking/trivial-chat compactness ---

// Trivial prompt under the THINKING composer control (measured).
//
// v1.2.5 field report (the user's machine, Windows, measured):
//
//      FAST simple prompt      ≈ 1,103 tokens
//      THINKING simple prompt  ≈ 12,962 tokens   ("No visible answer returned")
//
// Root cause (source-audited): the thinking control added +20 complexity
// in the classifier AND forced the STANDARD tier floor — so a one-line
// question paid the full ~7.4K-token briefing + recall + project card +
// up to 12 tool schemas.
//
// v1.2.6 policy: THINKING enables REASONING (the nudge), not context size.
// The tier stays what the task's own signals justify (FAST for a trivial
// question) and only real run evidence escalates it.
//
// This test measures what the engine ACTUALLY receives and asserts the fix.

func v126RunMeasured(t *testing.T, thinking string) (promptTokens, toolSchemas int, tier string) {
	t.Helper()

	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("The answer is 4.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	cfg.ThinkingMode = false // the global toggle is off; only the control drives it

	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	for _, tool := range []Tool{
		&fakeTool{name: "shell"},
		&fakeTool{name: "files"},
		&fakeTool{name: "codeExec"},
		&fakeTool{name: "webSearch"},
		&fakeTool{name: "git"},
		&fakeTool{name: "browser"},
		&fakeTool{name: "dataAnalysis"},
		&fakeTool{name: "json"},
		&fakeTool{name: "archive"},
		&fakeTool{name: "fetch"},
		&fakeTool{name: "diff"},
		&fakeTool{name: "screenshot"},
		&fakeTool{name: "linux"},
		&fakeTool{name: "research"},
		&fakeTool{name: "memory"},
		&fakeTool{name: "pipeline"},
	} {
		orch.Register(tool)
	}

	// Optional-block providers that would gladly inject content when a
	// tier allows it — the thinking fix must not pay for any of them on a
	// trivial question.
	orch.SetProjectCard(func() string {
		return strings.Repeat("## PROJECT INTELLIGENCE\n- facts\n", 20)
	})

	orch.SetRecaller(recallerFunc(func(query string, k, maxTokens int) string {
		return "user asked: baseline\nassistant answered: baseline"
	}))

	var opts []RunOption

	if thinking != "" {
		opts = append(opts, WithThinkingMode(thinking))
	}

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "2+2"}},
		func(_ Activity) {},
		opts...)

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	return chunking.EstimateTokens(seen.allContent()), len(seen.tools), res.Tier
}

func TestThinkingSimplePromptStaysCompact(t *testing.T) {
	fastTokens, fastTools, fastTier := v126RunMeasured(t, "fast")
	thinkTokens, thinkTools, thinkTier := v126RunMeasured(t, "thinking")

	t.Logf(
		"v1.2.6 measured — FAST: ~%d tokens, %d tools, tier %s | THINKING: ~%d tokens, %d tools, tier %s",
		fastTokens, fastTools, fastTier, thinkTokens, thinkTools, thinkTier,
	)

	// The measured v1.2.5 baseline for the SAME shape was ~12,962 tokens
	// (STANDARD tier: full briefing + recall + card + 12 tool schemas).
	// v1.2.6: thinking on a trivial prompt must remain in the same compact
	// class as fast — reasoning enabled, context minimal.
	if thinkTier != taskclassify.TierFast {
		t.Fatalf("thinking on a trivial prompt: tier = %s, want FAST (reasoning must not inflate context)", thinkTier)
	}

	if thinkTokens > 4000 {
		t.Fatalf("thinking first prompt = ~%d tokens — the v1.2.5 over-injection is back (baseline was ~12,962)", thinkTokens)
	}

	if thinkTools > 6 {
		t.Fatalf("thinking offered %d tool schemas; the FAST tier caps at 6 (trivial chat must offer 0)", thinkTools)
	}

	// The exact contract: a trivial question with NO capability signal
	// offers ZERO tool schemas under BOTH controls.
	if thinkTools != 0 || fastTools != 0 {
		t.Fatalf("trivial chat tool schemas — fast: %d, thinking: %d; want 0 for both (chat → none/minimal)", fastTools, thinkTools)
	}

	// The thinking nudge is tiny; the two postures must be within a small
	// margin of each other.
	if delta := thinkTokens - fastTokens; delta < 0 || delta > 400 {
		t.Fatalf("thinking prompt (~%d) should be within a small margin of fast (~%d)", thinkTokens, fastTokens)
	}
}

func TestTrivialChatOffersNoToolsAndCompactBriefing(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("Hello!") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	for _, tool := range []Tool{
		&fakeTool{name: "shell"},
		&fakeTool{name: "files"},
		&fakeTool{name: "memory"},
		&fakeTool{name: "webSearch"},
	} {
		orch.Register(tool)
	}

	orch.SetProjectCard(func() string { return "## PROJECT INTELLIGENCE should not ride" })
	orch.SetRecaller(recallerFunc(func(string, int, int) string { return "recall should not ride" }))

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "hi"}},
		func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if res.Tier != taskclassify.TierFast {
		t.Fatalf("tier = %s, want FAST", res.Tier)
	}

	if len(seen.tools) != 0 {
		t.Fatalf("trivial chat offered %d tool schemas (%v); want 0", len(seen.tools), seen.tools)
	}

	if content := seen.allContent(); strings.Contains(content, "PROJECT INTELLIGENCE") || strings.Contains(content, "recall should not ride") {
		t.Fatal("project card / recall block rode a trivial chat prompt — the over-injection contract is broken")
	}
}
