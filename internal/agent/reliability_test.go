package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// --- ClassifyFailure ---------------------------------------------------------

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		name string
		in   ToolFailure
		want FailureCategory
	}{
		{"timeout flag", ToolFailure{Err: "boom", Timeout: true}, CatProcess},
		{"deadline text", ToolFailure{Err: "context deadline exceeded"}, CatProcess},
		{"missing path", ToolFailure{Err: "open /no/such/file: no such file or directory"}, CatFilesystem},
		{"command not found", ToolFailure{Err: "exec: \"git\": executable file not found in $PATH"}, CatEnvironment},
		{"perm denied", ToolFailure{Err: "open /etc/shadow: permission denied"}, CatFilesystem},
		{"path jail", ToolFailure{Err: "path escapes the workspace jail"}, CatFilesystem},
		{"conn refused", ToolFailure{Err: "dial tcp 127.0.0.1:8080: connect: connection refused"}, CatNetwork},
		{"dns", ToolFailure{Err: "lookup example.com: no such host"}, CatNetwork},
		{"offline", ToolFailure{Err: "offline mode: network access disabled"}, CatNetwork},
		{"context window", ToolFailure{Err: "the request exceeds the model context window"}, CatContext},
		{"bad json args", ToolFailure{Err: "invalid character 'x' looking for beginning of value"}, CatToolArguments},
		{"missing param", ToolFailure{Err: "missing required parameter: path"}, CatToolArguments},
		{"unknown tool", ToolFailure{Err: "unknown tool \"foo\""}, CatToolSelection},
		{"tests failed", ToolFailure{Err: "exit status 1", Output: "--- FAIL: TestX"}, CatExecution},
		{"verification failed", ToolFailure{Err: "verification failed: checks did not pass"}, CatVerification},
		{"stale session", ToolFailure{Err: "session expired: no longer active"}, CatState},
		{"unclassifiable", ToolFailure{Err: "something completely different"}, CatUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyFailure(tc.in); got != tc.want {
				t.Fatalf("ClassifyFailure(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRepairHintNeverPrescribesBlindRetry(t *testing.T) {
	cats := []FailureCategory{
		CatKnowledge, CatReasoning, CatPlanning, CatToolSelection,
		CatToolArguments, CatExecution, CatFilesystem, CatProcess,
		CatNetwork, CatModelCapability, CatContext, CatVerification,
		CatEnvironment, CatState, CatUnknown,
	}

	for _, c := range cats {
		hint := RepairHint(c)
		if !strings.HasPrefix(hint, "Recover:") {
			t.Errorf("category %s: hint does not start with a recovery instruction: %q", c, hint)
		}
		if hint == "" {
			t.Errorf("category %s: empty hint", c)
		}
	}
}

// --- LoopGuard ----------------------------------------------------------------

func TestLoopGuardWarnsThenBlocksRepeats(t *testing.T) {
	g := NewLoopGuard()
	args := `{"path":"/tmp/x"}`

	// First call: clean pass.
	obs := g.Observe("read_file", args)
	if obs.Repeat || obs.Warn != "" || obs.Block != "" {
		t.Fatalf("first call must pass clean: %+v", obs)
	}

	// Second call: warned (strategy change), still executed.
	obs = g.Observe("read_file", args)
	if !obs.Repeat || obs.Warn == "" || obs.Block != "" {
		t.Fatalf("second call must warn, not block: %+v", obs)
	}

	// Third call: still executed (MaxSameCall=3).
	obs = g.Observe("read_file", args)
	if obs.Block != "" {
		t.Fatalf("third call must still execute: %+v", obs)
	}

	// Fourth call: refused.
	obs = g.Observe("read_file", args)
	if obs.Block == "" {
		t.Fatalf("fourth identical call must be blocked: %+v", obs)
	}
}

func TestLoopGuardDistinctArgumentsAreNotRepeats(t *testing.T) {
	g := NewLoopGuard()

	for _, a := range []string{
		`{"path":"a.txt"}`,
		`{"path":"b.txt"}`,
		`  {"path" : "c.txt"} `,
	} {
		if obs := g.Observe("read_file", a); obs.Repeat {
			t.Fatalf("distinct arguments flagged as repeat: %s", a)
		}
	}

	// Reformatted-but-identical call IS a repeat: whitespace is
	// normalized so the model cannot reset the counter by reformatting.
	if obs := g.Observe("read_file", `{"path":"a.txt"}`); !obs.Repeat {
		t.Fatal("reformatted identical call must count as a repeat")
	}
}

func TestLoopGuardToolCallBudget(t *testing.T) {
	g := NewLoopGuard()
	g.MaxToolCalls = 5

	for i := 0; i < 5; i++ {
		if obs := g.Observe("t", fmt.Sprintf(`{"i":%d}`, i)); obs.Block != "" {
			t.Fatalf("call %d within budget was blocked", i)
		}
	}

	if obs := g.Observe("t", `{"i":99}`); obs.Block == "" {
		t.Fatal("call beyond the tool budget must be blocked")
	}
}

// fakeClock returns a deterministic time source advancing only when the
// test moves it. Wall-clock budget logic must never depend on the host
// scheduler: on Windows the monotonic clock is coarse enough that a
// nanosecond budget measured with the real clock can read elapsed == 0.
func fakeClock(start time.Time) (*time.Time, func() time.Time) {
	current := start
	return &current, func() time.Time { return current }
}

func TestLoopGuardWallClockBudget(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("already expired blocks immediately", func(t *testing.T) {
		at, now := fakeClock(base)
		g := NewLoopGuard()
		g.now = now
		g.started = *at
		g.WallClock = time.Minute

		*at = base.Add(time.Minute) // exactly at expiry

		if obs := g.Observe("t", `{}`); obs.Block == "" {
			t.Fatal("wall clock exactly at expiry must block")
		}
	})

	t.Run("well past expiry blocks", func(t *testing.T) {
		at, now := fakeClock(base)
		g := NewLoopGuard()
		g.now = now
		g.started = *at
		g.WallClock = time.Minute

		*at = base.Add(2 * time.Hour)

		if obs := g.Observe("t", `{}`); obs.Block == "" {
			t.Fatal("expired wall clock must block")
		}
	})

	t.Run("just before expiry does not block", func(t *testing.T) {
		at, now := fakeClock(base)
		g := NewLoopGuard()
		g.now = now
		g.started = *at
		g.WallClock = time.Minute

		*at = base.Add(time.Minute - time.Nanosecond)

		obs := g.Observe("t", `{}`)

		if obs.Block != "" {
			t.Fatalf("budget not yet exhausted must not block: %q", obs.Block)
		}

		if strings.Contains(obs.Warn, "wall-clock") {
			t.Fatalf("wall-clock exhaustion leaked into the warning: %q", obs.Warn)
		}
	})

	t.Run("zero budget is disabled", func(t *testing.T) {
		at, now := fakeClock(base)
		g := NewLoopGuard()
		g.now = now
		g.started = *at
		g.WallClock = 0 // disabled

		*at = base.Add(100 * time.Hour)

		if obs := g.Observe("t", `{}`); obs.Block != "" {
			t.Fatalf("disabled budget must never block on wall clock: %q", obs.Block)
		}
	})

	t.Run("negative budget is disabled", func(t *testing.T) {
		at, now := fakeClock(base)
		g := NewLoopGuard()
		g.now = now
		g.started = *at
		g.WallClock = -time.Second

		*at = base.Add(100 * time.Hour)

		if obs := g.Observe("t", `{}`); obs.Block != "" {
			t.Fatalf("negative budget must behave as disabled: %q", obs.Block)
		}
	})

	t.Run("budget applies across multiple calls", func(t *testing.T) {
		at, now := fakeClock(base)
		g := NewLoopGuard()
		g.now = now
		g.started = *at
		g.WallClock = 30 * time.Second

		// Three distinct calls, each consuming 10s of budget.
		for i := 0; i < 3; i++ {
			*at = base.Add(time.Duration(i) * 10 * time.Second)

			if obs := g.Observe("t", fmt.Sprintf(`{"i":%d}`, i)); obs.Block != "" {
				t.Fatalf("call %d within budget was blocked: %q", i, obs.Block)
			}
		}

		// 30s consumed — the next call is past the budget.
		*at = base.Add(30 * time.Second)

		if obs := g.Observe("t", `{"i":99}`); obs.Block == "" {
			t.Fatal("call after the budget was consumed must block")
		}
	})
}

func TestLoopGuardSameResultDetection(t *testing.T) {
	g := NewLoopGuard()
	args := `{"q":"x"}`

	g.Observe("search", args)
	g.Record("search", args, "no results")

	obs := g.Observe("search", args)
	if !obs.SameResult {
		t.Fatal("repeat with recorded result must set SameResult")
	}
}

func TestLoopGuardProgressSummary(t *testing.T) {
	g := NewLoopGuard()

	g.Observe("a", `{"n":1}`)
	g.Observe("a", `{"n":1}`)
	g.Observe("b", `{"n":2}`)

	distinct, total, repeats := g.ProgressSummary()
	if distinct != 2 || total != 3 || repeats != 1 {
		t.Fatalf("summary = %d/%d/%d, want 2/3/1", distinct, total, repeats)
	}
}

// --- EvidenceCollector --------------------------------------------------------

func TestEvidenceCollectorVerdicts(t *testing.T) {
	t.Run("no evidence is not verified", func(t *testing.T) {
		c := NewEvidenceCollector()
		if got := c.Report().Outcome; got != VerificationNone {
			t.Fatalf("outcome = %q, want not_verified", got)
		}
	})

	t.Run("lab verify pass is verified", func(t *testing.T) {
		c := NewEvidenceCollector()
		c.ObserveToolResult("coding_lab", `{"action":"verify"}`, "task verified: 3/3 checks passed", false)
		if got := c.Report().Outcome; got != VerificationVerified {
			t.Fatalf("outcome = %q, want verified", got)
		}
	})

	t.Run("lab verify failure is failed", func(t *testing.T) {
		c := NewEvidenceCollector()
		c.ObserveToolResult("coding_lab", `{"action":"verify"}`, "verification failed: 1/3 checks passed", false)
		if got := c.Report().Outcome; got != VerificationFailed {
			t.Fatalf("outcome = %q, want failed", got)
		}
	})

	t.Run("mixed evidence is partial", func(t *testing.T) {
		c := NewEvidenceCollector()
		c.ObserveToolResult("shell", `{"cmd":"go test ./..."}`, "ok all tests passed", false)
		c.ObserveToolResult("shell", `{"cmd":"go build ./..."}`, "build error: undefined: Foo", false)
		if got := c.Report().Outcome; got != VerificationPartial {
			t.Fatalf("outcome = %q, want partially_verified", got)
		}
	})

	t.Run("unknown tools contribute nothing", func(t *testing.T) {
		c := NewEvidenceCollector()
		c.ObserveToolResult("read_file", `{"path":"x"}`, "file contents here", false)
		c.ObserveToolResult("web_search", `{"q":"x"}`, "10 results", false)
		if got := c.Report().Outcome; got != VerificationNone {
			t.Fatalf("outcome = %q, want not_verified", got)
		}
	})

	t.Run("shell success without test marker is not evidence", func(t *testing.T) {
		c := NewEvidenceCollector()
		c.ObserveToolResult("shell", `{"cmd":"echo hello"}`, "hello", false)
		if got := c.Report().Outcome; got != VerificationNone {
			t.Fatalf("outcome = %q, want not_verified (no explicit check)", got)
		}
	})
}

func TestVerificationSummaryRenders(t *testing.T) {
	r := VerificationReport{Outcome: VerificationNone}
	if !strings.Contains(r.Summary(), "Not verified") {
		t.Fatalf("summary = %q", r.Summary())
	}
}

// --- orchestrator integration --------------------------------------------------

// failingTool fails once with a filesystem error, then succeeds.
type failingTool struct {
	mu     chan struct{}
	name   string
	calls  int
	ranErr bool
}

func (f *failingTool) Name() string        { return f.name }
func (f *failingTool) Description() string { return "test tool that fails first" }
func (f *failingTool) Parameters() any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"input": map[string]any{"type": "string"}},
		"required":   []string{"input"},
	}
}

