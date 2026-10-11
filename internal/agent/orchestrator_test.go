package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// fakeTool is a minimal real tool for the tool-loop test.
type fakeTool struct {
	name    string
	calls   int
	mu      sync.Mutex
	lastRun string
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "test tool: doubles the input" }
func (f *fakeTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string"},
		},
		"required": []string{"input"},
	}
}

func (f *fakeTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var parsed struct {
		Input string `json:"input"`
	}

	_ = json.Unmarshal(args, &parsed)

	f.mu.Lock()
	f.calls++
	f.lastRun = parsed.Input
	f.mu.Unlock()

	return "result-of(" + parsed.Input + ")", nil
}

// sseChunk builds one OpenAI-compatible SSE data frame.
func sseChunk(content string) string {
	delta := map[string]any{"role": "assistant"}

	if content != "" {
		delta["content"] = content
	}

	body, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl-test",
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}},
	})

	return "data: " + string(body) + "\n\n"
}

func sseToolCall(id, name, args string) string {
	body, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-test",
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index": 0,
					"id":    id,
					"type":  "function",
					"function": map[string]any{
						"name":      name,
						"arguments": args,
					},
				}},
			},
		}},
	})

	return "data: " + string(body) + "\n\n"
}

const sseDone = "data: [DONE]\n\n"

// newFakeEngine spins an OpenAI-compatible SSE server. The handler decides
// per-request how many turns the fake model takes.
func newFakeEngine(t *testing.T, respond func(turn int, body map[string]any) string) (*httptest.Server, *int) {
	t.Helper()

	turns := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)

			return
		}

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "text/event-stream")

		turns++

		_, _ = w.Write([]byte(respond(turns, body)))
	}))

	t.Cleanup(server.Close)

	return server, &turns
}

func remoteConfig(t *testing.T, baseURL string) *config.Config {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "remote"
	cfg.RemoteBaseURL = baseURL + "/v1"
	cfg.RemoteAPIKey = "test-key"
	cfg.RemoteModel = "fake-remote-model"
	cfg.MaxIterations = 8

	return cfg
}

func TestFirstInferenceStreamsAndCompletes(t *testing.T) {
	server, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
		var b strings.Builder

		b.WriteString(sseChunk("Hello"))
		b.WriteString(sseChunk(" from"))
		b.WriteString(sseChunk(" the fake model."))
		b.WriteString(sseDone)

		return b.String()
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	var events []Activity

	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "say hi"},
	}, func(a Activity) {
		events = append(events, a)
	})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if result.Text != "Hello from the fake model." {
		t.Fatalf("final text mismatch: %q", result.Text)
	}

	sawResponse := false
	sawDone := false

	for _, ev := range events {
		if ev.Type == "response" {
			sawResponse = true
		}

		if ev.Type == "done" {
			sawDone = true
		}
	}

	if !sawResponse || !sawDone {
		t.Fatalf("missing streaming/done events: %+v", events)
	}
}

func TestToolLoopExecutesToolAndFollowsUp(t *testing.T) {
	var gotToolResultInHistory bool

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		if turn == 1 {
			return sseToolCall("call-1", "echo", `{"input":"ping"}`) + sseDone
		}

		// Second turn: the request must carry the tool result.
		msgs, _ := body["messages"].([]any)

		for _, m := range msgs {
			msg, _ := m.(map[string]any)

			if msg["role"] == "tool" {
				if strings.Contains(fmt.Sprint(msg["content"]), "result-of(ping)") {
					gotToolResultInHistory = true
				}
			}
		}

		return sseChunk("The tool said: result-of(ping)") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	tool := &fakeTool{name: "echo"}
	orch.Register(tool)

	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "use the echo tool"},
	}, func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if tool.calls != 1 {
		t.Fatalf("tool executed %d times, want 1", tool.calls)
	}

	if !gotToolResultInHistory {
		t.Fatal("follow-up turn did not carry the tool result")
	}

	if len(result.ToolsUsed) != 1 || result.ToolsUsed[0] != "echo" {
		t.Fatalf("tools used mismatch: %v", result.ToolsUsed)
	}

	if !strings.Contains(result.Text, "result-of(ping)") {
		t.Fatalf("final answer should reference the tool result: %q", result.Text)
	}
}

func TestUnknownToolSurfacesErrorNotProse(t *testing.T) {
	server, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
		if turn == 1 {
			return sseToolCall("call-1", "nonexistent_tool", `{}`) + sseDone
		}

		return sseChunk("understood") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	_, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "call the missing tool"},
	}, func(_ Activity) {})

	if err != nil {
		t.Fatalf("unknown tool must not fail the run: %v", err)
	}
}

