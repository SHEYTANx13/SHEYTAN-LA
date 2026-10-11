package agent

import (
        "context"
        "strings"
        "testing"

        "encoding/json"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/ctxtelemetry"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// preflight_test.go — Phase 7 regression tests for the preflight budget
// pipeline: the request must never intentionally exceed the effective
// context, overflow must be repaired automatically where possible, and the
// engine must never be called when the budget cannot be satisfied.

// aiContextStub lets tests control the system-prefixed message exactly
// (hasAIContext matches the sentinel, so RunDetailed will not prepend the
// real briefing).
func aiContextStub(content string) llm.Message {
        return llm.Message{
                Role:    "system",
                Content: "SHEYTAN™ Local-Agent — AI Operating Instructions\n" + content,
        }
}

// bigTool is a fake tool with a configurable (possibly huge) schema.
type bigTool struct {
        name  string
        desc  string
        onRun func() string
}

func (b bigTool) Name() string        { return b.name }
func (b bigTool) Description() string { return b.desc }
func (b bigTool) Parameters() any     { return struct{}{} }
func (b bigTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
        if b.onRun != nil {
                return b.onRun(), nil
        }
        return "ok", nil
}

func TestPreflightImpossibleBudgetNeverCallsEngine(t *testing.T) {
        server, turns := newFakeEngine(t, func(turn int, _ map[string]any) string {
                return sseChunk("should never happen") + sseDone
        })

        cfg := remoteConfig(t, server.URL)
        cfg.LLM.NumCtx = 1024 // tiny window
        cfg.LLM.MaxTokens = 512

        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        // A huge tool schema makes even the COMPACT briefing + reduced
        // toolset impossible inside the smallest window.
        orch.Register(bigTool{name: "shell", desc: strings.Repeat("huge schema. ", 800)})

        _, err := orch.RunDetailed(context.Background(), []llm.Message{
                // v1.2.6: the task names the shell tool — under the task-selective
                // surface that is an explicit capability signal, so the huge shell
                // schema rides the request and the budget stays impossible.
                {Role: "user", Content: "run the shell tool"},
        }, func(_ Activity) {})

        if err == nil {
                t.Fatal("preflight must refuse an impossible budget")
        }
        if !strings.Contains(err.Error(), "context budget impossible") {
                t.Fatalf("error should name the impossible budget: %v", err)
        }
        if *turns != 0 {
                t.Fatalf("engine must never be called when preflight fails; calls = %d", *turns)
        }
}

func TestPreflightToolsetReductionOnOverflow(t *testing.T) {
        var toolNamesInRequest []string

        server, turns := newFakeEngine(t, func(turn int, body map[string]any) string {
                if turn == 1 {
                        if tools, ok := body["tools"].([]any); ok {
                                for _, tl := range tools {
                                        if m, ok := tl.(map[string]any); ok {
                                                if fn, ok := m["function"].(map[string]any); ok {
                                                        name, _ := fn["name"].(string)
                                                        toolNamesInRequest = append(toolNamesInRequest, name)
                                                }
                                        }
                                }
                        }
                        return sseChunk("done") + sseDone
                }
                return sseChunk("done") + sseDone
        })

        cfg := remoteConfig(t, server.URL)
        cfg.LLM.NumCtx = 3072
        cfg.LLM.MaxTokens = 512

        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        // A controlled small briefing + several huge-schema tools: the FULL
        // surface overflows; the coding-relevant subset must fit.
        orch.Register(bigTool{name: "shell", desc: strings.Repeat("shell tool for coding. ", 120)})
        orch.Register(bigTool{name: "files", desc: strings.Repeat("files tool for coding. ", 120)})
        orch.Register(bigTool{name: "webSearch", desc: strings.Repeat("web search tool. ", 120)})
        orch.Register(bigTool{name: "browser", desc: strings.Repeat("browser tool. ", 120)})
        orch.Register(bigTool{name: "memory", desc: strings.Repeat("memory tool. ", 120)})

        messages := []llm.Message{
                aiContextStub(strings.Repeat("brief. ", 20)),
                {Role: "user", Content: "fix the bug in the parser and verify with tests"},
        }

        result, err := orch.RunDetailed(context.Background(), messages, func(_ Activity) {})
        if err != nil {
                t.Fatalf("preflight must repair via toolset reduction: %v", err)
        }

        if len(toolNamesInRequest) == 0 || len(toolNamesInRequest) >= 5 {
                t.Fatalf("toolset reduction did not engage; tools offered: %v", toolNamesInRequest)
        }
        for _, name := range toolNamesInRequest {
                if name == "browser" {
                        t.Fatalf("browser (irrelevant to a coding task) must be reduced away: %v", toolNamesInRequest)
                }
        }
        if *turns != 1 {
                t.Fatalf("run must complete normally, turns = %d", *turns)
        }
        if result.Text != "done" {
                t.Fatalf("text = %q", result.Text)
        }
}

func TestPreflightCompactBriefingOnOverflow(t *testing.T) {
        var sawCompact bool
        var sawFull bool

        server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
                if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
                        first, _ := msgs[0].(map[string]any)
                        content, _ := first["content"].(string)
                        if strings.Contains(content, "compact briefing") {
                                sawCompact = true
                        }
                        if strings.Contains(content, "LIVE ENVIRONMENT") && len(content) > 6000 {
                                sawFull = true
                        }
                }
                return sseChunk("ok") + sseDone
        })

        cfg := remoteConfig(t, server.URL)
        cfg.LLM.NumCtx = 3072
        cfg.LLM.MaxTokens = 512

        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        // The full briefing is ~3k tokens; a 3072 window cannot carry it with
        // the history floor. The ladder must compact it.
        _, err := orch.RunDetailed(context.Background(), []llm.Message{
                {Role: "user", Content: "summarize"},
        }, func(_ Activity) {})
        if err != nil {
                t.Fatalf("compact-briefing run failed: %v", err)
        }

        if !sawCompact {
                t.Fatal("the compact briefing never reached the engine")
        }
        if sawFull {
                t.Fatal("the full briefing should have been compacted away")
        }
}