func (f *failingTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var parsed struct {
		Input string `json:"input"`
	}
	_ = json.Unmarshal(args, &parsed)

	f.calls++

	if f.calls == 1 {
		return "partial stderr", errors.New("open /missing/path: no such file or directory")
	}

	return "ok: " + parsed.Input, nil
}

func TestFailureClassificationReachesModel(t *testing.T) {
	var sawHint bool
	var sawCategoryActivity bool

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		if turn == 1 {
			return sseToolCall("call-1", "flaky", `{"input":"x"}`) + sseDone
		}

		// Second turn: the tool result must carry the category tag
		// and the repair hint.
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, _ := m.(map[string]any)
			if msg["role"] == "tool" {
				content := fmt.Sprint(msg["content"])
				if strings.Contains(content, "[filesystem]") &&
					strings.Contains(content, "Recover:") {
					sawHint = true
				}
			}
		}

		return sseChunk("handled") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	tool := &failingTool{name: "flaky"}
	orch.Register(tool)

	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "use the flaky tool"},
	}, func(a Activity) {
		if a.Type == "failure" && a.Caption == "Diagnosed failure: filesystem" {
			sawCategoryActivity = true
		}
	})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if !sawHint {
		t.Fatal("repair hint with category tag did not reach the model")
	}

	if !sawCategoryActivity {
		t.Fatal("failure classification was not surfaced as an activity")
	}

	if result.FailureTally[CatFilesystem] != 1 {
		t.Fatalf("failure tally = %v, want filesystem:1", result.FailureTally)
	}
}