func TestAbortCancelsRun(t *testing.T) {
	release := make(chan struct{})

	server, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
		<-release // hold the stream open until the test aborts

		return sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})

	var runErr error

	go func() {
		defer close(done)

		_, runErr = orch.RunDetailed(ctx, []llm.Message{
			{Role: "user", Content: "hang forever"},
		}, func(_ Activity) {})
	}()

	time.Sleep(200 * time.Millisecond)

	cancel()
	close(release)

	select {
	case <-done:
		if runErr != nil {
			t.Fatalf("aborted run must not error: %v", runErr)
		}

	case <-time.After(5 * time.Second):
		t.Fatal("abort did not cancel the run in time")
	}
}

func TestLLMErrorPropagatesToCaller(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"model exploded"}`, http.StatusInternalServerError)
	}))

	defer server.Close()

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	_, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "trigger the failure"},
	}, func(_ Activity) {})

	if err == nil {
		t.Fatal("engine failure must surface as an error")
	}
}

func TestSplitThinkHandlesMultipleBlocks(t *testing.T) {
	raw := "<think>first</think>visible A<think>second</think>visible B"

	reasoning, content := SplitThink(raw)

	if !strings.Contains(reasoning, "first") || !strings.Contains(reasoning, "second") {
		t.Fatalf("reasoning incomplete: %q", reasoning)
	}

	if !strings.Contains(content, "visible A") || !strings.Contains(content, "visible B") {
		t.Fatalf("content incomplete: %q", content)
	}
}

func TestExtractImageMarkers(t *testing.T) {
	clean, images := ExtractImageMarkers("before [[IMG:/tmp/shot.png]] after")

	if len(images) != 1 || images[0] != "/tmp/shot.png" {
		t.Fatalf("images = %v", images)
	}

	if !strings.Contains(clean, "before") || !strings.Contains(clean, "after") {
		t.Fatalf("clean text = %q", clean)
	}
}

// TestGenerationRouterSeamDrivesTheLoop (Phase 5): with a router
// installed, the agent loop's generation flows THROUGH it (the runtime's
// backend-aware selection), while the loop mechanics (streaming events,
// activity, completion) stay identical. With no router, the direct
// client path serves (pinned by every other test in this file).
func TestGenerationRouterSeamDrivesTheLoop(t *testing.T) {
	server, turns := newFakeEngine(t, func(turn int, _ map[string]any) string {
		var b strings.Builder
		b.WriteString(sseChunk("via "))
		b.WriteString(sseChunk("client."))
		b.WriteString(sseDone)
		return b.String()
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	routerCalls := 0
	orch.SetGenerationStream(func(ctx context.Context, req *llm.ChatRequest,
		onEvent func(llm.StreamEvent) error) (llm.PerfStats, error) {

		routerCalls++

		// The router "selects" a backend — this stub emulates the native
		// one: streams two chunks then the terminal event, exactly like
		// the native backend adapter does.
		if err := onEvent(llm.StreamEvent{Content: "via "}); err != nil {
			return llm.PerfStats{}, err
		}
		if err := onEvent(llm.StreamEvent{Content: "router."}); err != nil {
			return llm.PerfStats{}, err
		}
		_ = onEvent(llm.StreamEvent{
			FinishReason: "stop",
			Usage: &struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
		})

		return llm.PerfStats{Tokens: 2, TokensPerSec: 10, TTFTMs: 5, WallMs: 200}, nil
	})

	var events []Activity
	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "say hi"},
	}, func(a Activity) {
		events = append(events, a)
	})
	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if routerCalls == 0 {
		t.Fatal("the generation router was never invoked — the loop bypassed the seam")
	}

	if result.Text != "via router." {
		t.Fatalf("final text = %q, want %q (router-served)", result.Text, "via router.")
	}

	sawResponse := false
	sawDone := false
	for _, ev := range events {
		if ev.Type == "response" {
			sawResponse = true
		}
		if ev.Type == "done" {
			sawDone = true
		}
	}
	if !sawResponse || !sawDone {
		t.Fatalf("streaming activity missing: response=%v done=%v", sawResponse, sawDone)
	}

	// The fake client engine never served a request in this run.
	if *turns != 0 {
		t.Fatalf("client served %d requests, want 0 (router owns generation)", *turns)
	}

	// Removing the router restores the direct client path.
	orch.SetGenerationStream(nil)

	var result2 RunResult
	result2, err = orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "say hi again"},
	}, func(a Activity) {})
	if err != nil {
		t.Fatalf("second RunDetailed: %v", err)
	}
	if result2.Text != "via client." {
		t.Fatalf("second text = %q, want the client-served output", result2.Text)
	}
}
