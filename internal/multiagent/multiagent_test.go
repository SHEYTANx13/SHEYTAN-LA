package multiagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// engineScript describes what the fake engine answers per phase (phase
// detection by system-prompt keyword: PLANNER, CRITIC, SUMMARIZER, else
// executor). The critic's incoming user message is recorded for assertions.
type engineScript struct {
	plan      string
	critiques []string // consumed one per critic call
	summary   string
	executor  string

	mu           sync.Mutex
	criticInputs []string
}

func (s *engineScript) recordCriticInput(in string) {
	s.mu.Lock()
	s.criticInputs = append(s.criticInputs, in)
	s.mu.Unlock()
}

func (s *engineScript) criticInput(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if i >= len(s.criticInputs) {
		return ""
	}

	return s.criticInputs[i]
}

func newMultiAgentEngine(t *testing.T, s *engineScript) *httptest.Server {
	t.Helper()

	criticCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		sys := ""
		user := ""
		if msgs, ok := body["messages"].([]any); ok {
			if m, ok := msgs[0].(map[string]any); ok {
				sys, _ = m["content"].(string)
			}
			if len(msgs) > 1 {
				if m, ok := msgs[len(msgs)-1].(map[string]any); ok {
					user, _ = m["content"].(string)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")

		var out string

		switch {
		case strings.Contains(sys, "PLANNER"):
			out = s.plan
		case strings.Contains(sys, "CRITIC"):
			s.recordCriticInput(user)

			i := criticCalls
			criticCalls++

			if i < len(s.critiques) {
				out = s.critiques[i]
			} else {
				out = `{"satisfied": true, "issues": [], "next_step": ""}`
			}
		case strings.Contains(sys, "SUMMARIZER"):
			out = s.summary
		default:
			out = s.executor
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": out},
			}},
		})
	}))

	t.Cleanup(server.Close)

	return server
}

func newTestMultiAgent(t *testing.T, server *httptest.Server, maxIter int) *MultiAgent {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "remote"
	cfg.RemoteBaseURL = server.URL + "/v1"
	cfg.RemoteAPIKey = "test"
	cfg.RemoteModel = "fake"
	cfg.MaxIterations = 4

	client := llm.NewClient(config.NewSource(cfg))
	orch := agent.New(config.NewSource(cfg), client)

	return NewMultiAgent(client, orch, nil, func() string { return "fake" }, maxIter)
}

func TestCriticSeesObjectiveVerification(t *testing.T) {
	script := &engineScript{
		plan:      `{"summary":"one step","steps":[{"id":1,"goal":"do it","tool":"llm","args":{}}]}`,
		summary:   "summarized",
		executor:  "executor answer claiming success",
		critiques: []string{`{"satisfied": true, "issues": [], "next_step": ""}`},
	}

	server := newMultiAgentEngine(t, script)
	m := newTestMultiAgent(t, server, 2)

	if _, err := m.Run(context.Background(), "do something", func(_ agent.Activity) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	in := script.criticInput(0)
	if in == "" {
		t.Fatal("critic was never called")
	}

	// The critic must receive the measured verification verdict.
	if !strings.Contains(in, "Objective verification:") {
		t.Fatalf("critic input missing verification verdict:\n%s", in)
	}

	// The executor's unverified run reports not_verified.
	if !strings.Contains(in, "Not verified") {
		t.Fatalf("critic input missing honest not_verified verdict:\n%s", in)
	}

	// The result is an explicitly bounded excerpt.
	if !strings.Contains(in, "Executor's result (excerpt)") {
		t.Fatalf("critic input not marked as excerpt:\n%s", in)
	}
}

func TestBoundedExcerptClipsAndMarks(t *testing.T) {
	long := strings.Repeat("x", 10_000)

	got := boundedExcerpt(long, 100)

	if len(got) > 100+80 { // clip + marker allowance
		t.Fatalf("excerpt too long: %d", len(got))
	}

	if !strings.Contains(got, "[excerpt") {
		t.Fatalf("excerpt must be marked: %q", got[len(got)-60:])
	}

	// Short strings pass through unchanged.
	if got := boundedExcerpt("short", 100); got != "short" {
		t.Fatalf("short string altered: %q", got)
	}

	// The clip must not split a rune.
	multibyte := strings.Repeat("ü", 500)
	got = boundedExcerpt(multibyte, 21)

	if !utf8.ValidString(got) {
		t.Fatalf("excerpt split a rune: %q", got)
	}
}

func TestMaxIterZeroStillRunsCritic(t *testing.T) {
	// maxIter < 1 normalizes to 3 — the critic loop must still execute
	// (regression: the loop previously read the raw field and never ran).
	script := &engineScript{
		plan:      `{"summary":"s","steps":[]}`,
		summary:   "final summary",
		executor:  "did the thing",
		critiques: []string{`{"satisfied": true, "issues": [], "next_step": ""}`},
	}

	server := newMultiAgentEngine(t, script)
	m := newTestMultiAgent(t, server, 0)

	out, err := m.Run(context.Background(), "task", func(_ agent.Activity) {})
	if err != nil {
		t.Fatalf("Run with maxIter=0: %v", err)
	}

	if out != "final summary" {
		t.Fatalf("output = %q", out)
	}

	if script.criticInput(0) == "" {
		t.Fatal("critic loop did not run with maxIter=0")
	}
}

func TestCriticRevisionLoopReExecutes(t *testing.T) {
	script := &engineScript{
		plan:     `{"summary":"s","steps":[{"id":1,"goal":"g","tool":"llm","args":{}}]}`,
		summary:  "summary",
		executor: "attempt",
		critiques: []string{
			`{"satisfied": false, "issues": ["incomplete"], "next_step": "finish it"}`,
			`{"satisfied": true, "issues": [], "next_step": ""}`,
		},
	}

	server := newMultiAgentEngine(t, script)
	m := newTestMultiAgent(t, server, 3)

	if _, err := m.Run(context.Background(), "task", func(_ agent.Activity) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Two critic calls: first unsatisfied, second satisfied.
	if got := script.criticInput(1); got == "" {
		t.Fatal("revision loop did not invoke the critic a second time")
	}
}