func TestRunReportsNotVerifiedWithoutEvidence(t *testing.T) {
	server, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
		return sseChunk("I am done, trust me.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	var sawVerificationEvent bool

	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "claim completion"},
	}, func(a Activity) {
		if a.Type == "verification" {
			sawVerificationEvent = true
		}
	})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if result.Verification.Outcome != VerificationNone {
		t.Fatalf("outcome = %q, want not_verified", result.Verification.Outcome)
	}

	if !sawVerificationEvent {
		t.Fatal("verification event missing from the activity stream")
	}
}

// codingLabTool emulates the Coding Lab verify action result shape.
type codingLabTool struct{}

func (c *codingLabTool) Name() string        { return "coding_lab" }
func (c *codingLabTool) Description() string { return "lab" }
func (c *codingLabTool) Parameters() any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"action": map[string]any{"type": "string"}},
		"required":   []string{"action"},
	}
}

func (c *codingLabTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "verification summary: 3/3 checks passed — task verified", nil
}

func TestRunCollectsLabVerifyEvidence(t *testing.T) {
	server, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
		if turn == 1 {
			return sseToolCall("call-1", "coding_lab", `{"action":"verify"}`) + sseDone
		}

		return sseChunk("All verified.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)
	orch.Register(&codingLabTool{})

	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "verify the work"},
	}, func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if result.Verification.Outcome != VerificationVerified {
		t.Fatalf("outcome = %q, want verified (lab verify evidence)", result.Verification.Outcome)
	}

	if len(result.Verification.Evidence) != 1 ||
		result.Verification.Evidence[0].Kind != EvidenceLabVerify {
		t.Fatalf("evidence = %+v", result.Verification.Evidence)
	}
}