func TestInLoopToolResultCompaction(t *testing.T) {
        var turn2Prompt string

        server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
                if turn == 1 {
                        return sseToolCall("call-1", "huge", "{}") + sseDone
                }
                if msgs, ok := body["messages"].([]any); ok {
                        var b strings.Builder
                        for _, m := range msgs {
                                msg, _ := m.(map[string]any)
                                role, _ := msg["role"].(string)
                                content, _ := msg["content"].(string)
                                b.WriteString(role + ":" + content + "\n")
                        }
                        turn2Prompt = b.String()
                }
                return sseChunk("final") + sseDone
        })

        cfg := remoteConfig(t, server.URL)
        cfg.LLM.NumCtx = 3072
        cfg.LLM.MaxTokens = 512

        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        huge := strings.Repeat("x", 24000) // ~6000 tokens of tool output
        orch.Register(bigTool{name: "huge", desc: "returns a huge result", onRun: func() string {
                return huge
        }})

        messages := []llm.Message{
                aiContextStub(strings.Repeat("brief. ", 20)),
                {Role: "user", Content: "run the huge tool"},
        }

        _, err := orch.RunDetailed(context.Background(), messages, func(_ Activity) {})
        if err != nil {
                t.Fatalf("in-loop compaction run: %v", err)
        }

        if turn2Prompt == "" {
                t.Fatal("second turn never happened")
        }
        if !strings.Contains(turn2Prompt, "[tool result elided under context pressure") {
                t.Fatal("the oversized tool result was not elided mid-turn")
        }
}

func TestPreflightTelemetryRecorded(t *testing.T) {
        server, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
                return sseChunk("hello there") + sseDone
        })

        cfg := remoteConfig(t, server.URL)

        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        store := ctxtelemetry.NewStore(cfg.DataDir)
        orch.SetTelemetry(store)

        _, err := orch.RunDetailed(context.Background(), []llm.Message{
                {Role: "user", Content: "hi"},
        }, func(_ Activity) {})
        if err != nil {
                t.Fatalf("run: %v", err)
        }

        sum := store.Summarize()
        if sum.Turns != 1 {
                t.Fatalf("telemetry turns = %d, want 1", sum.Turns)
        }
}

func TestCompactToolResultsPreservesStructure(t *testing.T) {
        msgs := []llm.Message{
                {Role: "system", Content: "sys"},
                {Role: "user", Content: "go"},
                {Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
                        ID: "1", Type: "function",
                }}},
                {Role: "tool", Content: strings.Repeat("a", 100000), ToolCallID: "1"},
        }
        msgs[2].ToolCalls[0].Function.Name = "shell"
        msgs[2].ToolCalls[0].Function.Arguments = "{}"

        elided, savedTokens := compactToolResults(msgs, 1000)
        if elided != 1 {
                t.Fatalf("elided = %d, want 1", elided)
        }
        if savedTokens <= 0 {
                t.Fatalf("savedTokens = %d, want > 0", savedTokens)
        }
        if msgs[3].Role != "tool" || msgs[3].ToolCallID != "1" {
                t.Fatal("tool message structure must be preserved")
        }
        if !strings.HasPrefix(msgs[3].Content, "[tool result elided") {
                t.Fatal("body must be replaced by the explicit marker")
        }
        // Running again is a no-op (already elided).
        if again, againSaved := compactToolResults(msgs, 1000); again != 0 || againSaved != 0 {
                t.Fatalf("second pass must not re-elide, got %d", again)
        }
}

func TestContextSafetyMarginClamped(t *testing.T) {
        if got := contextSafetyMargin(1024); got != 64 {
                t.Fatalf("small window margin = %d, want 64", got)
        }
        if got := contextSafetyMargin(8192); got != 128 {
                t.Fatalf("8k margin = %d, want 128", got)
        }
        if got := contextSafetyMargin(131072); got != 256 {
                t.Fatalf("huge window margin = %d, want the 256 clamp", got)
        }
}

// --- small-context degradation and in-loop fit ---

