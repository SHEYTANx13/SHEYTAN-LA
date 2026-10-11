package agent

// orchestrator_tiers_test.go — v1.2.5 acceptance tests for the adaptive
// turn pipeline: tier selection on the REAL path, tier-scoped composition
// (compact briefing, skipped recall/card work), per-request thinking
// control, manual tool policy enforcement, the tool result cache, context
// escalation with evidence reasons, the per-request timing clock, and the
// new wire events. Every assertion checks MEASURED behaviour of the
// orchestrator against the fake engine — no telemetry value is trusted
// unless the engine actually saw it.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/taskclassify"
)

// collector records every activity event of one run.
type collector struct {
	mu     sync.Mutex
	events []Activity
}

func (c *collector) observe(a Activity) {
	c.mu.Lock()
	c.events = append(c.events, a)
	c.mu.Unlock()
}

func (c *collector) of(t string) []Activity {
	c.mu.Lock()
	defer c.mu.Unlock()

	var out []Activity
	for _, e := range c.events {
		if e.Type == t {
			out = append(out, e)
		}
	}

	return out
}

func (c *collector) has(t string) bool {
	return len(c.of(t)) > 0
}

// engineSeen captures what the fake engine actually received.
type engineSeen struct {
	mu       sync.Mutex
	turns    int
	messages []map[string]any // last turn's messages
	tools    []string         // last turn's offered tools
}

func (e *engineSeen) record(turn int, body map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.turns = turn

	if msgs, ok := body["messages"].([]any); ok {
		e.messages = nil
		for _, m := range msgs {
			if mm, ok := m.(map[string]any); ok {
				e.messages = append(e.messages, mm)
			}
		}
	}

	e.tools = e.tools[:0]

	if tools, ok := body["tools"].([]any); ok {
		for _, t := range tools {
			if tm, ok := t.(map[string]any); ok {
				if fn, ok := tm["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						e.tools = append(e.tools, name)
					}
				}
			}
		}
	}
}

func (e *engineSeen) systemContent() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, m := range e.messages {
		if role, _ := m["role"].(string); role == "system" {
			c, _ := m["content"].(string)
			return c
		}
	}

	return ""
}

func (e *engineSeen) allContent() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	var b strings.Builder
	for _, m := range e.messages {
		stringifyValue(&b, m["content"])
		b.WriteString("\n")
	}

	return b.String()
}

// stringifyValue renders any JSON value shape (string, array of content
// parts, nested objects) — multimodal messages carry array content.
func stringifyValue(b *strings.Builder, v any) {
	switch t := v.(type) {
	case string:
		b.WriteString(t)
	case []any:
		for _, item := range t {
			stringifyValue(b, item)
		}
	case map[string]any:
		for key, val := range t {
			b.WriteString(key)
			b.WriteString(":")
			stringifyValue(b, val)
			b.WriteString(" ")
		}
	}
}

// --- 1. Simple chat: FAST tier, compact briefing, no optional composition.

func TestAdaptiveSimpleChatRunsFastTier(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("hello!") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	var recallCalls int

	orch.SetRecaller(recallerFunc(func(query string, k, maxTokens int) string {
		recallCalls++
		return "user asked: something\nassistant answered: something"
	}))

	rec := &collector{}
	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "hi"}},
		rec.observe)

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if res.Text != "hello!" {
		t.Fatalf("text = %q", res.Text)
	}

	if res.Tier != taskclassify.TierFast {
		t.Fatalf("simple chat tier = %q, want FAST", res.Tier)
	}

	if res.FinalTier != taskclassify.TierFast {
		t.Fatalf("final tier = %q, want FAST (no escalation expected)", res.FinalTier)
	}

	// The FAST briefing is the compact form.
	if sys := seen.systemContent(); !strings.Contains(sys, "compact mode") {
		t.Fatalf("FAST briefing should be the compact form, got: %.200s", sys)
	}

	// FAST skips recall composition entirely — no retrieval I/O at all.
	if recallCalls != 0 {
		t.Fatalf("recall composed %d times on FAST; want 0", recallCalls)
	}

	// The measured timeline exists and total is sane.
	if res.Timing.TotalMs < 0 {
		t.Fatalf("negative total timing: %+v", res.Timing)
	}

	if res.Timing.ClassifyMs < 0 || res.Timing.ContextMs < 0 || res.Timing.PromptMs < 0 {
		t.Fatalf("negative stage timing: %+v", res.Timing)
	}
}

type recallerFunc func(query string, k, maxTokens int) string