func TestLoopGuardBlocksRunawayLoop(t *testing.T) {
	// The model calls the same tool with the same arguments forever.
	// The guard must (a) warn once, (b) refuse further identical calls,
	// and (c) the run must still terminate with the model's final answer.
	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		if turn <= 6 {
			return sseToolCall("call-1", "echo", `{"input":"same"}`) + sseDone
		}

		var sawRefusal bool

		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, _ := m.(map[string]any)
			if msg["role"] == "tool" {
				content := fmt.Sprint(msg["content"])
				if strings.Contains(content, "loop guard") ||
					strings.Contains(content, "refused") {
					sawRefusal = true
				}
			}
		}

		if !sawRefusal {
			return sseToolCall("call-9", "echo", `{"input":"same"}`) + sseDone
		}

		return sseChunk("I stopped looping.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	tool := &fakeTool{name: "echo"}
	orch.Register(tool)

	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "loop forever"},
	}, func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	// MaxSameCall = 3: the tool executes at most 3 times no matter how
	// often the model asks.
	if tool.calls > 3 {
		t.Fatalf("tool executed %d times, want <= 3 (repeat bound)", tool.calls)
	}

	if len(result.LoopStats) != 1 ||
		result.LoopStats[0].Tool != "echo" ||
		result.LoopStats[0].Calls != tool.calls {
		t.Fatalf("loop stats = %+v, tool.calls = %d", result.LoopStats, tool.calls)
	}

	if !strings.Contains(result.Text, "stopped looping") {
		t.Fatalf("final text = %q", result.Text)
	}
}

func TestProjectCardInjectedBeforeRun(t *testing.T) {
	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		return sseChunk("working with the project facts.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	orch.SetProjectCard(func() string {
		return "## PROJECT INTELLIGENCE (measured facts)\n- Languages: Go\n- Test command (verified in Lab): go test ./... -count=1"
	})

	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "fix the test"},
	}, func(_ Activity) {})
	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if !strings.Contains(result.Text, "project facts") {
		t.Fatalf("final text = %q", result.Text)
	}
}

func TestProjectCardInjectionPosition(t *testing.T) {
	var gotMessages []llm.Message

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		if msgs, ok := body["messages"].([]any); ok && turn == 1 {
			for _, m := range msgs {
				msg, _ := m.(map[string]any)
				role, _ := msg["role"].(string)
				content, _ := msg["content"].(string)
				gotMessages = append(gotMessages, llm.Message{Role: role, Content: content})
			}
		}

		return sseChunk("ok") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	orch.SetProjectCard(func() string {
		return "## PROJECT INTELLIGENCE\n- Languages: Go"
	})

	// v1.2.5: the adaptive tier composes the project card for
	// coding-shaped work (STANDARD+); a trivial chat turn deliberately
	// skips it. This fixture exercises the card-carrying tier.
	if _, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "fix the bug in main.go"},
	}, func(_ Activity) {}); err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	// Find the card, assert it is a system message positioned before the
	// final user message (not after it, where the model would miss it).
	cardIdx := -1
	userIdx := -1

	for i, m := range gotMessages {
		if strings.Contains(m.Content, "PROJECT INTELLIGENCE") {
			cardIdx = i
		}

		if m.Role == "user" && m.Content == "do the thing" {
			userIdx = i
		}
	}

	if cardIdx < 0 {
		t.Fatalf("project card never reached the engine: %+v", gotMessages)
	}

	if userIdx >= 0 && cardIdx > userIdx {
		t.Fatalf("project card injected AFTER the user message (idx %d > %d)", cardIdx, userIdx)
	}
}