// TestSmallContextTurnDegradesAndFitsCeiling runs a long conversation
// through RunDetailed at small windows and verifies graceful degradation
// with an in-ceiling wire request.
func TestSmallContextTurnDegradesAndFitsCeiling(t *testing.T) {
        for _, numCtx := range []int{2048, 4096, 8192} {
                t.Run(strings.Join([]string{"numCtx", strings.TrimSpace(intString(numCtx))}, "-"), func(t *testing.T) {
                        var lastRequest map[string]any
                        var engineCalled bool

                        server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
                                engineCalled = true
                                lastRequest = body
                                return sseChunk("ack.") + sseDone
                        })

                        cfg := remoteConfig(t, server.URL)
                        cfg.LLM.NumCtx = numCtx
                        cfg.LLM.MaxTokens = 512

                        client := llm.NewClient(config.NewSource(cfg))
                        orch := New(config.NewSource(cfg), client)

                        // A growing transcript: plenty of elidable history plus the
                        // fresh user turn — exactly the shape that used to refuse.
                        messages := []llm.Message{
                                aiContextStub(strings.Repeat("brief. ", 20)),
                        }

                        for i := 0; i < 40; i++ {
                                messages = append(messages,
                                        llm.Message{Role: "user", Content: strings.Repeat("history turn ", 8)},
                                        llm.Message{Role: "assistant", Content: strings.Repeat("older answer ", 8)},
                                )
                        }

                        messages = append(messages, llm.Message{Role: "user", Content: "continue the work"})

                        _, err := orch.RunDetailed(context.Background(), messages, func(_ Activity) {})
                        if err != nil {
                                t.Fatalf("small-context turn refused instead of degrading: %v", err)
                        }

                        if !engineCalled || lastRequest == nil {
                                t.Fatal("the engine was never called")
                        }

                        // Measured ceiling: usable window minus the safety margin.
                        safety := ContextSafetyMargin(numCtx)
                        reserve := outputReserveForTest(numCtx, 512)
                        ceiling := numCtx - reserve - safety

                        // The serialized request must honor the ceiling.
                        estimate := 0
                        if msgs, ok := lastRequest["messages"].([]any); ok {
                                for _, m := range msgs {
                                        if mm, ok := m.(map[string]any); ok {
                                                if c, ok := mm["content"].(string); ok {
                                                        estimate += chunking.EstimateTokens(c)
                                                }
                                        }
                                }
                        }

                        if estimate > ceiling {
                                t.Fatalf("wire request ~%d tok exceeds the measured ceiling %d (numCtx %d)",
                                        estimate, ceiling, numCtx)
                        }
                })
        }
}

func intString(n int) string {
        digits := ""
        if n == 0 {
                return "0"
        }
        for n > 0 {
                digits = string(rune('0'+n%10)) + digits
                n /= 10
        }
        return digits
}

func outputReserveForTest(numCtx, maxTokens int) int {
        if maxTokens <= 0 {
                maxTokens = 1024
        }
        if maxTokens > numCtx/2 {
                maxTokens = numCtx / 2
        }
        if maxTokens < 512 {
                maxTokens = 512
        }
        return maxTokens
}

// TestInLoopGrowthRefusesWhenUnfittable pins the v1.2.8.1 in-loop gate:
// when even after tool-result compaction the request cannot fit, the run
// refuses with a measured error instead of silently sending an
// over-ceiling request.
func TestInLoopGrowthRefusesWhenUnfittable(t *testing.T) {
        var turn int

        server, _ := newFakeEngine(t, func(t int, _ map[string]any) string {
                turn = t
                if t == 1 {
                        return sseToolCall("call-1", "huge", "{}") + sseDone
                }
                return sseChunk("should never get here") + sseDone
        })

        cfg := remoteConfig(t, server.URL)
        // A tiny window plus a huge non-tool tail: compaction has nothing
        // left to remove — the in-loop gate must refuse.
        cfg.LLM.NumCtx = 2048
        cfg.LLM.MaxTokens = 512

        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        orch.Register(bigTool{name: "huge", desc: "returns a huge result", onRun: func() string {
                return strings.Repeat("x", 40000) // ~10000 tokens of tool output
        }})

        // An oversized current user turn eats the window on top of the tool
        // result (the current turn is never truncated; the in-loop gate is
        // the honest refusal path).
        messages := []llm.Message{
                aiContextStub(strings.Repeat("brief. ", 20)),
                {Role: "user", Content: strings.Repeat("huge current request ", 700)},
        }

        _, err := orch.RunDetailed(context.Background(), messages, func(_ Activity) {})
        if err == nil {
                // v1.4.0: this was previously t.Skip — which meant a BROKEN
                // in-loop gate (the oversized request going through and the
                // engine answering) produced a green SKIP instead of a red
                // failure. The skip was masking the exact regression this test
                // exists to pin: the run MUST refuse, so a nil error IS the
                // failure.
                t.Fatal("the run completed although the request could not fit — the in-loop fit gate did not refuse")
        }

        if !strings.Contains(err.Error(), "context fit violation") {
                t.Fatalf("expected the in-loop fit violation, got: %v", err)
        }

        _ = turn
}