func (f recallerFunc) RelevantBlock(query string, k, maxTokens int) string {
	return f(query, k, maxTokens)
}

// --- 2. Coding work: STANDARD or deeper, full briefing, card travels.

func TestAdaptiveCodingWorkCarriesCard(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("fixed.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	orch.SetProjectCard(func() string {
		return "## PROJECT INTELLIGENCE\n- Languages: Go"
	})

	rec := &collector{}
	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "fix the bug in main.go"}},
		rec.observe)

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if taskclassify.TierRank(res.Tier) < taskclassify.TierRank(taskclassify.TierStandard) {
		t.Fatalf("coding tier = %q, want >= STANDARD", res.Tier)
	}

	if !strings.Contains(seen.allContent(), "PROJECT INTELLIGENCE") {
		t.Fatalf("project card missing from the coding-tier prompt")
	}

	if sys := seen.systemContent(); strings.Contains(sys, "compact mode") {
		t.Fatalf("coding tier should carry the FULL briefing, got compact")
	}
}

// --- 3. Thinking control changes the ACTUAL backend request.

func TestThinkingControlFastForcesFastTierAndNoNudge(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("quick answer") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	// The task is complex enough for STANDARD without the control.
	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "fix the bug in main.go"}},
		func(_ Activity) {},
		WithThinkingMode("fast"))

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if res.Tier != taskclassify.TierFast {
		t.Fatalf("fast control tier = %q, want FAST", res.Tier)
	}

	if sys := seen.systemContent(); strings.Contains(sys, thinkingNudgeSentinel) {
		t.Fatalf("fast control must not add the thinking nudge")
	}
}

func TestThinkingControlThinkingEnablesNudgeAndDepth(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("deep answer") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	cfg.ThinkingMode = false // the global toggle is OFF…

	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	// …but the per-request control overrides it.
	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "hi"}},
		func(_ Activity) {},
		WithThinkingMode("thinking"))

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	// v1.2.6: thinking enables the REASONING nudge but no longer floors the
	// tier — a trivial "hi" stays FAST (compact context), the depth arrives
	// through the task's own signals + the evidence-driven escalation ladder.
	if res.Tier != taskclassify.TierFast {
		t.Fatalf("thinking control tier = %q, want FAST for a trivial task (reasoning ≠ bigger context)", res.Tier)
	}

	if sys := seen.systemContent(); !strings.Contains(sys, thinkingNudgeSentinel) {
		t.Fatalf("thinking control must add the thinking nudge")
	}
}

// --- 4. Manual tool policy: only selected tools offered AND executable.

func TestManualToolPolicyRestrictsOfferAndExecution(t *testing.T) {
	seen := &engineSeen{}
	refused := ""

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)

		if turn == 1 {
			// The model reaches for a tool OUTSIDE the manual selection.
			return sseToolCall("call-1", "shell", `{"command":"ls"}`) + sseDone
		}

		// Turn 2: the refusal came back; finish with text.
		return sseChunk("adapted.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	orch.Register(&fakeTool{name: "echo"})
	orch.Register(&fakeTool{name: "shell"})

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "run the tests and fix the bug"}},
		func(a Activity) {
			if a.Type == "tool_end" && strings.Contains(a.Caption, "shell") {
				if d, ok := a.Detail.(string); ok {
					refused = d
				}
			}
		},
		WithToolPolicy("manual", []string{"echo"}))

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	// The offered surface contains ONLY the manual selection.
	for _, name := range seen.tools {
		if name != "echo" {
			t.Fatalf("manual mode offered %q — only 'echo' is allowed", name)
		}
	}

	if len(seen.tools) == 0 {
		t.Fatalf("manual mode should still offer the allowed tool")
	}

	// The out-of-policy call is refused with a clear message.
	if !strings.Contains(refused, "manual tool selection") {
		t.Fatalf("refusal message = %q, want the manual-selection explanation", refused)
	}

	if res.Text != "adapted." {
		t.Fatalf("text = %q", res.Text)
	}
}

// --- 5. Tool result cache: identical deterministic calls are reused.

func TestToolResultCacheServesRepeats(t *testing.T) {
	// A cacheable deterministic tool (diff is in the allowlist).
	served := &fakeTool{name: "diff"}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		switch turn {
		case 1, 2:
			return sseToolCall("call-1", "diff", `{"left":"a","right":"b"}`) + sseDone
		default:
			return sseChunk("done.") + sseDone
		}
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)
	orch.Register(served)

	// Drain the cache so other tests cannot interfere.
	globalResultCache.mu.Lock()
	globalResultCache.entries = map[string]string{}
	globalResultCache.order = nil
	globalResultCache.bytes = 0
	globalResultCache.mu.Unlock()

	rec := &collector{}
	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "fix the bug and run the tests"}},
		rec.observe)

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if served.calls != 1 {
		t.Fatalf("diff executed %d times; the second identical call must come from the cache", served.calls)
	}

	cachedEvents := 0
	for _, e := range rec.of("tool_end") {
		if strings.Contains(e.Caption, "served from cache") {
			cachedEvents++
		}
	}

	if cachedEvents != 1 {
		t.Fatalf("cache-served tool_end events = %d, want 1", cachedEvents)
	}

	if res.Text != "done." {
		t.Fatalf("text = %q", res.Text)
	}
}

// --- 6. Context escalation: real evidence moves the tier up mid-run.

func TestContextEscalationOnMissingFileEvidence(t *testing.T) {
	seen := &engineSeen{}
	escalations := []Activity{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)

		if turn == 1 {
			return sseToolCall("call-1", "files", `{"action":"read","path":"src/main.go"}`) + sseDone
		}

		return sseChunk("here is the file context, fixed.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	// A files tool whose result honestly reports a missing file — the
	// evidence the escalation watcher listens for.
	orch.Register(missingFileTool{})

	orch.SetProjectCard(func() string {
		return "## PROJECT INTELLIGENCE\n- Languages: Go\n- Layout: cmd/ internal/ web/"
	})

	rec := &collector{}

	// v1.2.6: the task must legitimately signal file use ("fix the bug in
	// src/main.go") so the files tool is offered under the task-selective
	// surface. The old "hi" setup relied on the v1.2.5 over-injection
	// (core tools on EVERY task) that v1.2.6 removed.
	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "fix the bug in src/main.go"}},
		func(a Activity) {
			rec.observe(a)

			if a.Type == "escalation" {
				escalations = append(escalations, a)
			}
		})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if res.Tier != taskclassify.TierStandard {
		t.Fatalf("starting tier = %q, want STANDARD (coding task)", res.Tier)
	}

	if res.FinalTier != taskclassify.TierDeep {
		t.Fatalf("final tier = %q, want DEEP after MissingFileContext escalation", res.FinalTier)
	}

	if len(res.Escalations) != 1 {
		t.Fatalf("escalations = %+v, want exactly 1", res.Escalations)
	}

	if string(res.Escalations[0].Reason) != string(taskclassify.ReasonMissingFileContext) {
		t.Fatalf("escalation reason = %q, want MissingFileContext", res.Escalations[0].Reason)
	}

	if res.Escalations[0].From != taskclassify.TierStandard || res.Escalations[0].To != taskclassify.TierDeep {
		t.Fatalf("escalation move = %s → %s, want STANDARD → DEEP",
			res.Escalations[0].From, res.Escalations[0].To)
	}

	// The escalation activity carries the honest log line.
	if len(escalations) != 1 {
		t.Fatalf("escalation activities = %d, want 1", len(escalations))
	}

	if !strings.Contains(escalations[0].Caption, "reason=MissingFileContext") {
		t.Fatalf("escalation caption = %q", escalations[0].Caption)
	}

	if !strings.Contains(escalations[0].Caption, "STANDARD") || !strings.Contains(escalations[0].Caption, "DEEP") {
		t.Fatalf("escalation caption must name both tiers: %q", escalations[0].Caption)
	}

	// The second engine request carries the project card the escalation
	// injected (the enrichment that resolves the missing context).
	if !strings.Contains(seen.allContent(), "PROJECT INTELLIGENCE") {
		t.Fatalf("escalated request missing the project card enrichment")
	}

	// The live-status events reported the tier move.
	found := false
	for _, e := range rec.of("status") {
		if strings.Contains(e.Caption, "Using more context") {
			found = true
		}
	}

	if !found {
		t.Fatalf("no 'Using more context' status event after escalation")
	}
}

type missingFileTool struct{}

func (missingFileTool) Name() string        { return "files" }
func (missingFileTool) Description() string { return "read files" }
func (missingFileTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (missingFileTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "Error: file not found: src/main.go (not in the provided context)", nil
}

// --- 7. The new wire events: status, thinking markers, complete.

func TestRunEmitsV125EventVocabulary(t *testing.T) {
	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		// Reasoning block + answer: exercises the thinking markers.
		return sseChunk("<think>step one</think>final answer") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	cfg.ThinkingMode = true

	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	rec := &collector{}
	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "fix the bug in main.go"}},
		rec.observe)

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if !rec.has("thinking_start") {
		t.Fatalf("thinking_start event missing")
	}

	if !rec.has("thinking_end") {
		t.Fatalf("thinking_end event missing")
	}

	if !rec.has("status") {
		t.Fatalf("status events missing")
	}

	if !rec.has("complete") {
		t.Fatalf("complete event missing")
	}

	// The reasoning content itself still flows through the legacy
	// reasoning events (content only once — no duplication).
	if !rec.has("reasoning") {
		t.Fatalf("reasoning event missing")
	}

	if res.Reasoning != "step one" {
		t.Fatalf("reasoning = %q", res.Reasoning)
	}

	if res.Text != "final answer" {
		t.Fatalf("text = %q", res.Text)
	}
}

// --- 8. RunClock: measured stage timeline.

func TestRunClockMeasuredTimeline(t *testing.T) {
	c := NewRunClock()

	earlier := time.Now().Add(-500 * time.Millisecond)
	c.SetReceived(earlier)

	c.Mark(StageClassified)
	c.Mark(StageContextStart)
	c.Mark(StageContextEnd)
	c.Mark(StagePromptStart)
	c.Mark(StagePromptEnd)
	c.Mark(StageSerialized)
	c.Mark(StageRequestSent)

	time.Sleep(3 * time.Millisecond) // a real, measurable gap

	c.Mark(StageFirstToken)
	c.Mark(StageGenerationEnd)
	c.Mark(StageDone)

	snap := c.Snapshot()

	if snap.TotalMs < 400 {
		t.Fatalf("total = %dms, want >= 400 (anchored to the earlier received mark)", snap.TotalMs)
	}

	if snap.TTFTMs <= 0 {
		t.Fatalf("TTFT = %dms, want > 0 (request_sent → first_token)", snap.TTFTMs)
	}

	if snap.ClassifyMs < 0 || snap.ContextMs < 0 || snap.PromptMs < 0 || snap.SerializationMs < 0 {
		t.Fatalf("negative stage durations: %+v", snap)
	}

	// Unmarked stages report 0 — never invented.
	if snap.ToolMs != 0 || snap.VerificationMs != 0 {
		t.Fatalf("unmarked stages must be 0: %+v", snap)
	}

	// SinceReceived reflects the anchored mark.
	if c.SinceReceived() < 400 {
		t.Fatalf("SinceReceived = %dms, want >= 400", c.SinceReceived())
	}
}

// --- 9. Tool spec cache: one marshal per registry generation.

func TestSpecCacheMemoizesSerialization(t *testing.T) {
	o := New(nil, nil)

	tool := &fakeTool{name: "echo"}
	o.Register(tool)

	specs1, tokens1 := o.specs.BuildSpecs([]Tool{tool}, o.ToolsGeneration())
	specs2, tokens2 := o.specs.BuildSpecs([]Tool{tool}, o.ToolsGeneration())

	if tokens1 != tokens2 || len(specs1) != len(specs2) {
		t.Fatalf("memoized specs differ: %d/%d vs %d/%d", len(specs1), tokens1, len(specs2), tokens2)
	}

	if tokens1 <= 0 {
		t.Fatalf("token estimate = %d, want > 0", tokens1)
	}

	if specs1[0].Function.Name != "echo" {
		t.Fatalf("spec name = %q", specs1[0].Function.Name)
	}

	// Invalidation drops the memo (a re-registered tool re-serializes).
	o.Register(tool)

	o.specs.mu.RLock()
	entries := len(o.specs.entries)
	o.specs.mu.RUnlock()

	if entries != 0 {
		t.Fatalf("cache should be empty after Register, has %d entries", entries)
	}
}

// --- 10. Escalation ladder bound: at most 2 upgrades per run.

func TestEscalationLadderBounded(t *testing.T) {
	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		if turn <= 3 {
			return sseToolCall("call-"+string(rune('0'+turn)), "files", `{"action":"read","path":"x.go"}`) + sseDone
		}

		return sseChunk("done.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)
	orch.Register(missingFileTool{})

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "hi"}},
		func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if len(res.Escalations) > maxEscalations {
		t.Fatalf("escalations = %d, want <= %d", len(res.Escalations), maxEscalations)
	}

	// FAST → STANDARD → DEEP at most.
	if res.FinalTier != taskclassify.TierDeep {
		t.Fatalf("final tier = %q, want DEEP (two bounded upgrades from FAST)", res.FinalTier)
	}
}
